// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — транспорт notify-api: gRPC-серверы сервисов извещений.
// Тонкий слой: запрос → use-case → ответ. Решений здесь нет — ни проверки
// формы, ни права, ни SQL; право методов InternalNoticeService судит звено
// носителя по аннотации, методов NoticeService — use-case (З16).
package handler

import (
	"context"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/cancel"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/complete"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/create"
	noticeget "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/get"
	noticelist "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/list"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/start"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/update"
	publicget "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice/get"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice/getbyaccount"
	publiclist "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice/list"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice/listbyaccount"
)

// InternalNotice — набор use-case'ов InternalNoticeService.
type InternalNotice struct {
	Create   *create.UseCase
	Get      *noticeget.UseCase
	List     *noticelist.UseCase
	Update   *update.UseCase
	Start    *start.UseCase
	Complete *complete.UseCase
	Cancel   *cancel.UseCase
}

var _ notifyv1.InternalNoticeServiceServer = (*internalNoticeHandler)(nil)

// internalNoticeHandler — адаптер имён: методы сервера совпадают по именам с полями
// use-case'ов, поэтому сервер — отдельный тип над набором.
type internalNoticeHandler struct {
	notifyv1.UnimplementedInternalNoticeServiceServer
	uc *InternalNotice
}

// Server — сервер InternalNoticeService над набором use-case'ов.
func (s *InternalNotice) Server() notifyv1.InternalNoticeServiceServer {
	return &internalNoticeHandler{uc: s}
}

func (s *internalNoticeHandler) Create(ctx context.Context, r *notifyv1.CreateNoticeRequest) (*operationv1.Operation, error) {
	return s.uc.Create.Execute(ctx, r)
}

func (s *internalNoticeHandler) Get(ctx context.Context, r *notifyv1.GetInternalNoticeRequest) (*notifyv1.InternalNotice, error) {
	return s.uc.Get.Execute(ctx, r)
}

func (s *internalNoticeHandler) List(ctx context.Context, r *notifyv1.ListInternalNoticesRequest) (*notifyv1.ListInternalNoticesResponse, error) {
	return s.uc.List.Execute(ctx, r)
}

func (s *internalNoticeHandler) Update(ctx context.Context, r *notifyv1.UpdateNoticeRequest) (*operationv1.Operation, error) {
	return s.uc.Update.Execute(ctx, r)
}

func (s *internalNoticeHandler) Start(ctx context.Context, r *notifyv1.StartNoticeRequest) (*operationv1.Operation, error) {
	return s.uc.Start.Execute(ctx, r)
}

func (s *internalNoticeHandler) Complete(ctx context.Context, r *notifyv1.CompleteNoticeRequest) (*operationv1.Operation, error) {
	return s.uc.Complete.Execute(ctx, r)
}

func (s *internalNoticeHandler) Cancel(ctx context.Context, r *notifyv1.CancelNoticeRequest) (*operationv1.Operation, error) {
	return s.uc.Cancel.Execute(ctx, r)
}

// PublicNotice — набор use-case'ов NoticeService.
type PublicNotice struct {
	List          *publiclist.UseCase
	ListByAccount *listbyaccount.UseCase
	Get           *publicget.UseCase
	GetByAccount  *getbyaccount.UseCase
}

type publicNoticeHandler struct {
	notifyv1.UnimplementedNoticeServiceServer
	uc *PublicNotice
}

var _ notifyv1.NoticeServiceServer = (*publicNoticeHandler)(nil)

// Server — сервер NoticeService над набором use-case'ов.
func (s *PublicNotice) Server() notifyv1.NoticeServiceServer { return &publicNoticeHandler{uc: s} }

func (s *publicNoticeHandler) List(ctx context.Context, r *notifyv1.ListNoticesRequest) (*notifyv1.ListNoticesResponse, error) {
	return s.uc.List.Execute(ctx, r)
}

func (s *publicNoticeHandler) ListByAccount(ctx context.Context, r *notifyv1.ListNoticesByAccountRequest) (*notifyv1.ListNoticesResponse, error) {
	return s.uc.ListByAccount.Execute(ctx, r)
}

func (s *publicNoticeHandler) Get(ctx context.Context, r *notifyv1.GetNoticeRequest) (*notifyv1.Notice, error) {
	return s.uc.Get.Execute(ctx, r)
}

func (s *publicNoticeHandler) GetByAccount(ctx context.Context, r *notifyv1.GetNoticeByAccountRequest) (*notifyv1.Notice, error) {
	return s.uc.GetByAccount.Execute(ctx, r)
}
