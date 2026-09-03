// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupDB and setupCappedPoolDB each started a container AND replayed the whole
// migration chain on every call, so the package spent its budget on its harness
// rather than on its tests.
//
// The container starts once, the migrations run once into a template database,
// and each caller gets its own database cloned from it. The clone is a genuinely
// separate database — own rows, own sequences, own advisory-lock space — so the
// VIP-pool and CAS proofs are unchanged, and the capped-pool test still owns the
// only pool pointed at its own database. See internal/pgtest for the full
// argument.
//
// It also removes the reason gooseMu existed: goose's package-level globals are
// now touched once, from here, before any test runs.
//
// The container starts lazily, so under -short — where both helpers skip — none
// is started.
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
