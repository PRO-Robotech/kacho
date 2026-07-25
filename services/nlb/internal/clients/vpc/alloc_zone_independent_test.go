// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// A REGIONAL (anycast) load balancer has NO zone — its public VIP must be carved
// out of the ZONE-INDEPENDENT address pool. On the wire that is an
// `External{Ipv4,Ipv6}AddressSpec` with an EMPTY `zone_id`, which vpc documents as
// valid and routes to the zone-independent pool (address/create.go
// `validateExternalZone`: "пустой zone_id ВАЛИДЕН (anycast из global-пула,
// зоне-независим)"; addresspool/resolve.go global step).
//
// The client used to reject an empty zone in `validateExternalReq`, which is why
// the use-case had to synthesise one (`sort(zones)[0]`) and thereby pin every
// "anycast" VIP to a single zone's pool. The zone is now optional exactly like it
// is at vpc; it stays forwarded verbatim when the caller does supply one.
func TestInternalAddressClient_AllocateExternalIP_ZoneIndependent(t *testing.T) {
	allocResp := &vpcpb.Address{
		Id:        "adr-anycast-v4",
		ProjectId: "prj-1",
		Address: &vpcpb.Address_ExternalIpv4Address{
			ExternalIpv4Address: &vpcpb.ExternalIpv4Address{Address: "203.0.113.7"},
		},
	}
	addrSvc := &fakeAddressForAlloc{createResp: allocResp}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	resp, err := c.AllocateExternalIP(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1",
		Name:      "nlb-vip-v4",
		Owner:     AddressOwner{Kind: "network_load_balancer", ID: "nlb-1"},
	})
	require.NoError(t, err, "a zone-less (anycast) external allocation is valid")
	assert.Equal(t, "adr-anycast-v4", resp.AddressID)
	require.NotNil(t, addrSvc.lastCreate)
	assert.Empty(t, addrSvc.lastCreate.GetExternalIpv4AddressSpec().GetZoneId(),
		"no zone may be invented on the wire — that is what pins the anycast VIP to one zone")
}

func TestInternalAddressClient_AllocateExternalIPv6_ZoneIndependent(t *testing.T) {
	allocResp := &vpcpb.Address{
		Id:        "adr-anycast-v6",
		ProjectId: "prj-1",
		Address: &vpcpb.Address_ExternalIpv6Address{
			ExternalIpv6Address: &vpcpb.ExternalIpv6Address{Address: "2001:db8::7"},
		},
	}
	addrSvc := &fakeAddressForAlloc{createResp: allocResp}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	resp, err := c.AllocateExternalIPv6(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1",
		Name:      "nlb-vip-v6",
		Owner:     AddressOwner{Kind: "network_load_balancer", ID: "nlb-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "adr-anycast-v6", resp.AddressID)
	require.NotNil(t, addrSvc.lastCreate)
	assert.Empty(t, addrSvc.lastCreate.GetExternalIpv6AddressSpec().GetZoneId())
}

// A caller-supplied zone is still forwarded verbatim (the zonal lane is untouched).
func TestInternalAddressClient_AllocateExternalIP_ZoneForwardedWhenGiven(t *testing.T) {
	allocResp := &vpcpb.Address{
		Id: "adr-zonal-v4",
		Address: &vpcpb.Address_ExternalIpv4Address{
			ExternalIpv4Address: &vpcpb.ExternalIpv4Address{Address: "203.0.113.8"},
		},
	}
	addrSvc := &fakeAddressForAlloc{createResp: allocResp}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	_, err := c.AllocateExternalIP(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1", Name: "vip", ZoneID: "ru-central1-b",
		Owner: AddressOwner{Kind: "network_load_balancer", ID: "nlb-1"},
	})
	require.NoError(t, err)
	require.NotNil(t, addrSvc.lastCreate)
	assert.Equal(t, "ru-central1-b", addrSvc.lastCreate.GetExternalIpv4AddressSpec().GetZoneId())
}
