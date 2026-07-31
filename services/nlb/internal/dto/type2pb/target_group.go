// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package type2pb

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// targetGroup — трансфер kachorepo.TargetGroupRecord → *lbv1.TargetGroup.
//
// Inline-вставляет Target через target{}.toPb (Targets — embedded child).
// HealthCheck — через healthCheckToPb helper (не registry-transfer; см.
// health_check.go).
type targetGroup struct{}

func (targetGroup) toPb(rec kachorepo.TargetGroupRecord) (*lbv1.TargetGroup, error) {
	ts, err := timeObj{}.toPb(rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	statusPb, err := tgStatusToPb(rec.Status)
	if err != nil {
		return nil, err
	}
	// Проекция строится из TargetStates — набора, который несёт lifecycle-
	// состояние цели. Прежде она собиралась из доменного `Targets`, у которого
	// состояния нет вовсе: снятая цель оставалась в наборе до истечения
	// задержки и выглядела обычной. Запасного пути на `Targets` намеренно нет —
	// «состояние неизвестно» проецировалось бы как «активна», то есть ровно тем
	// утверждением, которое и было неверным.
	var targetsPb []*lbv1.Target
	if len(rec.TargetStates) > 0 {
		targetsPb = make([]*lbv1.Target, 0, len(rec.TargetStates))
		for _, t := range rec.TargetStates {
			tpb, err := target{}.toPb(t)
			if err != nil {
				return nil, fmt.Errorf("convert target: %w", err)
			}
			targetsPb = append(targetsPb, tpb)
		}
	}
	return &lbv1.TargetGroup{
		Id:                  string(rec.ID),
		ProjectId:           string(rec.ProjectID),
		CreatedAt:           ts,
		Name:                string(rec.Name),
		Description:         string(rec.Description),
		Labels:              domain.LabelsToMap(rec.Labels),
		RegionId:            string(rec.RegionID),
		Targets:             targetsPb,
		HealthCheck:         healthCheckToPb(rec.HealthCheck, rec.Port),
		DeregistrationDelay: durationpb.New(time.Duration(rec.DeregistrationDelay)),
		SlowStart:           durationpb.New(time.Duration(rec.SlowStart)),
		Status:              statusPb,
		Port:                int32(rec.Port),
	}, nil
}

func tgStatusToPb(s domain.TargetGroupStatus) (lbv1.TargetGroup_Status, error) {
	switch s {
	case domain.TargetGroupStatusActive:
		return lbv1.TargetGroup_ACTIVE, nil
	case domain.TargetGroupStatusDeleting:
		return lbv1.TargetGroup_DELETING, nil
	}
	return lbv1.TargetGroup_STATUS_UNSPECIFIED, fmt.Errorf("unknown TargetGroupStatus: %q", s)
}

func init() {
	dto.RegTransfer(dto.Fn2Face(targetGroup{}.toPb))
}
