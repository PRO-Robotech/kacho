// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/geo/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// newTestPool used to start a container AND replay the whole migration chain on
// every call, and it is called 55 times here — so the package spent its budget on
// its harness rather than on its tests. The container now starts once, the chain
// runs once into a template, and each caller gets its own database cloned from it.
// See internal/pgtest for why a clone is the same isolation a separate container
// gave.
//
// The template is the empty catalogue newTestPool always handed back: geo seeds no
// rows, every test creates its own.
//
// The container starts lazily on first use, so `-short` — under which newTestPool
// skips before asking for a database — still pays nothing.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `internal/pgtest` §searchpath.
		SearchPath: "kacho_geo,public",
		Name:       "geo",
		User:       "geo",
		Password:   "secret",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}
