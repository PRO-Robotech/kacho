// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package type2pb

import (
	"testing"
	"time"

	"github.com/H-BF/corlib/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

func TestListener_Transfer(t *testing.T) {
	// VIP консолидирован на LoadBalancer: листенер больше не несёт address-полей
	// (region_id/ip_version/address_id/allocated_address/subnet_id сняты с proto).
	// DB-колонки ещё существуют (удаляются поздней миграцией) — repo их читает, но
	// в proto-проекцию они не выходят.
	rec := kachorepo.ListenerRecord{
		Listener: domain.Listener{
			ID:               "lst01ABCDEF1234567xx",
			ProjectID:        "prj01ABCDEF1234567ll",
			LoadBalancerID:   "nlb01ABCDEF1234567xx",
			RegionID:         "ru-central1",
			Name:             "ext-lst",
			Description:      "ext listener",
			Labels:           domain.LabelsFromMap(map[string]string{"role": "edge"}),
			Protocol:         domain.ProtoTCP,
			Port:             443,
			TargetPort:       8443,
			IPVersion:        domain.IPVersionV4,
			AddressID:        option.MustNewOption(domain.AddressID("e9b01ADDRESS")),
			AllocatedAddress: "203.0.113.10",
			SubnetID:         option.ValueOf[domain.SubnetID]{},
			ProxyProtocolV2:  true,
			Status:           domain.ListenerStatusActive,
		},
		CreatedAt: time.Date(2026, 5, 24, 1, 2, 3, 0, time.UTC),
	}
	var pb *lbv1.Listener
	require.NoError(t, dto.Transfer(dto.FromTo(rec, &pb)))
	require.NotNil(t, pb)
	assert.Equal(t, "lst01ABCDEF1234567xx", pb.Id)
	assert.Equal(t, "nlb01ABCDEF1234567xx", pb.LoadBalancerId)
	assert.Equal(t, lbv1.Listener_TCP, pb.Protocol)
	assert.Equal(t, int64(443), pb.Port)
	assert.Equal(t, int64(8443), pb.TargetPort)
	assert.True(t, pb.ProxyProtocolV2)
	assert.Equal(t, lbv1.Listener_ACTIVE, pb.Status)
}

// NLB-1b EXPAND (additive): target_group_id echoes the same ref as
// default_target_group_id; resolved_backend_port°/substatus° derive from the wired
// TargetGroup.port (ListenerRecord.ResolvedBackendPort, computed by the repo read).
func TestListener_NLB_1b_TargetGroupProjection(t *testing.T) {
	mk := func(mut func(*kachorepo.ListenerRecord)) *lbv1.Listener {
		rec := kachorepo.ListenerRecord{
			Listener: domain.Listener{
				ID: "lst01ABCDEF1234567xx", ProjectID: "p1", LoadBalancerID: "nlb1",
				Name: "l1", Protocol: domain.ProtoTCP, Port: 443,
				Status: domain.ListenerStatusActive,
			},
			CreatedAt: time.Now(),
		}
		mut(&rec)
		var pb *lbv1.Listener
		require.NoError(t, dto.Transfer(dto.FromTo(rec, &pb)))
		return pb
	}

	// wired TG resolves → target_group_id echoed, resolved_backend_port° = TG.port,
	// substatus° OK.
	port := int32(8080)
	wired := mk(func(rec *kachorepo.ListenerRecord) {
		rec.DefaultTargetGroupID = option.MustNewOption(domain.ResourceID("tgr-wired00000000001"))
		rec.ResolvedBackendPort = &port
	})
	assert.Equal(t, "tgr-wired00000000001", wired.GetTargetGroupId())
	assert.Equal(t, "tgr-wired00000000001", wired.GetDefaultTargetGroupId())
	assert.Equal(t, int64(8080), wired.GetResolvedBackendPort())
	assert.Equal(t, lbv1.Listener_OK, wired.GetSubstatus())

	// no resolvable TG → resolved_backend_port° 0, substatus° MISCONFIGURED.
	unwired := mk(func(*kachorepo.ListenerRecord) {})
	assert.Equal(t, "", unwired.GetTargetGroupId())
	assert.Equal(t, int64(0), unwired.GetResolvedBackendPort())
	assert.Equal(t, lbv1.Listener_MISCONFIGURED, unwired.GetSubstatus())
}

// NLB-1b F5 (NLB-1-27/28): the resolved VIP is echoed as a SINGLE managed
// projection address{type,id,name°,ip°,hostname°}. address.id carries the
// immutable addressId (NOT duplicated as a flat field). No VIP anchor → nil
// address (MIGRATE optional-first: VIP-on-Listener is optional).
func TestListener_NLB_1_27_AddressProjection(t *testing.T) {
	mk := func(mut func(*kachorepo.ListenerRecord)) *lbv1.Listener {
		rec := kachorepo.ListenerRecord{
			Listener: domain.Listener{
				ID: "lst01ABCDEF1234567xx", ProjectID: "p1", LoadBalancerID: "nlb1",
				RegionID: "eu-north", Name: "l1", Protocol: domain.ProtoTCP, Port: 443,
				Status: domain.ListenerStatusActive,
			},
			CreatedAt: time.Now(),
		}
		mut(&rec)
		var pb *lbv1.Listener
		require.NoError(t, dto.Transfer(dto.FromTo(rec, &pb)))
		return pb
	}

	// BYO resolved VIP → single address° projection; id == addressId; ip echoed.
	byo := mk(func(rec *kachorepo.ListenerRecord) {
		rec.AddressID = option.MustNewOption(domain.AddressID("e9b-t8y2u4i6o8p0aq"))
		rec.AllocatedAddress = "203.0.113.40"
		rec.VipOrigin = domain.VipOriginBYO
	})
	require.NotNil(t, byo.GetAddress())
	assert.Equal(t, "vpc.address", byo.GetAddress().GetType())
	assert.Equal(t, "e9b-t8y2u4i6o8p0aq", byo.GetAddress().GetId())
	assert.Equal(t, "203.0.113.40", byo.GetAddress().GetIp())
	assert.Equal(t, domain.ListenerVIPHostname("lst01ABCDEF1234567xx", "eu-north"), byo.GetAddress().GetHostname())

	// No VIP anchor → address° is nil (optional VIP-on-Listener during MIGRATE).
	none := mk(func(*kachorepo.ListenerRecord) {})
	assert.Nil(t, none.GetAddress())
}

func TestListener_StatusMapping(t *testing.T) {
	tests := []struct {
		domain domain.ListenerStatus
		pb     lbv1.Listener_Status
	}{
		{domain.ListenerStatusCreating, lbv1.Listener_CREATING},
		{domain.ListenerStatusActive, lbv1.Listener_ACTIVE},
		{domain.ListenerStatusUpdating, lbv1.Listener_UPDATING},
		{domain.ListenerStatusDeleting, lbv1.Listener_DELETING},
	}
	for _, tc := range tests {
		got, err := listenerStatusToPb(tc.domain)
		require.NoError(t, err)
		assert.Equal(t, tc.pb, got, "for %s", tc.domain)
	}
}

func TestListener_ProtocolAndIPVersionUnknownFail(t *testing.T) {
	_, err := listenerProtocolToPb(domain.LbProto("HTTP"))
	require.Error(t, err)
	_, err = ipVersionToPb(domain.IPVersion("V42"))
	require.Error(t, err)
	_, err = listenerStatusToPb(domain.ListenerStatus("UNKNOWN"))
	require.Error(t, err)
}
