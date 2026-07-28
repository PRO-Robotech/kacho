// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_filter.go — per-object visibility filtering for read handlers.
//
// Each public List handler (Disk / Image / Snapshot / Instance) reads a PAGE from
// its own DB first (cursor, project-scoped) and only then asks kacho-iam which of
// the page's ids the calling subject may see — `AuthorizeService.BatchCheck`,
// batches ≤100, relation `viewer ∪ v_list`. `Image.GetLatestByFamily` runs the
// same direct question on the single resolved image id.
//
// The order is deliberate: the page comes from the DB FIRST, rights are checked on
// its ids. The reverse order ("enumerate every allowed id → narrow the SQL to it")
// hit the hard OpenFGA ListObjects cap (1000, no continuation token) and made a
// tenant's own resources invisible — see the `internal/authzfilter` package doc.
// Side effect of the new order: a page may come back PARTIAL (some rows filtered
// out) — normal for cursor pagination, `next_page_token` is derived from the last
// SCANNED row, so a full traversal still skips nothing.
//
// filterVisible encapsulates the per-RPC contract:
//   - filter == nil (FGA disabled config-gate / dev) → passthrough (no FGA call)
//   - subject == "" (system principal / no identity) → FAIL-CLOSED (empty).
//     This is NOT a bypass: a missing identity must never widen visibility to the
//     whole project. Cluster-admin / owner are allowed per-object by kacho-iam
//     (cluster-admin super-gate inside Check), not by a handler-side bypass.
//   - normal caller → BatchCheck → visible subset
//   - iam unreachable and fail-closed → Unavailable (returned to caller)
//   - iam unreachable and fail-open → unfiltered page + audit warn (filter handles)
//
// Caller-identity (subject) comes from the request Principal — the SAME source
// per-RPC Check uses. The former `x-kacho-subject*` gRPC-metadata source is gone:
// api-gateway never sends those headers, which yielded subject="" → bypass → the
// whole list leaking past list-authz (over-show leak).
package handler

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
)

// filterVisible returns the subset of items visible to the calling subject,
// preserving the repo's (cursor) order — re-ordering would break pagination.
//
// idOf extracts the FGA object id of an item. err != nil → the caller MUST
// propagate it, never fall back to the unfiltered page (fail-closed).
func filterVisible[T any](
	ctx context.Context,
	f authzfilter.Filter,
	resourceType, action string,
	items []T,
	idOf func(T) string,
) ([]T, error) {
	if f == nil || len(items) == 0 {
		return items, nil
	}
	subject := authzfilter.SubjectFromPrincipal(ctx)
	if subject == "" {
		// No caller-identity (system principal / anonymous): fail-closed. A missing
		// subject must never bypass the filter — that is exactly the over-show leak.
		// Nothing is visible, so the handler responds with an empty page (the
		// existence of other resources stays unknowable).
		return nil, nil
	}

	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, idOf(it))
	}
	visibleIDs, err := f.FilterVisibleIDs(ctx, subject, resourceType, action, ids)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]T, 0, len(visibleIDs))
	for _, it := range items {
		if _, ok := visible[idOf(it)]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}
