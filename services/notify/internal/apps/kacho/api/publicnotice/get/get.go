// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package get — use-case `NoticeService.Get` (приёмка NTF-5 Р16; замысел
// issue-2924 З13 п.1, п.3): извещение, видимое из области, с суженными ссылками.
package get

import (
	"context"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/authzcheck"
)

// UseCase — Get.
type UseCase struct{ deps publicnotice.Deps }

// New — use-case над портами чтения и сужения.
func New(deps publicnotice.Deps) *UseCase { return &UseCase{deps: deps} }

// Execute: обязательность и форма области, форма id извещения, право
// (authzcheck.RequireScope), выборка с видимостью, сужение ссылок.
func (u *UseCase) Execute(ctx context.Context, req *notifyv1.GetNoticeRequest) (*notifyv1.Notice, error) {
	scope, err := publicnotice.ProjectScope(req.GetProjectId())
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
