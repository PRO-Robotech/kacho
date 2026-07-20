// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — тонкий gRPC-transport kacho-storage (parse → use-case →
// format, БЕЗ бизнес-логики). Здесь публичные хендлеры (:9090):
// VolumeService/SnapshotService/DiskTypeService; admin/cluster-internal —
// в internal.go (:9091). Тела делегируют use-case; конверсия domain↔proto —
// через protoconv; ошибки — через serviceerr.
//
// Мутационные детали (update_mask discipline, malformed-id-first, извлечение полей)
// живут в use-case (api-conventions.md) — тонкий handler их НЕ дублирует.
package handler

import (
	"context"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// ── VolumeService (public :9090) ──────────────────────────────────────────

// VolumeHandler реализует storagev1.VolumeServiceServer.
type VolumeHandler struct {
	storagev1.UnimplementedVolumeServiceServer
	uc *volume.UseCase
}

// NewVolumeHandler конструирует VolumeHandler.
func NewVolumeHandler(uc *volume.UseCase) *VolumeHandler { return &VolumeHandler{uc: uc} }

// Get возвращает Volume по id.
func (h *VolumeHandler) Get(ctx context.Context, req *storagev1.GetVolumeRequest) (*storagev1.Volume, error) {
	v, err := h.uc.Get(ctx, req.GetVolumeId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.Volume(v), nil
}

// List возвращает тома проекта (cursor-пагинация).
func (h *VolumeHandler) List(ctx context.Context, req *storagev1.ListVolumesRequest) (*storagev1.ListVolumesResponse, error) {
	vols, next, err := h.uc.List(ctx, volume.Pagination{
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
		ProjectID: req.GetProjectId(),
		Filter:    req.GetFilter(),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &storagev1.ListVolumesResponse{NextPageToken: next}
	for _, v := range vols {
		resp.Volumes = append(resp.Volumes, protoconv.Volume(v))
	}
	return resp, nil
}

// Create создаёт Volume (async Operation).
func (h *VolumeHandler) Create(ctx context.Context, req *storagev1.CreateVolumeRequest) (*operationpb.Operation, error) {
	v := &domain.Volume{
		ProjectID:      req.GetProjectId(),
		Name:           req.GetName(),
		Description:    req.GetDescription(),
		Labels:         req.GetLabels(),
		ZoneID:         req.GetZoneId(),
		DiskTypeID:     req.GetDiskTypeId(),
		SizeBytes:      req.GetSizeBytes(),
		BlockSize:      req.GetBlockSize(),
		SourceSnapshot: req.GetSourceSnapshotId(),
		SourceImage:    req.GetSourceImageId(),
	}
	op, err := h.uc.Create(ctx, v)
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Update меняет mutable-поля Volume (async Operation). Тонкий transport: извлекает
// update_mask + значения тела и делегирует use-case (immutable-switch → UpdateMask →
// full-PATCH-семантика живут в use-case, api-conventions.md).
func (h *VolumeHandler) Update(ctx context.Context, req *storagev1.UpdateVolumeRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Update(ctx, req.GetVolumeId(), req.GetUpdateMask().GetPaths(),
		req.GetName(), req.GetDescription(), req.GetLabels(), req.GetSizeBytes())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Delete удаляет Volume (async Operation).
func (h *VolumeHandler) Delete(ctx context.Context, req *storagev1.DeleteVolumeRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Delete(ctx, req.GetVolumeId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// ListOperations возвращает операции по Volume.
func (h *VolumeHandler) ListOperations(ctx context.Context, req *storagev1.ListVolumeOperationsRequest) (*storagev1.ListVolumeOperationsResponse, error) {
	ops, next, err := h.uc.ListOperations(ctx, req.GetVolumeId(), volume.Pagination{PageSize: req.GetPageSize(), PageToken: req.GetPageToken()})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &storagev1.ListVolumeOperationsResponse{NextPageToken: next}
	for i := range ops {
		resp.Operations = append(resp.Operations, operationToProto(&ops[i]))
	}
	return resp, nil
}

// ── SnapshotService (public :9090) ────────────────────────────────────────

// SnapshotHandler реализует storagev1.SnapshotServiceServer.
type SnapshotHandler struct {
	storagev1.UnimplementedSnapshotServiceServer
	uc *snapshot.UseCase
}

// NewSnapshotHandler конструирует SnapshotHandler.
func NewSnapshotHandler(uc *snapshot.UseCase) *SnapshotHandler { return &SnapshotHandler{uc: uc} }

// Get возвращает Snapshot по id.
func (h *SnapshotHandler) Get(ctx context.Context, req *storagev1.GetSnapshotRequest) (*storagev1.Snapshot, error) {
	s, err := h.uc.Get(ctx, req.GetSnapshotId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.Snapshot(s), nil
}

// List возвращает снимки проекта (cursor-пагинация).
func (h *SnapshotHandler) List(ctx context.Context, req *storagev1.ListSnapshotsRequest) (*storagev1.ListSnapshotsResponse, error) {
	snaps, next, err := h.uc.List(ctx, snapshot.Pagination{
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
		ProjectID: req.GetProjectId(),
		Filter:    req.GetFilter(),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &storagev1.ListSnapshotsResponse{NextPageToken: next}
	for _, s := range snaps {
		resp.Snapshots = append(resp.Snapshots, protoconv.Snapshot(s))
	}
	return resp, nil
}

// Create создаёт Snapshot тома (async Operation).
func (h *SnapshotHandler) Create(ctx context.Context, req *storagev1.CreateSnapshotRequest) (*operationpb.Operation, error) {
	s := &domain.Snapshot{
		ProjectID:      req.GetProjectId(),
		Name:           req.GetName(),
		Description:    req.GetDescription(),
		Labels:         req.GetLabels(),
		SourceVolumeID: req.GetSourceVolumeId(),
	}
	op, err := h.uc.Create(ctx, s)
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Update меняет mutable-поля Snapshot (async Operation). Тонкий transport:
// извлекает update_mask + значения тела, делегирует use-case (immutable-switch →
// UpdateMask → full-PATCH-семантика — в use-case, api-conventions.md).
func (h *SnapshotHandler) Update(ctx context.Context, req *storagev1.UpdateSnapshotRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Update(ctx, req.GetSnapshotId(), req.GetUpdateMask().GetPaths(),
		req.GetName(), req.GetDescription(), req.GetLabels())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Delete удаляет Snapshot (async Operation).
func (h *SnapshotHandler) Delete(ctx context.Context, req *storagev1.DeleteSnapshotRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Delete(ctx, req.GetSnapshotId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// ── ImageService (public :9090) ───────────────────────────────────────────

// ImageHandler реализует storagev1.ImageServiceServer.
type ImageHandler struct {
	storagev1.UnimplementedImageServiceServer
	uc *image.UseCase
}

// NewImageHandler конструирует ImageHandler.
func NewImageHandler(uc *image.UseCase) *ImageHandler { return &ImageHandler{uc: uc} }

// Get возвращает Image по id.
func (h *ImageHandler) Get(ctx context.Context, req *storagev1.GetImageRequest) (*storagev1.Image, error) {
	i, err := h.uc.Get(ctx, req.GetImageId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.Image(i), nil
}

// List возвращает образы проекта (cursor-пагинация).
func (h *ImageHandler) List(ctx context.Context, req *storagev1.ListImagesRequest) (*storagev1.ListImagesResponse, error) {
	imgs, next, err := h.uc.List(ctx, image.Pagination{
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
		ProjectID: req.GetProjectId(),
		Filter:    req.GetFilter(),
	})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &storagev1.ListImagesResponse{NextPageToken: next}
	for _, i := range imgs {
		resp.Images = append(resp.Images, protoconv.Image(i))
	}
	return resp, nil
}

// Create создаёт Image (async Operation).
func (h *ImageHandler) Create(ctx context.Context, req *storagev1.CreateImageRequest) (*operationpb.Operation, error) {
	i := &domain.Image{
		ProjectID:      req.GetProjectId(),
		Name:           req.GetName(),
		Description:    req.GetDescription(),
		Labels:         req.GetLabels(),
		RegionID:       req.GetRegionId(),
		SourceSnapshot: req.GetSourceSnapshotId(),
		SourceVolume:   req.GetSourceVolumeId(),
	}
	op, err := h.uc.Create(ctx, i)
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Update меняет mutable-поля Image (async Operation). Тонкий transport: делегирует
// use-case (immutable-switch → UpdateMask → full-PATCH-семантика — в use-case).
func (h *ImageHandler) Update(ctx context.Context, req *storagev1.UpdateImageRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Update(ctx, req.GetImageId(), req.GetUpdateMask().GetPaths(),
		req.GetName(), req.GetDescription(), req.GetLabels())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// Delete удаляет Image (async Operation).
func (h *ImageHandler) Delete(ctx context.Context, req *storagev1.DeleteImageRequest) (*operationpb.Operation, error) {
	op, err := h.uc.Delete(ctx, req.GetImageId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return operationToProto(op), nil
}

// ListOperations возвращает операции по Image.
func (h *ImageHandler) ListOperations(ctx context.Context, req *storagev1.ListImageOperationsRequest) (*storagev1.ListImageOperationsResponse, error) {
	ops, next, err := h.uc.ListOperations(ctx, req.GetImageId(), image.Pagination{PageSize: req.GetPageSize(), PageToken: req.GetPageToken()})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &storagev1.ListImageOperationsResponse{NextPageToken: next}
	for i := range ops {
		resp.Operations = append(resp.Operations, operationToProto(&ops[i]))
	}
	return resp, nil
}

// ── DiskTypeService (public :9090, read-only) ─────────────────────────────

// DiskTypeHandler реализует storagev1.DiskTypeServiceServer (public read-only;
// admin CRUD — InternalDiskTypeService на :9091, см. internal.go).
type DiskTypeHandler struct {
	storagev1.UnimplementedDiskTypeServiceServer
	uc *disktype.UseCase
}

// NewDiskTypeHandler конструирует DiskTypeHandler.
func NewDiskTypeHandler(uc *disktype.UseCase) *DiskTypeHandler { return &DiskTypeHandler{uc: uc} }

// Get возвращает DiskType по id.
func (h *DiskTypeHandler) Get(ctx context.Context, req *storagev1.GetDiskTypeRequest) (*storagev1.DiskType, error) {
	d, err := h.uc.Get(ctx, req.GetDiskTypeId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.DiskType(d), nil
}

// List возвращает типы дисков (cursor-пагинация).
func (h *DiskTypeHandler) List(ctx context.Context, req *storagev1.ListDiskTypesRequest) (*storagev1.ListDiskTypesResponse, error) {
	types, next, err := h.uc.List(ctx, disktype.Pagination{PageSize: req.GetPageSize(), PageToken: req.GetPageToken()})
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &storagev1.ListDiskTypesResponse{NextPageToken: next}
	for _, d := range types {
		resp.DiskTypes = append(resp.DiskTypes, protoconv.DiskType(d))
	}
	return resp, nil
}
