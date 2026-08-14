// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Fail-closed posture of `checkTargetGroupViewer` on the two listener wiring
// lanes (Create / Update).
//
// It used to answer "authorized" when the decision could not be made: no Check
// client wired, or a caller that cannot be named as an FGA subject (empty
// principal, the reserved anonymity word, a `system` type). An absent decider is
// not a permission (`security.md` §"Пустой субъект отсекается БЕЗУСЛОВНО").
//
// Each case asserts the OUTCOME — the refusal AND that no listener row was
// created / no reference repointed. Refusals are paired with positive controls
// so they cannot go green on a lane that refuses unconditionally.

const failClosedTGID = domain.ResourceID("tgr-failclosed000001")

func ctxSystemPrincipal() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "system", ID: "bootstrap"})
}

func ctxAnonymousPrincipal() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: domain.AnonymousPrincipalID})
}

// unnameableContexts — the ctx shapes `domain.FGASubjectFromPrincipal` refuses
// to spell as an FGA subject.
func unnameableContexts() map[string]context.Context {
	return map[string]context.Context{
		"no principal": context.Background(),
		"system type":  ctxSystemPrincipal(),
		"anonymous id": ctxAnonymousPrincipal(),
	}
}

func createTGWiringReq(lbID string, port int64) *lbv1.CreateListenerRequest {
	return &lbv1.CreateListenerRequest{
		LoadBalancerId: lbID,
		Name:           "tcp-wire",
		Protocol:       lbv1.Listener_TCP,
		Port:           port,
		TargetGroupId:  string(failClosedTGID),
	}
}

// --- Create ----------------------------------------------------------------

func TestCreateListener_NoCheckClient_Refused(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	// Same project as the LB: the cross-project lane is already closed, so the
	// object-scoped decision is the only thing left to make.
	seedListenerTG(repo, failClosedTGID, lb.ProjectID, lb.RegionID)
	uc := newCreateUCNoDecider(repo, ops) // no decider wired

	_, err := uc.Run(contextWithSubject("user:test-actor"), createTGWiringReq(string(lb.ID), 443))
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Empty(t, listenerByLB(repo, string(lb.ID)),
		"no listener may be created when the wiring decision cannot be made")
}

func TestCreateListener_UnnameableCaller_Refused(t *testing.T) {
	t.Parallel()
	for name, ctx := range unnameableContexts() {
		t.Run(name, func(t *testing.T) {
			repo := newFakeRepo()
			ops := newFakeOpsRepo()
			lb := seedParentLB(t, repo)
			seedListenerTG(repo, failClosedTGID, lb.ProjectID, lb.RegionID)
			chk := &fakeTGCheckClient{allowed: true} // would say yes if asked
			uc := newCreateUC(repo, ops).WithCheckClient(chk)

			_, err := uc.Run(ctx, createTGWiringReq(string(lb.ID), 443))
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			require.Empty(t, listenerByLB(repo, string(lb.ID)),
				"no listener may be created for a caller we cannot name")
			require.Zero(t, chk.calls,
				"an unnameable caller must not be asked about under an invented subject")
		})
	}
}

// Positive control for both Create refusals.
func TestCreateListener_NamedCallerAllowed_Wires(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	seedListenerTG(repo, failClosedTGID, lb.ProjectID, lb.RegionID)
	chk := &fakeTGCheckClient{allowed: true}
	uc := newCreateUC(repo, ops).WithCheckClient(chk)

	_, err := uc.Run(contextWithSubject("user:test-actor"), createTGWiringReq(string(lb.ID), 443))
	require.NoError(t, err)
	require.Equal(t, 1, chk.calls)
}

// --- Update ----------------------------------------------------------------

func TestUpdateListener_NoCheckClient_Refused(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuiteNoDecider(t)
	seedListenerTG(suite.repo, failClosedTGID, suite.listener.ProjectID, suite.listener.RegionID)

	_, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:    string(suite.listener.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"target_group_id"}},
		TargetGroupId: string(failClosedTGID),
	})
	require.Equal(t, codes.Unavailable, status.Code(err))
	_, wired := suite.getListener(string(suite.listener.ID)).DefaultTargetGroupID.Maybe()
	require.False(t, wired,
		"the reference must not be repointed when the decision cannot be made")
}

func TestUpdateListener_UnnameableCaller_Refused(t *testing.T) {
	t.Parallel()
	for name, ctx := range unnameableContexts() {
		t.Run(name, func(t *testing.T) {
			suite := newUpdateSuite(t)
			seedListenerTG(suite.repo, failClosedTGID, suite.listener.ProjectID, suite.listener.RegionID)
			chk := &fakeTGCheckClient{allowed: true}
			suite.uc.WithCheckClient(chk)

			_, err := suite.uc.Run(ctx, &lbv1.UpdateListenerRequest{
				ListenerId:    string(suite.listener.ID),
				UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"target_group_id"}},
				TargetGroupId: string(failClosedTGID),
			})
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			_, wired := suite.getListener(string(suite.listener.ID)).DefaultTargetGroupID.Maybe()
			require.False(t, wired,
				"the reference must not be repointed for a caller we cannot name")
			require.Zero(t, chk.calls)
		})
	}
}

// Positive control for both Update refusals.
func TestUpdateListener_NamedCallerAllowed_Repoints(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	seedListenerTG(suite.repo, failClosedTGID, suite.listener.ProjectID, suite.listener.RegionID)
	chk := &fakeTGCheckClient{allowed: true}
	suite.uc.WithCheckClient(chk)

	_, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:    string(suite.listener.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"target_group_id"}},
		TargetGroupId: string(failClosedTGID),
	})
	require.NoError(t, err)
	require.Equal(t, 1, chk.calls)
}
