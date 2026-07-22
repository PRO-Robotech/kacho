// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"fmt"
	"testing"

	"github.com/H-BF/corlib/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Delete OK (no attached LB, no targets).
func TestDelete_Happy(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "del-happy")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewDeleteTargetGroupUseCase(repo, opsRepo, nil)

	op, err := uc.Execute(context.Background(), &lbv1.DeleteTargetGroupRequest{
		TargetGroupId: string(tg.ID),
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error)

	events := repo.outboxEvents()
	require.Len(t, events, 1)
	assert.Equal(t, kachorepo.OutboxActionDeleted, events[0].Action)
}

// NLB-1-41: Delete fails when a listener references the TG (FK RESTRICT,
// friendly blocker-list enumerating the referencing listener ids).
func TestDelete_ReferencedByListener_NLB_1_41(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "del-ref")
	repo.seedTG(tg)
	repo.seedReferencingListener(string(tg.ID), "lst-7h3k9m2x4q8w1t0y")
	uc := NewDeleteTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

	_, err := uc.Execute(context.Background(), &lbv1.DeleteTargetGroupRequest{
		TargetGroupId: string(tg.ID),
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	// Exact contract text — enumerates blocking listener ids so the order need
	// not be guessed.
	require.Contains(t, status.Convert(err).Message(),
		"target group is referenced by listeners: [lst-7h3k9m2x4q8w1t0y]")
}

// Delete fails when targets exist.
func TestDelete_HasTargets(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "del-tgt")
	repo.seedTG(tg)
	tr := kachoTarget(string(tg.ID), domain.Target{
		InstanceID: option.MustNewOption(domain.InstanceID("epd0X1")),
		Weight:     100,
	})
	repo.seedTarget(string(tg.ID), &tr)
	uc := NewDeleteTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

	_, err := uc.Execute(context.Background(), &lbv1.DeleteTargetGroupRequest{
		TargetGroupId: string(tg.ID),
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(),
		"has 1 target(s); remove them first via RemoveTargets")
}

// concurrent AddTargets between precheck and DELETE → FK fallback
// FailedPrecondition (TOCTOU). Simulated via failOnDelete injected in fake.
func TestDelete_FKFallback_OnConcurrentAdd(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "del-fk")
	repo.seedTG(tg)
	repo.failOnDelete = fmt.Errorf("%w: TargetGroup %s has child targets (FK 23503)",
		kachorepo.ErrFailedPrecondition, tg.ID)
	opsRepo := newFakeOpsRepo()
	uc := NewDeleteTargetGroupUseCase(repo, opsRepo, nil)

	op, err := uc.Execute(context.Background(), &lbv1.DeleteTargetGroupRequest{
		TargetGroupId: string(tg.ID),
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error)
	require.Equal(t, int32(codes.FailedPrecondition), final.Error.Code)
}

func TestDelete_EmptyID(t *testing.T) {
	uc := NewDeleteTargetGroupUseCase(newFakeRepo(), newFakeOpsRepo(), nil)
	_, err := uc.Execute(context.Background(), &lbv1.DeleteTargetGroupRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDelete_NotFound(t *testing.T) {
	uc := NewDeleteTargetGroupUseCase(newFakeRepo(), newFakeOpsRepo(), nil)
	_, err := uc.Execute(context.Background(), &lbv1.DeleteTargetGroupRequest{
		TargetGroupId: "tgr-missing",
	})
	require.Equal(t, codes.NotFound, status.Code(err))
}
