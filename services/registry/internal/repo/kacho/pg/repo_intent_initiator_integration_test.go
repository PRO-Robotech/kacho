// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// repo_intent_initiator_integration_test.go — решения Д115 и Д116 для
// намерения репозитория (`emitRepoIntent`): транзакцию открывает помощник
// записи журнала, и её инициатор — принципал контекста.
//
//   - Д115: запись по push несёт инициатора ПРОВЕРЕННОГО принципала; контекст
//     без принципала — отказ записи до базы, никакой строки очереди и признака,
//     и никакой подстановки «system»;
//   - Д116: подметальщик осиротевших объектов — фоновый путь; его снятие несёт
//     инициатора компонента `(registry, orphan-sweep)`.
//
// Наблюдаемое: инициатор транзакции, в которой легла строка очереди. Строка
// `registry_outbox` колонки инициатора не несёт (это не журнал подписки), поэтому
// проба ставит СВОЙ триггер на очередь, читающий настройку транзакции
// `kacho_journal.initiator` в момент вставки, — тот же источник, из которого
// умолчание колонки журнала берёт значение. Триггер живёт в собственной базе
// пробы (`pgtest.NewDB`), продукт он не трогает.

package pg_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// installInitiatorProbe ставит на очередь намерений триггер, записывающий
// инициатора транзакции каждой вставки.
func installInitiatorProbe(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`CREATE TABLE kacho_registry.d115_initiator_probe (resource_id text NOT NULL, event_type text NOT NULL, initiator text)`,
		`CREATE FUNCTION kacho_registry.d115_initiator_probe_fn() RETURNS trigger LANGUAGE plpgsql AS $$
		 BEGIN
		     INSERT INTO kacho_registry.d115_initiator_probe (resource_id, event_type, initiator)
		     VALUES (NEW.resource_id, NEW.event_type, NULLIF(current_setting('kacho_journal.initiator', true), ''));
		     RETURN NEW;
		 END $$`,
		`CREATE TRIGGER d115_initiator_probe_trg AFTER INSERT ON kacho_registry.registry_outbox
		     FOR EACH ROW EXECUTE FUNCTION kacho_registry.d115_initiator_probe_fn()`,
	} {
		_, err := pool.Exec(ctx, q)
		require.NoError(t, err, "ФИКСТУРА: триггер пробы инициатора не поставлен")
	}
}

func probedInitiators(t *testing.T, pool *pgxpool.Pool, resourceID, eventType string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT coalesce(initiator, '<NULL>') FROM kacho_registry.d115_initiator_probe
		  WHERE resource_id = $1 AND event_type = $2`, resourceID, eventType)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestRepoIntent_D115_PushTransactionCarriesThePrincipalInitiator — законный
// близнец: запись под принципалом сервисного аккаунта несёт его инициатора.
func TestRepoIntent_D115_PushTransactionCarriesThePrincipalInitiator(t *testing.T) {
	pool := setupTestDB(t)
	installInitiatorProbe(t, pool)
	repo := kachopg.NewRegistryRepo(pool)
	regID := seedRegistry(t, pool, "prj-P", "reg-d115")
	sub := ids.NewHyphenID(ids.PrefixServiceAccount)
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{Type: "service_account", ID: sub})

	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, "team/app", "prj-P", "service_account:"+sub)))
	require.Equal(t, []string{"service_account:" + sub},
		probedInitiators(t, pool, regID+"/team/app", domain.FGAEventRegister),
		"Д115: транзакция намерения push несёт инициатора проверенного принципала")
}

// TestRepoIntent_D115_NoPrincipalIsRefusedAndWritesNothing — тот же вызов без
// принципала: отказ, ни строки очереди, ни признака существования.
func TestRepoIntent_D115_NoPrincipalIsRefusedAndWritesNothing(t *testing.T) {
	pool := setupTestDB(t)
	installInitiatorProbe(t, pool)
	repo := kachopg.NewRegistryRepo(pool)
	regID := seedRegistry(t, pool, "prj-P", "reg-d115-none")

	err := repo.RegisterRepository(context.Background(),
		domain.RegisterIntentForRepoPush(regID, "team/app", "prj-P", "service_account:sva-anyone"))
	require.Error(t, err, "Д115: запись без проверенного принципала обязана отказать")
	require.True(t, errors.Is(err, regerrors.ErrInternal), "Д115: отказ классом, без текста базы: %v", err)
	require.Equal(t, 0, countOutbox(t, pool, regID+"/team/app", domain.FGAEventRegister),
		"Д115: строки очереди нет")
	require.Equal(t, 0, countRegistration(t, pool, regID, "team/app"), "Д115: признака существования нет")
	require.Empty(t, probedInitiators(t, pool, regID+"/team/app", domain.FGAEventRegister))
}

// TestRepoIntent_D116_OrphanSweepCarriesTheComponentInitiator — фоновый путь:
// проход подметальщика без принципала в контексте (так его зовёт процесс)
// снимает объект с инициатором компонента.
func TestRepoIntent_D116_OrphanSweepCarriesTheComponentInitiator(t *testing.T) {
	pool := setupTestDB(t)
	installInitiatorProbe(t, pool)
	repo := kachopg.NewRegistryRepo(pool)
	regID := seedRegistry(t, pool, "prj-P", "reg-d116")
	require.NoError(t, repo.RegisterRepository(journalPrincipalCtx(context.Background()),
		domain.RegisterIntentForRepoPush(regID, "team/app", "prj-P", "service_account:sva-ci")))
	_, err := pool.Exec(context.Background(),
		`DELETE FROM kacho_registry.registry_repository_registration WHERE registry_id = $1 AND repo = $2`,
		regID, "team/app")
	require.NoError(t, err)

	named, err := repo.SweepOrphanedRepositories(context.Background(), 0)
	require.NoError(t, err, "Д116: проход подметальщика отвергнут")
	require.Equal(t, []string{regID + "/team/app"}, named)
	require.Equal(t, []string{"system:registry-orphan-sweep"},
		probedInitiators(t, pool, regID+"/team/app", domain.FGAEventUnregister),
		"Д116: снятие фонового пути несёт инициатора компонента")
}
