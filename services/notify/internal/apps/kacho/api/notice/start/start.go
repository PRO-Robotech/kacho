// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package start — use-case `InternalNoticeService.Start` (приёмка NTF-5 Р5; замысел
// issue-2924 З5): переход одной записью с условием, операция done=true в той же
// транзакции.
package start

import (
	"context"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// UseCase — переход Start.
type UseCase struct {
	store notice.Store
	now   notice.Clock
}

// New — use-case над хранилищем и часами notify.
func New(store notice.Store, now notice.Clock) *UseCase { return &UseCase{store: store, now: now} }

// Execute исполняет переход Start.
func (u *UseCase) Execute(ctx context.Context, req *notifyv1.StartNoticeRequest) (*operationv1.Operation, error) {
	id := req.GetNoticeId()
	return notice.Transit(ctx, u.store, u.now, rules.VerbStart, id, "Start notice",
		&notifyv1.StartNoticeMetadata{NoticeId: id})
}
