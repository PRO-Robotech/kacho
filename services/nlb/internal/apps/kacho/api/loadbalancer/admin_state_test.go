// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// NLB-1-13 (F3): adminState=DISABLED via Update persists (LIVE-mutable). status
// auto-recompute wiring (0013 trigger) is MIGRATE/NLB-1c — EXPAND only stores the
// desired administrative state.
func TestLoadBalancer_NLB_1_13_AdminStateDisabled_Update(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	require.Equal(t, domain.AdminStateEnabled, repo.lbs[lbID].AdminState, "seed defaults to ENABLED")
	opsRepo := newFakeOpsRepo()
	uc := NewUpdateLoadBalancerUseCase(repo, opsRepo, &fakeZoneClient{}, slog.Default())

	op, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		AdminState:            lbv1.NetworkLoadBalancer_ADMIN_STATE_DISABLED,
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"admin_state"}},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
	require.Equal(t, domain.AdminStateDisabled, repo.lbs[lbID].AdminState)
}

// NLB-1-14 (F3): Update never auto-ENABLE a DISABLED LB — a labels-only Update
// (admin_state NOT in mask) preserves DISABLED; only an explicit admin_state:ENABLED
// re-enables.
func TestLoadBalancer_NLB_1_14_UpdateNeverAutoEnable(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	repo.lbs[lbID].AdminState = domain.AdminStateDisabled
	opsRepo := newFakeOpsRepo()
	uc := NewUpdateLoadBalancerUseCase(repo, opsRepo, &fakeZoneClient{}, slog.Default())

	// labels-only Update — admin_state absent from mask → stays DISABLED.
	op, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		Labels:                map[string]string{"tier": "x"},
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
	require.Equal(t, domain.AdminStateDisabled, repo.lbs[lbID].AdminState, "labels Update must not auto-ENABLE")

	// explicit admin_state:ENABLED re-enables.
	op2, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		AdminState:            lbv1.NetworkLoadBalancer_ADMIN_STATE_ENABLED,
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"admin_state"}},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op2.ID).Error)
	require.Equal(t, domain.AdminStateEnabled, repo.lbs[lbID].AdminState)
}

// Create defaults admin_state to ENABLED; explicit DISABLED persists.
func TestLoadBalancer_AdminState_Create(t *testing.T) {
	t.Parallel()

	t.Run("default ENABLED", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{addr: &fakeAddressClient{}})
		req := baseCreateReq()
		req.Type = lbv1.NetworkLoadBalancer_EXTERNAL
		req.V4Source = vipPublic()
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		require.Equal(t, domain.AdminStateEnabled, lbByName(t, repo, "lb-1").AdminState)
	})

	t.Run("explicit DISABLED", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		uc := newCreateUC(repo, opsRepo, createDeps{addr: &fakeAddressClient{}})
		req := baseCreateReq()
		req.Type = lbv1.NetworkLoadBalancer_EXTERNAL
		req.V4Source = vipPublic()
		req.AdminState = lbv1.NetworkLoadBalancer_ADMIN_STATE_DISABLED
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		require.Equal(t, domain.AdminStateDisabled, lbByName(t, repo, "lb-1").AdminState)
	})
}
