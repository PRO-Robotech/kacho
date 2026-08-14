// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto

import (
	reference "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/reference"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/safeconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// cidrGroup — receiver-объект под трансфер kachorepo.CidrGroupRecord →
// *vpcv1.CidrGroup.
type cidrGroup struct{}

// toPb формирует *vpcv1.CidrGroup из repo-entity. CreatedAt — усечение до секунд
// через тот же time-трансфер, что и у остальных ресурсов: микросекунды базы на
// провод не текут.
func (cidrGroup) toPb(rec kachorepo.CidrGroupRecord) (*vpcv1.CidrGroup, error) {
	ts, err := (timeObj{}).toPb(rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	p := &vpcv1.CidrGroup{
		Id:          rec.ID,
		ProjectId:   rec.ProjectID,
		CreatedAt:   ts,
		Name:        string(rec.Name),
		Description: string(rec.Description),
		Labels:      domain.LabelsToMap(rec.Labels),
		// Состав отдаётся как есть — порядок задаёт слой хранения и он
		// детерминирован, поэтому два одинаковых чтения не выглядят правкой.
		V4CidrBlocks: rec.V4CidrBlocks,
		V6CidrBlocks: rec.V6CidrBlocks,
		// Число членов считается ОДНОЙ функцией домена, а не пересчитывается
		// здесь: два места об одном числе разошлись бы молча.
		CidrBlockCount: safeconv.IntToInt32(rec.CidrGroupBlockCount()),
	}
	// used_by — потребители набора: группы правил, чьи правила на него
	// ссылаются. Выводится на чтении из проекции ссылок, поэтому у поля есть
	// производитель, а не объявление. Наружу уезжают вид, идентификатор и имя
	// группы; идентификаторы правил — нет.
	for _, ref := range rec.UsedBy {
		p.UsedBy = append(p.UsedBy, &reference.Reference{
			Referrer: &reference.Referrer{
				Type: "vpc.securityGroup",
				Id:   ref.SecurityGroupID,
				Name: ref.SecurityGroupName,
			},
			Type: reference.Reference_USED_BY,
			// owned=false всегда: группа правил ссылается на набор, но не владеет
			// им — снятие правила набора не удаляет.
			Owned: false,
		})
	}
	return p, nil
}

func init() {
	dto.RegTransfer(dto.Fn2Face(cidrGroup{}.toPb))
}
