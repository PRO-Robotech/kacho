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
	"context"
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
		Table:           "kacho_storage.fga_register_outbox",
		PartitionColumn: reconciler.RegisterOutboxPartition,
	}, nil)
	require.NoError(t, err, "a redrive-only reconciler must construct without domain adapters")
	require.NotNil(t, r)
}

func TestNewRedriveOnly_StillRequiresPoolAndTable(t *testing.T) {
	t.Parallel()

	_, err := reconciler.NewRedriveOnly(nil, reconciler.Config{
		Table: "t", PartitionColumn: reconciler.RegisterOutboxPartition,
	}, nil)
	require.Error(t, err, "a nil pool must still be refused")

	_, err = reconciler.NewRedriveOnly(nonNilPool(t), reconciler.Config{
		PartitionColumn: reconciler.RegisterOutboxPartition,
	}, nil)
	require.Error(t, err, "an empty table must still be refused — the redrive is SQL over that table")
}

// TestNewRedriveOnly_RefusesAnUnsetPartitionKey — the ordering key has no default
// ON PURPOSE, and this is what keeps it that way.
//
// The revival's guard is "do not raise a poisoned intent past a DELIVERED successor
// of the same partition". With no key there is no partition, so every poisoned row
// looks unopposed and the pass revives all of them — including a grant whose own
// revocation already landed. The queue would be repaired straight back into the
// over-grant the ordering exists to prevent, and nothing would look wrong: the
// backstop is present, it runs, it reports rows revived.
//
// So the value that means "no ordering" must be unconstructible, not merely
// discouraged. The lawful twin below is the other half — a real key still builds,
// otherwise this would pass on a constructor that refuses everything.
func TestNewRedriveOnly_RefusesAnUnsetPartitionKey(t *testing.T) {
	t.Parallel()

	_, err := reconciler.NewRedriveOnly(nonNilPool(t), reconciler.Config{Table: "t"}, nil)
	require.Error(t, err, "an unset partition key must refuse to build — it would read as \"no ordering\"")
	require.Contains(t, err.Error(), "PartitionColumn",
		"the refusal must name the knob, or the operator cannot act on it: %v", err)

	// The name is interpolated into the redrive statement. It is checked at
	// construction so a bad value cannot become a syntax error minutes later on the
	// first pass — and cannot carry SQL at all.
	for _, bad := range []string{"resource id", "resource_id; DROP TABLE x", "1col", `"quoted"`} {
		_, err := reconciler.NewRedriveOnly(nonNilPool(t), reconciler.Config{
			Table: "t", PartitionColumn: bad,
		}, nil)
		require.Errorf(t, err, "a non-identifier partition key must be refused: %q", bad)
	}

	// Lawful twin: both keys in the platform build.
	for _, good := range []string{reconciler.RegisterOutboxPartition, "tuple_key"} {
		r, err := reconciler.NewRedriveOnly(nonNilPool(t), reconciler.Config{
			Table: "t", PartitionColumn: good,
		}, nil)
		require.NoErrorf(t, err, "a real partition key must build: %q", good)
		require.NotNil(t, r)
	}
}

// TestNew_RefusesANonRegisterPartitionKey — a FULL reconciler reasons about
// resource_id directly (intendedRegistered, lockResource, the synthesised
// project-hierarchy intent), so it is a register-outbox reconciler by construction.
// Handing it the tuple key would build a Reconciler whose redrive guards one column
// while its backfill and GC read another — two halves of one pass, on two keys.
func TestNew_RefusesANonRegisterPartitionKey(t *testing.T) {
	t.Parallel()

	_, err := reconciler.New(nonNilPool(t), reconciler.Config{
		Table: "t", PartitionColumn: "tuple_key",
	}, reconciler.Adapters{Enumerator: nilAdapters{}, Registry: nilAdapters{}}, nil)
	require.Error(t, err, "the state passes are register-outbox specific; say so at construction")
	require.Contains(t, err.Error(), "NewRedriveOnly",
		"the refusal must name the constructor that DOES fit: %v", err)
}

// TestRedriveOnly_RefusesTheStatePasses — the passes that DO need the adapters must
// say so instead of dereferencing nil. Constructing without adapters is a narrowing
// of capability, and the narrowing has to be enforced where it matters.
func TestRedriveOnly_RefusesTheStatePasses(t *testing.T) {
	t.Parallel()

	r, err := reconciler.NewRedriveOnly(nonNilPool(t), reconciler.Config{
		Table: "t", PartitionColumn: reconciler.RegisterOutboxPartition,
	}, nil)
	require.NoError(t, err)

	_, err = r.BackfillFromState(t.Context())
	require.Error(t, err, "BackfillFromState needs the enumerator; it must refuse, not panic")

	_, err = r.GCOrphans(t.Context())
	require.Error(t, err, "GCOrphans needs the registry; it must refuse, not panic")
}

// nilAdapters satisfies both domain-adapter interfaces without a database. The
// constructor guard under test decides before any pass runs, so these methods are
// never called — and if one ever were, the nil pool above would make it loud.
type nilAdapters struct{}

func (nilAdapters) ListResources(context.Context) ([]reconciler.ResourceRow, error) {
	return nil, nil
}
func (nilAdapters) ResourceExists(context.Context, string, string) (bool, error) { return false, nil }
func (nilAdapters) ListRegistered(context.Context) ([]reconciler.RegisteredTuple, error) {
	return nil, nil
}
