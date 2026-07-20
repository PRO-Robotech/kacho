// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// NLB-1b EXPAND (additive): Create with the new target_group_id wires the listener's
// TG reference (maps to the same DefaultTargetGroupID as the legacy field).
func TestCreateListener_NLB_1b_TargetGroupId(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	uc := newCreateUC(repo, ops)

	op, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetPort:     8080,
		TargetGroupId:  "tgr-wired00000000001",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID, testTimeout).Error)

	got := listenerByLB(repo, string(lb.ID))
	require.Len(t, got, 1)
	v, ok := got[0].DefaultTargetGroupID.Maybe()
	require.True(t, ok)
	require.Equal(t, domain.ResourceID("tgr-wired00000000001"), v)
}

// target_group_id takes precedence over the legacy default_target_group_id when both
// are supplied (both coexist in EXPAND).
func TestCreateListener_NLB_1b_TargetGroupId_Precedence(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	uc := newCreateUC(repo, ops)

	op, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId:       string(lb.ID),
		Name:                 "tcp-443",
		Protocol:             lbv1.Listener_TCP,
		Port:                 443,
		TargetPort:           8080,
		TargetGroupId:        "tgr-new0000000000001",
		DefaultTargetGroupId: "tgr-legacy0000000001",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID, testTimeout).Error)

	got := listenerByLB(repo, string(lb.ID))
	require.Len(t, got, 1)
	v, _ := got[0].DefaultTargetGroupID.Maybe()
	require.Equal(t, domain.ResourceID("tgr-new0000000000001"), v)
}

// NLB-1-22 (F4, EXPAND): target_group_id is LIVE-mutable — repoint the listener to
// another region-coherent TG via Update.
func TestUpdateListener_NLB_1_22_RepointTargetGroupId(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	tgID := domain.ResourceID(ids.NewID(ids.PrefixTargetGroup))
	suite.repo.seedTG(&kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID:        tgID,
			ProjectID: suite.listener.ProjectID,
			RegionID:  suite.listener.RegionID,
			Name:      domain.LbName("repoint-tg"),
			Status:    domain.TargetGroupStatusActive,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})

	op, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId:    string(suite.listener.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"target_group_id"}},
		TargetGroupId: string(tgID),
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, suite.ops, op.ID, time.Second).Error)

	got := suite.getListener(string(suite.listener.ID))
	v, ok := got.DefaultTargetGroupID.Maybe()
	require.True(t, ok)
	require.Equal(t, tgID, v)
}
