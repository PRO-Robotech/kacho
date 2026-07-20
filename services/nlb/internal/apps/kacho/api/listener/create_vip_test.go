// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// newCreateUCVIP — Create UC с VIP-клиентами (NLB-1b F5 alloc-saga).
func newCreateUCVIP(repo *fakeRepo, ops *fakeOpsRepo, addr *fakeInternalAddressClient, subnet *fakeSubnetClient) *CreateUseCase {
	return NewCreateUseCase(repo, ops, slog.Default()).WithVIP(addr, subnet)
}

// seedZonalLB — INTERNAL ZONAL LB (родитель VIP-листенера).
func seedZonalLB(t *testing.T, repo *fakeRepo) *kachorepo.LoadBalancerRecord {
	t.Helper()
	lb := seedParentLB(t, repo)
	lb.PlacementType = domain.PlacementZonal
	repo.seedLB(lb)
	return lb
}

// NLB-1-27: BYO address_id → VIP линкуется (AttachExisting, used_by=nlb_listener:<id>);
// листенер персистится с address_id + allocated_address, VipOrigin=byo, address°
// эхается в op-response.
func TestCreateListener_NLB_1_27_BYOAddress(t *testing.T) {
	t.Parallel()
	repo, ops := newFakeRepo(), newFakeOpsRepo()
	lb := seedZonalLB(t, repo)
	addr := newFakeInternalAddressClient()
	addr.nextAllocValue = "203.0.113.40"
	uc := newCreateUCVIP(repo, ops, addr, &fakeSubnetClient{placement: "ZONAL"})

	op, err := uc.Run(context.Background(), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID), Name: "https",
		Protocol: lbv1.Listener_TCP, Port: 443, TargetPort: 8080,
		AddressId: "e9b-t8y2u4i6o8p0aq",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID, testTimeout).Error)

	got := listenerByLB(repo, string(lb.ID))
	require.Len(t, got, 1)
	require.Equal(t, domain.ListenerStatusActive, got[0].Status)
	require.Equal(t, domain.VipOriginBYO, got[0].VipOrigin)
	id, ok := got[0].AddressID.Maybe()
	require.True(t, ok)
	require.Equal(t, "e9b-t8y2u4i6o8p0aq", string(id))
	require.Equal(t, "203.0.113.40", string(got[0].AllocatedAddress))

	// BYO link → AttachExisting recorded as setRef with owner nlb_listener:<id>.
	require.Len(t, addr.setRefCalls, 1)
	require.Equal(t, "e9b-t8y2u4i6o8p0aq", addr.setRefCalls[0].addressID)
	require.Equal(t, "nlb_listener", addr.setRefCalls[0].owner.Kind)
	require.Equal(t, string(got[0].ID), addr.setRefCalls[0].owner.ID)
	require.Empty(t, addr.allocInternalCalls, "BYO must not auto-allocate")
}

// NLB-1-28: auto subnet_id → свежий internal Address аллоцируется
// (AllocateInternalIP), VipOrigin=auto, subnet_id персистится, allocated_address
// эхается.
func TestCreateListener_NLB_1_28_AutoSubnet(t *testing.T) {
	t.Parallel()
	repo, ops := newFakeRepo(), newFakeOpsRepo()
	lb := seedZonalLB(t, repo)
	addr := newFakeInternalAddressClient()
	addr.nextAllocID = "e9bAUTOADDR0000001"
	addr.nextAllocValue = "10.0.5.7"
	uc := newCreateUCVIP(repo, ops, addr, &fakeSubnetClient{placement: "ZONAL", zoneID: "eu-north-a"})

	op, err := uc.Run(context.Background(), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID), Name: "internal",
		Protocol: lbv1.Listener_TCP, Port: 443, TargetPort: 8080,
		SubnetId: "e9b-3e5r7t9y1u3i5o7p",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID, testTimeout).Error)

	got := listenerByLB(repo, string(lb.ID))
	require.Len(t, got, 1)
	require.Equal(t, domain.VipOriginAuto, got[0].VipOrigin)
	sub, ok := got[0].SubnetID.Maybe()
	require.True(t, ok)
	require.Equal(t, "e9b-3e5r7t9y1u3i5o7p", string(sub))
	id, ok := got[0].AddressID.Maybe()
	require.True(t, ok)
	require.Equal(t, "e9bAUTOADDR0000001", string(id))
	require.Equal(t, "10.0.5.7", string(got[0].AllocatedAddress))

	require.Len(t, addr.allocInternalCalls, 1)
	require.Equal(t, "e9b-3e5r7t9y1u3i5o7p", addr.allocInternalCalls[0].SubnetID)
	require.Equal(t, "nlb_listener", addr.allocInternalCalls[0].Owner.Kind)
}

// VIP-анкер: address_id и subnet_id взаимоисключающие → InvalidArgument (sync).
func TestCreateListener_VIPAnchor_MutuallyExclusive(t *testing.T) {
	t.Parallel()
	repo, ops := newFakeRepo(), newFakeOpsRepo()
	lb := seedZonalLB(t, repo)
	uc := newCreateUCVIP(repo, ops, newFakeInternalAddressClient(), &fakeSubnetClient{placement: "ZONAL"})
	_, err := uc.Run(context.Background(), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID), Name: "both",
		Protocol: lbv1.Listener_TCP, Port: 443, TargetPort: 8080,
		AddressId: "e9b-addr", SubnetId: "e9b-subnet",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// NLB-1-32: VIP-subnet placementType не совпадает с placement LB →
// FAILED_PRECONDITION (ZONAL LB + REGIONAL subnet).
func TestCreateListener_NLB_1_32_PlacementMismatch(t *testing.T) {
	t.Parallel()
	repo, ops := newFakeRepo(), newFakeOpsRepo()
	lb := seedZonalLB(t, repo)
	uc := newCreateUCVIP(repo, ops, newFakeInternalAddressClient(),
		&fakeSubnetClient{placement: "REGIONAL"})
	_, err := uc.Run(context.Background(), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID), Name: "mismatch",
		Protocol: lbv1.Listener_TCP, Port: 443, TargetPort: 8080,
		SubnetId: "e9b-regional",
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, err.Error(), "placement")
}

