// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupTestDB used to start a container AND replay the whole migration chain on
// every call, and it is called 52 times here — so the package spent its budget on
// its harness rather than on its tests. The container now starts once, the chain
// runs once into a template, and each caller gets its own database cloned from it.
// See pkg/pgtest for why a clone is the same isolation a separate container
// gave: the CAS / UNIQUE / lost-update / race proofs in this package still run
// their goroutines against the same real rows of the same real database.
//
// The fixture machine types stay in setupTestDB rather than moving into the
// template, because they are the caller's fixture, not the schema.
//
// The container starts lazily on first use, so `-short` — under which every
// integration test here skips itself — still pays nothing.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:     "compute",
		User:     "compute",
		Password: "compute",
		Migrate:  pgtest.Goose(migrations.FS),
	}))
}
