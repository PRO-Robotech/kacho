// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build integration

package repo

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedInstanceForBinding — машина, которую будут захватывать.
func seedInstanceForBinding(t *testing.T, r *InstanceRepo, id string) {
	t.Helper()
	_, err := r.pool.Exec(context.Background(), `INSERT INTO instances
		(id, project_id, name, zone_id, status, instance_kind, bs_type, bs_id)
		VALUES ($1, 'prj-bind', $1, 'ru-central1-a', 'PROVISIONING', 1, 'storage.image', 'img-x')`, id)
	require.NoError(t, err)
}

// TestNodeBinding_TwoNodesOneWins — ДВА УЗЛА ОДНОВРЕМЕННО, побеждает ровно один.
//
// Это воспроизведение того отказа, ради которого владение вводится: при разрыве
// связи с плоскостью управления второй узел может начать исполнять машину,
// которую первый ещё исполняет, — а том у них один.
//
// # Почему конкурентно, а не последовательно
//
// Последовательный вызов проверяет, что второй видит результат первого, — то
// есть свойство ЧТЕНИЯ. Гонка проверяет свойство ЗАПИСИ: что между решением и
// записью не помещается чужая запись. Проба без конкуренции зеленеет на коде,
// где условие обмена проверено отдельным запросом, — то есть на том самом
// дефекте, который она должна ловить.
func TestNodeBinding_TwoNodesOneWins(t *testing.T) {
	pool := auditTestPool(t)
	r := NewInstanceRepo(pool)
	ctx := context.Background()

	const instanceID = "ins-bind-race"
	seedInstanceForBinding(t, r, instanceID)

	const nodes = 8
	var wg sync.WaitGroup
	results := make([]error, nodes)
	winners := make([]string, nodes)

	start := make(chan struct{})
	for i := 0; i < nodes; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // все стартуют разом, иначе это не гонка
			b, err := r.ClaimInstance(ctx, NodeBinding{
				InstanceID:   instanceID,
				NodeID:       "node-" + string(rune('a'+idx)),
				ClaimedSeqNo: int64(idx + 1),
				LeaseUntil:   time.Now().Add(time.Minute),
			}, 30*time.Second)
			results[idx] = err
			if b != nil {
				winners[idx] = b.NodeID
			}
		}(i)
	}
	close(start)
	wg.Wait()

	var won int
	for i := range results {
		if results[i] == nil {
			won++
			continue
		}
		require.ErrorIs(t, results[i], ErrHeldByAnotherNode,
			"проигравший узел обязан получить именно «занято», а не общий отказ")
	}
	require.Equal(t, 1, won, "ровно один узел получает машину; двое — это два писателя в один том")

	// Победитель в базе — тот же, что вернул успех: иначе успех означал бы не то,
	// что записано.
	//
	// Читаем строку напрямую, а не через метод репозитория: чтения привязки в
	// прод-коде нет, и заводить его РАДИ ПРОБЫ значило бы расширить поверхность
	// пакета под нужды теста. Проба внутренняя — база ей доступна.
	var boundNode string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT node_id FROM instance_node_bindings WHERE instance_id = $1`,
		instanceID).Scan(&boundNode))
	for i := range winners {
		if winners[i] != "" {
			require.Equal(t, winners[i], boundNode)
		}
	}
}

// TestNodeBinding_SameNodeReclaimIsIdempotent — повторный захват своим узлом
// проходит.
//
// Агент, перезапустившийся и не помнящий о себе, не должен терять свою же
// машину: иначе перезапуск агента превращается в потерю машины, то есть в
// отказ, вызванный лечением.
func TestNodeBinding_SameNodeReclaimIsIdempotent(t *testing.T) {
	pool := auditTestPool(t)
	r := NewInstanceRepo(pool)
	ctx := context.Background()

	const instanceID = "ins-bind-idem"
	seedInstanceForBinding(t, r, instanceID)

	first, err := r.ClaimInstance(ctx, NodeBinding{
		InstanceID: instanceID, NodeID: "node-a", ClaimedSeqNo: 1,
		LeaseUntil: time.Now().Add(time.Minute),
	}, 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, "node-a", first.NodeID)

	second, err := r.ClaimInstance(ctx, NodeBinding{
		InstanceID: instanceID, NodeID: "node-a", ClaimedSeqNo: 2,
		LeaseUntil: time.Now().Add(2 * time.Minute),
	}, 30*time.Second)
	require.NoError(t, err, "свой узел обязан переподтверждать свою же машину")
	require.Equal(t, int64(2), second.ClaimedSeqNo)
}

// TestNodeBinding_ExpiryAloneIsNotEnough — истечения аренды НЕДОСТАТОЧНО.
//
// Это половина решения дыры, найденной разбором: разрыв связи с плоскостью
// управления НЕ разрывает связь с хранилищем, поэтому «аренда истекла ⇒
// перехват законен» допускает двух писателей. Перехват законен только с
// запасом, превышающим срок самоостановки узла.
//
// Отрицание идёт в паре с положительным: без второй половины проба зеленела бы
// на коде, где перехват не работает вовсе.
func TestNodeBinding_ExpiryAloneIsNotEnough(t *testing.T) {
	pool := auditTestPool(t)
	r := NewInstanceRepo(pool)
	ctx := context.Background()

	const instanceID = "ins-bind-expiry"
	seedInstanceForBinding(t, r, instanceID)

	// Аренда истекла пять секунд назад.
	_, err := r.ClaimInstance(ctx, NodeBinding{
		InstanceID: instanceID, NodeID: "node-a", ClaimedSeqNo: 1,
		LeaseUntil: time.Now().Add(-5 * time.Second),
	}, 30*time.Second)
	require.NoError(t, err)

	// (−) запас не выдержан — перехват отвергается, хотя аренда формально истекла
	_, err = r.ClaimInstance(ctx, NodeBinding{
		InstanceID: instanceID, NodeID: "node-b", ClaimedSeqNo: 2,
		LeaseUntil: time.Now().Add(time.Minute),
	}, 30*time.Second)
	require.ErrorIs(t, err, ErrHeldByAnotherNode,
		"истечения мало: прежний узел мог не успеть остановить машину")

	// (+) запас выдержан — перехват проходит
	got, err := r.ClaimInstance(ctx, NodeBinding{
		InstanceID: instanceID, NodeID: "node-b", ClaimedSeqNo: 3,
		LeaseUntil: time.Now().Add(time.Minute),
	}, time.Second)
	require.NoError(t, err, "с выдержанным запасом перехват обязан проходить")
	require.Equal(t, "node-b", got.NodeID)
}
