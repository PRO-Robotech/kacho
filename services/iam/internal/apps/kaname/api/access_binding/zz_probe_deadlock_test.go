// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

package access_binding_test

// ВРЕМЕННЫЙ ЗОНД — не для посадки.

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	accessbindingapp "github.com/PRO-Robotech/kaname/internal/apps/kaname/api/access_binding"
	"github.com/PRO-Robotech/kaname/internal/apps/kaname/api/access_binding/reconcile"
	"github.com/PRO-Robotech/kaname/internal/domain"
	kanamepg "github.com/PRO-Robotech/kaname/internal/repo/kaname/pg"
	"github.com/PRO-Robotech/kaname/internal/testsupport/catalogfixture"
)

func seedRulesRoleVerbs(t *testing.T, ctx context.Context, pool *pgxpoolT, acc domain.AccountID, name string) domain.RoleID {
	t.Helper()
	rid := domain.RoleID(ids.NewID(domain.PrefixRole))
	_, err := pool.Exec(ctx, `
		INSERT INTO kaname.roles (id, account_id, name, description, permissions, rules)
		VALUES ($1, $2, $3, $4, '[]'::jsonb,
		        '[{"module":"vpc","resources":["network"],"verbs":["get","list","update"]}]'::jsonb)`,
		string(rid), string(acc), name, "rules role "+name)
	require.NoError(t, err)
	return rid
}

// Гипотеза: ФАН-АУТ по объекту держит строки прямого факта и хочет advisory
// последней привязки; УДАЛЕНИЕ держит advisory той же привязки и хочет те же
// строки прямого факта. Порядок обратный ⇒ взаимная блокировка.
func TestZZProbe_FanoutRacesDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool := poolFromDSN(t, dsn)
	repo := kanamepg.New(pool, nil)
	opsRepo := operations.NewRepo(pool, "kaname")

	adapter := kanamepg.NewReconcileAdapter(pool, catalogfixture.Source())
	rec := reconcile.New(adapter, nil, catalogfixture.Source())

	owner := mustSeedUser(t, ctx, pool, "zzf")
	acc := seedAccountByOwner(t, ctx, pool, "acc-zzf", owner)
	prj := seedProjectInAccount(t, ctx, pool, acc, "prj-zzf")
	member := mustSeedUser(t, ctx, pool, "zzfm")
	role := seedRulesRoleVerbs(t, ctx, pool, acc, "zzf_vpc")

	const obj = "net-zzf-0001"
	_, err := pool.Exec(ctx, `
		INSERT INTO kaname.resource_mirror
		  (object_type, object_id, parent_project_id, parent_account_id, labels, source_version, updated_at)
		VALUES ('vpc.network',$1,$2,$3,'{}'::jsonb,$4, now())`,
		obj, string(prj), string(acc), time.Now())
	require.NoError(t, err)

	mk := func() domain.AccessBindingID {
		id := domain.AccessBindingID(ids.NewID(domain.PrefixAccessBinding))
		w, err := repo.Writer(ctx)
		require.NoError(t, err)
		_, err = w.AccessBindingsW().Insert(ctx, domain.AccessBinding{
			ID: id, SubjectType: domain.SubjectTypeUser, SubjectID: domain.SubjectID(member),
			RoleID: role, ResourceType: "account", ResourceID: string(acc),
		})
		require.NoError(t, err)
		require.NoError(t, w.Commit(ctx))
		return id
	}

	const iters = 40
	var mu sync.Mutex
	var aborted []string
	var other []string

	for it := 0; it < iters; it++ {
		// K свежих привязок + одна МАТЕРИАЛИЗОВАННАЯ с НАИБОЛЬШИМ id.
		const k = 25
		var all []domain.AccessBindingID
		for i := 0; i < k; i++ {
			all = append(all, mk())
		}
		// цель удаления — с наибольшим id, чтобы фан-аут дошёл до неё ПОСЛЕДНЕЙ
		var victim domain.AccessBindingID
		for {
			victim = mk()
			all = append(all, victim)
			sorted := append([]domain.AccessBindingID(nil), all...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
			if sorted[len(sorted)-1] == victim {
				break
			}
			// не максимальная — снимаем и пробуем снова
			_, _ = pool.Exec(ctx, `DELETE FROM kaname.access_bindings WHERE id=$1`, string(victim))
			all = all[:len(all)-1]
		}
		// материализуем ТОЛЬКО жертву — у остальных ведомость пуста,
		// поэтому вычитание не защитит её набор снятия
		require.NoError(t, rec.ReconcileBindingForward(ctx, victim))

		uc := accessbindingapp.NewDeleteAccessBindingUseCase(repo, opsRepo).
			WithRelationStore(denyingRelations{}, nil)

		delay := time.Duration(it%20) * time.Millisecond
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := rec.ReconcileObject(ctx, "vpc.network", obj); err != nil {
				mu.Lock()
				var pgErr *pgconn.PgError
				if asPg(err, &pgErr) {
					if pgErr.Code == "40P01" {
						aborted = append(aborted, fmt.Sprintf("FANOUT 40P01 (delay=%v): %s || DETAIL: %s", delay, pgErr.Message, pgErr.Detail))
					} else {
						other = append(other, "fanout "+pgErr.Code)
					}
				} else {
					other = append(other, "fanout: "+err.Error())
				}
				mu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			time.Sleep(delay)
			op, err := uc.Execute(asUser(ctx, owner), victim)
			if err != nil {
				mu.Lock()
				other = append(other, "delete-sync: "+err.Error())
				mu.Unlock()
				return
			}
			done := awaitOp(t, ctx, opsRepo, op.ID)
			if done.Error != nil {
				mu.Lock()
				aborted = append(aborted, fmt.Sprintf("DELETE op error (delay=%v): code=%d msg=%q", delay, done.Error.Code, done.Error.Message))
				mu.Unlock()
			}
		}()
		wg.Wait()
		_, _ = pool.Exec(ctx, `DELETE FROM kaname.access_bindings WHERE resource_id=$1`, string(acc))
	}

	t.Logf("перепись: итераций %d; ABORTED/40P01 %d, прочих %d", iters, len(aborted), len(other))
	for i, d := range aborted {
		if i < 5 {
			t.Logf("ОТКАЗ: %s", d)
		}
	}
	counts := map[string]int{}
	for _, e := range other {
		counts[e]++
	}
	for kk, v := range counts {
		t.Logf("прочее ×%d: %s", v, kk)
	}
	require.Empty(t, aborted, "ни одна сторона не должна получить взаимную блокировку")
}
