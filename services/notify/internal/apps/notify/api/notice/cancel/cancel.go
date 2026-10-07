// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package cancel — use-case `InternalNoticeService.Cancel` (приёмка NTF-5 Р5; замысел
// issue-2924 З5): переход одной записью с условием, операция done=true в той же
// транзакции.
package cancel

import (
	"context"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// UseCase — переход Cancel.
type UseCase struct {
	store notice.Store
	now   notice.Clock
}

// New — use-case над хранилищем и часами notify.
func New(store notice.Store, now notice.Clock) *UseCase { return &UseCase{store: store, now: now} }

// Execute исполняет переход Cancel.
func (u *UseCase) Execute(ctx context.Context, req *notifyv1.CancelNoticeRequest) (*operationv1.Operation, error) {
	id := req.GetNoticeId()
	return notice.Transit(ctx, u.store, u.now, rules.VerbCancel, id, "Cancel notice",
		&notifyv1.CancelNoticeMetadata{NoticeId: id})
}
