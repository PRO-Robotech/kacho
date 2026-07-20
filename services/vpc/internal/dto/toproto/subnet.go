// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto

import (
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// subnet — receiver-объект под трансфер kachorepo.SubnetRecord → *vpcv1.Subnet.
type subnet struct{}

// toPb формирует *vpcv1.Subnet из repo-entity. CreatedAt — truncate до секунд
// через inline вызов time-трансфера.
func (subnet) toPb(rec kachorepo.SubnetRecord) (*vpcv1.Subnet, error) {
	ts, err := (timeObj{}).toPb(rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	// VPC-1 F7: the flat repo array splits into the redesign proto shape —
	// ipv4_cidr_primary = blocks[0] (immutable placement anchor), ipv4_cidr_blocks =
	// blocks[1:] (additional ranges added via AddCidrBlocks). Same split for v6.
	v4Primary, v4Additional := splitCidrPrimary(rec.V4CidrBlocks)
	v6Primary, v6Additional := splitCidrPrimary(rec.V6CidrBlocks)
	p := &vpcv1.Subnet{
		Id:              rec.ID,
		ProjectId:       rec.ProjectID,
		CreatedAt:       ts,
		Name:            string(rec.Name),
		Description:     string(rec.Description),
		Labels:          domain.LabelsToMap(rec.Labels),
		NetworkId:       rec.NetworkID,
		PlacementType:   placementToPb(rec.PlacementType),
		ZoneId:          rec.ZoneID,
		RegionId:        rec.RegionID,
		Ipv4CidrPrimary: v4Primary,
		Ipv4CidrBlocks:  v4Additional,
		Ipv6CidrPrimary: v6Primary,
		Ipv6CidrBlocks:  v6Additional,
		RouteTableId:    rec.RouteTableID,
	}
	// VPC-1-43: DhcpOptions dropped by design — no dhcp_options on the public
	// Subnet contract (Network-level DHCP/DNS-resolver knobs absent).
	return p, nil
}

// splitCidrPrimary разбивает плоский repo-массив CIDR-блоков на primary-anchor
// (blocks[0], immutable) + additional (blocks[1:], мутируется через verb-pair).
// Пустой массив → пустой primary и nil additional (v-only subnet: primary="").
func splitCidrPrimary(blocks []string) (primary string, additional []string) {
	if len(blocks) == 0 {
		return "", nil
	}
	if len(blocks) == 1 {
		return blocks[0], nil
	}
	return blocks[0], blocks[1:]
}

// placementToPb — domain-дискриминатор → proto-enum. Пустое (UNSPECIFIED)
// доменное значение в выдаче не появляется (CHECK гарантирует ZONAL/REGIONAL),
// но маппится в UNSPECIFIED defensively.
func placementToPb(p domain.SubnetPlacementType) vpcv1.SubnetPlacementType {
	switch p {
	case domain.PlacementZonal:
		return vpcv1.SubnetPlacementType_ZONAL
	case domain.PlacementRegional:
		return vpcv1.SubnetPlacementType_REGIONAL
	default:
		return vpcv1.SubnetPlacementType_SUBNET_PLACEMENT_TYPE_UNSPECIFIED
	}
}

func init() {
	dto.RegTransfer(dto.Fn2Face(subnet{}.toPb))
}
