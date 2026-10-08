// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package get — use-case `InternalNoticeService.Get`: извещение во внутренней
// проекции (приёмка NTF-5 Р3, Р18).
package get

import (
	"context"
	"errors"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice"
)

// UseCase — чтение одного извещения оператором.
type UseCase struct{ store notice.Store }

// New — use-case над хранилищем.
func New(store notice.Store) *UseCase { return &UseCase{store: store} }

// Execute: форма id первым оператором (Р3), затем чтение. Заявка создания этим
// чтением не видна — она в другой таблице (NTF5-18).
func (u *UseCase) Execute(ctx context.Context, req *notifyv1.GetInternalNoticeRequest) (*notifyv1.InternalNotice, error) {
	id := req.GetNoticeId()
	if err := notice.ValidateID(id); err != nil {
		return nil, err
	}
	n, err := u.store.Get(ctx, id)
	if errors.Is(err, notice.ErrNotFound) {
		return nil, notice.NotFound(id)
	}
	if err != nil {
		return nil, notice.StorageFailure()
	}
	return notice.Internal(n), nil
}
