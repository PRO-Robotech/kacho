// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_filter_whitelist_test.go — behaviour lock on the `filter` whitelist of
// every compute List repo (Instance / Disk / Image / Snapshot).
//
// The contract pinned here: this phase supports exactly `name="…"`
// (api-conventions.md §pagination/filter) and every OTHER field name is REJECTED
// with a sync InvalidArgument carrying the parser's fixed message. It is never
// silently dropped and never reaches SQL — silently ignoring an unsupported
// field is the worse failure: the caller gets rows back under a filter they
// believe was applied.
//
// Why widening the whitelist is not a one-token edit (evidence behind the
// reconciliation recorded in docs/architecture/07-known-divergences.md §12):
// pkg/filter.Parse emits FilterAST.Field VERBATIM as a SQL column identifier, so
//   - the acceptance spelling was camelCase (`instanceKind`, `placementGroupId`)
//     while the columns are snake_case → `instanceKind = $2` fails at the DB with
//     42703 (undefined column);
//   - `instances.instance_kind` is an INTEGER ordinal (migration 0016) while the
//     parser only ever produces a string value → 'CONTAINER' into an int4
//     comparison fails with 22P02.
//
// Both are 5xx, not "a filter that works".
//
// These cases never reach the pool: Parse fails before the first Query, so the
// nil pool is itself an assertion that no SQL is attempted on a rejected filter.
package repo

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// listFilterRejectCases — one row per field a caller might plausibly try. COMP-1
// acceptance F14 / COMP-1-36 used to promise `placementGroupId=` and
// `instanceKind=`; the doc was reconciled to the implementation and these rows
// are the executable half of that reconciliation.
var listFilterRejectCases = []struct {
	name    string
	filter  string
	wantMsg string
}{
	{
		name:    "instanceKind camelCase (former acceptance F14 spelling)",
		filter:  `instanceKind="CONTAINER"`,
		wantMsg: `Bad expression at column 1. Unknown field: "instanceKind"`,
	},
	{
		name:    "instance_kind column spelling",
		filter:  `instance_kind="CONTAINER"`,
		wantMsg: `Bad expression at column 1. Unknown field: "instance_kind"`,
	},
	{
		name:    "placementGroupId camelCase (former acceptance F14 spelling)",
		filter:  `placementGroupId="pg-1"`,
		wantMsg: `Bad expression at column 1. Unknown field: "placementGroupId"`,
	},
	{
		name:    "placement_group_id column spelling",
		filter:  `placement_group_id="pg-1"`,
		wantMsg: `Bad expression at column 1. Unknown field: "placement_group_id"`,
	},
	{
		name:    "projectId is scoped by its own request field, not by filter",
		filter:  `projectId="prj-acme"`,
		wantMsg: `Bad expression at column 1. Unknown field: "projectId"`,
	},
	{
		name:    "status is not filterable in this phase",
		filter:  `status="RUNNING"`,
		wantMsg: `Bad expression at column 1. Unknown field: "status"`,
	},
}

// assertUnknownFilterField pins the OBSERVABLE outcome — code AND message. Code
// alone would stay green if the rejection degraded into a generic "invalid
// filter" that hides which token the caller has to fix.
func assertUnknownFilterField(t *testing.T, err error, wantMsg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("filter with a non-whitelisted field was ACCEPTED (want InvalidArgument %q) — "+
			"an unsupported filter must be rejected, never silently ignored", wantMsg)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error is not a gRPC status: %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("code = %v, want %v (err=%v)", st.Code(), codes.InvalidArgument, err)
	}
	if st.Message() != wantMsg {
		t.Fatalf("message = %q, want %q", st.Message(), wantMsg)
	}
}

func TestInstanceRepoList_RejectsNonWhitelistedFilterField(t *testing.T) {
	r := NewInstanceRepo(nil)
	for _, tc := range listFilterRejectCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := r.List(context.Background(),
				ports.InstanceFilter{ProjectID: "prj-acme", Filter: tc.filter},
				ports.Pagination{PageSize: 10})
			assertUnknownFilterField(t, err, tc.wantMsg)
		})
	}
}

func TestDiskRepoList_RejectsNonWhitelistedFilterField(t *testing.T) {
	r := NewDiskRepo(nil)
	for _, tc := range listFilterRejectCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := r.List(context.Background(),
				ports.DiskFilter{ProjectID: "prj-acme", Filter: tc.filter},
				ports.Pagination{PageSize: 10})
			assertUnknownFilterField(t, err, tc.wantMsg)
		})
	}
}

func TestImageRepoList_RejectsNonWhitelistedFilterField(t *testing.T) {
	r := NewImageRepo(nil)
	for _, tc := range listFilterRejectCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := r.List(context.Background(),
				ports.ImageFilter{ProjectID: "prj-acme", Filter: tc.filter},
				ports.Pagination{PageSize: 10})
			assertUnknownFilterField(t, err, tc.wantMsg)
		})
	}
}

func TestSnapshotRepoList_RejectsNonWhitelistedFilterField(t *testing.T) {
	r := NewSnapshotRepo(nil)
	for _, tc := range listFilterRejectCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := r.List(context.Background(),
				ports.SnapshotFilter{ProjectID: "prj-acme", Filter: tc.filter},
				ports.Pagination{PageSize: 10})
			assertUnknownFilterField(t, err, tc.wantMsg)
		})
	}
}
