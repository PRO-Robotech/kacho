// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ListGatewaysUseCase — list gateways с пагинацией; project_id обязателен.
// Читает СТРАНИЦУ через read-only TX (`repo.Reader(ctx)`, курсор, project-scoped)
// и оставляет из неё только видимые subject'у строки — per-object, через
// `AuthorizeService.BatchCheck` (viewer ∪ v_list; read==enforce). При filter==nil
// или пустом subject — passthrough без обращения к FGA.
//
// Порядок принципиален: страница берётся из БД ПЕРВОЙ, права проверяются на её
// идентификаторах. Обратный порядок («перечисли все разрешённые id → сузь ими SQL»)
// упирался в жёсткий предел OpenFGA ListObjects (1000) и делал собственные ресурсы
// тенанта невидимыми — см. package-doc `internal/authzfilter`. Побочный эффект
// нового порядка: страница может вернуться НЕПОЛНОЙ (часть строк отфильтрована) —
// это нормально для cursor-пагинации, next_page_token берётся от последней
// ПРОСМОТРЕННОЙ строки, поэтому обхода без пропусков это не ломает.
type ListGatewaysUseCase struct {
	repo   Repo
	filter ListFilter
}

// NewListGatewaysUseCase создает ListGatewaysUseCase. filter может быть nil.
func NewListGatewaysUseCase(r Repo, filter ListFilter) *ListGatewaysUseCase {
	return &ListGatewaysUseCase{repo: r, filter: filter}
}

// Execute — проверяет project_id, читает страницу и фильтрует её per-object.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
func (u *ListGatewaysUseCase) Execute(ctx context.Context, subjectID string, f GatewayFilter, p Pagination) ([]*kacho.GatewayRecord, string, error) {
	if f.ProjectID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "project_id required")
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()

	if subjectID == "" && u.filter != nil {
		// identity не извлечен (anon) при включенном фильтре → fail-closed (no-leak).
		return nil, "", nil
	}
	// repo.List валидирует page_token/page_size — вызывается ВСЕГДА и ДО любого
	// authz-решения, поэтому malformed-token даёт InvalidArgument независимо от
	// grant-state (api-conventions.md: валидация пагинации до authz-short-circuit).
	gws, nextToken, lerr := rd.Gateways().List(ctx, f, p)
	if lerr != nil {
		return nil, "", serviceerr.MapRepoErr(lerr)
	}
	if u.filter == nil || len(gws) == 0 {
		return gws, nextToken, nil
	}
	visible, ferr := filterVisibleGateways(ctx, u.filter, subjectID, gws)
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, nextToken, nil
}

// filterVisibleGateways оставляет из страницы только видимые subject'у строки,
// сохраняя порядок курсора.
func filterVisibleGateways(ctx context.Context, filter ListFilter, subjectID string, gws []*kacho.GatewayRecord) ([]*kacho.GatewayRecord, error) {
	pageIDs := make([]string, 0, len(gws))
	for _, g := range gws {
		pageIDs = append(pageIDs, g.ID)
	}
	visibleIDs, err := filter.FilterVisibleIDs(ctx, subjectID,
		authzfilter.ResourceTypeGateway, authzfilter.ActionGatewayList, pageIDs)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]*kacho.GatewayRecord, 0, len(visibleIDs))
	for _, g := range gws {
		if _, ok := visible[g.ID]; ok {
			out = append(out, g)
		}
	}
	return out, nil
}

// ListOperationsUseCase — операции, относящиеся к конкретному gateway-id.
// NB: без repo.Get-precondition — операции должны быть доступны и после Delete
// (история).
type ListOperationsUseCase struct {
	opsRepo operations.Repo
}

// NewListOperationsUseCase создает ListOperationsUseCase.
func NewListOperationsUseCase(opsRepo operations.Repo) *ListOperationsUseCase {
	return &ListOperationsUseCase{opsRepo: opsRepo}
}

// Execute — id-валидация + list.
func (u *ListOperationsUseCase) Execute(ctx context.Context, gwID string, p Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID("gateway", ids.PrefixGateway, gwID); err != nil {
		return nil, "", err
	}
	return u.opsRepo.List(ctx, operations.ListFilter{
		ResourceID: gwID,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
