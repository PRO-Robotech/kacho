// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// subject_change_commit_order_integration_test.go — kacho#1374.
//
// Номер строки журнала выдаёт счётчик на ВСТАВКЕ, а строка становится видимой на
// ФИКСАЦИИ. У `kacho_iam.subject_change_outbox` номер приходит голым
// `nextval` — ничто не сериализует выдачу, — поэтому порядок номеров и порядок
// фиксаций НЕЗАВИСИМЫ.
//
// Потребитель (край, `gateway/internal/watcher/subject_change_watcher.go`) хранит
// курсор между проходами и двигает его на возвращённую позицию. Строка, чей номер
// выдан до фиксации, за курсор не вернётся НИКОГДА: перечитывание идёт строго
// «больше курсора», пропуска в нумерации потребитель не видит, и сходиться тут
// нечему.
//
// Предмет этого журнала — оповещение о том, что вердикты по субъекту пора считать
// заново. Недоехавшая строка означает, что сброса не будет до следующего события
// по тому же субъекту, а его может не быть вовсе.
//
// Проба воспроизводит ОКНО, а не форму запроса: писатель держит транзакцию
// открытой → читатель проходит мимо → писатель фиксирует → строка обязана
// доехать. Образец перенесён из `pkg/subscription/commitorder_integration_test.go`
// вместе с техникой, которой окно закрывается.

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	kachopg "github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/iam/internal/service"
)

// subjectChangeInsertSQL — вставка строки журнала теми же колонками, какими её
// пишет прод (`access_binding_repo.go`): тело обязано называть субъект, иначе
// строка не вставится вовсе.
const subjectChangeInsertSQL = `
	INSERT INTO kacho_iam.subject_change_outbox (subject_id, op, payload)
	VALUES ($1, 'binding_upsert', jsonb_build_object('subject_id', $1::text))
	RETURNING id`

// advanceLikeTheEdge двигает курсор ровно так, как это делает край: по
// наибольшему из возвращённых идентификаторов и по отданной позиции.
//
// Правило скопировано у потребителя намеренно. Проба, двигавшая бы курсор своим
// способом, утверждала бы о запросе, а не о том, что потребитель теряет.
func advanceLikeTheEdge(cursor int64, changes []service.SubjectChange, headID int64) int64 {
	for _, c := range changes {
		if c.ID > cursor {
			cursor = c.ID
		}
	}
	if headID > cursor {
		cursor = headID
	}
	return cursor
}

func TestSubjectChangePollNeverSkipsARowCommittedAfterThePositionWasIssued(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	repo := kachopg.NewSubjectChangeRepo(pool)

	t.Run("инверсный порядок фиксаций не теряет строку", func(t *testing.T) {
		// A берёт номер первым и остаётся в полёте.
		connA := mustConnectPG(t, ctx, dsn)
		txA, berr := connA.Begin(ctx)
		require.NoError(t, berr)
		var idA int64
		require.NoError(t, txA.QueryRow(ctx, subjectChangeInsertSQL, "usr-00000000000000a01").Scan(&idA))

		// B берёт СЛЕДУЮЩИЙ номер и коммитится ПЕРВЫМ.
		connB := mustConnectPG(t, ctx, dsn)
		txB, berr := connB.Begin(ctx)
		require.NoError(t, berr)
		var idB int64
		require.NoError(t, txB.QueryRow(ctx, subjectChangeInsertSQL, "usr-00000000000000b01").Scan(&idB))
		require.NoError(t, txB.Commit(ctx))
		require.Greater(t, idB, idA, "сценарий не воспроизведён: B обязан нести больший номер")

		// Проход, пока A в полёте. Отдать позицию за номером A читатель НЕ ВПРАВЕ:
		// за ней ещё появится меньший номер.
		var cursor int64
		changes, headID, perr := repo.PollSubjectChanges(ctx, cursor, 256)
		require.NoError(t, perr)
		cursor = advanceLikeTheEdge(cursor, changes, headID)
		require.Less(t, cursor, idA,
			"позиция ушла за номер писателя в полёте (курсор %d, номер в полёте %d): его строка не вернётся никогда",
			cursor, idA)

		// A коммитится — обе строки обязаны доехать, по возрастанию номера.
		require.NoError(t, txA.Commit(ctx))

		changes, headID, perr = repo.PollSubjectChanges(ctx, cursor, 256)
		require.NoError(t, perr)
		// Первое наблюдение запомнило писателя; подтверждает его следующее.
		if len(changes) == 0 {
			changes, headID, perr = repo.PollSubjectChanges(ctx, cursor, 256)
			require.NoError(t, perr)
		}
		require.Len(t, changes, 2, "после фиксации обязаны доехать обе строки")
		require.Equal(t, idA, changes[0].ID, "строка A потеряна: её номер меньше, а зафиксирована она позже")
		require.Equal(t, idB, changes[1].ID)
		require.GreaterOrEqual(t, headID, idB)
	})

	t.Run("откатившийся писатель не заклинивает журнал", func(t *testing.T) {
		// ПРЕДМЕТ здесь — ЖИВОСТЬ, а не потеря: номер отменённой транзакции не
		// появится никогда, и позиция обязана перенестись ЗА дыру.
		start := currentMaxSubjectChangeID(t, ctx, pool)

		connA := mustConnectPG(t, ctx, dsn)
		txA, berr := connA.Begin(ctx)
		require.NoError(t, berr)
		var idA int64
		require.NoError(t, txA.QueryRow(ctx, subjectChangeInsertSQL, "usr-00000000000000r01").Scan(&idA))

		connB := mustConnectPG(t, ctx, dsn)
		txB, berr := connB.Begin(ctx)
		require.NoError(t, berr)
		var idB int64
		require.NoError(t, txB.QueryRow(ctx, subjectChangeInsertSQL, "usr-00000000000000s01").Scan(&idB))
		require.NoError(t, txB.Commit(ctx))
		require.Greater(t, idB, idA)

		require.NoError(t, txA.Rollback(ctx))

		var got []service.SubjectChange
		cursor := start
		for attempt := 0; attempt < 3 && len(got) == 0; attempt++ {
			var headID int64
			var perr error
			got, headID, perr = repo.PollSubjectChanges(ctx, cursor, 256)
			require.NoError(t, perr)
			_ = headID
		}
		require.Len(t, got, 1, "после отката обязана доехать только строка B — журнал не залипает на дыре")
		require.Equal(t, idB, got[0].ID)
	})
}

