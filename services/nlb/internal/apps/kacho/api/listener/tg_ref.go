// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"errors"
	"fmt"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Wiring a Listener to a TargetGroup — resolve + authorize the caller-supplied
// `targetGroupId` (NLB-1b MIGRATE made it the authoritative wiring path, replacing
// the pivot `attached_target_groups` whose Attach RPC carried these guards).
//
// The per-RPC interceptor scopes `ListenerService/Create` on the parent LOAD
// BALANCER and `/Update` on the LISTENER (permission_map StaticExtractor) — the
// `targetGroupId` in the request body is a caller-supplied object NOBODY checked.
// Two guards restore the invariant (CWE-639/863, security.md §«Object-scoped
// authz, не только method-level»):
//
//  1. **Owning-project scope** — the referenced TG must live in the SAME project as
//     the listener/LB. Without it a caller authorized on their own LB could point
//     it at a VICTIM project's TargetGroup: their load balancer would forward live
//     traffic to the victim's targets (instances/NICs/addresses) and the victim's
//     TG would become undeletable (FK RESTRICT). Cross-project references are also
//     rejected by the composite FK (migration 0023) — the DB is the atomic backstop
//     for the sync-precheck→async-worker window (data-integrity.md ban #10).
//  2. **Object-scoped viewer Check** on the TG itself (parity with
//     `loadbalancer.checkTargetGroupViewer`) — closes narrowly-scoped custom grants
//     that hold a listener verb on the LB without any grant on the TG. The ordinary
//     project-editor cascade implies `viewer` on same-project TGs, so this is a
//     no-op for ordinary bindings.
//
// Guard (1) runs BEFORE (2) and reports the SAME error the caller's own "TG does
// not resolve" branch reports (byte-identical, security.md #6): a distinct code or
// message — including a PermissionDenied from an authz probe — would confirm that
// the id names a real TargetGroup in someone else's project (existence-oracle).

// errTargetGroupUnresolved — sentinel: the wired `targetGroupId` does not resolve
// inside the listener's own project (no such row, or the row belongs to another
// project — deliberately indistinguishable). Each call-site maps it to ITS OWN
// unresolved-reference status so the hide-existence response stays byte-identical
// to that call-site's genuine miss.
var errTargetGroupUnresolved = errors.New("target group unresolved")

// lookupWiredTargetGroup resolves a caller-supplied `targetGroupId` scoped to
// projectID (the listener's / parent LB's owning project).
//
// Returns `errTargetGroupUnresolved` for both a missing row and a row owned by
// another project; repo failures are returned as-is for `mapDomainErr`.
func lookupWiredTargetGroup(
	ctx context.Context, tgs kachorepo.TargetGroupReaderIface, tgID, projectID string,
) (*kachorepo.TargetGroupRecord, error) {
	tg, err := tgs.Get(ctx, tgID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, errTargetGroupUnresolved
		}
		return nil, err
	}
	if string(tg.ProjectID) != projectID {
		return nil, errTargetGroupUnresolved
	}
	return tg, nil
}

// targetGroupMissErr — the canonical "referenced TargetGroup does not resolve"
// error of the Update lane: byte-identical to the repo's own not-found
// (`"TargetGroup <id> not found"`, api-conventions contract tone), so a
// cross-project reference is indistinguishable from a genuine miss.
func targetGroupMissErr(tgID string) error {
	return mapDomainErr(fmt.Errorf("%w: TargetGroup %s not found", domain.ErrNotFound, tgID))
}

// checkTargetGroupViewer authorizes the caller (`viewer` on
// `nlb_target_group:<tg>`) against the caller-supplied TargetGroup object.
// Mirrors `loadbalancer.checkTargetGroupViewer` (same relation, same message).
//
// Fail-closed posture (missing decider / unnameable caller → refusal, never a
// pass) lives in `shared.AuthorizeObject` — one rule for every object-scoped
// decision of this service.
func checkTargetGroupViewer(ctx context.Context, checkClient CheckClient, tgID string) error {
	return shared.AuthorizeObject(ctx, checkClient,
		domain.FGARelationViewer,
		domain.FGAObjectRef(domain.FGAObjectTypeTargetGroup, tgID),
		fmt.Sprintf("caller is not authorized (viewer) on target group %s", tgID))
}
