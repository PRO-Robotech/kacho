// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupDB started a container AND replayed the whole migration chain on every
// call, so the package spent its budget on its harness rather than on its tests.
//
// The container starts once, the migrations run once into a template database,
// and each caller gets its own database cloned from it — a genuinely separate
// database, so the isolation the concurrent add/remove-targets proofs depend on
// is unchanged. See pkg/pgtest for the full argument.
//
// It also removes the reason gooseMu existed: goose's package-level globals are
// now touched once, from here, before any test runs.
//
// The container starts lazily, so under -short — where setupDB skips — none is
// started.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `pkg/pgtest` §searchpath.
		SearchPath: "kacho_nlb,public",
		Name:       "nlb",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}
