// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

// Region-coherence of the VIP source must hold for BOTH load balancer kinds and
// must fail CLOSED when it cannot be established.
//
// data-integrity.md §Placement-coherence: "региональный ↔ региональный — тот же
// region_id"; "зональный ↔ региональный — зона consumer'а ∈ регион peer'а";
// "cross-service — peer-validate на request-path (fail-closed Unavailable …)".

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
)

// ---- Defect 5 — the region check was open on failure ------------------------

// A subnet whose authoritative region is unknown (the mirror is empty because
// the zone→region resolver is unwired or geo answered nothing) must not slip
// through as "matching". A mutation whose precondition cannot be verified fails
// closed.
func TestCreate_SubnetRegionUnverifiable_FailsClosed(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{
		subnet: &fakeSubnetClient{getFunc: func(_ context.Context, id string) (*vpcclient.Subnet, error) {
			return &vpcclient.Subnet{
				ID: id, ProjectID: "prj-a", NetworkID: "net-1",
				PlacementType: "ZONAL", ZoneID: "region-1-a",
				RegionID: "", // unresolved — coherence unverifiable
			}, nil
		}},
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL
	req.V4Source = vipSubnet(lbTestSubnetZonal)

	_, err := uc.Execute(context.Background(), req)
	require.Error(t, err, "unverifiable subnet region must fail closed")
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, "subnet region lookup unavailable", status.Convert(err).Message())
}

// Same hole on the linked-address lane of an INTERNAL load balancer.
func TestCreate_LinkedAddressSubnetRegionUnverifiable_FailsClosed(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{
		reader: &fakeAddressReader{subnetID: "snt01000000000000000"},
		subnet: &fakeSubnetClient{getFunc: func(_ context.Context, id string) (*vpcclient.Subnet, error) {
			return &vpcclient.Subnet{
				ID: id, ProjectID: "prj-a", NetworkID: "net-1",
				PlacementType: "ZONAL", ZoneID: "region-1-a", RegionID: "",
			}, nil
		}},
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL
	req.V4Source = vipAddress(lbTestAddrInternal)

	_, err := uc.Execute(context.Background(), req)
	require.Error(t, err, "unverifiable region of the linked address subnet must fail closed")
	require.Equal(t, codes.Unavailable, status.Code(err))
}

// ---- Defect 2 — an external load balancer never checked its linked address ---

// An EXTERNAL load balancer is always REGIONAL. A BYO external address that
// declares a zone belongs to that zone's region; linking one from a FOREIGN
// region is incoherent. Anti-oracle: the answer stays the generic one.
func TestCreate_ExternalLinkedAddress_ForeignRegion_Rejected(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{
		reader: &fakeAddressReader{external: true, zoneID: "region-2-a"},
		zoneRegion: &fakeZoneRegionClient{regions: map[string]string{
			"region-2-a": "region-2",
		}},
	})
	req := baseCreateReq() // region-1
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipAddress(lbTestAddrExternal)

	_, err := uc.Execute(context.Background(), req)
	require.Error(t, err, "external address from a foreign region must not be linked")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "Illegal argument addressId", status.Convert(err).Message(),
		"anti-oracle: must not disclose the address placement")
}

// A zone-bound external address of the SAME region stays acceptable.
func TestCreate_ExternalLinkedAddress_SameRegion_OK(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{
		reader: &fakeAddressReader{external: true, zoneID: "region-1-a"},
		zoneRegion: &fakeZoneRegionClient{regions: map[string]string{
			"region-1-a": "region-1",
		}},
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipAddress(lbTestAddrExternal)

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
}

// An anycast external address carries no zone — it is zone-independent by
// construction and there is nothing to compare it against. It must still link.
func TestCreate_ExternalLinkedAddress_Anycast_OK(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{
		reader:     &fakeAddressReader{external: true, zoneID: ""},
		zoneRegion: &fakeZoneRegionClient{},
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipAddress(lbTestAddrExternal)

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
}

// Zone→region resolution unavailable on a zone-bound external address → the
// mutation fails closed, it is not admitted.
func TestCreate_ExternalLinkedAddress_ZoneRegionUnavailable_FailsClosed(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{
		reader:     &fakeAddressReader{external: true, zoneID: "region-1-a"},
		zoneRegion: &fakeZoneRegionClient{err: errZoneRegionUnavailable},
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipAddress(lbTestAddrExternal)

	_, err := uc.Execute(context.Background(), req)
	require.Error(t, err, "geo down must fail closed on a mutation")
	require.Equal(t, codes.Unavailable, status.Code(err))
}
