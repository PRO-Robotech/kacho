// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
	svc "github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

// ImageHandler реализует computev1.ImageServiceServer (тонкий transport-слой).
// access-bindings RPC наследуются из UnimplementedImageServiceServer (Unimplemented).
type ImageHandler struct {
	computev1.UnimplementedImageServiceServer
	svc        *svc.ImageService
	listFilter authzfilter.Filter
}

// NewImageHandler создаёт ImageHandler. listFilter может быть nil — тогда
// FGA-фильтрация на List отключена (dev/breakglass).
func NewImageHandler(s *svc.ImageService, listFilter authzfilter.Filter) *ImageHandler {
	return &ImageHandler{svc: s, listFilter: listFilter}
}

// Get возвращает Image по id.
func (h *ImageHandler) Get(ctx context.Context, req *computev1.GetImageRequest) (*computev1.Image, error) {
	if req.ImageId == "" {
		return nil, status.Error(codes.InvalidArgument, "image_id required")
	}
	i, err := h.svc.Get(ctx, req.ImageId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, i.ProjectID); err != nil {
		return nil, err
	}
	return protoconv.Image(i), nil
}

// GetLatestByFamily возвращает самый новый Image в family.
//
// Per-object authz: interceptor гейтит этот RPC лишь на project-tier `viewer`
// (target image-id неизвестен до резолва family, поэтому per-object gate на
// interceptor'е невозможен). После резолва образа задаём ПРЯМОЙ per-object вопрос
// о РАЗРЕШЁННОСТИ именно этого id (viewer ∪ v_list, один BatchCheck) — иначе
// project-member с одним `viewer on project` (но без per-object гранта на образе
// X) прочитал бы содержимое X (name/description/labels/min_disk_size/product_ids/
// os), когда X — новейший в своей family (BOLA-lite / over-show, CWE-863).
// Зеркалит per-object gate у Get. Отказ прячет существование (NotFound
// "Image <family> not found", неотличимо от пустой family), а не 403-oracle.
//
// Прежняя форма — членство id в перечислении всех разрешённых образов
// (iam.ListObjects) — упиралась в жёсткий предел OpenFGA (1000 без
// continuation-token'а) и на большом сторе отдавала NotFound для СВОЕГО
// разрешённого образа; см. package-doc `internal/authzfilter`.
func (h *ImageHandler) GetLatestByFamily(ctx context.Context, req *computev1.GetImageLatestByFamilyRequest) (*computev1.Image, error) {
	if err := AssertProjectOwnership(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	i, err := h.svc.GetLatestByFamily(ctx, req.ProjectId, req.Family)
	if err != nil {
		return nil, err
	}
	visible, err := isVisible(ctx, h.listFilter,
		authzfilter.ResourceTypeImage, authzfilter.ActionImageRead, i.ID)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, status.Errorf(codes.NotFound, "Image %s not found", req.Family)
	}
	return protoconv.Image(i), nil
}

// List возвращает список образов в folder.
//
// Страница читается из БД ПЕРВОЙ, затем per-object фильтруется через
// iam.AuthorizeService.BatchCheck (viewer ∪ v_list) — см. list_filter.go.
func (h *ImageHandler) List(ctx context.Context, req *computev1.ListImagesRequest) (*computev1.ListImagesResponse, error) {
	if err := AssertProjectOwnership(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	// Validate pagination BEFORE anything authz-related (see disk_handler).
	if err := svc.ValidateListPagination(svc.Pagination{PageToken: req.PageToken, PageSize: req.PageSize}); err != nil {
		return nil, err
	}
	imgs, nextToken, err := h.svc.List(ctx, svc.ImageFilter{ProjectID: req.ProjectId, Filter: req.Filter},
		svc.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	visible, err := filterVisible(ctx, h.listFilter,
		authzfilter.ResourceTypeImage, authzfilter.ActionImageRead, imgs,
		func(i *domain.Image) string { return i.ID })
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListImagesResponse{NextPageToken: nextToken}
	for _, i := range visible {
		resp.Images = append(resp.Images, protoconv.Image(i))
	}
	return resp, nil
}

// Create инициирует создание Image.
func (h *ImageHandler) Create(ctx context.Context, req *computev1.CreateImageRequest) (*operationpb.Operation, error) {
	if err := AssertProjectOwnership(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	op, err := h.svc.Create(ctx, svc.CreateImageReq{
		ProjectID:          req.ProjectId,
		Name:               req.Name,
		Description:        req.Description,
		Labels:             req.Labels,
		Family:             req.Family,
		MinDiskSize:        req.MinDiskSize,
		ProductIDs:         req.ProductIds,
		ImageID:            req.GetImageId(),
		DiskID:             req.GetDiskId(),
		SnapshotID:         req.GetSnapshotId(),
		URI:                req.GetUri(),
		Os:                 req.Os,
		Pooled:             req.Pooled,
		HardwareGeneration: req.HardwareGeneration,
	})
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Update инициирует обновление Image.
func (h *ImageHandler) Update(ctx context.Context, req *computev1.UpdateImageRequest) (*operationpb.Operation, error) {
	if req.ImageId == "" {
		return nil, status.Error(codes.InvalidArgument, "image_id required")
	}
	i, err := h.svc.Get(ctx, req.ImageId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, i.ProjectID); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	op, err := h.svc.Update(ctx, svc.UpdateImageReq{
		ImageID:     req.ImageId,
		Name:        req.Name,
		Description: req.Description,
		Labels:      req.Labels,
		MinDiskSize: req.MinDiskSize,
		UpdateMask:  mask,
	})
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Delete инициирует удаление Image.
func (h *ImageHandler) Delete(ctx context.Context, req *computev1.DeleteImageRequest) (*operationpb.Operation, error) {
	if req.ImageId == "" {
		return nil, status.Error(codes.InvalidArgument, "image_id required")
	}
	i, err := h.svc.Get(ctx, req.ImageId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, i.ProjectID); err != nil {
		return nil, err
	}
	op, err := h.svc.Delete(ctx, req.ImageId)
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// ListOperations возвращает операции для Image.
func (h *ImageHandler) ListOperations(ctx context.Context, req *computev1.ListImageOperationsRequest) (*computev1.ListImageOperationsResponse, error) {
	if req.ImageId == "" {
		return nil, status.Error(codes.InvalidArgument, "image_id required")
	}
	i, err := h.svc.Get(ctx, req.ImageId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, i.ProjectID); err != nil {
		return nil, err
	}
	ops, nextToken, err := h.svc.ListOperations(ctx, req.ImageId, svc.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListImageOperationsResponse{NextPageToken: nextToken}
	for i := range ops {
		resp.Operations = append(resp.Operations, operationToProto(&ops[i]))
	}
	return resp, nil
}
