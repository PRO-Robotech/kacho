// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Cross-project TargetGroup wiring guard (BOLA / CWE-639).
//
// `ListenerService/Create` and `/Update` are scoped by the per-RPC interceptor on
// the LOAD BALANCER / LISTENER object only (permission_map StaticExtractor reads
// `load_balancer_id` / `listener_id`) — the caller-supplied `targetGroupId` is an
// UNCHECKED object. Without an in-service guard a caller authorized on their own
// LB could wire a VICTIM project's TargetGroup: their LB would then forward
// traffic to the victim's targets, and the differing error tone would confirm the
// foreign TG's existence (existence-oracle).
//
// The guard must be indistinguishable from a plain miss (security.md #6:
// hide-existence must be byte-identical to a genuine backend miss).

const (
	crossProjectVictimProject = domain.ProjectID("prj02VICTIM000000001")
	crossProjectVictimTGID    = domain.ResourceID("tgr-victimtg00000001")
	crossProjectAbsentTGID    = "tgr-absenttg00000001"
)

// fakeTGCheckClient — in-memory CheckClient двойник (parity with the loadbalancer
// package fake): records the last Check and replies with a canned verdict.
type fakeTGCheckClient struct {
	allowed     bool
	err         error
	calls       int
	gotSubject  string
	gotRelation string
	gotObject   string
}

func (f *fakeTGCheckClient) Check(_ context.Context, subject, relation, object string) (bool, error) {
	f.calls++
	f.gotSubject, f.gotRelation, f.gotObject = subject, relation, object
	if f.err != nil {
		return false, f.err
	}
	return f.allowed, nil
}

var _ CheckClient = (*fakeTGCheckClient)(nil)

// TestCreateListener_CrossProjectTG_HiddenAsMissing — Listener.Create wiring a
// TargetGroup owned by ANOTHER project is rejected, and the rejection is
// byte-identical to the plain "TG does not exist" rejection (no existence-oracle).
// No listener row is created.
func TestCreateListener_CrossProjectTG_HiddenAsMissing(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	// Victim TG: region-coherent with the LB (so ONLY the project differs) but
	// owned by another project.
	seedListenerTG(repo, crossProjectVictimTGID, crossProjectVictimProject, lb.RegionID)
	uc := newCreateUC(repo, ops)

	_, foreignErr := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  string(crossProjectVictimTGID),
	})
	require.Error(t, foreignErr, "cross-project TargetGroup must not be wireable")

	_, absentErr := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-444",
		Protocol:       lbv1.Listener_TCP,
		Port:           444,
		TargetGroupId:  crossProjectAbsentTGID,
	})
	require.Error(t, absentErr)

	require.Equal(t, status.Code(absentErr), status.Code(foreignErr),
		"cross-project rejection must carry the same code as a genuine miss")
	require.Equal(t, status.Convert(absentErr).Message(), status.Convert(foreignErr).Message(),
		"hide-existence: cross-project message must be byte-identical to the miss message")
	require.NotContains(t, status.Convert(foreignErr).Message(), string(crossProjectVictimProject),
		"victim project id must never leak into the error")

	require.Empty(t, listenerByLB(repo, string(lb.ID)),
		"no listener row may be created when the wired TG is cross-project")
}

