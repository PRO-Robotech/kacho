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

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
)

// TestHandler_DispatchesAll — Handler — тонкая обёртка над use-case'ами.
// Тест проверяет, что каждый RPC handler-метода действительно вызывает
// соответствующий use-case (а не panic'ит / возвращает unimplemented).
func TestHandler_DispatchesAll(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	tgID := seedTG(t, repo, "prj-a", "ru-central1", "tg-1")
	opsRepo := newFakeOpsRepo()
	h := NewHandler(repo, opsRepo, &fakeProjectClient{}, nil, &fakeRegionClient{}, &fakeZoneClient{}, &fakeSubnetClient{}, &fakeAddressReader{}, &fakeAddressClient{}, nil, slog.Default())

	ctx := context.Background()

	// Get
	got, err := h.Get(ctx, &lbv1.GetNetworkLoadBalancerRequest{NetworkLoadBalancerId: lbID})
	require.NoError(t, err)
	require.Equal(t, "edge", got.GetName())

	// List
	resp, err := h.List(ctx, &lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetNetworkLoadBalancers())

	// Create (INTERNAL REGIONAL subnet-auto)
	op, err := h.Create(ctx, &lbv1.CreateNetworkLoadBalancerRequest{
		ProjectId: "prj-a", RegionId: "ru-central1", Name: "edge-2",
		Placement: lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL,
		V4Source:  vipSubnet(lbTestSubnetRegional),
	})
	require.NoError(t, err)
	require.NotEmpty(t, op.GetId())
	awaitOpDone(t, opsRepo, op.GetId())

	// Update
	op2, err := h.Update(ctx, &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID, Name: "edge-renamed",
	})
	require.NoError(t, err)
	awaitOpDone(t, opsRepo, op2.GetId())

	// GetTargetStates
	_, err = h.GetTargetStates(ctx, &lbv1.GetTargetStatesRequest{
		NetworkLoadBalancerId: lbID, TargetGroupId: tgID,
	})
	require.NoError(t, err)

	// ListOperations
	_, err = h.ListOperations(ctx, &lbv1.ListNetworkLoadBalancerOperationsRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.NoError(t, err)

	// Move (need destination project; no listener wired to a TG)
	opMove, err := h.Move(ctx, &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID, DestinationProjectId: "prj-b",
	})
	require.NoError(t, err)
	awaitOpDone(t, opsRepo, opMove.GetId())

	// Delete (ensure no listeners/TG)
	opDel, err := h.Delete(ctx, &lbv1.DeleteNetworkLoadBalancerRequest{NetworkLoadBalancerId: lbID})
	require.NoError(t, err)
	awaitOpDone(t, opsRepo, opDel.GetId())
}

func TestHandler_NewHandler_NilLogger_OK(t *testing.T) {
	t.Parallel()
	h := NewHandler(newFakeRepo(), newFakeOpsRepo(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NotNil(t, h)
}

func TestHandler_Get_PropagatesErr(t *testing.T) {
	t.Parallel()
	h := NewHandler(newFakeRepo(), newFakeOpsRepo(), nil, nil, nil, nil, nil, nil, nil, nil, slog.Default())
	_, err := h.Get(context.Background(), &lbv1.GetNetworkLoadBalancerRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
