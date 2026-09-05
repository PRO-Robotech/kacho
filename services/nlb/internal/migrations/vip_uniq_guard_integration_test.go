// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrations_test

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// indexIsValid читает pg_index.indisvalid для именованного индекса схемы kacho_nlb.
// found=false — индекс отсутствует.
func indexIsValid(t *testing.T, db *sql.DB, name string) (valid, found bool) {
	t.Helper()
	row := db.QueryRow(`
		SELECT i.indisvalid
		  FROM pg_index i
		  JOIN pg_class c ON c.oid = i.indexrelid
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'kacho_nlb' AND c.relname = $1`, name)
	var v bool
	switch err := row.Scan(&v); err {
	case nil:
		return v, true
	case sql.ErrNoRows:
		return false, false
	default:
		t.Fatalf("query index validity %s: %v", name, err)
		return false, false
	}
}

// TestMigration_RegionVIPUniq_HealsInvalidIndex — регрессия к audit-finding
// "per-region VIP uniqueness index CONCURRENTLY+IF NOT EXISTS can silently
// remain unenforced after a failed build".
//
// Сценарий: 0009 создаёт region-uniq индексы CONCURRENTLY; прерванный build
// оставляет INVALID-индекс (indisvalid=false), который НЕ энфорсит уникальность.
// 0009's IF NOT EXISTS при повторе такой индекс не пересоздаёт → инвариант молча
// отсутствует. Heal-миграция обязана обнаружить INVALID-индекс, снять его и
// пересобрать валидным (+ пост-условие: fail миграции если остался INVALID).
//
// RED без heal-миграции: после инъекции indisvalid=false индекс остаётся
// невалидным. GREEN: heal-миграция чинит его.
func TestMigration_RegionVIPUniq_HealsInvalidIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	// Своя ПУСТАЯ база на одном контейнере пакета: тест сам ведёт цепочку миграций
	// (UpTo 9 → инъекция → Up), поэтому мигрированный шаблон был бы не той точкой старта.
	db, err := sql.Open("pgx", pgtest.NewEmptyDB(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))

	// Apply through 0009 — this is where the region-uniq indexes are born.
	require.NoError(t, goose.UpTo(db, ".", 9))
	valid, found := indexIsValid(t, db, "load_balancers_region_v4_uniq")
	require.True(t, found, "0009 must create load_balancers_region_v4_uniq")
	require.True(t, valid, "index is valid on a clean build")

	// Simulate an interrupted CONCURRENTLY build: flip the index to INVALID via
	// the catalog (equivalent to a build that crashed mid-way). The testcontainers
	// bootstrap user is a superuser, so the catalog UPDATE is permitted.
	_, err = db.Exec(`UPDATE pg_index SET indisvalid = false
		WHERE indexrelid = 'kacho_nlb.load_balancers_region_v4_uniq'::regclass`)
	require.NoError(t, err)
	valid, _ = indexIsValid(t, db, "load_balancers_region_v4_uniq")
	require.False(t, valid, "precondition: index is now INVALID (unenforced)")

	// Apply the remaining migrations, including the heal migration. It must drop
	// the invalid index and rebuild it valid; its post-condition assertion must
	// fail the migration if any region-uniq index stays INVALID.
	require.NoError(t, goose.Up(db, "."))

	valid, found = indexIsValid(t, db, "load_balancers_region_v4_uniq")
	require.True(t, found, "heal migration must leave the index present")
	require.True(t, valid, "heal migration must rebuild the region-uniq index VALID (uniqueness re-enforced)")

	// v6 sibling must also be valid (never injected, must survive untouched).
	v6valid, v6found := indexIsValid(t, db, "load_balancers_region_v6_uniq")
	require.True(t, v6found)
	require.True(t, v6valid, "v6 region-uniq index must remain valid")
}
