// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupTestDB used to start a container AND replay the whole migration chain on
// every call, and it is called 85 times here — so the package spent its whole
// budget on its harness rather than on its tests, and `go test` without an
// explicit -timeout was heading for the tool's own 600s default. The container now
// starts once, the chain runs once into a template, and each caller gets its own
// database cloned from it. See internal/pgtest for why a clone is the same
// isolation a separate container gave: the CAS / UNIQUE / EXCLUDE / SKIP LOCKED
// and IPAM-allocation proofs in this package still run their goroutines against
// the same real rows of the same real database.
//
// setupTestDBUpTo and the 0014 up/down roundtrip take an EMPTY database instead —
// they must start before the current head of the chain and walk forward
// themselves, so a pre-migrated template would be the wrong starting point.
//
// The container starts lazily on first use, so `-short` — under which every
// integration test here skips itself — still pays nothing.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `internal/pgtest` §WithSearchPath.
		SearchPath: "kacho_vpc,public",
		Name:       "vpc",
		User:       "vpc",
		Password:   "vpc",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}
