// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// REG-1-01/10/14 — F4 region/placement round-trip: Insert записывает region_id +
// placement_type='REGIONAL', Get их читает (миграция 0006 применена).
func TestRepo_REG_1_10_RegionPlacementRoundTrip(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	reg := newReg("prj-P", "payments", nil)
	reg.RegionID = "eu-north-1"
	intent := domain.RegisterIntentForCreate(reg, "user", "usr-alice")
	created, err := repo.Insert(ctx, reg, intent)
	require.NoError(t, err)
	require.Equal(t, "eu-north-1", created.RegionID)
	require.Equal(t, domain.PlacementTypeRegional, created.PlacementType)

	got, err := repo.Get(ctx, reg.ID)
	require.NoError(t, err)
	require.Equal(t, "eu-north-1", got.RegionID, "region_id persisted + read back")
	require.Equal(t, domain.PlacementTypeRegional, got.PlacementType, "placement_type REGIONAL")
}

// REG-1-14 (DB-invariant) — placement-anchor CHECK отвергает пустой region_id
// (23514 check_violation) — within-service инвариант на DB-уровне (ban #10), не
// software check-then-act. Registry с пустым регионом структурно невыразим.
func TestRepo_REG_1_14_PlacementAnchorCheck_RejectsEmptyRegion(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()

	// Прямой INSERT в обход domain-валидации: пустой region_id должен упереться в CHECK.
	_, err := pool.Exec(ctx, `
		INSERT INTO kacho_registry.registries (id, project_id, name, status, region_id, placement_type)
		VALUES ($1, 'prj-P', 'anchor-neg', 'ACTIVE', '', 'REGIONAL')`,
		ids.NewID(ids.PrefixRegistry))
	require.Error(t, err, "пустой region_id при placement REGIONAL нарушает placement-anchor CHECK")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23514", pgErr.Code, "check_violation (placement-anchor)")
}
