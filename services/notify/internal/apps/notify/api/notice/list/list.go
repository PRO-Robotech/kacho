// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package list — use-case `InternalNoticeService.List`: все извещения установки
// во внутренней проекции, курсором `(created_at, id)` (приёмка NTF-5 Р18;
// замысел issue-2924 З13 п.2).
package list

import (
	"context"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice/paging"
)

// UseCase — страница извещений оператору.
type UseCase struct{ store notice.Store }

// New — use-case над хранилищем.
func New(store notice.Store) *UseCase { return &UseCase{store: store} }

// Execute: разбор page_size/page_token — первым оператором (З13 п.2), затем
// чтение страницы с одной лишней строкой — признаком следующей.
func (u *UseCase) Execute(ctx context.Context, req *notifyv1.ListInternalNoticesRequest) (*notifyv1.ListInternalNoticesResponse, error) {
	pg, err := paging.Parse(req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	rows, err := u.store.List(ctx, pg.Query())
	if err != nil {
		return nil, notice.StorageFailure()
	}
	rows, next := paging.Cut(pg, rows, notice.Key)
	out := &notifyv1.ListInternalNoticesResponse{NextPageToken: next}
	for _, n := range rows {
		out.Notices = append(out.Notices, notice.Internal(n))
	}
	return out, nil
}
