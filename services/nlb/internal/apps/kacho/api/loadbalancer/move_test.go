// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/option"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

func TestMove_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-src", "edge")
	opsRepo := newFakeOpsRepo()
	uc := NewMoveLoadBalancerUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, slog.Default())
	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-dst",
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error)
	require.Equal(t, domain.ProjectID("prj-dst"), repo.lbs[lbID].ProjectID)
	// project-rewrite = unregister(src) THEN register(dst) in the writer-tx.
	// The order is the contract, not the style: both intents are about the same
	// object, so they drain in emission order and the SURVIVING state must be
	// emitted LAST. The end-to-end consequence is locked in
	// move_mirror_projection_integration_test.go.
	require.Len(t, repo.fga, 2, "expected unregister(src)+register(dst) intents")
	require.Equal(t, domain.FGAEventUnregister, repo.fga[0].EventType,
		"the source scope comes down FIRST")
	require.Equal(t, "project:prj-src", repo.fga[0].Intent.Tuples[0].SubjectID)
	require.Equal(t, domain.FGAEventRegister, repo.fga[1].EventType,
		"the destination scope goes up LAST — it is the state that must survive")
	require.Equal(t, "project:prj-dst", repo.fga[1].Intent.Tuples[0].SubjectID)
}

// TestMove_RegisterDstCarriesLabelsAndParent — regression: the register(dst)
// FGA intent must mirror lbMirrorIntent semantics (Labels from the moved record +
// ParentProjectID=dst), NOT reuse the bare lbUnregisterIntent (which drops both).
// Previously Move emitted register(dst) with Labels=nil / ParentProjectID="",
// wiping the kacho-iam resource_mirror row feeding the γ label/parent selector →
// label-based grants and parent-scoped queries silently excluded the moved LB in
// the destination project until an unrelated Update repaired the mirror.
func TestMove_RegisterDstCarriesLabelsAndParent(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-src", "edge")
	repo.lbs[lbID].Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
	opsRepo := newFakeOpsRepo()
	uc := NewMoveLoadBalancerUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, slog.Default())
	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-dst",
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error)
	require.Len(t, repo.fga, 2)

	// unregister(src) stays bare (IAM uses only object+source_version on unregister).
	unreg := repo.fga[0]
	require.Equal(t, domain.FGAEventUnregister, unreg.EventType)
	require.Equal(t, "project:prj-src", unreg.Intent.Tuples[0].SubjectID)
	require.Empty(t, unreg.Intent.ParentProjectID)
	require.Nil(t, unreg.Intent.Labels)

	// register(dst) must carry the mirror fields for the destination.
	reg := repo.fga[1]
	require.Equal(t, domain.FGAEventRegister, reg.EventType)
	require.Equal(t, "prj-dst", reg.Intent.ParentProjectID,
		"register(dst) must set ParentProjectID=dst for the γ parent selector")
	require.Equal(t, map[string]string{"env": "prod"}, reg.Intent.Labels,
		"register(dst) must carry the moved LB's labels for the γ label selector")
}

func TestMove_SameProject(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	uc := NewMoveLoadBalancerUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, nil, nil)
	_, err := uc.Execute(context.Background(), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-a",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestMove_BlockedIfListenerWiredToTG — NLB CONTRACT replacement for the removed
// attach-pivot Move guard: a LB that has a listener wired to a target group
// (default_target_group_id set) cannot be moved cross-project — repoint first.
func TestMove_BlockedIfListenerWiredToTG(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	repo.lists[lbID] = []*kachorepo.ListenerRecord{
		{Listener: domain.Listener{
			ID:                   domain.ResourceID(ids.NewID(ids.PrefixListener)),
			LoadBalancerID:       domain.ResourceID(lbID),
			DefaultTargetGroupID: option.MustNewOption(domain.ResourceID("tgr-fake")),
		}},
	}
	uc := NewMoveLoadBalancerUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, nil, nil)
	_, err := uc.Execute(context.Background(), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-dst",
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "wired to a target group")
}

