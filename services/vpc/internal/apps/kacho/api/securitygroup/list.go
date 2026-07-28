// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

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

// ListSecurityGroupsUseCase — список SG с пагинацией; project_id обязателен
// (закрывает cross-project enumeration). Читает СТРАНИЦУ через CQRS Reader
// (read-only TX, курсор, project-scoped) и оставляет из неё только видимые
// subject'у строки — per-object, через `AuthorizeService.BatchCheck`
// (viewer ∪ v_list; read==enforce). При filter==nil или пустом subject —
// passthrough без обращения к FGA.
//
// Порядок принципиален: страница берётся из БД ПЕРВОЙ, права проверяются на её
// идентификаторах. Обратный порядок («перечисли все разрешённые id → сузь ими SQL»)
// упирался в жёсткий предел OpenFGA ListObjects (1000) и делал собственные ресурсы
// тенанта невидимыми — см. package-doc `internal/authzfilter`. Побочный эффект
// нового порядка: страница может вернуться НЕПОЛНОЙ (часть строк отфильтрована) —
// это нормально для cursor-пагинации, next_page_token берётся от последней
// ПРОСМОТРЕННОЙ строки, поэтому обхода без пропусков это не ломает.
type ListSecurityGroupsUseCase struct {
	repo   Repo
	filter ListFilter
}

// NewListSecurityGroupsUseCase создает ListSecurityGroupsUseCase. filter может
// быть nil (list-filter disabled / dev) → unfiltered passthrough.
func NewListSecurityGroupsUseCase(r Repo, filter ListFilter) *ListSecurityGroupsUseCase {
	return &ListSecurityGroupsUseCase{repo: r, filter: filter}
}

// Execute — проверяет project_id, читает страницу и фильтрует её per-object.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
func (u *ListSecurityGroupsUseCase) Execute(ctx context.Context, subjectID string, f SecurityGroupFilter, p Pagination) ([]*kacho.SecurityGroupRecord, string, error) {
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
	sgs, next, lerr := rd.SecurityGroups().List(ctx, f, p)
	if lerr != nil {
		return nil, "", lerr
	}
	if u.filter == nil || len(sgs) == 0 {
		return sgs, next, nil
	}
	visible, ferr := filterVisibleSecurityGroups(ctx, u.filter, subjectID, sgs)
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, next, nil
}

// filterVisibleSecurityGroups оставляет из страницы только видимые subject'у
// строки, сохраняя порядок курсора.
func filterVisibleSecurityGroups(ctx context.Context, filter ListFilter, subjectID string, sgs []*kacho.SecurityGroupRecord) ([]*kacho.SecurityGroupRecord, error) {
	pageIDs := make([]string, 0, len(sgs))
	for _, sg := range sgs {
		pageIDs = append(pageIDs, sg.ID)
	}
	visibleIDs, err := filter.FilterVisibleIDs(ctx, subjectID,
		authzfilter.ResourceTypeSecurityGroup, authzfilter.ActionSecurityGroupList, pageIDs)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]*kacho.SecurityGroupRecord, 0, len(visibleIDs))
	for _, sg := range sgs {
		if _, ok := visible[sg.ID]; ok {
			out = append(out, sg)
		}
	}
	return out, nil
}

// ListOperationsUseCase — операции, относящиеся к конкретному SG.
//
// Семантика: с repo.Get-precondition (для SG ListOperations предполагает, что SG
// еще жив; если удален — возвращается sync NotFound через precondition Get).
type ListOperationsUseCase struct {
	repo    Repo
	opsRepo operations.Repo
}

// NewListOperationsUseCase создает ListOperationsUseCase.
func NewListOperationsUseCase(r Repo, opsRepo operations.Repo) *ListOperationsUseCase {
	return &ListOperationsUseCase{repo: r, opsRepo: opsRepo}
}

// Execute — id-валидация + existence-check + список операций.
func (u *ListOperationsUseCase) Execute(ctx context.Context, id string, p Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID("security group", ids.PrefixSecurityGroup, id); err != nil {
		return nil, "", err
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	if _, gerr := rd.SecurityGroups().Get(ctx, id); gerr != nil {
		_ = rd.Close()
		return nil, "", serviceerr.MapRepoErr(gerr)
	}
	_ = rd.Close()
	return u.opsRepo.List(ctx, operations.ListFilter{
		ResourceID: id,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
