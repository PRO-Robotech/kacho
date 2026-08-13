// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/guestaccesskey"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
)

// GuestAccessKeyHandler — транспортный слой ключей входа в машину.
//
// Тонкий по построению: разбор материала, проверка владельца-проекта и запуск
// операции живут в use-case, здесь только перевод формы запроса в аргументы и
// формы ответа обратно.
type GuestAccessKeyHandler struct {
	computev1.UnimplementedGuestAccessKeyServiceServer
	svc        *guestaccesskey.Service
	listFilter *listnarrow.Narrower
}

// NewGuestAccessKeyHandler создаёт обработчик.
//
// Сужатель может быть nil — тогда страница не сужается, и это допустимо ТОЛЬКО
// объявленным аварийным режимом: право проекта не отвечает на вопрос «можно ли
// этому вызывающему видеть ЭТИ строки», поэтому без сужения всякий участник
// проекта видел бы каждый ключ проекта.
func NewGuestAccessKeyHandler(s *guestaccesskey.Service, listFilter *listnarrow.Narrower) *GuestAccessKeyHandler {
	return &GuestAccessKeyHandler{svc: s, listFilter: listFilter}
}

// Get возвращает ключ.
func (h *GuestAccessKeyHandler) Get(ctx context.Context, req *computev1.GetGuestAccessKeyRequest) (*computev1.GuestAccessKey, error) {
	k, err := h.svc.Get(ctx, req.GuestAccessKeyId)
	if err != nil {
		return nil, err
	}
	return protoconv.GuestAccessKey(k), nil
}

// List возвращает страницу ключей проекта.
func (h *GuestAccessKeyHandler) List(ctx context.Context, req *computev1.ListGuestAccessKeysRequest) (*computev1.ListGuestAccessKeysResponse, error) {
	keys, next, err := h.svc.List(ctx, req.ProjectId, ports.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	visible, err := listnarrow.Page(ctx, h.listFilter,
		authzfilter.ResourceTypeGuestAccessKey, authzfilter.ActionGuestAccessKeyRead, keys,
		func(k *domain.GuestAccessKey) string { return k.ID })
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListGuestAccessKeysResponse{NextPageToken: next}
	for _, k := range visible {
		resp.GuestAccessKeys = append(resp.GuestAccessKeys, protoconv.GuestAccessKey(k))
	}
	return resp, nil
}

// Create заводит ключ.
func (h *GuestAccessKeyHandler) Create(ctx context.Context, req *computev1.CreateGuestAccessKeyRequest) (*operationpb.Operation, error) {
	op, err := h.svc.Create(ctx, guestaccesskey.CreateReq{
		ProjectID: req.ProjectId,
		Name:      req.Name,
		PublicKey: req.PublicKey,
		Labels:    req.Labels,
	})
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Delete снимает ключ.
func (h *GuestAccessKeyHandler) Delete(ctx context.Context, req *computev1.DeleteGuestAccessKeyRequest) (*operationpb.Operation, error) {
	op, err := h.svc.Delete(ctx, req.GuestAccessKeyId)
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Update правит косметическую часть ключа.
func (h *GuestAccessKeyHandler) Update(ctx context.Context, req *computev1.UpdateGuestAccessKeyRequest) (*operationpb.Operation, error) {
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.GetPaths()
	}
	op, err := h.svc.Update(ctx, guestaccesskey.UpdateReq{
		ID:         req.GuestAccessKeyId,
		UpdateMask: mask,
		Name:       req.Name,
		Labels:     req.Labels,
	})
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// ListOperations возвращает операции над ключом.
//
// Сторож стоит ЗДЕСЬ, до чтения страницы операций: вызывающий, который не видит
// сам ключ, не должен получать его историю. Use-case проверяет существование
// повторно — это не дубль ради надёжности, а свойство разных вызывающих: проверка
// защищает ровно тот путь, который через неё проходит.
func (h *GuestAccessKeyHandler) ListOperations(ctx context.Context, req *computev1.ListGuestAccessKeyOperationsRequest) (*computev1.ListGuestAccessKeyOperationsResponse, error) {
	if _, err := h.svc.Get(ctx, req.GuestAccessKeyId); err != nil {
		return nil, err
	}
	ops, next, err := h.svc.ListOperations(ctx, req.GuestAccessKeyId,
		ports.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListGuestAccessKeyOperationsResponse{NextPageToken: next}
	for i := range ops {
		resp.Operations = append(resp.Operations, operationToProto(&ops[i]))
	}
	return resp, nil
}
