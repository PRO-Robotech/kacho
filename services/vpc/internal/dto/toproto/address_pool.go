// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto

import (
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// addressPool — receiver-объект под трансфер kachorepo.AddressPoolRecord →
// *vpcv1.AddressPool.
//
// Потребителей у проекции ДВА — ответ чтения и мутации пула (`addresspool`) и
// состояние события пула в потоке изменений (`subscriptionjournal`), — поэтому
// она живёт в реестре, а не помощником одного из них: второе отображение
// разошлось бы с первым молча, и подписчик получал бы пул, отличный от того,
// что отдаёт `Get`.
type addressPool struct{}

// toPb формирует *vpcv1.AddressPool из repo-entity. CreatedAt — усечение до
// секунд тем же time-трансфером, что у остальных ресурсов: микросекунды базы на
// провод не текут. Имя и метки — из self-validating newtypes.
func (addressPool) toPb(rec kachorepo.AddressPoolRecord) (*vpcv1.AddressPool, error) {
	ts, err := (timeObj{}).toPb(rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &vpcv1.AddressPool{
		Id:               rec.ID,
		CreatedAt:        ts,
		Name:             string(rec.Name),
		Description:      string(rec.Description),
		Labels:           domain.LabelsToMap(rec.Labels),
		V4CidrBlocks:     rec.V4CIDRBlocks,
		V6CidrBlocks:     rec.V6CIDRBlocks,
		Kind:             vpcv1.AddressPoolKind(rec.Kind),
		ZoneId:           rec.ZoneID,
		IsDefault:        rec.IsDefault,
		SelectorLabels:   domain.LabelsToMap(rec.SelectorLabels),
		SelectorPriority: rec.SelectorPriority,
	}, nil
}

func init() {
	dto.RegTransfer(dto.Fn2Face(addressPool{}.toPb))
}
