// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupDrainerDB used to start a container AND replay the whole migration chain
// on every call; the container now starts once, the chain runs once into a
// template, and each caller gets its own database cloned from it. See
// pkg/pgtest for why a clone is the same isolation a separate container
// gave — including for the concurrent-replica drainer proofs, whose goroutines
// still contend on the same real rows of the same real database.
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
