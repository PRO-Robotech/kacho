// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	uc "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/dataplane"
	dataplanepg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/dataplane"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
)

// dataplaneFixture — своя база, пул и адаптер проекции намерения.
func dataplaneFixture(t *testing.T) (context.Context, *pgxpool.Pool, *dataplanepg.Store) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	return ctx, pool, dataplanepg.New(pool)
}

// insertNetwork создаёт сеть напрямую в таблице.
//
// Намеренно сырым SQL, а не через репозиторий: предмет проб ниже — то, что
// проекция ведётся ТРИГГЕРОМ, то есть отвечает КАЖДОМУ писателю, а не только
// тому, который зовёт эмиттер. Пройдя через репозиторий, проба доказывала бы
// куда более слабое утверждение.
func insertNetwork(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixNetwork)
	_, err := pool.Exec(ctx,
		`INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1, $2, $3)`,
		id, "prj-dataplane", name)
	require.NoError(t, err)
	return id
}

func revisionOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) int64 {
	t.Helper()
	var rev int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT revision FROM kacho_vpc.dataplane_intent WHERE resource_id = $1`, id).Scan(&rev))
	return rev
}

// Создание ресурса ЛЮБЫМ писателем заводит намерение с положительной ревизией, а
// удаление превращает его в снятие — не удаляет строку.
//
// Снятие обязано пережить сам ресурс: исполнитель, узнавший о нём позже, иначе
// оставил бы применённое навсегда.
func TestIntegration_DataplaneIntent_TrackedByTheDatabaseItself(t *testing.T) {
	ctx, pool, _ := dataplaneFixture(t)

	id := insertNetwork(t, ctx, pool, "dp-tracked")
	created := revisionOf(t, ctx, pool, id)
	assert.Positive(t, created, "создание не завело намерения")

	_, err := pool.Exec(ctx, `UPDATE kacho_vpc.networks SET description = 'изменено' WHERE id = $1`, id)
	require.NoError(t, err)
	updated := revisionOf(t, ctx, pool, id)
	assert.Greater(t, updated, created, "правка не подняла ревизию — исполнитель не узнал бы об изменении")

	// Правка, ничего не изменившая, ревизию НЕ двигает: иначе исполнитель
	// переприменял бы неизменённое, а на потоке это неотличимо от изменения.
	_, err = pool.Exec(ctx, `UPDATE kacho_vpc.networks SET description = 'изменено' WHERE id = $1`, id)
	require.NoError(t, err)
	assert.Equal(t, updated, revisionOf(t, ctx, pool, id), "пустая правка подняла ревизию")

	_, err = pool.Exec(ctx, `DELETE FROM kacho_vpc.networks WHERE id = $1`, id)
	require.NoError(t, err)

	var withdrawn bool
	var afterDelete int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT withdrawn, revision FROM kacho_vpc.dataplane_intent WHERE resource_id = $1`, id).
		Scan(&withdrawn, &afterDelete))
	assert.True(t, withdrawn, "удаление не оставило снятия намерения")
	assert.Greater(t, afterDelete, updated, "снятие не подняло ревизию")
}

