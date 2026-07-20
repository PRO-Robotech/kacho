// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package type2pb

import (
	"fmt"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// listener — трансфер kachorepo.ListenerRecord → *lbv1.Listener.
type listener struct{}

func (listener) toPb(rec kachorepo.ListenerRecord) (*lbv1.Listener, error) {
	ts, err := timeObj{}.toPb(rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	protoPb, err := listenerProtocolToPb(rec.Protocol)
	if err != nil {
		return nil, err
	}
	statusPb, err := listenerStatusToPb(rec.Status)
	if err != nil {
		return nil, err
	}
	defaultTGID := ""
	if v, ok := rec.DefaultTargetGroupID.Maybe(); ok {
		defaultTGID = string(v)
	}
	// NLB-1b EXPAND (additive): target_group_id echoes the same underlying reference
	// as default_target_group_id (both coexist). resolved_backend_port° + substatus°
	// are derived from whether the wired TargetGroup resolves (rec.ResolvedBackendPort,
	// computed by the repo read subquery): OK when it resolves, MISCONFIGURED otherwise.
	substatus := lbv1.Listener_MISCONFIGURED
	var resolvedBackendPort int64
	if rec.ResolvedBackendPort != nil {
		resolvedBackendPort = int64(*rec.ResolvedBackendPort)
		substatus = lbv1.Listener_OK
	}
	// NLB-1b F5 (NLB-1-27/28): the VIP anchor returns to the Listener. The resolved
	// VIP is echoed as a SINGLE managed projection address{type,id,name°,ip°,hostname°};
	// the immutable addressId input is carried by address.id (NOT duplicated as a flat
	// field). nil until a VIP is resolved (MIGRATE optional-first: VIP-on-Listener is
	// optional — legacy VIP-on-LB listeners carry no address_id → address° stays nil).
	return &lbv1.Listener{
		Id:                   string(rec.ID),
		ProjectId:            string(rec.ProjectID),
		LoadBalancerId:       string(rec.LoadBalancerID),
		CreatedAt:            ts,
		Name:                 string(rec.Name),
		Description:          string(rec.Description),
		Labels:               domain.LabelsToMap(rec.Labels),
		Protocol:             protoPb,
		Port:                 int64(rec.Port),
		TargetPort:           int64(rec.TargetPort),
		ProxyProtocolV2:      rec.ProxyProtocolV2,
		DefaultTargetGroupId: defaultTGID,
		Status:               statusPb,
		TargetGroupId:        defaultTGID,
		ResolvedBackendPort:  resolvedBackendPort,
		Substatus:            substatus,
		Address:              listenerAddressProjection(rec),
	}, nil
}

// listenerAddressProjection — NLB-1b F5 managed VIP projection. Populated only
// when a VIP anchor is resolved (address_id set); the addressId is echoed via
// address.id (single projection, no flat duplicate). name° is best-effort (not
// durably stored — omitted rather than fabricated); hostname° is the deterministic
// managed CNAME-target. Returns nil when no VIP anchor is set.
func listenerAddressProjection(rec kachorepo.ListenerRecord) *lbv1.Listener_Address {
	addrID, ok := rec.AddressID.Maybe()
	if !ok || string(addrID) == "" {
		return nil
	}
	return &lbv1.Listener_Address{
		Type:     "vpc.address",
		Id:       string(addrID),
		Ip:       string(rec.AllocatedAddress),
		Hostname: domain.ListenerVIPHostname(rec.ID, rec.RegionID),
	}
}

func listenerProtocolToPb(p domain.LbProto) (lbv1.Listener_Protocol, error) {
	switch p {
	case domain.ProtoTCP:
		return lbv1.Listener_TCP, nil
	case domain.ProtoUDP:
		return lbv1.Listener_UDP, nil
	}
	return lbv1.Listener_PROTOCOL_UNSPECIFIED, fmt.Errorf("unknown LbProto: %q", p)
}

func ipVersionToPb(v domain.IPVersion) (lbv1.IpVersion, error) {
	switch v {
	case domain.IPVersionV4:
		return lbv1.IpVersion_IPV4, nil
	case domain.IPVersionV6:
		return lbv1.IpVersion_IPV6, nil
	}
	return lbv1.IpVersion_IP_VERSION_UNSPECIFIED, fmt.Errorf("unknown IPVersion: %q", v)
}

func listenerStatusToPb(s domain.ListenerStatus) (lbv1.Listener_Status, error) {
	switch s {
	case domain.ListenerStatusCreating:
		return lbv1.Listener_CREATING, nil
	case domain.ListenerStatusActive:
		return lbv1.Listener_ACTIVE, nil
	case domain.ListenerStatusUpdating:
		return lbv1.Listener_UPDATING, nil
	case domain.ListenerStatusDeleting:
		return lbv1.Listener_DELETING, nil
	}
	return lbv1.Listener_STATUS_UNSPECIFIED, fmt.Errorf("unknown ListenerStatus: %q", s)
}

func init() {
	dto.RegTransfer(dto.Fn2Face(listener{}.toPb))
}
