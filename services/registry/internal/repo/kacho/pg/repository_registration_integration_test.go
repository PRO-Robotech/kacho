// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// repository_registration_integration_test.go — durable-признак существования
// репозитория (registry_repository_registration, миграция 0014) против реального
// Postgres 16 (testcontainers).
//
// Предмет: признак и намерение пишутся ОДНОЙ транзакцией и потому не могут разъехаться.
// Это не «удобно», а несущее свойство: пока признак жил бы отдельно, оставались бы два
// наблюдаемых состояния — «намерение уехало, а ресурса нет» (следующая запись снова идёт
// полосой создания и выписывает владельца заново) и «ресурс есть, а прав ему никто не
// выдаст» (намерение потеряно, переигрывать нечего).
package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// countRegistration — есть ли durable-признак существования репозитория.
func countRegistration(t *testing.T, pool *pgxpool.Pool, registryID, repo string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM kacho_registry.registry_repository_registration
		 WHERE registry_id = $1 AND repo = $2`, registryID, repo).Scan(&n))
	return n
}

// TestRepoRegistration_EmitAndDropAreAtomicWithIntent — register пишет признак И строку
// очереди; unregister снимает признак И пишет строку очереди. Оба — одной транзакцией.
func TestRepoRegistration_EmitAndDropAreAtomicWithIntent(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-registration")

	declared, err := repo.RepositoryDeclared(ctx, regID, "team/app")
	require.NoError(t, err)
	require.False(t, declared, "до регистрации репозиторий как ресурс не существует")

	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, "team/app", "prj-P", "service_account:sva-ci")))
	require.Equal(t, 1, countRegistration(t, pool, regID, "team/app"), "признак записан")
	require.Equal(t, 1, countOutbox(t, pool, regID+"/team/app", domain.FGAEventRegister),
		"намерение записано ТОЙ ЖЕ транзакцией")

	declared, err = repo.RepositoryDeclared(ctx, regID, "team/app")
	require.NoError(t, err)
	require.True(t, declared, "после регистрации репозиторий существует как ресурс")

	// Повторная регистрация (перезапись/принятие поверх проекции) идемпотентна.
	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, "team/app", "prj-P", "service_account:sva-ci")))
	require.Equal(t, 1, countRegistration(t, pool, regID, "team/app"), "признак не дублируется")

	require.NoError(t, repo.UnregisterRepository(ctx, domain.UnregisterIntentForRepo(regID, "team/app")))
	require.Equal(t, 0, countRegistration(t, pool, regID, "team/app"), "признак снят")
	require.Equal(t, 1, countOutbox(t, pool, regID+"/team/app", domain.FGAEventUnregister),
		"намерение снятия записано той же транзакцией")

	declared, err = repo.RepositoryDeclared(ctx, regID, "team/app")
	require.NoError(t, err)
	require.False(t, declared, "после снятия ресурса снова нет — следующая запись идёт полосой создания")
}

// TestRepoRegistration_FailureInsideTx_LeavesNeitherRowNorIntent — сбой ВНУТРИ
// транзакции: реестра, под которым регистрируют репозиторий, не существует, поэтому
// вставка признака нарушает внешний ключ. Ожидание: не появляется НИ признака, НИ строки
// очереди — транзакция откатывается целиком.
//
// Это и есть доказательство атомарности на наблюдаемом уровне: до объединения записей
// строка очереди уехала бы, а признака бы не было.
func TestRepoRegistration_FailureInsideTx_LeavesNeitherRowNorIntent(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	const ghostReg = "regGHOST000000000000" // такого реестра нет
	err := repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(ghostReg, "app", "prj-P", "service_account:sva-ci"))
	require.Error(t, err, "регистрация под несуществующим реестром отвергается внешним ключом")

	require.Equal(t, 0, countRegistration(t, pool, ghostReg, "app"), "признака нет")
	require.Equal(t, 0, countOutbox(t, pool, ghostReg+"/app", domain.FGAEventRegister),
		"строки очереди тоже нет — откат забрал обе записи")
}

// TestRepoRegistration_PublicGrantIntent_DoesNotTouchRegistration — намерение выдачи
// публичного чтения адресует ТОТ ЖЕ объект-репозиторий, но признака существования не
// касается: видимость и существование — разные вопросы. Контроль-кейс к дискриминатору
// (он опирается на структурную привязку, а не на тип объекта).
func TestRepoRegistration_PublicGrantIntent_DoesNotTouchRegistration(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-public-grant")

	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, "app", "prj-P", "service_account:sva-ci")))
	require.Equal(t, 1, countRegistration(t, pool, regID, "app"))

	// Снятие публичной выдачи (repo стал приватным) НЕ должно снимать существование.
	require.NoError(t, repo.UnregisterRepository(ctx, domain.UnregisterIntentForRepoPublicGrant(regID, "app")))
	require.Equal(t, 1, countRegistration(t, pool, regID, "app"),
		"смена видимости не отменяет существование ресурса")
}

// TestRepoRegistration_ControlPlaneDeleteDropsRegistration — снятие репозитория
// control-plane'ом (DeleteConfig эмитит unregister в своей транзакции, НЕ через
// emitRepoIntent) обязано снять и признак.
//
// Без этого имя оказалось бы заперто: наложение снято, а признак остался ⇒ предикат всё
// ещё отвечает «существует» ⇒ следующая запись гейтится правом на объект, у которого
// больше нет ни одного tuple, и не проходит ни у кого, включая владельца реестра. Именно
// поэтому поддержка признака сидит в общем для обоих писателей emitFGAIntent.
func TestRepoRegistration_ControlPlaneDeleteDropsRegistration(t *testing.T) {
	pool := setupTestDB(t)
	regRepo := kachopg.NewRegistryRepo(pool)
	cfgRepo := kachopg.NewRepositoryConfigRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-cp-delete")

	// Репозиторий появился первым push'ем...
	require.NoError(t, regRepo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, "app", "prj-P", "service_account:sva-ci")))
	// ...и затем принят control-plane'ом (наложение поверх проекции — путь adopt).
	_, err := cfgRepo.InsertConfig(ctx, newCfg(regID, "app", domain.VisibilityPrivate, nil))
	require.NoError(t, err, "принятие уже запушенного репозитория остаётся рабочим")
	require.Equal(t, 1, countRegistration(t, pool, regID, "app"))

	declared, err := regRepo.RepositoryDeclared(ctx, regID, "app")
	require.NoError(t, err)
	require.True(t, declared)

	// DeleteRepository: снимает наложение и эмитит unregister — в своей транзакции.
	require.NoError(t, cfgRepo.DeleteConfig(ctx, regID, "app",
		registry.OutboxIntent{Event: domain.FGAEventUnregister, Intent: domain.UnregisterIntentForRepo(regID, "app")},
		registry.OutboxIntent{Event: domain.FGAEventUnregister, Intent: domain.UnregisterIntentForRepoPublicGrant(regID, "app")},
	))

	require.Equal(t, 0, countRegistration(t, pool, regID, "app"), "признак снят и этим путём тоже")
	declared, err = regRepo.RepositoryDeclared(ctx, regID, "app")
	require.NoError(t, err)
	require.False(t, declared, "имя не заперто: следующая запись снова идёт полосой создания")
}

// TestRepoRegistration_OverlayAloneMakesResourceExist — репозиторий ЗАЯВЛЕН через
// control-plane и ещё пуст: строки регистрации нет, но наложение есть ⇒ ресурс
// существует. Это ровно тот случай, на котором прежний предикат (теги в движке) отвечал
// «нет» и отдавал чужой репозиторий соседу по реестру.
func TestRepoRegistration_OverlayAloneMakesResourceExist(t *testing.T) {
	pool := setupTestDB(t)
	regRepo := kachopg.NewRegistryRepo(pool)
	cfgRepo := kachopg.NewRepositoryConfigRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-overlay-only")

	_, err := cfgRepo.InsertConfig(ctx, newCfg(regID, "declared/app", domain.VisibilityPrivate, nil))
	require.NoError(t, err)
	require.Equal(t, 0, countRegistration(t, pool, regID, "declared/app"),
		"строки регистрации нет — репозиторий заявлен, но ни разу не запушен")

	declared, err := regRepo.RepositoryDeclared(ctx, regID, "declared/app")
	require.NoError(t, err)
	require.True(t, declared, "одного наложения достаточно: ресурс существует и пустым")
}

// TestRepoRegistration_ScopedPerRegistryAndRepo — признак ключуется парой
// (реестр, репозиторий): одноимённый репозиторий в другом реестре и другой репозиторий в
// том же реестре предикатом не задеваются.
func TestRepoRegistration_ScopedPerRegistryAndRepo(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regA := seedRegistry(t, pool, "prj-P", "reg-scope-a")
	regB := seedRegistry(t, pool, "prj-P", "reg-scope-b")

	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regA, "app", "prj-P", "service_account:sva-ci")))

	for _, tc := range []struct {
		name        string
		reg, repoNm string
		want        bool
	}{
		{"тот же реестр, тот же репозиторий", regA, "app", true},
		{"тот же реестр, другой репозиторий", regA, "other", false},
		{"другой реестр, то же имя", regB, "app", false},
	} {
		got, err := repo.RepositoryDeclared(ctx, tc.reg, tc.repoNm)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, got, tc.name)
	}
}

// TestRepoRegistration_RegistryDeleteCascadesRegistrations — удаление реестра забирает
// признаки его репозиториев (same-DB cascade по внешнему ключу): осиротевших строк,
// переживших своего владельца, не остаётся.
func TestRepoRegistration_RegistryDeleteCascadesRegistrations(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-cascade")

	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, "app", "prj-P", "service_account:sva-ci")))
	require.Equal(t, 1, countRegistration(t, pool, regID, "app"))

	require.NoError(t, repo.Delete(ctx, regID, domain.UnregisterIntentForDelete(regID, "prj-P")))
	require.Equal(t, 0, countRegistration(t, pool, regID, "app"), "признаки ушли вместе с реестром")
}
