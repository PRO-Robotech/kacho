// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// retired_role_grants_nothing_integration_test.go — ВЫДАЧА НА СНЯТУЮ РОЛЬ
// ДОСТУПА НЕ ДАЁТ, и это утверждается ВЕРДИКТОМ, а не механизмом.
//
// Приёмка `services/iam/docs/engineering/acceptance/role-withdrawal-has-a-producer.md`
// (APPROVED круга 4); сценарий IAM-RW-1-10. Задача продукта #1913.
//
// # Почему отдельная проба, если проекции и так снимаются
//
// Соседние пробы утверждают МЕХАНИЗМ: строк проекции не осталось, ключ держит
// порядок, реконсайлер роль пропускает. Ни одна из них не спрашивает то, ради
// чего всё это сделано, — «даёт ли выдача доступ». Утверждение о механизме
// переживает свой предмет: цепь вердикта может начать читать другую таблицу, и
// все три пробы останутся зелёными.
//
// Здесь спрашивается ИСХОД, и спрашивается он у того же кода, что отвечает
// арендатору.
//
// # Положительный контроль обязателен и стоит ПЕРВЫМ
//
// «Отказано» после снятия ничего не утверждает, если до снятия было то же
// самое: субъект без прав отвечает отказом всегда. Поэтому сперва — «разрешено»,
// и только потом снятие.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg/relverdict"
)

// TestIAMRW110AGrantOnARetiredRoleGrantsNothing — IAM-RW-1-10.
func TestIAMRW110AGrantOnARetiredRoleGrantsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires Docker)")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	defer pool.Close()

	fx := setupGamma(t, ctx, pool, "rwvrd")
	rec, _ := newReconciler(pool)

	rule := forwardAnchorRule() // compute.instance {get, update}
	roleID := seedRulesRole(t, ctx, pool, fx.repo, fx.prj, "rwvrdrole", domain.Rules{rule})
	bid := insertThinBinding(t, ctx, fx.repo, fx.member, roleID, fx.prj)

	seedMirrorRow(t, ctx, pool, "compute.instance", "iVrd", string(fx.prj), string(fx.accID),
		nil, time.Now())
	require.NoError(t, rec.ReconcileBindingForward(ctx, bid))

	// ── ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: до снятия доступ ЕСТЬ ────────────────────────
	before, _, err := askUserVerdict(t, ctx, pool, string(fx.member),
		"compute_instance", "iVrd", "v_update")
	require.NoError(t, err, "вердикт не вычислен — исходов у него ТРИ, и «не вычислено» "+
		"не есть «нет прав»")
	require.Equal(t, relverdict.Allow, before,
		"до снятия доступа не было: отрицание ниже зеленело бы на субъекте без прав")

	// ── СНЯТИЕ роли: проекции прочь, потом пометка ───────────────────────────
	for _, table := range []string{"role_verb", "role_rule_ref", "role_rule_selectors",
		"access_binding_target_members"} {
		_, derr := pool.Exec(ctx,
			`DELETE FROM kacho_iam.`+table+` WHERE role_id = $1`, string(roleID))
		require.NoErrorf(t, derr, "снятие проекции %s", table)
	}
	_, err = pool.Exec(ctx, `
		UPDATE kacho_iam.roles
		   SET live = false, retired_at = now(), retired_reason = 'проба', retired_by = 'проба'
		 WHERE id = $1`, string(roleID))
	require.NoError(t, err, "пометка роли снятой")

	// ── НЕСУЩЕЕ: тот же вопрос того же субъекта — ОТКАЗАНО ───────────────────
	after, _, err := askUserVerdict(t, ctx, pool, string(fx.member),
		"compute_instance", "iVrd", "v_update")
	require.NoError(t, err, "вердикт не вычислен после снятия: «не вычислено» не есть отказ")
	assert.Equal(t, relverdict.Deny, after,
		"выдача на СНЯТУЮ роль по-прежнему даёт доступ: пометка права не отбирает, "+
			"его отбирает снятие проекций — и если вердикт всё ещё «разрешено», значит "+
			"он читает не то, что снимает отзыв")

	// ВЫДАЧА при этом ЦЕЛА — на этом стоит обратимость (§2.4).
	var bindings int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.access_bindings WHERE id = $1`, string(bid)).
		Scan(&bindings))
	assert.Equal(t, 1, bindings,
		"выдача снесена вместе с ролью: оживление стало бы бессмысленным — кому роль "+
			"была выдана, не знал бы никто")
	t.Logf("перепись: вердикт до снятия %v, после %v; строк выдачи %d", before, after, bindings)
}

// askUserVerdict — вердикт о ЧЕЛОВЕКЕ. Сосед `askVerdict` спрашивает о служебной
// учётке и приставку субъекта вшивает; здесь субъект другой, и подставить его в
// чужую функцию значило бы задать вопрос не о том.
//
// Транзакция ЧИТАЮЩАЯ: вердикт ничего не пишет, и открывать писательскую значило
// бы дать пробе право, которого у пути чтения нет.
func askUserVerdict(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	userID, modelType, objectID, relation string,
) (relverdict.Verdict, relverdict.Grounds, error) {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	return relverdict.Ask(ctx, tx, relverdict.Query{
		Subject: "user:" + userID, ObjectType: modelType,
		ObjectID: objectID, Relation: relation,
	})
}
