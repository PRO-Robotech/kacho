// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package operation_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupTestDB started a container AND replayed the whole migration chain on every
// call, so the package spent its budget on its harness rather than on its tests.
//
// The container starts once, the migrations run once into a template database,
// and each caller gets its own database cloned from it — a genuinely separate
// database, with its own operations rows. See internal/pgtest for the full
// argument.
//
// The container starts lazily, so a run whose selected tests never ask for a
// database pays for nothing.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:    "nlb",
		Migrate: pgtest.Goose(migrations.FS),
	}))
}
