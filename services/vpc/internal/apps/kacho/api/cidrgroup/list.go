// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/listpage"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ListCidrGroupsUseCase — страница наборов, суженная ПОСТРОЧНО.
//
// Порядок принципиален: страница берётся из своей базы курсором ПЕРВОЙ, а права
// спрашиваются об идентификаторах ЭТОЙ страницы, партиями. Обратный порядок
// («перечисли все разрешённые id → сузь ими запрос») упирается в жёсткий предел
// перечисления у владельца прав и делает собственные ресурсы арендатора
// невидимыми — см. package-doc `internal/authzfilter`.
type ListCidrGroupsUseCase struct {
	repo     Repo
	narrower *listnarrow.Narrower
}

// NewListCidrGroupsUseCase создаёт ListCidrGroupsUseCase. narrower может быть nil
// (фильтр выключен) → сквозной проход.
func NewListCidrGroupsUseCase(r Repo, n *listnarrow.Narrower) *ListCidrGroupsUseCase {
	return &ListCidrGroupsUseCase{repo: r, narrower: n}
}

// Execute — формат пагинации, затем проект, затем сужение, затем страница.
func (u *ListCidrGroupsUseCase) Execute(ctx context.Context, f CidrGroupFilter, p Pagination) ([]*kachorepo.CidrGroupRecord, string, error) {
	// Формат пагинации — ПЕРВЫМ стейтментом, ДО замыкания по личности
	// вызывающего.
	//
	// Проверка стоит в ТОЙ ЖЕ функции, которая замыкается: пока формат курсора
	// проверял только репозиторий, один и тот же мусорный курсор получал разный
	// ответ в зависимости от того, что вызывающему выдано, — то есть ответ на
	// некорректный ввод зависел от прав. Репозиторий остаётся авторитетным на
	// служимом пути.
	if err := listpage.ValidatePagination(p.PageToken, p.PageSize); err != nil {
		return nil, "", err
	}
	if f.ProjectID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "project_id required")
	}
	// Предусловие сужения — ДО чтения страницы: безымянный запрос иначе оплатил
	// бы курсорное чтение, чтобы получить отказ на прочитанном.
	if err := listnarrow.Precheck(ctx, u.narrower); err != nil {
		return nil, "", err
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()

	groups, next, lerr := r.CidrGroups().List(ctx, f, p)
	if lerr != nil {
		return nil, "", serviceerr.MapRepoErr(lerr)
	}
	if len(groups) == 0 {
		return groups, next, nil
	}
	visible, ferr := listnarrow.Page(ctx, u.narrower,
		authzfilter.ResourceTypeCidrGroup, authzfilter.ActionCidrGroupList, groups,
		func(rec *kachorepo.CidrGroupRecord) string { return rec.ID })
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, next, nil
}

// ListOperationsUseCase — операции, относящиеся к конкретному набору.
//
// Без existence-check: история обязана оставаться доступной и после удаления.
// Выдача сужена до операций САМОГО вызывающего (`operations.ListForCaller` —
// предикат владения внутри SQL WHERE): строка операции несёт тело ресурса и
// личность инициатора, поэтому кросс-принципальная история на этой поверхности
// не выдаётся.
type ListOperationsUseCase struct {
	opsRepo operations.Repo
}

// NewListOperationsUseCase создаёт ListOperationsUseCase.
func NewListOperationsUseCase(opsRepo operations.Repo) *ListOperationsUseCase {
	return &ListOperationsUseCase{opsRepo: opsRepo}
}

// Execute — формат id, затем список операций ресурса.
func (u *ListOperationsUseCase) Execute(ctx context.Context, groupID string, p Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID("cidr_group", ids.PrefixCidrGroupHyphen, groupID); err != nil {
		return nil, "", err
	}
	return operations.ListForCaller(ctx, u.opsRepo, operations.ListFilter{
		ResourceID: groupID,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
