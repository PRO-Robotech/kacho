// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package jobs

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupTestDB started a container AND replayed the whole migration chain on each
// of its ~20 calls, so the package spent its budget on its harness rather than on
// the reconciler proofs it exists for.
//
// The container starts once, the migrations run once into a template database,
// and each caller gets its own database cloned from it. That is the same
// isolation a separate container gave: a clone has its own rows, its own
// sequences and its own advisory-lock space, so the multi-replica FOR UPDATE SKIP
// LOCKED proofs are unchanged — concurrent goroutines inside one test still
// contend on the same real rows. See internal/pgtest for the full argument.
//
// The container starts lazily, so a run whose selected tests all skip pays for
// nothing.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `internal/pgtest` §searchpath.
		SearchPath: "kacho_nlb,public",
		Name:       "nlb",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}
