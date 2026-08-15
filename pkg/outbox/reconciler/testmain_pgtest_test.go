// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package reconciler_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
)

// TestMain hands this package one Postgres instead of one per test.
//
// Both register-outbox shapes this package exercises go into the one template:
// they live in different Postgres schemas (kacho_apps, kacho_svc) and each helper
// addresses its own by qualified name, so carrying the other one's empty table
// costs nothing and saves a second template. The re-drive and GC proofs contend
// on real rows of the caller's OWN database — which a clone still is — and the
// NOTIFY the kacho_svc trigger fires is delivered only inside it.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:    "reconciler",
		Migrate: pgtest.SQL(reconcilerSchema, registerOutboxSchema, tupleOutboxSchema),
	}))
}
