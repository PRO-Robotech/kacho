// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// orphan_sweep_integration_test.go — предикат «объект, чей владелец о нём не знает»
// и снятие РОВНО того, что предикат назвал.
//
// # Предмет
//
// Каскад по внешнему ключу унёс признаки существования репозиториев, не эмитировав
// ни одного намерения о снятии. Эмиссия на удалении реестра починена, поэтому
// накопление остановлено — но накопленное осталось: объекты стоят в хранилище прав
// со всем, что на них было, при нуле живых репозиториев.
//
// Для репозитория это не «висящая строка». Его идентичность — ПУТЬ
// `<реестр>/<имя>`, то есть ИМЯ, а не случайный идентификатор. Уцелевшее отношение
// на удалённом репозитории возвращается в силу, как только кто-то создаст
// репозиторий с тем же именем в том же реестре.
//
// # Почему предикат читает ТОЛЬКО свою базу
//
// Владелец — единственный, кто вправе сказать «такого ресурса у меня нет»: iam
// хранит зеркало и не может ни подтвердить, ни опровергнуть его. Спросить владельца
// из iam нельзя и по другой причине — iam лист, он не зовёт потребителей, иначе в
// графе появится цикл. Поэтому предикат целиком внутри базы владельца: журнал
// намерений говорит, что объект был поставлен на учёт и не снят, а живые таблицы —
// что ресурса нет.
//
// # Что здесь утверждается
//
// Не «сколько строк удалилось», а ИСХОД для принимающей стороны: на осиротевший
// объект в очередь легло намерение о снятии, а на живой и на свежий — не легло.
// Отрицание идёт в паре с положительным контролем, иначе оно зеленело бы на
// подметальщике, который вообще ничего не делает.
package pg_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// TestOrphanSweep_WithdrawsObjectTheOwnerDoesNotKnow — положительный случай:
// признак унесён молча, ресурса у владельца нет ⇒ предикат называет объект и
// подметальщик эмитирует снятие.
func TestOrphanSweep_WithdrawsObjectTheOwnerDoesNotKnow(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-orphan")

	const orphan = "team/app"
	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, orphan, "prj-P", "service_account:sva-ci")))
	require.Equal(t, 1, countRegistration(t, pool, regID, orphan),
		"положительный контроль: признак существования записан")

	// Каскад: признак исчезает, ни одного намерения не эмитируется.
	_, err := pool.Exec(ctx,
		`DELETE FROM kacho_registry.registry_repository_registration
		 WHERE registry_id = $1 AND repo = $2`, regID, orphan)
	require.NoError(t, err)
	require.Equal(t, 0, countOutbox(t, pool, regID+"/"+orphan, domain.FGAEventUnregister),
		"предпосылка: снятия не эмитировал никто — именно это и оставляло объект стоять")

	named, err := repo.SweepOrphanedRepositories(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, []string{regID + "/" + orphan}, named,
		"предикат обязан НАЗВАТЬ объект, о котором владелец не знает")

	require.Equal(t, 1, countOutbox(t, pool, regID+"/"+orphan, domain.FGAEventUnregister),
		"на осиротевший объект обязано лечь намерение о снятии: без него отношение на "+
			"удалённом репозитории вернётся в силу при повторном создании того же имени")
}

// TestOrphanSweep_LeavesLiveObjectAlone — отрицательный случай в паре с
// положительным: у живого репозитория есть признак, и трогать его нельзя.
//
// Без этой пробы «подметальщик» мог бы снимать вообще всё и оставаться зелёным на
// предыдущей.
func TestOrphanSweep_LeavesLiveObjectAlone(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-live")

	const live = "team/live"
	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, live, "prj-P", "service_account:sva-ci")))

	named, err := repo.SweepOrphanedRepositories(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, named, "живой репозиторий предикат называть не вправе")
	require.Equal(t, 0, countOutbox(t, pool, regID+"/"+live, domain.FGAEventUnregister),
		"снятие живого объекта отняло бы доступ у владельца работающего репозитория")
}

// TestOrphanSweep_LeavesFreshObjectAlone — окно отсрочки. Постановка на учёт и
// появление признака происходят в одной транзакции, но подметальщик — сторонний
// читатель: без отсрочки он вправе увидеть объект между этими двумя фактами в
// чужом снимке и снять то, что как раз создаётся.
func TestOrphanSweep_LeavesFreshObjectAlone(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-fresh")

	const fresh = "team/fresh"
	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, fresh, "prj-P", "service_account:sva-ci")))
	_, err := pool.Exec(ctx,
		`DELETE FROM kacho_registry.registry_repository_registration
		 WHERE registry_id = $1 AND repo = $2`, regID, fresh)
	require.NoError(t, err)

	named, err := repo.SweepOrphanedRepositories(ctx, time.Hour)
	require.NoError(t, err)
	require.Empty(t, named,
		"объект моложе отсрочки предикат называть не вправе — иначе подметальщик "+
			"снимает то, что прямо сейчас создаётся")

	// Тот же объект вне отсрочки — называется. Пара доказывает, что отрицание выше
	// про ВОЗРАСТ, а не про то, что предикат вообще ничего не находит.
	named, err = repo.SweepOrphanedRepositories(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, []string{regID + "/" + fresh}, named)
}

// TestOrphanSweep_IsIdempotentAndConverges — повтор безопасен и сходится: после
// первого прохода объект больше не называется, второго намерения не появляется.
//
// Несходящийся подметальщик наливал бы очередь одним и тем же снятием каждый цикл —
// то есть выглядел бы работающим и был бы источником нагрузки, а не порядка.
func TestOrphanSweep_IsIdempotentAndConverges(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-idem")

	const orphan = "team/idem"
	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, orphan, "prj-P", "service_account:sva-ci")))
	_, err := pool.Exec(ctx,
		`DELETE FROM kacho_registry.registry_repository_registration
		 WHERE registry_id = $1 AND repo = $2`, regID, orphan)
	require.NoError(t, err)

	first, err := repo.SweepOrphanedRepositories(ctx, 0)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := repo.SweepOrphanedRepositories(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, second, "второй проход не вправе назвать уже снятый объект")
	require.Equal(t, 1, countOutbox(t, pool, regID+"/"+orphan, domain.FGAEventUnregister),
		"повтор не вправе добавить второе снятие: подметальщик обязан сходиться")
}

// TestOrphanSweep_DeclaredThroughOverlayCountsAsLive — репозиторий, заявленный
// наложением, живой даже без строки признака.
//
// Предикат обязан читать ОБА источника существования — тот же предикат, которым
// data-plane выбирает глагол записи. Читай он один, подметальщик снимал бы
// заявленные через control-plane и ещё ни разу не запушенные репозитории.
func TestOrphanSweep_DeclaredThroughOverlayCountsAsLive(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-overlay")

	const declared = "team/declared"
	require.NoError(t, repo.RegisterRepository(ctx,
		domain.RegisterIntentForRepoPush(regID, declared, "prj-P", "service_account:sva-ci")))
	_, err := pool.Exec(ctx,
		`DELETE FROM kacho_registry.registry_repository_registration
		 WHERE registry_id = $1 AND repo = $2`, regID, declared)
	require.NoError(t, err)
	// Наложение объявляет тот же репозиторий существующим.
	_, err = pool.Exec(ctx,
		`INSERT INTO kacho_registry.repository_configs (registry_id, name) VALUES ($1, $2)`,
		regID, declared)
	require.NoError(t, err)

	named, err := repo.SweepOrphanedRepositories(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, named,
		"репозиторий, заявленный наложением, существует как ресурс — снимать его нельзя")
}
