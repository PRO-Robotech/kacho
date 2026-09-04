// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupTestDB and startPG each started a container per call, ~69 times between
// them, and setupTestDB replayed the whole migration chain on top — so the
// package spent its budget on its harness rather than on its tests.
//
// The container starts once, the migrations run once into a template database,
// and each caller gets its own database: a clone of the template (setupTestDB) or
// an empty one (startPG, whose tests walk the chain from below the head
// themselves). Either way it is a separate database, so the isolation the CAS /
// partial-UNIQUE / advisory-lock race proofs depend on is unchanged, and
// concurrent goroutines inside one test still contend on the same real rows. See
// pkg/pgtest for the full argument.
//
// The container starts lazily, so under -short — where every test above skips —
// none is started.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `pkg/pgtest` §WithSearchPath.
		SearchPath: "kacho_registry,public",
		Name:       "registry",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}
