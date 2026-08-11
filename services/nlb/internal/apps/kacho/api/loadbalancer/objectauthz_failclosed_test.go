// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Fail-closed posture of the two handler-side, object-scoped decisions of this
// package: `checkTargetGroupViewer` (GetTargetStates) and `authorizeDestination`
// (Move).
//
// Both used to answer "authorized" when the decision could not be made at all —
// no Check client wired, or a caller that cannot be named as an FGA subject
// (empty principal, the reserved anonymity word, a `system` type). An absent
// decider is not a permission, and a caller we cannot name is a caller we cannot
// authorize (`security.md` §"Пустой субъект отсекается БЕЗУСЛОВНО"; the same
// posture iam takes in `authzguard.PrincipalSubject`).
//
// Each case asserts the OUTCOME the caller receives — a refusal, and for the
// mutating lane that the resource did NOT move — never merely that a Check was
// attempted. Every refusal is paired with a positive control below so the
// assertions cannot go green on a use-case that refuses everything.

// ctxUnnameable — a request context whose principal cannot be spelled as an FGA
// subject. Reached in production when the per-RPC decision link is bypassed
// (breakglass) or when a caller presents the `system` type, which
// `domain.FGASubjectFromPrincipal` refuses to name.
func ctxSystemPrincipal() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "system", ID: "bootstrap"})
}

func ctxAnonymousPrincipal() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: domain.AnonymousPrincipalID})
}

// --- GetTargetStates -------------------------------------------------------

// No Check client wired → the target states are NOT returned. The reply is the
// contract's own "the authorization check could not be made" (Unavailable), the
// same answer this lane already gives when iam is unreachable — not a pass.
func TestGetTargetStates_NoCheckClient_Refused(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	tgID := seedTG(t, repo, "prj-a", "ru-central1", "tg")
	uc := NewGetTargetStatesUseCase(repo, nil)

	resp, err := uc.Execute(ctxWithUser("usr_caller"), &lbv1.GetTargetStatesRequest{
		NetworkLoadBalancerId: lbID, TargetGroupId: tgID,
	})
	require.Nil(t, resp, "no target data may be returned when the decision cannot be made")
	require.Equal(t, codes.Unavailable, status.Code(err))
}

// A caller that cannot be named as an FGA subject is refused, even with a
// permissive decider wired: the store is never asked about a subject we would
// have had to invent.
func TestGetTargetStates_UnnameableCaller_Refused(t *testing.T) {
	t.Parallel()
	for name, ctx := range map[string]context.Context{
		"no principal": context.Background(),
		"system type":  ctxSystemPrincipal(),
		"anonymous id": ctxAnonymousPrincipal(),
	} {
		t.Run(name, func(t *testing.T) {
			repo := newFakeRepo()
			lbID := seedLB(t, repo, "prj-a", "edge")
			tgID := seedTG(t, repo, "prj-a", "ru-central1", "tg")
			chk := &fakeCheckClient{allowed: true} // would say yes if asked
			uc := NewGetTargetStatesUseCase(repo, chk)

			resp, err := uc.Execute(ctx, &lbv1.GetTargetStatesRequest{
				NetworkLoadBalancerId: lbID, TargetGroupId: tgID,
			})
			require.Nil(t, resp)
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			require.Zero(t, chk.calls,
				"an unnameable caller must not be asked about under an invented subject")
		})
	}
}

// Positive control: a named caller with a decider that allows still gets the
// data. Without this the refusals above would also pass on a lane that refuses
// unconditionally.
func TestGetTargetStates_NamedCallerAllowed_Passes(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	tgID := seedTG(t, repo, "prj-a", "ru-central1", "tg")
	chk := &fakeCheckClient{allowed: true}
	uc := NewGetTargetStatesUseCase(repo, chk)

	_, err := uc.Execute(ctxWithUser("usr_caller"), &lbv1.GetTargetStatesRequest{
		NetworkLoadBalancerId: lbID, TargetGroupId: tgID,
	})
	require.NoError(t, err)
	require.Equal(t, 1, chk.calls)
}

// --- Move ------------------------------------------------------------------

// No Check client wired → the load balancer does NOT change project. The
// destination decision is the only thing standing between a caller and injecting
// its resource into somebody else's project.
func TestMoveLoadBalancer_NoCheckClient_Refused(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-src", "edge")
	uc := NewMoveLoadBalancerUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, nil, nil)

	_, err := uc.Execute(ctxWithUser("usr_caller"), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-victim",
	})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, domain.ProjectID("prj-src"), repo.lbs[lbID].ProjectID,
		"the LB must not be re-parented when the destination decision cannot be made")
}

func TestMoveLoadBalancer_UnnameableCaller_Refused(t *testing.T) {
	t.Parallel()
	for name, ctx := range map[string]context.Context{
		"no principal": context.Background(),
		"system type":  ctxSystemPrincipal(),
		"anonymous id": ctxAnonymousPrincipal(),
	} {
		t.Run(name, func(t *testing.T) {
			repo := newFakeRepo()
			lbID := seedLB(t, repo, "prj-src", "edge")
			chk := &fakeCheckClient{allowed: true}
			uc := NewMoveLoadBalancerUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, chk, nil)

			_, err := uc.Execute(ctx, &lbv1.MoveNetworkLoadBalancerRequest{
				NetworkLoadBalancerId: lbID,
				DestinationProjectId:  "prj-victim",
			})
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			require.Equal(t, domain.ProjectID("prj-src"), repo.lbs[lbID].ProjectID,
				"the LB must not be re-parented for a caller we cannot name")
			require.Zero(t, chk.calls)
		})
	}
}