func TestMove_EmptyDst(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	uc := NewMoveLoadBalancerUseCase(repo, newFakeOpsRepo(), nil, nil, nil)
	_, err := uc.Execute(context.Background(), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestMove_NotFound(t *testing.T) {
	t.Parallel()
	uc := NewMoveLoadBalancerUseCase(newFakeRepo(), newFakeOpsRepo(), nil, nil, nil)
	_, err := uc.Execute(context.Background(), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: "nlb-x",
		DestinationProjectId:  "prj-dst",
	})
	require.Equal(t, codes.NotFound, status.Code(err))
}

// SECURITY (CWE-862/863): the caller must be authorized on
// the DESTINATION project (editor on project:<dst>). The per-RPC interceptor only
// checks the source LB; a caller with editor on the source but NO grant on the
// destination must be denied — otherwise it can inject its LB into a victim's
// project. With a check-client that denies, Move → PermissionDenied and the LB
// must NOT move.
func TestMove_DeniesUnauthorizedDestination(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-src", "edge")
	chk := &fakeCheckClient{allowed: false} // caller lacks editor on dst
	uc := NewMoveLoadBalancerUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, chk, slog.Default())

	_, err := uc.Execute(ctxWithUser("usr_attacker"), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-victim",
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Equal(t, domain.ProjectID("prj-src"), repo.lbs[lbID].ProjectID,
		"LB must not be re-parented when dst authz is denied")
	// Check was performed against the destination project with the editor relation.
	require.Equal(t, 1, chk.calls)
	require.Equal(t, "user:usr_attacker", chk.gotSubject)
	require.Equal(t, domain.FGARelationEditor, chk.gotRelation)
	require.Equal(t, "project:prj-victim", chk.gotObject)
}

// A caller authorized (editor) on the destination project passes the dst-authz
// gate and the Move proceeds.
func TestMove_AllowsAuthorizedDestination(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-src", "edge")
	chk := &fakeCheckClient{allowed: true}
	opsRepo := newFakeOpsRepo()
	uc := NewMoveLoadBalancerUseCase(repo, opsRepo, &fakeProjectClient{}, chk, slog.Default())

	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-dst",
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error)
	require.Equal(t, domain.ProjectID("prj-dst"), repo.lbs[lbID].ProjectID)
	require.Equal(t, 1, chk.calls)
}

// IAM unavailable during the dst-authz check → fail-closed Unavailable (never a
// silent allow).
func TestMove_DestCheckUnavailableFailsClosed(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-src", "edge")
	// CheckClient contract: transport-unavailable surfaces as domain.ErrUnavailable.
	chk := &fakeCheckClient{err: domain.ErrUnavailable}
	uc := NewMoveLoadBalancerUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, chk, slog.Default())

	_, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-dst",
	})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, domain.ProjectID("prj-src"), repo.lbs[lbID].ProjectID)
}

// Проба `TestLbMovedPayload_KeysOnTheWire` СНЯТА вместе со своим предметом
// (#1551).
//
// Она утверждала имена ключей `old_project_id` / `new_project_id` у нагрузки
// переезда — то есть у МИНИМАЛЬНОГО СНИМКА, которого больше нет: переезд кладёт
// полное состояние тем же строителем, что и остальные шесть точек вида.
//
// `old_project_id` при этом СНЯТ осознанно, а не потерян: читателя у него не было
// ни одного, а состояние события несёт проект ЦЕЛЕВОЙ — прежний у подписчика уже
// есть, это то, что лежит в его собственном состоянии до применения события.
// Разбор — в соседнем `payloads.go` этого пакета.
//
// Снято вместе с предметом, а не ослаблено: проба, у которой исчез вход, не
// краснеет и не зеленеет — она молчит, продолжая считаться исполненной.
