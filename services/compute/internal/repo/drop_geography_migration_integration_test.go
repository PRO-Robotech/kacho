// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
)

// TestIntegration_DropGeographyMigration verifies the S7 drop-migration
// (0011_drop_geography): Geography (Region/Zone) is now owned by kacho-geo, so
// compute's local `zones`/`regions` tables are dropped. Down recreates
// regions+zones (FK zones.region_id→regions ON DELETE RESTRICT) + reseeds
// ru-central1{,-a,-b,-d}.
//
// It also pins the block-storage retire at the same head, because that is where
// the two meet: `disk_types` used to be asserted as SURVIVING the geography drop
// (it shared the catalog schema), and it no longer does — 0022 drops it, along
// with 0021 dropping disks/images/snapshots. Asserting the tables are gone at head
// is what makes the retire migrations real rather than merely written: DownTo(10)
// below runs their Down as well, so both directions are exercised here.
func TestIntegration_DropGeographyMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()

	// EMPTY-база на контейнере пакета: этот тест сам идёт по цепочке миграций
	// вверх и вниз, поэтому предмигрированный шаблон был бы неверной точкой старта.
	dsn := pgtest.NewEmptyDB(t)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))

	// Up to head — drop-geography must have run: zones/regions gone.
	require.NoError(t, goose.Up(db, "."))
	require.False(t, tableExists(t, db, "zones"), "zones table must be dropped at head")
	require.False(t, tableExists(t, db, "regions"), "regions table must be dropped at head")

	// Block-storage retire (0021/0022): kacho-storage owns Volume/Image/Snapshot/
	// DiskType, so none of compute's duplicates may exist at head.
	for _, tbl := range []string{"disks", "images", "snapshots", "disk_types"} {
		require.Falsef(t, tableExists(t, db, tbl),
			"%s must be dropped at head — kacho-storage owns block storage", tbl)
	}
	require.True(t, tableExists(t, db, "instances"), "instances must survive both drops")
	require.True(t, tableExists(t, db, "machine_types"), "machine_types must survive both drops")

	// Down to version 10 — reverts every migration after 0010 (which includes
	// 0011_drop_geography and the 0021/0022 block-storage drops), so 0011's Down
	// recreates regions + zones with seed + FK, and 0021/0022's Down recreate the
	// retired tables. Targeting version 10 explicitly (not a single Down step) keeps
	// this assertion stable as later additive migrations land on top of 0011.
	require.NoError(t, goose.DownTo(db, ".", 10))
	require.True(t, tableExists(t, db, "zones"), "Down must recreate zones")
	require.True(t, tableExists(t, db, "regions"), "Down must recreate regions")
	for _, tbl := range []string{"disks", "images", "snapshots", "disk_types"} {
		require.Truef(t, tableExists(t, db, tbl), "Down must recreate %s", tbl)
	}
	require.Equal(t, 4, rowCount(t, db, "disk_types"), "0022 Down must restore the 4 seeded disk types")
	require.Equal(t, 3, rowCount(t, db, "zones"), "Down must reseed 3 zones")
	require.Equal(t, 1, rowCount(t, db, "regions"), "Down must reseed ru-central1")

	// FK zones.region_id → regions ON DELETE RESTRICT: a region with zones cannot be deleted.
	_, err = db.ExecContext(ctx, `DELETE FROM regions WHERE id = 'ru-central1'`)
	require.Error(t, err, "FK RESTRICT must block deleting a region that still has zones")
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1`, name,
	).Scan(&n))
	return n > 0
}

func rowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
	return n
}
