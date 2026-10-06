// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// free_ip_runner_forwarded_identity_s1a7_integration_test.go — полоса RED S1-A7
// (issue-2918, NTF-3, Н3-Ф2): задание освобождения адресов nlb пересылает
// владельцу адреса (vpc `ReleaseOwnedAddress`) ТУ ЖЕ личность компонента, под
// которой открыта его транзакция журнала, — `system:nlb-free-ip-runner`, а не
// безымянную системную `{system, bootstrap}`.
//
// Основание — замысел issue-2918: З6 и З13 («отправитель ставит
// `auth.SystemPrincipalFor("nlb", "free-ip-runner")` через `journaltx.AsComponent`
// вместо `operations.SystemPrincipal()`, который сегодня стоит в
// `free_ip_runner.go`»; строка таблицы фоновых путей «vpc `ReleaseOwnedAddress`
// из `releaseFamily` задания nlb — пересланный принципал отправителя (тот же
// принципал прохода)»), CX3D-01 (б) («системная личность пересылается с именем
// компонента у nlb; установка первым оператором `reconcileOne`, установка в
// `releaseFamily` снимается»). Гейт того же предмета по дереву — УК3-31
// (`internal/repohygiene/backgroundentryidentity*.go`); здесь — поведение.
//
// # Что наблюдается
//
// Дублёр владельца адреса снимает с контекста вызова ровно то, что настоящий
// клиент кладёт на провод: `auth.PropagateOutgoing(ctx)` — пару метаданных
// «тип, идентификатор» принципала. Сравнение идёт с той же функцией,
// применённой к контексту с принципалом компонента, поэтому форма провода не
// выписывается литералом.
//
// Порядок: сперва фикстура (вызов владельца состоялся ровно один раз и с тем
// адресом; транзакция прохода записала `DELETED` с инициатором компонента — это
// положительный близнец «та же личность, другое место наблюдения»), потом
// предмет.

package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/corelib/auth"
	"github.com/PRO-Robotech/corelib/operations"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
)

// wirePrincipal — то, что клиент владельца кладёт на провод.
type wirePrincipal struct{ Type, ID string }

func wirePrincipalOf(ctx context.Context) (wirePrincipal, bool) {
	md, ok := metadata.FromOutgoingContext(auth.PropagateOutgoing(ctx))
	if !ok {
		return wirePrincipal{}, false
	}
	ts, ids := md.Get(auth.MDKeyPrincipalType), md.Get(auth.MDKeyPrincipalID)
	if len(ts) == 0 || len(ids) == 0 {
		return wirePrincipal{}, false
	}
	return wirePrincipal{Type: ts[0], ID: ids[0]}, true
}

// wireRecordingReleaser — дублёр владельца с семантикой `fakeReleaser`,
// дополнительно снимающий личность с провода каждого `ReleaseLease`.
type wireRecordingReleaser struct {
	*fakeReleaser
	mu   sync.Mutex
	seen []wirePrincipal
	none int
}

func (w *wireRecordingReleaser) ReleaseLease(
	ctx context.Context, req vpcclient.ReleaseLeaseRequest,
) (vpcclient.LeaseOutcome, error) {
	p, ok := wirePrincipalOf(ctx)
	w.mu.Lock()
	if ok {
		w.seen = append(w.seen, p)
	} else {
		w.none++
	}
	w.mu.Unlock()
	return w.fakeReleaser.ReleaseLease(ctx, req)
}

// TestFreeIP_S1A7_ReleaseForwardsTheComponentIdentity — З13, CX3D-01 (б): вызов
// `ReleaseLease` из `releaseFamily` несёт на проводе принципал компонента
// `(nlb, free-ip-runner)` — тот же, что инициатор строки журнала прохода.
func TestFreeIP_S1A7_ReleaseForwardsTheComponentIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	pool, lbID := freeIPStand(t)
	rec := &wireRecordingReleaser{fakeReleaser: &fakeReleaser{}}
	r := newFreeIPRunner(t, pool, rec, time.Minute)

	_, err := r.reconcileOnce(context.Background())
	require.NoError(t, err, "ФИКСТУРА: проход задания освобождения адресов отвергнут")

	// --- ФИКСТУРА: вызов владельца состоялся, транзакция под компонентом ------
	require.Equal(t, []string{"adr0000000INITIATOR1"}, rec.clears(),
		"ФИКСТУРА: вызов владельца адреса не состоялся либо не с тем адресом")
	require.Equal(t, []string{"system:nlb-free-ip-runner"}, freeIPJournalInitiators(t, pool, lbID),
		"ФИКСТУРА (близнец): DELETED прохода не несёт инициатора компонента — транзакция не под личностью компонента")

	want, ok := wirePrincipalOf(operations.WithPrincipal(context.Background(),
		auth.SystemPrincipalFor("nlb", "free-ip-runner")))
	require.True(t, ok, "ФИКСТУРА: принципал компонента не ложится на провод")

	// --- ПРЕДМЕТ ----------------------------------------------------------
	rec.mu.Lock()
	seen, none := append([]wirePrincipal(nil), rec.seen...), rec.none
	rec.mu.Unlock()
	require.Zero(t, none, "S1-A7 (CX3D-01 (б)): вызов владельца ушёл без принципала на проводе")
	require.Equal(t, []wirePrincipal{want}, seen,
		"S1-A7 (З13, CX3D-01 (б)): releaseFamily пересылает владельцу адреса не личность компонента прохода")
}
