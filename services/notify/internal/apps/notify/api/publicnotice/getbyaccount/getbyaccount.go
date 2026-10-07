// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package getbyaccount — use-case `NoticeService.GetByAccount` (приёмка NTF-5 Р16; замысел
// issue-2924 З13 п.1, п.3): извещение, видимое из области, с суженными ссылками.
package getbyaccount

import (
	"context"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/publicnotice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/authzcheck"
)

// UseCase — GetByAccount.
type UseCase struct{ deps publicnotice.Deps }

// New — use-case над портами чтения и сужения.
func New(deps publicnotice.Deps) *UseCase { return &UseCase{deps: deps} }

// Execute: обязательность и форма области, форма id извещения, право
// (authzcheck.RequireScope), выборка с видимостью, сужение ссылок.
func (u *UseCase) Execute(ctx context.Context, req *notifyv1.GetNoticeByAccountRequest) (*notifyv1.Notice, error) {
	scope, err := publicnotice.AccountScope(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	id := req.GetNoticeId()
	if err := notice.ValidateID(id); err != nil {
		return nil, err
	}
	if err := authzcheck.RequireScope(ctx, u.deps.Checker, publicnotice.RelationRead, publicnotice.Scope(scope)); err != nil {
		return nil, err
	}
	return publicnotice.One(ctx, u.deps, scope, id)
}