// Ревизия выдаётся в порядке ФИКСАЦИИ: пока незафиксированная транзакция держит
// свою ревизию, никакая более поздняя не становится видимой.
//
// Ради этого свойства выдача идёт под консультативной блокировкой. Без него
// возможен ровно один, зато неисправимый исход: исполнитель видит ревизию 6,
// двигает курсор на 6, и ревизия 5 не отдаётся ему НИКОГДА. Потерянное изменение
// неотличимо от непришедшего.
func TestIntegration_DataplaneIntent_RevisionsAreIssuedInCommitOrder(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	before, err := store.Bounds(ctx)
	require.NoError(t, err)

	// Транзакция A берёт ревизию и НЕ фиксируется.
	txA, err := pool.Begin(ctx)
	require.NoError(t, err)
	idA := ids.NewID(ids.PrefixNetwork)
	_, err = txA.Exec(ctx,
		`INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1, $2, $3)`,
		idA, "prj-dataplane", "dp-order-a")
	require.NoError(t, err)

	// Транзакция B пытается взять ревизию следом.
	bDone := make(chan error, 1)
	go func() {
		_, e := pool.Exec(ctx,
			`INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1, $2, $3)`,
			ids.NewID(ids.PrefixNetwork), "prj-dataplane", "dp-order-b")
		bDone <- e
	}()

	select {
	case e := <-bDone:
		_ = txA.Rollback(ctx)
		t.Fatalf("вторая транзакция получила ревизию, пока первая держит свою (err=%v): "+
			"порядок выдачи разошёлся с порядком фиксации, и изменение первой потерялось бы навсегда", e)
	case <-time.After(300 * time.Millisecond):
		// Ожидаемо: B ждёт освобождения блокировки.
	}

	// Голова журнала не сдвинулась: ни одна ревизия выше не стала видимой.
	during, err := store.Bounds(ctx)
	require.NoError(t, err)
	assert.Equal(t, before.Head, during.Head,
		"пока первая транзакция открыта, голова журнала ушла вперёд")

	require.NoError(t, txA.Commit(ctx))
	require.NoError(t, <-bDone, "вторая транзакция не прошла после фиксации первой")

	after, err := store.Bounds(ctx)
	require.NoError(t, err)
	assert.Greater(t, after.Head, before.Head)
	assert.Greater(t, revisionOf(t, ctx, pool, idA), before.Head,
		"ревизия первой транзакции не попала в журнал")
}

// Выдача с нуля отдаёт живые объекты с телами; продолжение с ревизии отдаёт
// только то, что после неё.
func TestIntegration_DataplaneIntent_SnapshotThenResume(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	idOne := insertNetwork(t, ctx, pool, "dp-one")
	idTwo := insertNetwork(t, ctx, pool, "dp-two")

	b, err := store.Bounds(ctx)
	require.NoError(t, err)

	snapshot, err := store.Page(ctx, 0, b.Head, uc.PageLimit)
	require.NoError(t, err)

	seen := map[string]uc.IntentRow{}
	var prev int64
	for _, row := range snapshot {
		require.NoError(t, row.Validate(), "строка выдачи не имеет формы, пригодной для доставки")
		assert.Greater(t, row.Revision, prev, "выдача не упорядочена по ревизии")
		prev = row.Revision
		seen[row.ResourceID] = row
	}
	require.Contains(t, seen, idOne)
	require.Contains(t, seen, idTwo)
	require.NotNil(t, seen[idOne].Network, "у живого намерения нет тела ресурса")
	assert.Equal(t, "dp-one", string(seen[idOne].Network.Name))
	assert.Positive(t, seen[idOne].Network.VRFID,
		"координата изоляции не доехала — исполнителю нечем отличить одну изоляцию от другой")

	// Продолжение с головы: только то, что появилось после.
	head, err := store.Bounds(ctx)
	require.NoError(t, err)
	idThree := insertNetwork(t, ctx, pool, "dp-three")

	delta, err := store.Page(ctx, head.Head, 0, uc.PageLimit)
	require.NoError(t, err)
	require.Len(t, delta, 1, "продолжение отдало не только то, что после названной ревизии")
	assert.Equal(t, idThree, delta[0].ResourceID)
}

