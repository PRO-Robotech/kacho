// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto

import (
	reference "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/reference"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// networkInterface — receiver-объект под трансфер kacho.NetworkInterfaceRecord →
// *vpcv1.NetworkInterface; parity с network.go. Принимает repo-entity
// (NetworkInterfaceRecord), потому что в pb-выходе требуется CreatedAt — он
// живет в repo-проекции, не в domain.NetworkInterface. DTO — мост между
// repo-entity и proto, поэтому импорт kachorepo здесь уместен.
type networkInterface struct{}

// toPb формирует *vpcv1.NetworkInterface из repo-entity. CreatedAt — truncate
// до секунд через inline вызов time-трансфера.
//
// Проекция чисто control-plane (без инфра/data-plane-полей) — kacho-vpc их не
// хранит.
func (networkInterface) toPb(rec kachorepo.NetworkInterfaceRecord) (*vpcv1.NetworkInterface, error) {
	ts, err := (timeObj{}).toPb(rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	p := &vpcv1.NetworkInterface{
		Id:               rec.ID,
		ProjectId:        rec.ProjectID,
		CreatedAt:        ts,
		Name:             string(rec.Name),
		Description:      string(rec.Description),
		Labels:           domain.LabelsToMap(rec.Labels),
		SubnetId:         rec.SubnetID,
		V4AddressIds:     rec.V4AddressIDs,
		V6AddressIds:     rec.V6AddressIDs,
		SecurityGroupIds: rec.SecurityGroupIDs,
		MacAddress:       rec.MAC,
		Status:           niStatusToPb(rec.Status),
		// Ноль на выходе означает «ограничения нет» — то же, что и на входе. Особой
		// ветви под него нет намеренно: второе представление отсутствия однажды
		// разошлось бы с первым.
		BandwidthLimitMbps: rec.BandwidthLimitMbps,
	}
	// used_by (kacho extension, output-only) — кто приаттачил этот NIC.
	// Shape — как у Address.used_by: Reference{referrer{type,id}, type=USED_BY}.
	if rec.UsedByID != "" {
		p.UsedBy = &reference.Reference{
			Referrer: &reference.Referrer{Type: rec.UsedByType, Id: rec.UsedByID},
			Type:     reference.Reference_USED_BY,
		}
	}
	return p, nil
}

// niStatusToPb — состояние привязки в контрактный вид.
//
// Ветвей ровно столько, сколько значений у перечисления контракта: три снятых
// значения статуса не производятся ни одним путём записи, поэтому ветви под них
// были бы недостижимым кодом, «документирующим» контракт, которого нет.
func niStatusToPb(s domain.NetworkInterfaceStatus) vpcv1.NetworkInterface_Status {
	switch s {
	case domain.NIStatusActive:
		return vpcv1.NetworkInterface_ACTIVE
	case domain.NIStatusAvailable:
		return vpcv1.NetworkInterface_AVAILABLE
	}
	return vpcv1.NetworkInterface_STATUS_UNSPECIFIED
}

func init() {
	dto.RegTransfer(dto.Fn2Face(networkInterface{}.toPb))
}
