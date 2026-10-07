// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package list — use-case `NoticeService.List` (приёмка NTF-5 Р16; замысел
// issue-2924 З13 п.2): страница видимых из области извещений.
package list

import (
	"context"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice/paging"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/publicnotice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/authzcheck"
)

// UseCase — List.
type UseCase struct{ deps publicnotice.Deps }

// New — use-case над портами чтения.
func New(deps publicnotice.Deps) *UseCase { return &UseCase{deps: deps} }

// Execute: обязательность и форма области первыми, затем страница и право
// (authzcheck.RequireScope), затем выборка.
func (u *UseCase) Execute(ctx context.Context, req *notifyv1.ListNoticesRequest) (*notifyv1.ListNoticesResponse, error) {
	scope, err := publicnotice.ProjectScope(req.GetProjectId())
	if err != nil {
		return nil, err
	}
	pg, err := paging.Parse(req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	if err := authzcheck.RequireScope(ctx, u.deps.Checker, publicnotice.RelationRead, publicnotice.Scope(scope)); err != nil {
		return nil, err
	}
	return publicnotice.Page(ctx, u.deps, scope, pg)
}
