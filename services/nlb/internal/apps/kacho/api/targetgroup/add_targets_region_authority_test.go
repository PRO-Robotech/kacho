// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/compute"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
)

// Region-coherence of an AddTargets target must be decided by the AUTHORITATIVE
// region of the peer resource (vpc.Subnet.region_id for a REGIONAL subnet,
// geo zone→region for anything zonal), never by string surgery on the zone id.
//
// data-integrity.md §Placement-coherence: "региональный ↔ региональный — тот же
// region_id"; "Существование zone_id/region_id — валидировать peer-вызовом
// geo.v1.ZoneService.Get / RegionService.Get (не локально), fail-closed".

// A REGIONAL (anycast) subnet carries no zone at all — a name-derived region is
// empty for it, so the coherence check degenerates into a no-op and a target
// from a FOREIGN region is accepted. The subnet's own region_id says otherwise.
func TestAdd_IPRef_RegionalSubnetForeignRegion_Rejected(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "ipref-regional-foreign") // region ru-central1
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewAddTargetsUseCase(repo, opsRepo,
		&fakeInstanceClient{}, &fakeNICClient{},
		&fakeSubnetClient{getFunc: func(_ context.Context, id string) (*vpc.Subnet, error) {
			return &vpc.Subnet{
				ID:            id,
				PlacementType: vpc.SubnetPlacementRegional,
				ZoneID:        "", // anycast: zone-independent by construction
				RegionID:      "ru-central2",
				V4CIDRBlocks:  []string{"10.0.0.0/24"},
			}, nil
		}},
		&fakeZoneRegionClient{},
		nil,
	)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_IpRef{IpRef: &lbv1.Target_InCloudIP{
				SubnetId: "e9b-anycast", Address: "10.0.0.5",
			}}, Weight: 100},
		},
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error, "cross-region REGIONAL subnet must not be accepted")
	require.Equal(t, int32(codes.InvalidArgument), final.Error.Code)
	require.Contains(t, final.Error.Message, "region 'ru-central2'")
	require.Contains(t, final.Error.Message, "target_group region 'ru-central1'")
}

// Same hole reached through a NIC whose parent subnet is REGIONAL.
func TestAdd_NIC_RegionalSubnetForeignRegion_Rejected(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "nic-regional-foreign")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewAddTargetsUseCase(repo, opsRepo,
		&fakeInstanceClient{},
		&fakeNICClient{getFunc: func(_ context.Context, id string) (*vpc.NetworkInterface, error) {
			return &vpc.NetworkInterface{ID: id, SubnetID: "e9b-anycast"}, nil
		}},
		&fakeSubnetClient{getFunc: func(_ context.Context, id string) (*vpc.Subnet, error) {
			return &vpc.Subnet{
				ID:            id,
				PlacementType: vpc.SubnetPlacementRegional,
				ZoneID:        "",
				RegionID:      "ru-central2",
				V4CIDRBlocks:  []string{"10.0.0.0/24"},
			}, nil
		}},
		&fakeZoneRegionClient{},
		nil,
	)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_NicId{NicId: "enp-anycast"}, Weight: 100},
		},
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error, "cross-region REGIONAL subnet must not be accepted via NIC")
	require.Equal(t, int32(codes.InvalidArgument), final.Error.Code)
	require.Contains(t, final.Error.Message, "region 'ru-central2'")
}

// An instance whose zone id does not spell out its region (hyphen-canon
// `<prefix>-<crockford-base32>`, api-conventions.md B3) also derives to an empty
// region — same no-op. geo is the owner of zone→region and must be asked.
func TestAdd_Instance_OpaqueZoneID_ForeignRegion_Rejected(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "inst-opaque-zone")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewAddTargetsUseCase(repo, opsRepo,
		&fakeInstanceClient{getFunc: func(_ context.Context, id string) (*compute.Instance, error) {
			return &compute.Instance{ID: id, ZoneID: "zn-01hx9k3m2p4q7r8t"}, nil
		}},
		&fakeNICClient{}, &fakeSubnetClient{},
		&fakeZoneRegionClient{regions: map[string]string{"zn-01hx9k3m2p4q7r8t": "ru-central2"}},
		nil,
	)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_InstanceId{InstanceId: "epd-opaque"}, Weight: 100},
		},
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error, "cross-region instance with an opaque zone id must not be accepted")
	require.Equal(t, int32(codes.InvalidArgument), final.Error.Code)
	require.Contains(t, final.Error.Message, "region 'ru-central2'")
	require.Contains(t, final.Error.Message, "target_group region 'ru-central1'")
}

// Region unverifiable (subnet mirror empty — geo unwired/unreachable) must
// fail CLOSED on a mutation, not silently admit the target.
func TestAdd_IPRef_RegionUnverifiable_FailsClosed(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "ipref-unverifiable")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewAddTargetsUseCase(repo, opsRepo,
		&fakeInstanceClient{}, &fakeNICClient{},
		&fakeSubnetClient{getFunc: func(_ context.Context, id string) (*vpc.Subnet, error) {
			return &vpc.Subnet{
				ID:            id,
				PlacementType: "ZONAL",
				ZoneID:        "ru-central1-a",
				RegionID:      "", // mirror not filled — coherence unverifiable
				V4CIDRBlocks:  []string{"10.0.0.0/24"},
			}, nil
		}},
		&fakeZoneRegionClient{},
		nil,
	)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_IpRef{IpRef: &lbv1.Target_InCloudIP{
				SubnetId: "e9b-sub1", Address: "10.0.0.5",
			}}, Weight: 100},
		},
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error, "unverifiable region must fail closed")
	require.Equal(t, int32(codes.Unavailable), final.Error.Code)
}

// Instance zone→region resolution unavailable (geo down) must fail CLOSED too.
func TestAdd_Instance_ZoneRegionUnavailable_FailsClosed(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "inst-geo-down")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewAddTargetsUseCase(repo, opsRepo,
		&fakeInstanceClient{}, &fakeNICClient{}, &fakeSubnetClient{},
		&fakeZoneRegionClient{err: errZoneRegionUnavailable},
		nil,
	)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_InstanceId{InstanceId: "epd-i1"}, Weight: 100},
		},
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error, "geo down must fail closed on a mutation")
	require.Equal(t, int32(codes.Unavailable), final.Error.Code)
}