// Снятый вход `default_target_group_id` отвергается РАНЬШЕ, чем что-либо
// спрашивается о названной группе, — и потому не заводит нового оракула
// существования (задача продукта #1596).
//
// Проба переориентирована, а не ослаблена: её прежний предикат — «межпроектная
// группа не привязывается и через легаси-поле» — потерял СРЕДСТВО (второго
// входного поля больше нет), но не смысл. Сам предикат по-прежнему держит
// TestCreateListener_CrossProjectTG_HiddenAsMissing на живом входе; здесь
// утверждается то, что стало новым: отказ наступает до резолва, поэтому о чужой
// группе не сообщается ничего.
func TestCreateListener_CrossProjectTG_LegacyField_Rejected(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	seedListenerTG(repo, crossProjectVictimTGID, crossProjectVictimProject, lb.RegionID)
	uc := newCreateUC(repo, ops)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId:       string(lb.ID),
		Name:                 "tcp-443",
		Protocol:             lbv1.Listener_TCP,
		Port:                 443,
		DefaultTargetGroupId: string(crossProjectVictimTGID),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"снятый вход обязан отвергаться явно, а не молча отбрасываться")
	msg := status.Convert(err).Message()
	require.Contains(t, msg, "target_group_id", "отказ обязан назвать замену")
	require.NotContains(t, msg, string(crossProjectVictimProject),
		"проект чужой группы не может утечь в отказ")
	require.NotContains(t, msg, string(crossProjectVictimTGID),
		"отказ наступает ДО резолва, поэтому id чужой группы в нём появиться не может")
	require.Empty(t, listenerByLB(repo, string(lb.ID)))
}

// TestUpdateListener_CrossProjectTG_HiddenAsMissing — repointing an existing
// listener at a foreign-project TargetGroup is rejected byte-identically to a
// plain miss; the persisted reference stays untouched.
func TestUpdateListener_CrossProjectTG_HiddenAsMissing(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	suite.repo.seedTG(&kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID:        crossProjectVictimTGID,
			ProjectID: crossProjectVictimProject,
			RegionID:  suite.listener.RegionID,
			Name:      domain.LbName("victim-tg"),
			Status:    domain.TargetGroupStatusActive,
		},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})

	_, foreignErr := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId:    string(suite.listener.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"target_group_id"}},
		TargetGroupId: string(crossProjectVictimTGID),
	})
	require.Error(t, foreignErr, "cross-project TargetGroup must not be wireable via Update")

	_, absentErr := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId:    string(suite.listener.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"target_group_id"}},
		TargetGroupId: crossProjectAbsentTGID,
	})
	require.Error(t, absentErr)

	// The genuine-miss contract of this lane is unchanged (NOT_FOUND + repo tone) —
	// the cross-project case merely joins it.
	require.Equal(t, codes.NotFound, status.Code(absentErr))
	require.Equal(t, "TargetGroup "+crossProjectAbsentTGID+" not found",
		status.Convert(absentErr).Message())
	require.Equal(t, status.Code(absentErr), status.Code(foreignErr),
		"cross-project rejection must carry the same code as a genuine miss")
	// Byte-identity modulo the echoed id: substituting the requested id into the
	// genuine-miss message must reproduce the cross-project message verbatim.
	require.Equal(t,
		strings.Replace(status.Convert(absentErr).Message(), crossProjectAbsentTGID, string(crossProjectVictimTGID), 1),
		status.Convert(foreignErr).Message(),
		"hide-existence: cross-project message must be byte-identical to the miss message")
	require.NotContains(t, status.Convert(foreignErr).Message(), string(crossProjectVictimProject),
		"victim project id must never leak into the error")

	got := suite.getListener(string(suite.listener.ID))
	_, wired := got.DefaultTargetGroupID.Maybe()
	require.False(t, wired, "listener must keep its (empty) TG reference after a rejected repoint")
}

// TestCreateListener_TGViewerCheck_Denied — object-scoped authz on the
// caller-supplied TargetGroup (security.md #3): the per-RPC interceptor gates only
// the parent LB, so a narrowly-scoped grant without `viewer` on the TG must not be
// able to wire it.
func TestCreateListener_TGViewerCheck_Denied(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	seedListenerTG(repo, "tgr-samproject000001", lb.ProjectID, lb.RegionID)
	check := &fakeTGCheckClient{allowed: false}
	uc := newCreateUC(repo, ops).WithCheckClient(check)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  "tgr-samproject000001",
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Equal(t, "caller is not authorized (viewer) on target group tgr-samproject000001",
		status.Convert(err).Message())
	require.Equal(t, 1, check.calls)
	require.Equal(t, domain.FGARelationViewer, check.gotRelation)
	require.Equal(t, domain.FGAObjectRef(domain.FGAObjectTypeTargetGroup, "tgr-samproject000001"), check.gotObject)
	require.Empty(t, listenerByLB(repo, string(lb.ID)))
}