// Надгробие объекта, удалённого ДО подписки, в полную выдачу не попадает; то же
// надгробие в ПРОДОЛЖЕНИИ отдаётся.
//
// Обе половины обязательны: без первой исполнитель получает шум об объектах,
// которых никогда не видел; без второй — не узнаёт об удалении и держит
// применённое навсегда.
func TestIntegration_DataplaneIntent_TombstoneVisibilityDependsOnWhereYouStart(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	idGone := insertNetwork(t, ctx, pool, "dp-gone")
	beforeDelete, err := store.Bounds(ctx)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM kacho_vpc.networks WHERE id = $1`, idGone)
	require.NoError(t, err)

	afterDelete, err := store.Bounds(ctx)
	require.NoError(t, err)

	// Полная выдача: надгробий не старше головы на момент подписки — нет.
	snapshot, err := store.Page(ctx, 0, afterDelete.Head, uc.PageLimit)
	require.NoError(t, err)
	for _, row := range snapshot {
		assert.NotEqual(t, idGone, row.ResourceID,
			"полная выдача несёт надгробие объекта, которого исполнитель никогда не видел")
	}

	// Продолжение с позиции ДО удаления: надгробие обязано прийти.
	delta, err := store.Page(ctx, beforeDelete.Head, 0, uc.PageLimit)
	require.NoError(t, err)
	var found bool
	for _, row := range delta {
		if row.ResourceID == idGone {
			found = true
			assert.True(t, row.Withdrawn)
			require.NoError(t, row.Validate())
			assert.Nil(t, row.Network, "снятое намерение несёт тело удалённого ресурса")
		}
	}
	assert.True(t, found, "продолжение не отдало снятия — применённое осталось бы у исполнителя навсегда")
}

// Подтверждение применения: свежее записывается, устаревшее НЕ засчитывается за
// свежее, повторное о той же ревизии проходит.
func TestIntegration_DataplaneApply_StaleReportIsNotCountedAsFresh(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	id := insertNetwork(t, ctx, pool, "dp-apply")
	first := revisionOf(t, ctx, pool, id)

	got, err := store.Record(ctx, uc.ApplyReport{
		ResourceID: id, Revision: first, Outcome: uc.OutcomeApplied})
	require.NoError(t, err)
	assert.True(t, got.Recorded)
	assert.Equal(t, first, got.CurrentRevision)

	// Намерение поехало вперёд.
	_, err = pool.Exec(ctx, `UPDATE kacho_vpc.networks SET description = 'вторая ревизия' WHERE id = $1`, id)
	require.NoError(t, err)
	second := revisionOf(t, ctx, pool, id)
	require.Greater(t, second, first)

	// Исполнитель докладывает про НОВУЮ ревизию — записывается.
	got, err = store.Record(ctx, uc.ApplyReport{
		ResourceID: id, Revision: second, Outcome: uc.OutcomeApplied})
	require.NoError(t, err)
	require.True(t, got.Recorded)

	// Опоздавший доклад про СТАРУЮ ревизию — не засчитывается.
	got, err = store.Record(ctx, uc.ApplyReport{
		ResourceID: id, Revision: first, Outcome: uc.OutcomeFailed, Reason: uc.ReasonTransient})
	require.NoError(t, err)
	assert.False(t, got.Recorded,
		"доклад о применении устаревшего намерения засчитан за применение свежего")
	assert.Equal(t, second, got.CurrentRevision)

	var storedRev int64
	var storedOutcome, storedReason string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT applied_revision, outcome, reason FROM kacho_vpc.dataplane_apply WHERE resource_id = $1`, id).
		Scan(&storedRev, &storedOutcome, &storedReason))
	assert.Equal(t, second, storedRev, "устаревший доклад перезаписал состояние")
	assert.Equal(t, "APPLIED", storedOutcome)
	assert.Empty(t, storedReason)

	// Повторный доклад о ТОЙ ЖЕ ревизии проходит и может нести другой исход:
	// исполнитель вправе повторить попытку и сообщить о ней.
	got, err = store.Record(ctx, uc.ApplyReport{
		ResourceID: id, Revision: second, Outcome: uc.OutcomeFailed, Reason: uc.ReasonCapacity})
	require.NoError(t, err)
	assert.True(t, got.Recorded)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT outcome, reason FROM kacho_vpc.dataplane_apply WHERE resource_id = $1`, id).
		Scan(&storedOutcome, &storedReason))
	assert.Equal(t, "FAILED", storedOutcome)
	assert.Equal(t, "CAPACITY", storedReason)
}

// Доклад о том, чего платформа не объявляла, отвергается — и два разных случая
// различаются: объекта нет вовсе против «такой ревизии ему не выдавали».
func TestIntegration_DataplaneApply_RefusesWhatWasNeverDeclared(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	_, err := store.Record(ctx, uc.ApplyReport{
		ResourceID: ids.NewID(ids.PrefixNetwork), Revision: 1, Outcome: uc.OutcomeApplied})
	require.ErrorIs(t, err, uc.ErrIntentUnknown)

	id := insertNetwork(t, ctx, pool, "dp-never")
	rev := revisionOf(t, ctx, pool, id)
	_, err = store.Record(ctx, uc.ApplyReport{
		ResourceID: id, Revision: rev + 1000, Outcome: uc.OutcomeApplied})
	require.ErrorIs(t, err, uc.ErrRevisionNotIssued)
}

// Уплотнение удаляет старые снятия и ПОДНИМАЕТ горизонт — иначе позиция,
// с которой продолжать уже нечем, выглядела бы годной.
func TestIntegration_DataplaneIntent_CompactionRaisesTheHorizon(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	id := insertNetwork(t, ctx, pool, "dp-compact")
	_, err := pool.Exec(ctx, `DELETE FROM kacho_vpc.networks WHERE id = $1`, id)
	require.NoError(t, err)
	tombstone := revisionOf(t, ctx, pool, id)

	before, err := store.Bounds(ctx)
	require.NoError(t, err)
	require.Less(t, before.Horizon, tombstone)

	// Срок хранения не истёк — уплотнять нечего. Положительный контроль: без
	// него проба зеленела бы на уплотнении, стирающем всё подряд, а исполнитель
	// получал бы «начни сначала» на каждое переподключение.
	removed, horizon, err := store.Compact(ctx, time.Hour)
	require.NoError(t, err)
	assert.Zero(t, removed, "уплотнение стёрло снятие, срок хранения которого не истёк")
	assert.Equal(t, before.Horizon, horizon)

	// Состарить надгробие и уплотнить.
	_, err = pool.Exec(ctx,
		`UPDATE kacho_vpc.dataplane_intent SET stamped_at = now() - interval '10 days' WHERE resource_id = $1`, id)
	require.NoError(t, err)

	removed, horizon, err = store.Compact(ctx, time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
	assert.GreaterOrEqual(t, horizon, tombstone, "горизонт не поднялся до стёртой ревизии")

	after, err := store.Bounds(ctx)
	require.NoError(t, err)
	assert.Equal(t, horizon, after.Horizon)

	// Непозитивный срок хранения — отказ, а не «стереть всё сейчас же».
	_, _, err = store.Compact(ctx, 0)
	require.Error(t, err)
}

// Сквозная проба: use-case поверх настоящей базы отдаёт полную выдачу и
// смыкает её на голове журнала.
func TestIntegration_DataplaneIntent_UseCaseServesTheSnapshotFromTheRealStore(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	idOne := insertNetwork(t, ctx, pool, "dp-e2e-one")
	idTwo := insertNetwork(t, ctx, pool, "dp-e2e-two")

	obs := uc.NewObserver(nil)
	watch := uc.NewWatchIntentUseCase(store, obs)

	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	sink := &recordingSink{stop: cancel, until: 3}

	require.NoError(t, watch.Run(runCtx, 0, sink))

	var ids []string
	var syncedAt int64
	for _, m := range sink.msgs {
		if in := m.GetIntent(); in != nil {
			ids = append(ids, in.GetNetwork().GetNetwork().GetId())
		}
		if s := m.GetSynced(); s != nil {
			syncedAt = s.GetRevision()
		}
	}
	assert.Contains(t, ids, idOne)
	assert.Contains(t, ids, idTwo)

	head, err := store.Bounds(ctx)
	require.NoError(t, err)
	assert.Equal(t, head.Head, syncedAt, "выдача сомкнулась не на голове журнала")
	assert.Equal(t, int64(2), obs.Totals().IntentsSent)
	assert.Zero(t, obs.Totals().Overflows)
	assert.Zero(t, obs.Totals().MissingBodies)
}

// recordingSink — получатель сообщений потока для сквозной пробы.
//
// Останавливает поток, приняв ожидаемое число сообщений: подписка живёт до
// срока, и без остановки проба ждала бы его целиком.
type recordingSink struct {
	msgs  []*vpcv1.WatchIntentResponse
	stop  func()
	until int
}

func (s *recordingSink) Send(m *vpcv1.WatchIntentResponse) error {
	s.msgs = append(s.msgs, m)
	if s.until > 0 && len(s.msgs) >= s.until && s.stop != nil {
		s.stop()
	}
	return nil
}
