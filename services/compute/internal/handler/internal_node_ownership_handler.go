// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/nodeownership"
)

// InternalNodeOwnershipHandler — владение машиной узлом (:9091).
//
// Живёт ТОЛЬКО на внутреннем слушателе: идентификатор узла раскрывает, какие
// машины разных арендаторов стоят на одном железе.
type InternalNodeOwnershipHandler struct {
	computev1.UnimplementedInternalNodeOwnershipServiceServer
	svc *nodeownership.Service
}

// NewInternalNodeOwnershipHandler создаёт обработчик.
func NewInternalNodeOwnershipHandler(s *nodeownership.Service) *InternalNodeOwnershipHandler {
	return &InternalNodeOwnershipHandler{svc: s}
}

// ClaimInstance — узел берёт машину в работу.
func (h *InternalNodeOwnershipHandler) ClaimInstance(
	ctx context.Context, req *computev1.ClaimInstanceRequest,
) (*computev1.ClaimInstanceResponse, error) {
	return h.claim(ctx, nodeownership.ClaimReq{
		InstanceID: req.InstanceId,
		NodeID:     req.NodeId,
		SequenceNo: req.DeliverySequenceNo,
		LeaseUntil: tsOrZero(req.LeaseUntil),
	})
}

// RenewLease — продление аренды тем же обменом.
//
// Один use-case на оба глагола by construction: «просто продлить» было бы
// записью без условия и возвращало бы владение тому, у кого его отобрали.
func (h *InternalNodeOwnershipHandler) RenewLease(
	ctx context.Context, req *computev1.RenewLeaseRequest,
) (*computev1.ClaimInstanceResponse, error) {
	return h.claim(ctx, nodeownership.ClaimReq{
		InstanceID: req.InstanceId,
		NodeID:     req.NodeId,
		SequenceNo: req.DeliverySequenceNo,
		LeaseUntil: tsOrZero(req.LeaseUntil),
	})
}

func (h *InternalNodeOwnershipHandler) claim(
	ctx context.Context, req nodeownership.ClaimReq,
) (*computev1.ClaimInstanceResponse, error) {
	b, err := h.svc.Claim(ctx, req)
	if err != nil {
		return nil, err
	}
	return &computev1.ClaimInstanceResponse{
		NodeId:             b.NodeID,
		DeliverySequenceNo: b.SequenceNo,
		LeaseUntil:         timestamppb.New(b.LeaseUntil.Truncate(time.Second)),
	}, nil
}

// ReleaseInstance — узел отпускает машину.
func (h *InternalNodeOwnershipHandler) ReleaseInstance(
	ctx context.Context, req *computev1.ReleaseInstanceRequest,
) (*computev1.ReleaseInstanceResponse, error) {
	if err := h.svc.Release(ctx, req.InstanceId, req.NodeId); err != nil {
		return nil, err
	}
	return &computev1.ReleaseInstanceResponse{}, nil
}

// tsOrZero переносит время КАК ПРИСЛАНО: пустое остаётся нулевым и отвергается
// use-case'ом по имени поля. Подставить здесь своё значило бы решить за узел,
// до какого момента он просит аренду.
func tsOrZero(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
