// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

// An instance is created in its own zone and its interfaces must be in that same
// zone. A REGIONAL (anycast) subnet is excluded from the zonal check by
// construction — it has no zone to compare — and only the regional coherence
// remains (data-integrity.md, placement-coherence).
//
// The check belongs on the request path: it must reject the Create, not be
// deferred to a launch saga.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// requireNoInstancePersisted — рефьюз обязан отвергнуть СОЗДАНИЕ, а не оставить
// durable-строку: List проекта пуст.
func requireNoInstancePersisted(t *testing.T, kit instSvcKit) {
	t.Helper()
	out, _, err := kit.repo.List(context.Background(), InstanceFilter{ProjectID: "prj-acme"}, Pagination{PageSize: 10})
	require.NoError(t, err)
	require.Empty(t, out, "no instance may be persisted")
}

// Instance in zone -a, NIC spec pointing at a ZONAL subnet in zone -b → reject.
func TestInstance_Create_NicSpecSubnetForeignZone_Rejected(t *testing.T) {
	kit := newInstanceSvcWithSubnets(t, true, portmock.NewSubnetRegistry(
		portmock.SubnetPlacement{ID: "sub-abc", ProjectID: "prj-acme", PlacementType: "ZONAL", ZoneID: "ru-central1-b"},
	))
	req := baseCreateReq() // zone ru-central1-a
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-abc"}}

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.NotNil(t, done.Error, "an interface in a foreign zone must not be accepted")
	require.Equal(t, int32(codes.FailedPrecondition), done.Error.Code)
	require.Equal(t,
		"NetworkInterface subnet is in zone ru-central1-b, instance zone is ru-central1-a",
		done.Error.Message)
	requireNoInstancePersisted(t, kit)
}

// Same zone → accepted.
func TestInstance_Create_NicSpecSubnetSameZone_OK(t *testing.T) {
	kit := newInstanceSvcWithSubnets(t, true, portmock.NewSubnetRegistry(
		portmock.SubnetPlacement{ID: "sub-abc", ProjectID: "prj-acme", PlacementType: "ZONAL", ZoneID: "ru-central1-a"},
	))
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-abc"}}

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.Nil(t, done.Error, "same-zone interface must be accepted: %v", done.Error)
}

// A REGIONAL (anycast) subnet of the instance's own region carries no zone —
// excluded from the zonal check by construction, accepted.
func TestInstance_Create_NicSpecAnycastSubnetSameRegion_OK(t *testing.T) {
	kit := newInstanceSvcWithSubnets(t, true, portmock.NewSubnetRegistry(
		portmock.SubnetPlacement{ID: "sub-any", ProjectID: "prj-acme", PlacementType: "REGIONAL", RegionID: "ru-central1"},
	))
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-any"}}

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.Nil(t, done.Error, "anycast subnet of the same region must be accepted: %v", done.Error)
}

// The anycast exception drops the ZONAL comparison, not the regional one: an
// anycast subnet of a FOREIGN region is still incoherent.
func TestInstance_Create_NicSpecAnycastSubnetForeignRegion_Rejected(t *testing.T) {
	kit := newInstanceSvcWithSubnets(t, true, portmock.NewSubnetRegistry(
		portmock.SubnetPlacement{ID: "sub-any", ProjectID: "prj-acme", PlacementType: "REGIONAL", RegionID: "ru-central2"},
	))
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-any"}}

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.NotNil(t, done.Error, "an anycast subnet of a foreign region must not be accepted")
	require.Equal(t, int32(codes.FailedPrecondition), done.Error.Code)
	require.Equal(t,
		"NetworkInterface subnet must be in the same region as the instance",
		done.Error.Message)
}

// vpc unreachable → the mutation fails closed, no instance is created.
func TestInstance_Create_NicSpecSubnetPeerUnavailable_FailsClosed(t *testing.T) {
	sub := portmock.NewSubnetRegistry()
	sub.Err = status.Error(codes.Unavailable, "vpc down")
	kit := newInstanceSvcWithSubnets(t, true, sub)
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-abc"}}

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.NotNil(t, done.Error, "unreachable vpc must fail closed on a mutation")
	require.Equal(t, int32(codes.Unavailable), done.Error.Code)
	requireNoInstancePersisted(t, kit)
}

// An unknown / inaccessible subnet is a precondition failure, and the answer is
// the same hide-existence wording a real miss produces.
func TestInstance_Create_NicSpecSubnetMissing_Rejected(t *testing.T) {
	kit := newInstanceSvcWithSubnets(t, true, portmock.NewSubnetRegistry())
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-ghost"}}

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.NotNil(t, done.Error)
	require.Equal(t, int32(codes.FailedPrecondition), done.Error.Code)
	require.Equal(t, "Subnet sub-ghost not found", done.Error.Message)
}
