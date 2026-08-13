// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"time"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/realization"
)

// InternalRealizationHandler — приём отчётов узлового агента (:9091).
//
// Живёт ТОЛЬКО на внутреннем слушателе: наблюдаемое состояние — факт исполнителя,
// а идентификатор узла и подробности исполнения на публичную поверхность не
// выходят никогда.
type InternalRealizationHandler struct {
	computev1.UnimplementedInternalRealizationServiceServer
	svc *realization.Service
}

// NewInternalRealizationHandler создаёт обработчик.
func NewInternalRealizationHandler(s *realization.Service) *InternalRealizationHandler {
	return &InternalRealizationHandler{svc: s}
}

// ReportInstanceRealization принимает один отчёт.
//
// Время наблюдения переносится КАК ПРИСЛАНО. Пустое поле остаётся нулевым и
// отвергается use-case'ом по имени — подставить здесь своё время значило бы
// решить за узел, когда он наблюдал.
func (h *InternalRealizationHandler) ReportInstanceRealization(
	ctx context.Context, req *computev1.ReportInstanceRealizationRequest,
) (*computev1.ReportInstanceRealizationResponse, error) {
	var observedAt time.Time
	if ts := req.ObservedAt; ts != nil {
		observedAt = ts.AsTime()
	}
	res, err := h.svc.Apply(ctx, realization.Report{
		InstanceID: req.InstanceId,
		State:      req.ObservedState.String(),
		SequenceNo: req.DeliverySequenceNo,
		ObservedAt: observedAt,
		Reason:     req.Reason,
	})
	if err != nil {
		return nil, err
	}
	return &computev1.ReportInstanceRealizationResponse{
		Applied:           res.Applied,
		CurrentSequenceNo: res.CurrentSeq,
	}, nil
}
