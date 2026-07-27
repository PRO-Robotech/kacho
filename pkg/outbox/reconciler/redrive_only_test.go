// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package reconciler_test

// redrive_only_test.go — a service that only wants poisoned rows re-driven must not
// be forced to invent the two domain adapters it will never call.
//
// RedrivePoisoned is pure SQL over the outbox table: it reads neither the resource
// enumerator nor the tuple registry. Demanding them anyway pushed services into one
// of two bad shapes — write stub adapters that exist solely to satisfy a constructor
// (dead code that reads as if a reconcile loop were running), or skip the backstop
// entirely. Both happened: storage and registry have no redrive at all, which is why
// a poisoned register intent there is permanent until someone edits the database by
// hand.

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
)

// nonNilPool is a pool handle that is non-nil but never dialled. These tests only
// exercise construction and the adapter guards, and both must decide before any
// query is issued — if a guard ever reached the database, this pool would make that
// obvious rather than silently passing.
func nonNilPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return &pgxpool.Pool{}
}

func TestNewRedriveOnly_NeedsNoDomainAdapters(t *testing.T) {
	t.Parallel()

	r, err := reconciler.NewRedriveOnly(nonNilPool(t), reconciler.Config{
		Table: "kacho_storage.fga_register_outbox",
	}, nil)
	require.NoError(t, err, "a redrive-only reconciler must construct without domain adapters")
	require.NotNil(t, r)
}

func TestNewRedriveOnly_StillRequiresPoolAndTable(t *testing.T) {
	t.Parallel()

	_, err := reconciler.NewRedriveOnly(nil, reconciler.Config{Table: "t"}, nil)
	require.Error(t, err, "a nil pool must still be refused")

	_, err = reconciler.NewRedriveOnly(nonNilPool(t), reconciler.Config{}, nil)
	require.Error(t, err, "an empty table must still be refused — the redrive is SQL over that table")
}

// TestRedriveOnly_RefusesTheStatePasses — the passes that DO need the adapters must
// say so instead of dereferencing nil. Constructing without adapters is a narrowing
// of capability, and the narrowing has to be enforced where it matters.
func TestRedriveOnly_RefusesTheStatePasses(t *testing.T) {
	t.Parallel()

	r, err := reconciler.NewRedriveOnly(nonNilPool(t), reconciler.Config{Table: "t"}, nil)
	require.NoError(t, err)

	_, err = r.BackfillFromState(t.Context())
	require.Error(t, err, "BackfillFromState needs the enumerator; it must refuse, not panic")

	_, err = r.GCOrphans(t.Context())
	require.Error(t, err, "GCOrphans needs the registry; it must refuse, not panic")
}