// NLB-1-32 (region): VIP-subnet region не совпадает с регионом LB →
// FAILED_PRECONDITION.
func TestCreateListener_VIPSubnet_RegionMismatch(t *testing.T) {
	t.Parallel()
	repo, ops := newFakeRepo(), newFakeOpsRepo()
	lb := seedZonalLB(t, repo)
	uc := newCreateUCVIP(repo, ops, newFakeInternalAddressClient(),
		&fakeSubnetClient{placement: "ZONAL", regionID: "other-region"})
	_, err := uc.Run(context.Background(), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID), Name: "regmis",
		Protocol: lbv1.Listener_TCP, Port: 443, TargetPort: 8080,
		SubnetId: "e9b-otherreg",
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, err.Error(), "region")
}

// VIP subnet not found (peer miss) → FAILED_PRECONDITION (by-lane split: foreign
// vpc id → PEER_RESOURCE_MISSING, not NotFound).
func TestCreateListener_VIPSubnet_PeerMiss(t *testing.T) {
	t.Parallel()
	repo, ops := newFakeRepo(), newFakeOpsRepo()
	lb := seedZonalLB(t, repo)
	uc := newCreateUCVIP(repo, ops, newFakeInternalAddressClient(),
		&fakeSubnetClient{getErr: domain.ErrNotFound})
	_, err := uc.Run(context.Background(), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID), Name: "miss",
		Protocol: lbv1.Listener_TCP, Port: 443, TargetPort: 8080,
		SubnetId: "e9b-missing",
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// NLB-1-33: ZONAL LB — второй listener c VIP-subnet в ДРУГОЙ зоне отвергается
// (set-once vip_zone anchor CAS). Первый пинит eu-north-a; второй (eu-north-b) →
// op error FAILED_PRECONDITION «load balancer VIP must be in the same zone». Тот же
// zone (idempotent) — проходит.
func TestCreateListener_NLB_1_33_ZoneCoherence(t *testing.T) {
	t.Parallel()
	repo, ops := newFakeRepo(), newFakeOpsRepo()
	lb := seedZonalLB(t, repo)
	addr := newFakeInternalAddressClient()

	createDone := func(name string, port int64, subnetZone string) *operations.Operation {
		snet := &fakeSubnetClient{placement: "ZONAL", zoneID: subnetZone}
		uc := newCreateUCVIP(repo, ops, addr, snet)
		op, err := uc.Run(context.Background(), &lbv1.CreateListenerRequest{
			LoadBalancerId: string(lb.ID), Name: name,
			Protocol: lbv1.Listener_TCP, Port: port, TargetPort: 8080,
			SubnetId: "e9b-subnet-" + subnetZone,
		})
		require.NoError(t, err)
		return awaitOpDone(t, ops, op.ID, testTimeout)
	}

	// First listener pins zone eu-north-a.
	require.Nil(t, createDone("l-a", 443, "eu-north-a").Error)
	// Second listener in a DIFFERENT zone → rejected.
	confl := createDone("l-b", 8443, "eu-north-b")
	require.NotNil(t, confl.Error)
	require.Equal(t, int32(codes.FailedPrecondition), confl.Error.GetCode())
	require.Contains(t, confl.Error.GetMessage(), "same zone")
	// Third listener in the SAME zone → allowed (idempotent pin).
	require.Nil(t, createDone("l-a2", 9443, "eu-north-a").Error)
}

// Compensation: acquire ok, но INSERT конфликтует по (lb,port,proto) → op error и
// освобождение VIP (auto → FreeIP), лизинг не течёт.
func TestCreateListener_VIP_CompensationOnInsertConflict(t *testing.T) {
	t.Parallel()
	repo, ops := newFakeRepo(), newFakeOpsRepo()
	lb := seedZonalLB(t, repo)
	// Pre-existing listener occupies (lb, 443, TCP).
	repo.seedListener(&kachorepo.ListenerRecord{
		Listener: domain.Listener{
			ID: "lst01EXISTING0000001", LoadBalancerID: lb.ID, ProjectID: lb.ProjectID,
			RegionID: lb.RegionID, Name: "existing", Protocol: domain.ProtoTCP,
			Port: 443, TargetPort: 8080, IPVersion: domain.IPVersionV4,
			Status: domain.ListenerStatusActive, VipOrigin: domain.VipOriginAuto,
		},
	})
	addr := newFakeInternalAddressClient()
	addr.nextAllocID = "e9bAUTOADDR0000009"
	uc := newCreateUCVIP(repo, ops, addr, &fakeSubnetClient{placement: "ZONAL", zoneID: "eu-north-a"})

	op, err := uc.Run(context.Background(), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID), Name: "dup",
		Protocol: lbv1.Listener_TCP, Port: 443, TargetPort: 9090,
		SubnetId: "e9b-3e5r7t9y1u3i5o7p",
	})
	require.NoError(t, err)
	final := awaitOpDone(t, ops, op.ID, testTimeout)
	require.NotNil(t, final.Error)
	require.Equal(t, int32(codes.AlreadyExists), final.Error.GetCode())
	// Acquired VIP was compensated (auto → FreeIP of the just-allocated address).
	require.Equal(t, []string{"e9bAUTOADDR0000009"}, addr.freeCalls)
}