// Authz-Check peer failure is fail-closed (never allow): iam unavailable →
// UNAVAILABLE, no listener row.
func TestCreateListener_TGViewerCheck_FailClosed(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	seedListenerTG(repo, "tgr-samproject000001", lb.ProjectID, lb.RegionID)
	check := &fakeTGCheckClient{err: domain.ErrUnavailable}
	uc := newCreateUC(repo, ops).WithCheckClient(check)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  "tgr-samproject000001",
	})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Empty(t, listenerByLB(repo, string(lb.ID)))
}

// no-path (no hierarchy tuple for the object) is a deny, not an allow.
func TestCreateListener_TGViewerCheck_NoPathDenied(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	seedListenerTG(repo, "tgr-samproject000001", lb.ProjectID, lb.RegionID)
	check := &fakeTGCheckClient{err: authz.ErrNoPath}
	uc := newCreateUC(repo, ops).WithCheckClient(check)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  "tgr-samproject000001",
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Empty(t, listenerByLB(repo, string(lb.ID)))
}

// Happy path with the Check wired: an authorized caller wiring a same-project,
// region-coherent TG still succeeds (the guard must not break legitimate wiring).
func TestCreateListener_TGViewerCheck_AllowedHappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	seedListenerTG(repo, "tgr-samproject000001", lb.ProjectID, lb.RegionID)
	check := &fakeTGCheckClient{allowed: true}
	uc := newCreateUC(repo, ops).WithCheckClient(check)

	op, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  "tgr-samproject000001",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID, testTimeout).Error)

	got := listenerByLB(repo, string(lb.ID))
	require.Len(t, got, 1)
	v, ok := got[0].DefaultTargetGroupID.Maybe()
	require.True(t, ok)
	require.Equal(t, domain.ResourceID("tgr-samproject000001"), v)
}

// The object-scoped Check must run AFTER the same-project resolve, so that a
// foreign-project TG is still hidden as a miss (a PermissionDenied would confirm
// the victim TG exists). Regression-lock for the ordering.
func TestCreateListener_CrossProjectTG_NotProbedByAuthz(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	seedListenerTG(repo, crossProjectVictimTGID, crossProjectVictimProject, lb.RegionID)
	check := &fakeTGCheckClient{allowed: true} // would allow — must never be consulted
	uc := newCreateUC(repo, ops).WithCheckClient(check)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  string(crossProjectVictimTGID),
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Zero(t, check.calls, "a cross-project TG must be hidden as a miss before any authz probe")
	require.Empty(t, listenerByLB(repo, string(lb.ID)))
}

// TestUpdateListener_TGViewerCheck_Denied — the repoint lane carries the same
// object-scoped authz guard: the interceptor scopes Update on the LISTENER, so the
// TG named in the body is a caller-supplied object that must be authorized too.
func TestUpdateListener_TGViewerCheck_Denied(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	tgID := domain.ResourceID("tgr-repoint000000001")
	suite.repo.seedTG(&kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID:        tgID,
			ProjectID: suite.listener.ProjectID,
			RegionID:  suite.listener.RegionID,
			Name:      domain.LbName("repoint-tg"),
			Status:    domain.TargetGroupStatusActive,
		},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	check := &fakeTGCheckClient{allowed: false}
	suite.uc.WithCheckClient(check)

	_, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:    string(suite.listener.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"target_group_id"}},
		TargetGroupId: string(tgID),
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Equal(t, "caller is not authorized (viewer) on target group "+string(tgID),
		status.Convert(err).Message())

	got := suite.getListener(string(suite.listener.ID))
	_, wired := got.DefaultTargetGroupID.Maybe()
	require.False(t, wired, "an unauthorized repoint must not persist")
}
