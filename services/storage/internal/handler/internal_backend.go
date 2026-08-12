// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

// Хендлеры двух внутренних ресурсов плоскости данных: зарегистрированного бэкенда и
// НЕИЗМЕНЯЕМОЙ ревизии привязки класса к нему.
//
// Оба живут ТОЛЬКО на внутреннем листенере (:9091), gRPC-only, без REST-аннотаций.
// Оба инфра-чувствительны целиком: координата бэкенда, пул и шаблон пространства
// арендатора на публичной поверхности не появляются ни одним полем.
//
// Мутации СИНХРОННЫ и возвращают ресурс: за правкой админ-справочника нет длящейся
// работы, и оборачивать её в операцию значило бы заставить администратора поллить
// готовое.
//
// У ревизии нет ни правки, ни вывода из обращения — и это не упущение, а механизм:
// ресурс ссылается на ревизию, под которой создан, поэтому изменение справочника
// физически не может задним числом изменить его свойства. Держится гейтом
// TestDiskTypeBindingRepoHasNoMutatingPath.

import (
	"context"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktypebinding"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/storagebackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
)

// ── InternalStorageBackendService (:9091) ─────────────────────────────────

// InternalStorageBackendHandler реализует storagev1.InternalStorageBackendServiceServer.
type InternalStorageBackendHandler struct {
	storagev1.UnimplementedInternalStorageBackendServiceServer
	uc *storagebackend.UseCase
}

// NewInternalStorageBackendHandler конструирует хендлер.
func NewInternalStorageBackendHandler(uc *storagebackend.UseCase) *InternalStorageBackendHandler {
	return &InternalStorageBackendHandler{uc: uc}
}

// Create регистрирует бэкенд.
func (h *InternalStorageBackendHandler) Create(ctx context.Context, req *storagev1.CreateStorageBackendRequest) (*storagev1.StorageBackend, error) {
	b := &domain.StorageBackend{
		Name:           req.GetName(),
		Kind:           backendKindFromProto(req.GetKind()),
		Description:    req.GetDescription(),
		ZoneIDs:        req.GetZoneIds(),
		Endpoint:       req.GetEndpoint(),
		CredentialsRef: domain.CredentialsRef(req.GetCredentialsRef()),
		Status:         backendStatusFromProto(req.GetStatus()),
	}
	created, err := h.uc.Create(ctx, b)
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.StorageBackend(created), nil
}

