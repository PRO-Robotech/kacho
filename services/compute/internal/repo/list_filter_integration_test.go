// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_filter_integration_test.go — integration guard (testcontainers Postgres)
// for the PAGINATED per-object filtered List contract.
//
// Visibility is no longer expressed in SQL: the repo returns a project-scoped
// cursor page and the handler then asks kaname about THAT page's ids
// (AuthorizeService.BatchCheck). The removed shape — "enumerate every allowed id,
// then narrow the SQL with `WHERE id = ANY(...)`" — ran into a hard server-side cap
// on the enumeration (1000 ids, no continuation token) and silently erased a
// tenant's own resources; see the `internal/authzfilter` package doc.
//
// That shape is now not merely rejected, it is UNSPEAKABLE: the enumerating RPC
// was retired with the external relation engine, and the narrowing port carries
// the per-page question and nothing else. This test therefore pins what the
// page-shaped filter must do, not which of two shapes was chosen.
//
// What must hold under the new shape, against real rows and a real cursor:
//   - a full traversal covers EXACTLY the accessible set — no holes. Individual
//     pages may come back PARTIAL (some rows filtered out); that is expected for
//     cursor pagination, because next_page_token is derived from the last SCANNED
//     row, not the last returned one.
//   - no inaccessible row ever appears on any page (no-leak).
//   - no row is returned twice and the traversal terminates.
//
// All tests are gated by `if testing.Short()` (like the rest of integration_test.go).
package repo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// TestIntegration_InstanceHandler_PaginatedFilteredList — page-then-check traversal
// over real Postgres. Seeds 150 instances in a project, grants access to a SCATTERED
// 60 of them, then pages with page_size=25.
//
// The guard was written over compute's own Disk resource, which is retired
// (kacho-storage owns block storage). The contract under test is the shared
// page-then-check List, not the resource, so it moved to Instance.
//
// RED-safety: this fails if the filter were applied to anything other than the
// page actually read (e.g. re-introducing an enumeration that truncates, which
// would drop accessible ids out of the union), if an ungranted row leaked onto a
// page, or if the cursor stalled / duplicated once pages became partial.
func TestIntegration_InstanceHandler_PaginatedFilteredList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	stub := &batchCheckStub{allowBySubject: map[string][]string{}}
	h, r, pool := newInstanceHandlerOnRealRepo(t, stub)
	defer pool.Close()

	const total = 150
	const granted = 60
	names := make([]string, 0, total)
	for i := 0; i < total; i++ {
		names = append(names, fmt.Sprintf("i%03d", i))
	}
	allIDs := seedInstances(t, r, "proj-a", names...)
	require.Len(t, allIDs, total)

	// Scatter the accessible subset across the whole ordered range, so the
	// traversal genuinely exercises partial pages (not "the first N rows happen
	// to be accessible").
	accessible := make([]string, 0, granted)
	accessibleSet := map[string]bool{}
	for i := 0; i < total && len(accessible) < granted; i += 2 {
		accessible = append(accessible, allIDs[i])
		accessibleSet[allIDs[i]] = true
	}
	require.Len(t, accessible, granted)
	stub.allowBySubject["user:usr_alice"] = accessible

	ctx := operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr_alice"})

	const pageSize = 25
	seen := map[string]bool{}
	var pages int
	token := ""
	for {
		resp, err := h.List(ctx, &computev1.ListInstancesRequest{
			ProjectId: "proj-a", PageSize: pageSize, PageToken: token,
		})
		require.NoError(t, err)
		pages++
		require.LessOrEqual(t, len(resp.Instances), pageSize, "page must not exceed page_size")
		for _, d := range resp.Instances {
			require.True(t, accessibleSet[d.Id], "inaccessible instance leaked onto a page: %s", d.Id)
			require.False(t, seen[d.Id], "duplicate across pages: %s", d.Id)
			seen[d.Id] = true
		}
		if resp.NextPageToken == "" {
			break
		}
		token = resp.NextPageToken
		require.LessOrEqual(t, pages, 20, "pagination did not terminate")
	}

	require.Equal(t, granted, len(seen), "a full traversal must cover exactly the accessible set (no holes)")
	require.Equal(t, total/pageSize, pages, "150 rows / page_size 25 → 6 scanned pages (each partially filtered)")
}
