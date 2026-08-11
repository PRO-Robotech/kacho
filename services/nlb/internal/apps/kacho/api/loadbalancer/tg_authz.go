// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// checkTargetGroupViewer authorizes the caller (`viewer` on
// `nlb_target_group:<tg>`) against a caller-supplied TargetGroup object
// (CWE-863 guard).
//
// Shared by every use-case that reads/mutates a TargetGroup referenced by a
// request field on an LB RPC (AttachTargetGroup, GetTargetStates, …): the
// per-RPC interceptor gates only the LoadBalancer object (its
// StaticExtractor resolves `network_load_balancer_id`), so without this
// explicit object-scoped Check a narrowly-scoped custom grant on the LB
// (e.g. `v_update`/`v_get` without project-editor) could read or wire in a
// TargetGroup the caller holds no authorization over — including one in a
// different project. The standard FGA cascade (project-editor ⇒ viewer on
// same-project TGs) already implies this Check for ordinary bindings, so it
// is a no-op there; it only bites narrowly-scoped custom grants and
// cross-project ids.
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
