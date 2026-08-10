// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// Бюджет свободной карты данных машины принадлежит БАЗЕ, а не только проверке
// запроса.
//
// Правка СЛИВАЕТСЯ в уже накопленное, поэтому проверка одной дельты границу не
// держит: карту той же величины набирают множеством приемлемых по отдельности
// правок. Программная проверка итога («прочитать → сложить → проверить →
// записать») оставила бы окно между проверкой и записью, в которое две
// одновременные правки проходят каждая по отдельности и превышают бюджет вместе.
// Поэтому инвариант выражен конструкцией базы, а слияние — одним стейтментом.

// mdValue — значение заданной длины.
func mdValue(n int) string { return strings.Repeat("x", n) }

// TestIntegration_InstanceMetadata_AccumulatedBudgetIsEnforcedByTheDatabase —
// каждая правка по отдельности в бюджете, их СУММА — нет.
func TestIntegration_InstanceMetadata_AccumulatedBudgetIsEnforcedByTheDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	defer pool.Close()

	r := repo.NewInstanceRepo(pool)
	in := seedMetadataInstance(t, ctx, r, "ins-mdbudget00000001")

	// Каждая порция — четверть бюджета: сама по себе приемлема.
	chunk := domain.MaxInstanceMetadataBytes / 4
	var lastErr error
	applied := 0
	for i := 0; i < 8; i++ {
		key := "k" + string(rune('a'+i))
		_, uerr := r.MergeMetadata(ctx, in.ID, nil, map[string]string{key: mdValue(chunk)})
		if uerr != nil {
			lastErr = uerr
			break
		}
		applied++
	}

	require.Error(t, lastErr,
		"восемь правок по четверти бюджета накопили карту вчетверо больше бюджета и ни одна не была "+
			"отвергнута: слияние накапливает, поэтому границу обязана держать БАЗА, а не проверка дельты")
	require.ErrorIs(t, lastErr, ports.ErrInvalidArg,
		"нарушение бюджета обязано доезжать до вызывающего как неверный аргумент, а не как внутренняя ошибка")
	require.LessOrEqual(t, applied, 4,
		"принято %d правок по четверти бюджета — база пропустила больше бюджета", applied)

	// Отказ не должен повредить уже накопленное: карта осталась в бюджете.
	var size int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT octet_length(metadata::text) FROM instances WHERE id = $1`, in.ID).Scan(&size))
	require.LessOrEqual(t, size, domain.MaxInstanceMetadataBytes,
		"после отвергнутой правки в строке лежит карта сверх бюджета (%d байт)", size)
}

// TestIntegration_InstanceMetadata_WithinBudgetStillWorks — обратная сторона:
// законная правка применяется, и предыдущая проверка не выполняется «запретом
// всего».
func TestIntegration_InstanceMetadata_WithinBudgetStillWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	defer pool.Close()

	r := repo.NewInstanceRepo(pool)
	in := seedMetadataInstance(t, ctx, r, "ins-mdbudget00000002")

	updated, err := r.MergeMetadata(ctx, in.ID, nil, map[string]string{
		"user-data": mdValue(4096), "ssh-keys": "ssh-ed25519 AAAA",
	})
	require.NoError(t, err)
	require.Equal(t, "ssh-ed25519 AAAA", updated.Metadata["ssh-keys"])

	// Удаление ключа освобождает бюджет — граница на РАЗМЕРЕ, а не «навсегда занято».
	updated, err = r.MergeMetadata(ctx, in.ID, []string{"user-data"}, nil)
	require.NoError(t, err)
	require.NotContains(t, updated.Metadata, "user-data")
}

// seedMetadataInstance вставляет минимальную строку машины для проб метаданных.
func seedMetadataInstance(t *testing.T, ctx context.Context, r *repo.InstanceRepo, id string) *domain.Instance {
	t.Helper()
	in, _, err := r.Insert(ctx, &domain.Instance{
		ID:            id,
		ProjectID:     "prj-md",
		ZoneID:        "ru-central1-a",
		Name:          "",
		MachineTypeID: "mt-std2",
		Status:        domain.InstanceStatusRunning,
	})
	require.NoError(t, err)
	return in
}
