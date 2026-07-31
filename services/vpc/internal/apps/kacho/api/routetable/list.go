// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/listpage"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ListRouteTablesUseCase — list route-table'ов с пагинацией; project_id обязателен.
// Читает СТРАНИЦУ через CQRS Reader (курсор, project-scoped) и оставляет из неё
// только видимые subject'у строки — per-object, через `AuthorizeService.BatchCheck`
// (viewer ∪ v_list; read==enforce). При filter==nil или пустом subject — passthrough
// без обращения к FGA.
//
// Порядок принципиален: страница берётся из БД ПЕРВОЙ, права проверяются на её
// идентификаторах. Обратный порядок («перечисли все разрешённые id → сузь ими SQL»)
// упирался в жёсткий предел OpenFGA ListObjects (1000) и делал собственные ресурсы
// тенанта невидимыми — см. package-doc `internal/authzfilter`. Побочный эффект
// нового порядка: страница может вернуться НЕПОЛНОЙ (часть строк отфильтрована) —
// это нормально для cursor-пагинации, next_page_token берётся от последней
// ПРОСМОТРЕННОЙ строки, поэтому обхода без пропусков это не ломает.
type ListRouteTablesUseCase struct {
	repo   Repo
	filter ListFilter
}

// NewListRouteTablesUseCase создает ListRouteTablesUseCase. filter может быть nil.
func NewListRouteTablesUseCase(r Repo, filter ListFilter) *ListRouteTablesUseCase {
	return &ListRouteTablesUseCase{repo: r, filter: filter}
}

// Execute — проверяет project_id, читает страницу и фильтрует её per-object.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
func (u *ListRouteTablesUseCase) Execute(ctx context.Context, subjectID string, f RouteTableFilter, p Pagination) ([]*kacho.RouteTableRecord, string, error) {
	// Формат пагинации — ПЕРВЫМ стейтментом, до всего остального.
	//
	// Ниже стоит замыкание по личности вызывающего: при неизвлечённом принципале
	// и включенном фильтре видимости страница не читается вовсе. Пока формат
	// курсора проверял только репозиторий, один и тот же мусорный page_token
	// получал разный ответ в зависимости от того, опознан ли вызывающий, — то
	// есть проверка ввода зависела от прав. Проверка формата решением о доступе
	// не является; репозиторий остаётся авторитетным на служимом пути.
	if err := listpage.ValidatePagination(p.PageToken, p.PageSize); err != nil {
		return nil, "", err
	}
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
	// Формат page_token/page_size уже проверен выше — repo.List повторяет обе
	// проверки как авторитетный backstop служимого пути.
	rts, next, lerr := rd.RouteTables().List(ctx, f, p)
	if lerr != nil {
		return nil, "", lerr
	}
	if u.filter == nil || len(rts) == 0 {
		return rts, next, nil
	}
	visible, ferr := filterVisibleRouteTables(ctx, u.filter, subjectID, rts)
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, next, nil
}

// filterVisibleRouteTables оставляет из страницы только видимые subject'у строки,
// сохраняя порядок курсора.
func filterVisibleRouteTables(ctx context.Context, filter ListFilter, subjectID string, rts []*kacho.RouteTableRecord) ([]*kacho.RouteTableRecord, error) {
	pageIDs := make([]string, 0, len(rts))
	for _, rt := range rts {
		pageIDs = append(pageIDs, rt.ID)
	}
	visibleIDs, err := filter.FilterVisibleIDs(ctx, subjectID,
		authzfilter.ResourceTypeRouteTable, authzfilter.ActionRouteTableList, pageIDs)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]*kacho.RouteTableRecord, 0, len(visibleIDs))
	for _, rt := range rts {
		if _, ok := visible[rt.ID]; ok {
			out = append(out, rt)
		}
	}
	return out, nil
}

// ListOperationsUseCase — операции, относящиеся к конкретному route-table id.
// NB: без repo.Get-precondition — операции должны быть доступны и после Delete
// (история).
//
// Выдача сужена до операций САМОГО вызывающего (operations.ListForCaller —
// предикат владения внутри SQL WHERE). Право на список не есть право на
// чтение: строка операции несёт ресурс целиком в Response и личность
// инициатора, поэтому кросс-принципальная история на этой поверхности не
// выдаётся.
type ListOperationsUseCase struct {
	opsRepo operations.Repo
}

// NewListOperationsUseCase создает ListOperationsUseCase.
func NewListOperationsUseCase(opsRepo operations.Repo) *ListOperationsUseCase {
	return &ListOperationsUseCase{opsRepo: opsRepo}
}

// Execute — id-валидация + list.
func (u *ListOperationsUseCase) Execute(ctx context.Context, rtID string, p Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID("route table", ids.PrefixRouteTable, rtID); err != nil {
		return nil, "", err
	}
	return operations.ListForCaller(ctx, u.opsRepo, operations.ListFilter{
		ResourceID: rtID,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
