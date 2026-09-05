// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// seedInstanceForObserved вставляет машину и возвращает её id вместе с номером
// последнего отправленного про неё события.
//
// Номер берётся ИЗ ПОТОКА, а не назначается пробой: признак «мы этого не
// отправляли» сверяется с тем же потоком, и проба, придумавшая номер сама,
// проверяла бы своё представление о нём, а не действительность.
func seedInstanceForObserved(t *testing.T, ctx context.Context, r *repo.InstanceRepo, pool *pgxpool.Pool) (string, int64) {
	t.Helper()
	inID := ids.NewID(ids.PrefixInstance)
	_, _, err := r.Insert(ctx, &domain.Instance{
		ID: inID, Name: inID, ProjectID: "f-obs", CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning, FQDN: inID + ".auto.internal",
		InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
		NetworkInterfaces:  []domain.NetworkInterface{{Index: "0", SubnetID: "e9bsub", PrimaryV4Address: "10.0.0.10"}},
	})
	require.NoError(t, err)

	var maxSeq int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(max(sequence_no), 0) FROM compute_outbox WHERE resource_id = $1`, inID,
	).Scan(&maxSeq))
	require.Positive(t, maxSeq, "создание машины обязано было отправить хотя бы одно событие — "+
		"иначе предпосылка пробы про «номер отправленного» пуста, и её молчание ничего не доказывает")
	return inID, maxSeq
}

// TestObservedState_ThreeOutcomesAreDistinct — приём отчёта различает применение,
// устаревание и неизвестное событие.
//
// Все три утверждаются В ОДНОЙ пробе намеренно: порознь каждое зеленеет на
// реализации, которая всегда отвечает своим исходом. «Отброшено как устаревшее»
// неотличимо от «не применяется никогда», пока рядом нет применения; «отвергнуто
// как неизвестное» неотличимо от «отвергается всё», пока рядом нет принятого.
func TestObservedState_ThreeOutcomesAreDistinct(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := repo.NewInstanceRepo(pool)
	inID, maxSeq := seedInstanceForObserved(t, ctx, r, pool)
	observedAt := time.Now().UTC().Truncate(time.Second)

	t.Run("применяется", func(t *testing.T) {
		applied, current, aerr := r.ApplyObservedState(ctx, repo.ObservedReport{
			InstanceID: inID, State: "OBSERVED_RUNNING",
			SequenceNo: maxSeq, ObservedAt: observedAt, Reason: "",
		})
		require.NoError(t, aerr)
		require.True(t, applied, "первый отчёт по отправленному событию обязан примениться")
		require.Equal(t, maxSeq, current)
	})

	t.Run("устаревший отбрасывается молча, а не отказом", func(t *testing.T) {
		applied, current, aerr := r.ApplyObservedState(ctx, repo.ObservedReport{
			InstanceID: inID, State: "OBSERVED_STOPPED",
			SequenceNo: maxSeq - 1, ObservedAt: observedAt,
		})
		require.NoError(t, aerr, "устаревший отчёт — штатный исход переупорядочения, не отказ")
		require.False(t, applied)
		require.Equal(t, maxSeq, current, "действующим обязано остаться более свежее наблюдение")
	})

	t.Run("неизвестное событие отвергается", func(t *testing.T) {
		_, _, aerr := r.ApplyObservedState(ctx, repo.ObservedReport{
			InstanceID: inID, State: "OBSERVED_RUNNING",
			SequenceNo: maxSeq + 1000, ObservedAt: observedAt,
		})
		require.ErrorIs(t, aerr, repo.ErrUnknownDelivery,
			"отчёт о намерении, которого мы не отправляли, — расхождение, а не новость")
	})

	t.Run("несуществующая машина отвечает «нет такой», а не отказом хранилища", func(t *testing.T) {
		_, _, aerr := r.ApplyObservedState(ctx, repo.ObservedReport{
			InstanceID: ids.NewID(ids.PrefixInstance), State: "OBSERVED_RUNNING",
			SequenceNo: 1, ObservedAt: observedAt,
		})
		require.ErrorIs(t, aerr, ports.ErrNotFound)
	})
}

// TestObservedState_ConcurrentReportsLeaveTheFreshest — свойство держит стейтмент,
// а не порядок вызовов.
//
// Два отчёта об одной машине идут одновременно. Победить обязан свежайший
// НЕЗАВИСИМО от того, кто закоммитился последним: условие свежести стоит внутри
// обмена, поэтому проигравший не находит строки. Проверка «прочитал → сравнил →
// записал» дала бы здесь последнего писателя, а не свежайшее наблюдение.
func TestObservedState_ConcurrentReportsLeaveTheFreshest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := repo.NewInstanceRepo(pool)
	inID, maxSeq := seedInstanceForObserved(t, ctx, r, pool)
	observedAt := time.Now().UTC().Truncate(time.Second)

	// Свежий и устаревший запускаются вместе; порядок их коммитов не назначен.
	var wg sync.WaitGroup
	results := make([]struct {
		applied bool
		err     error
	}, 2)
	seqs := []int64{maxSeq, maxSeq - 1}
	states := []string{"OBSERVED_RUNNING", "OBSERVED_STOPPED"}
	for i := range seqs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			applied, _, aerr := r.ApplyObservedState(ctx, repo.ObservedReport{
				InstanceID: inID, State: states[i], SequenceNo: seqs[i], ObservedAt: observedAt,
			})
			results[i].applied, results[i].err = applied, aerr
		}(i)
	}
	wg.Wait()

	for i := range results {
		require.NoError(t, results[i].err, "ни один из двух отчётов не является отказом")
	}
	require.True(t, results[0].applied, "свежайший отчёт обязан примениться при любом порядке коммитов")

	var state string
	var seq int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT observed_state, observed_sequence_no FROM instances WHERE id = $1`, inID,
	).Scan(&state, &seq))
	require.Equal(t, "OBSERVED_RUNNING", state, "в строке обязано остаться свежайшее наблюдение")
	require.Equal(t, maxSeq, seq)

	// Про ИСХОД устаревшего отчёта здесь намеренно ничего не утверждается, и это
	// не осторожность, а точность. Порядок коммитов не назначен: закоммитившись
	// первым, устаревший отчёт применяется законно — на тот момент наблюдения не
	// было вовсе, и он свежее пустоты. Требовать от него `applied=false` значило
	// бы закрепить не инвариант, а выигранную гонку, и проба мигала бы на
	// исправном коде.
	//
	// Инвариант же ровно один и утверждён выше: свежайший применяется при любом
	// порядке, и в строке остаётся он.
}
