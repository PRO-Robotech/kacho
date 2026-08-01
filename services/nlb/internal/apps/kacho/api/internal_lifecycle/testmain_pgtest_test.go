// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package internal_lifecycle

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupIntTestEnv started a container AND replayed the whole migration chain on
// each of its calls, so the package spent its budget on its harness rather than
// on the catchup / NOTIFY / stream-cap proofs it exists for.
//
// The container starts once, the migrations run once into a template database,
// and each caller gets its own database cloned from it. That is the isolation
// this file's header claims: a separate database has its own outbox rows, its own
// sequence_no and its own LISTEN/NOTIFY namespace, so no test can see another's
// events. See internal/pgtest for the full argument.
//
// The container starts lazily, so under -short — where setupIntTestEnv skips —
// none is started.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:    "nlb",
		Migrate: pgtest.Goose(migrations.FS),
	}))
}
