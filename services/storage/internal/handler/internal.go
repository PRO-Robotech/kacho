// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

// Cluster-internal хендлеры kacho-storage (:9091, НЕ на внешнем TLS endpoint —
// ban #6). InternalVolumeService (Attach/Detach/ListAttachments/GetInternal) —
// ребро compute→storage + инфра-проекция; InternalDiskTypeService (admin CRUD,
// sync). Всё регистрируется ТОЛЬКО на internal-листенере в composition root.
//
// per-RPC authz Check (system_admin для мутаций, system_viewer для read) энфорсится
// интерсептором обоих листенеров (собран composition root'ом serve.go, security.md).
// Attach/Detach/ListAttachments + admin CRUD реализованы; GetInternal (infra-
// проекция) — анкер data-plane (§0.3, out of scope).

import (
	"context"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// ── InternalVolumeService (:9091) ─────────────────────────────────────────

// InternalVolumeHandler реализует storagev1.InternalVolumeServiceServer.
type InternalVolumeHandler struct {
	storagev1.UnimplementedInternalVolumeServiceServer
	uc *volume.UseCase
}

// NewInternalVolumeHandler конструирует InternalVolumeHandler.
func NewInternalVolumeHandler(uc *volume.UseCase) *InternalVolumeHandler {
	return &InternalVolumeHandler{uc: uc}
}

// Attach — атомарный CAS-insert строки volume_attachments (идемпотентно на replay).
// Self-describing payload несёт placement инстанса (project/zone) для CAS-когерентности;
// storage сверяет свою строку volumes и не зовёт compute (ацикличность).
func (h *InternalVolumeHandler) Attach(ctx context.Context, req *storagev1.AttachVolumeRequest) (*storagev1.AttachVolumeResponse, error) {
	a := &domain.VolumeAttachment{
		VolumeID:     req.GetVolumeId(),
		InstanceID:   req.GetInstanceId(),
		InstanceName: req.GetInstanceName(),
		ProjectID:    req.GetProjectId(),
		ZoneID:       req.GetInstanceZoneId(),
		DeviceName:   req.GetDeviceName(),
		IsBoot:       req.GetIsBoot(),
		Mode:         domain.AttachmentMode(req.GetMode()),
		AutoDelete:   req.GetAutoDelete(),
	}
	v, err := h.uc.Attach(ctx, a)
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return &storagev1.AttachVolumeResponse{Volume: protoconv.Volume(v)}, nil
}

// Detach — идемпотентное удаление строки volume_attachments (0 rows → OK).
func (h *InternalVolumeHandler) Detach(ctx context.Context, req *storagev1.DetachVolumeRequest) (*storagev1.DetachVolumeResponse, error) {
	v, err := h.uc.Detach(ctx, req.GetVolumeId(), req.GetInstanceId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return &storagev1.DetachVolumeResponse{Volume: protoconv.Volume(v)}, nil
}

// ListAttachments — батч-чтение attachments по instance_id (compute-mirror, не N+1).
func (h *InternalVolumeHandler) ListAttachments(ctx context.Context, req *storagev1.ListAttachmentsRequest) (*storagev1.ListAttachmentsResponse, error) {
	atts, err := h.uc.ListAttachments(ctx, req.GetInstanceIds())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &storagev1.ListAttachmentsResponse{}
	for _, a := range atts {
		resp.Attachments = append(resp.Attachments, protoconv.VolumeAttachmentInfo(a))
	}
	return resp, nil
}

// GetInternal — full (infra) проекция Volume (internal-only).
func (h *InternalVolumeHandler) GetInternal(ctx context.Context, req *storagev1.GetInternalVolumeRequest) (*storagev1.VolumeInternal, error) {
	v, err := h.uc.GetInternal(ctx, req.GetVolumeId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return &storagev1.VolumeInternal{Volume: protoconv.Volume(v)}, nil
}

// ── InternalDiskTypeService (:9091, admin CRUD, sync) ─────────────────────

// InternalDiskTypeHandler реализует storagev1.InternalDiskTypeServiceServer.
type InternalDiskTypeHandler struct {
	storagev1.UnimplementedInternalDiskTypeServiceServer
	uc *disktype.UseCase
}

// NewInternalDiskTypeHandler конструирует InternalDiskTypeHandler.
func NewInternalDiskTypeHandler(uc *disktype.UseCase) *InternalDiskTypeHandler {
	return &InternalDiskTypeHandler{uc: uc}
}

// Create создаёт DiskType (admin, sync).
func (h *InternalDiskTypeHandler) Create(ctx context.Context, req *storagev1.CreateDiskTypeRequest) (*storagev1.DiskType, error) {
	d := &domain.DiskType{
		ID:              req.GetId(),
		Name:            req.GetName(),
		Description:     req.GetDescription(),
		ZoneIDs:         req.GetZoneIds(),
		PerformanceTier: req.GetPerformanceTier(),
	}
	created, err := h.uc.CreateAdmin(ctx, d)
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.DiskType(created), nil
}

// Update меняет mutable-поля DiskType (admin, sync, full-replace — proto без
// FieldMask). id immutable (path-param).
func (h *InternalDiskTypeHandler) Update(ctx context.Context, req *storagev1.UpdateDiskTypeRequest) (*storagev1.DiskType, error) {
	updated, err := h.uc.UpdateAdmin(ctx, req.GetDiskTypeId(), req.GetName(), req.GetDescription(),
		req.GetZoneIds(), req.GetPerformanceTier())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.DiskType(updated), nil
}

// Delete удаляет DiskType (admin, sync).
func (h *InternalDiskTypeHandler) Delete(ctx context.Context, req *storagev1.DeleteDiskTypeRequest) (*storagev1.DeleteDiskTypeResponse, error) {
	if err := h.uc.DeleteAdmin(ctx, req.GetDiskTypeId()); err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return &storagev1.DeleteDiskTypeResponse{}, nil
}