// Get возвращает бэкенд.
func (h *InternalStorageBackendHandler) Get(ctx context.Context, req *storagev1.GetStorageBackendRequest) (*storagev1.StorageBackend, error) {
	b, err := h.uc.Get(ctx, req.GetStorageBackendId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.StorageBackend(b), nil
}

// List возвращает страницу бэкендов.
func (h *InternalStorageBackendHandler) List(ctx context.Context, req *storagev1.ListStorageBackendsRequest) (*storagev1.ListStorageBackendsResponse, error) {
	items, next, err := h.uc.List(ctx, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &storagev1.ListStorageBackendsResponse{NextPageToken: next}
	for _, b := range items {
		resp.StorageBackends = append(resp.StorageBackends, protoconv.StorageBackend(b))
	}
	return resp, nil
}

// Update меняет изменяемые поля бэкенда ПО МАСКЕ: не названное маской не пишется.
func (h *InternalStorageBackendHandler) Update(ctx context.Context, req *storagev1.UpdateStorageBackendRequest) (*storagev1.StorageBackend, error) {
	var upd storagebackend.Update
	named := namedBy(req.GetUpdateMask().GetPaths())

	if named("name") {
		v := req.GetName()
		upd.Name = &v
	}
	if named("description") {
		v := req.GetDescription()
		upd.Description = &v
	}
	if named("zone_ids") {
		v := req.GetZoneIds()
		upd.ZoneIDs = &v
	}
	if named("endpoint") {
		v := req.GetEndpoint()
		upd.Endpoint = &v
	}
	if named("credentials_ref") {
		v := domain.CredentialsRef(req.GetCredentialsRef())
		upd.CredentialsRef = &v
	}
	if named("status") {
		v := backendStatusFromProto(req.GetStatus())
		upd.Status = &v
	}

	updated, err := h.uc.UpdateAdmin(ctx, req.GetStorageBackendId(), upd)
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.StorageBackend(updated), nil
}

// Delete снимает регистрацию бэкенда.
func (h *InternalStorageBackendHandler) Delete(ctx context.Context, req *storagev1.DeleteStorageBackendRequest) (*storagev1.DeleteStorageBackendResponse, error) {
	if err := h.uc.Delete(ctx, req.GetStorageBackendId()); err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return &storagev1.DeleteStorageBackendResponse{}, nil
}

// ── InternalDiskTypeBindingService (:9091) ────────────────────────────────

// InternalDiskTypeBindingHandler реализует storagev1.InternalDiskTypeBindingServiceServer.
type InternalDiskTypeBindingHandler struct {
	storagev1.UnimplementedInternalDiskTypeBindingServiceServer
	uc *disktypebinding.UseCase
}

// NewInternalDiskTypeBindingHandler конструирует хендлер.
func NewInternalDiskTypeBindingHandler(uc *disktypebinding.UseCase) *InternalDiskTypeBindingHandler {
	return &InternalDiskTypeBindingHandler{uc: uc}
}

// Create заводит НОВУЮ ревизию и вытесняет прежнюю. Номер ревизии и её состояние
// назначает регистрация, а не вызывающий: названное им отвергается явно.
func (h *InternalDiskTypeBindingHandler) Create(ctx context.Context, req *storagev1.CreateDiskTypeBindingRequest) (*storagev1.DiskTypeBinding, error) {
	b := &domain.DiskTypeBinding{
		DiskTypeID: req.GetDiskTypeId(),
		ZoneID:     req.GetZoneId(),
		BackendID:  req.GetBackendId(),
		Locator: domain.BindingLocator{
			Pool:              req.GetLocator().GetPool(),
			NamespaceTemplate: req.GetLocator().GetNamespaceTemplate(),
		},
		Capabilities: domain.BindingCapabilities{
			Snapshots:         req.GetCapabilities().GetSnapshots(),
			CloneFromSnapshot: req.GetCapabilities().GetCloneFromSnapshot(),
			CloneFromImage:    req.GetCapabilities().GetCloneFromImage(),
			CloneKeepsParent:  req.GetCapabilities().GetCloneKeepsParent(),
			OnlineGrow:        req.GetCapabilities().GetOnlineGrow(),
			MultiAttach:       req.GetCapabilities().GetMultiAttach(),
			EncryptionAtRest:  req.GetCapabilities().GetEncryptionAtRest(),
			TrashTTLSeconds:   req.GetCapabilities().GetTrashTtlSeconds(),
		},
		QoS: domain.BindingQoS{
			BaselineIOPS:            req.GetQos().GetBaselineIops(),
			IOPSPerGiB:              req.GetQos().GetIopsPerGib(),
			MaxIOPS:                 req.GetQos().GetMaxIops(),
			BaselineThroughputMiBps: req.GetQos().GetBaselineThroughputMibps(),
			ThroughputPerGiBMiBps:   req.GetQos().GetThroughputPerGibMibps(),
			MaxThroughputMiBps:      req.GetQos().GetMaxThroughputMibps(),
		},
	}
	created, err := h.uc.Register(ctx, b)
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.DiskTypeBinding(created), nil
}

// Get возвращает ревизию.
func (h *InternalDiskTypeBindingHandler) Get(ctx context.Context, req *storagev1.GetDiskTypeBindingRequest) (*storagev1.DiskTypeBinding, error) {
	b, err := h.uc.Get(ctx, req.GetDiskTypeBindingId())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	return protoconv.DiskTypeBinding(b), nil
}

// List возвращает страницу ревизий.
func (h *InternalDiskTypeBindingHandler) List(ctx context.Context, req *storagev1.ListDiskTypeBindingsRequest) (*storagev1.ListDiskTypeBindingsResponse, error) {
	items, next, err := h.uc.List(ctx, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, serviceerr.ToStatus(err)
	}
	resp := &storagev1.ListDiskTypeBindingsResponse{NextPageToken: next}
	for _, b := range items {
		resp.DiskTypeBindings = append(resp.DiskTypeBindings, protoconv.DiskTypeBinding(b))
	}
	return resp, nil
}

// namedBy — общий предикат маски правки: пустая маска означает полный PATCH
// изменяемых полей (единая дисциплина платформы), непустая — ровно названные.
func namedBy(paths []string) func(string) bool {
	return func(field string) bool {
		if len(paths) == 0 {
			return true
		}
		for _, p := range paths {
			if p == field {
				return true
			}
		}
		return false
	}
}

func backendKindFromProto(k storagev1.StorageBackend_BackendKind) domain.BackendKind {
	if k == storagev1.StorageBackend_CEPH_RBD {
		return domain.BackendKindCephRBD
	}
	return ""
}

// backendStatusFromProto переводит состояние бэкенда. UNSPECIFIED даёт ПУСТОЕ
// значение, а не ACTIVE: умолчание проставляет use-case в одном названном месте, и
// конверсия не вправе решать это за него — иначе опечатка администратора стала бы
// намерением.
func backendStatusFromProto(s storagev1.StorageBackend_Status) domain.BackendStatus {
	switch s {
	case storagev1.StorageBackend_ACTIVE:
		return domain.BackendStatusActive
	case storagev1.StorageBackend_DRAINING:
		return domain.BackendStatusDraining
	case storagev1.StorageBackend_DISABLED:
		return domain.BackendStatusDisabled
	default:
		return ""
	}
}