// TestSubjectChangePollIsSafeUnderConcurrentPollers — наблюдение ОДНО на процесс,
// а проходов много: край опрашивает каждой репликой, и все они приходят в один
// экземпляр адаптера.
//
// Проба держит ДВА свойства сразу, и второе важнее первого:
//
//  1. состязания нет (прогонять под `-race`);
//  2. ни один проход не отдаёт позицию за номером писателя в полёте — то есть
//     общий наблюдатель не разъезжается под конкуренцией. Свойство утверждается
//     о КАЖДОМ проходе, а не о совокупности: достаточно одного, отдавшего лишнее,
//     чтобы строка была потеряна для той реплики, которая его получила.
func TestSubjectChangePollIsSafeUnderConcurrentPollers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	repo := kachopg.NewSubjectChangeRepo(pool)

	// Писатель в полёте держит транзакцию всё время опроса.
	connA := mustConnectPG(t, ctx, dsn)
	txA, err := connA.Begin(ctx)
	require.NoError(t, err)
	var idA int64
	require.NoError(t, txA.QueryRow(ctx, subjectChangeInsertSQL, "usr-00000000000000c01").Scan(&idA))

	connB := mustConnectPG(t, ctx, dsn)
	txB, err := connB.Begin(ctx)
	require.NoError(t, err)
	var idB int64
	require.NoError(t, txB.QueryRow(ctx, subjectChangeInsertSQL, "usr-00000000000000d01").Scan(&idB))
	require.NoError(t, txB.Commit(ctx))
	require.Greater(t, idB, idA)

	const pollers = 8
	var wg sync.WaitGroup
	overshoot := make([]int64, pollers)
	for i := 0; i < pollers; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			var cursor int64
			for round := 0; round < 4; round++ {
				changes, headID, perr := repo.PollSubjectChanges(ctx, cursor, 256)
				if perr != nil {
					overshoot[slot] = -1
					return
				}
				cursor = advanceLikeTheEdge(cursor, changes, headID)
				if cursor >= idA {
					overshoot[slot] = cursor
					return
				}
			}
		}(i)
	}
	wg.Wait()

	for slot, got := range overshoot {
		require.Zero(t, got,
			"проход %d ушёл на позицию %d при номере в полёте %d: общий наблюдатель разъехался под конкуренцией",
			slot, got, idA)
	}

	// И после фиксации обе строки доезжают — наблюдение не залипло.
	require.NoError(t, txA.Commit(ctx))
	var changes []service.SubjectChange
	for attempt := 0; attempt < 3 && len(changes) < 2; attempt++ {
		changes, _, err = repo.PollSubjectChanges(ctx, 0, 256)
		require.NoError(t, err)
	}
	require.Len(t, changes, 2, "после фиксации обязаны доехать обе строки")
	require.Equal(t, idA, changes[0].ID)
	require.Equal(t, idB, changes[1].ID)
}

func mustConnectPG(t testing.TB, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func currentMaxSubjectChangeID(t testing.TB, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
},
) int64 {
	t.Helper()
	var m int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(id), 0) FROM kacho_iam.subject_change_outbox`).Scan(&m))
	return m
}
