// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// newTestPool was called 91 times and started a container AND replayed the whole
// migration chain on each call, so the package spent its budget on its harness
// rather than on its tests — past the 600s `go test` applies by default, which
// means `go test ./...` could not finish it at all.
//
// The container starts once, the migrations run once into a template database,
// and each caller gets its own database cloned from it. The isolation the CAS /
// UNIQUE / EXCLUDE / SKIP LOCKED proofs depend on is unchanged: a clone is a
// separate database, and concurrent goroutines inside one test still contend on
// the same real rows. Nothing here needs the SERVER to itself. See
// internal/pgtest for the full argument.
//
// The container starts lazily, so under -short — where every test above skips —
// none is started.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `internal/pgtest` §WithSearchPath.
		SearchPath: "kacho_storage,public",
		Name:       "storage",
		User:       "storage",
		Password:   "secret",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}
