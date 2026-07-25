// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ListSubnetsUseCase — list subnets с пагинацией; project_id обязателен (закрывает
// cross-project enumeration). Читает СТРАНИЦУ через CQRS Reader (курсор,
// project-scoped) и оставляет из неё только видимые subject'у строки — per-object,
// через `AuthorizeService.BatchCheck` (viewer ∪ v_list; read==enforce). При
// filter==nil или пустом subject — passthrough без обращения к FGA.
//
// Порядок принципиален: страница берётся из БД ПЕРВОЙ, права проверяются на её
// идентификаторах. Обратный порядок («перечисли все разрешённые id → сузь ими SQL»)
// упирался в жёсткий предел OpenFGA ListObjects (1000) и делал собственные ресурсы
// тенанта невидимыми — см. package-doc `internal/authzfilter`. Побочный эффект
// нового порядка: страница может вернуться НЕПОЛНОЙ (часть строк отфильтрована) —
// это нормально для cursor-пагинации, next_page_token берётся от последней
// ПРОСМОТРЕННОЙ строки, поэтому обхода без пропусков это не ломает.
type ListSubnetsUseCase struct {
	repo   Repo
	filter ListFilter
}

// NewListSubnetsUseCase создает ListSubnetsUseCase. filter может быть nil
// (list-filter disabled / dev) → unfiltered passthrough.
func NewListSubnetsUseCase(r Repo, filter ListFilter) *ListSubnetsUseCase {
	return &ListSubnetsUseCase{repo: r, filter: filter}
}

// Execute — проверяет project_id, читает страницу и фильтрует её per-object.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
func (u *ListSubnetsUseCase) Execute(ctx context.Context, subjectID string, f SubnetFilter, p Pagination) ([]*kachorepo.SubnetRecord, string, error) {
	if f.ProjectID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "project_id required")
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()

	if subjectID == "" && u.filter != nil {
		// identity не извлечен (anon) при включенном фильтре → fail-closed (no-leak).
		return nil, "", nil
	}
	// repo.List валидирует page_token/page_size — вызывается ВСЕГДА и ДО любого
	// authz-решения, поэтому malformed-token даёт InvalidArgument независимо от
	// grant-state (api-conventions.md: валидация пагинации до authz-short-circuit).
	subs, next, lerr := r.Subnets().List(ctx, f, p)
	if lerr != nil {
		return nil, "", lerr
	}
	if u.filter == nil || subjectID == authzfilter.SystemSubject || len(subs) == 0 {
		return subs, next, nil
	}
	visible, ferr := filterVisibleSubnets(ctx, u.filter, subjectID, subs)
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, next, nil
}

// filterVisibleSubnets оставляет из страницы только видимые subject'у строки,
// сохраняя порядок курсора.
func filterVisibleSubnets(ctx context.Context, filter ListFilter, subjectID string, subs []*kachorepo.SubnetRecord) ([]*kachorepo.SubnetRecord, error) {
	pageIDs := make([]string, 0, len(subs))
	for _, s := range subs {
		pageIDs = append(pageIDs, s.ID)
	}
	visibleIDs, err := filter.FilterVisibleIDs(ctx, subjectID,
		authzfilter.ResourceTypeSubnet, authzfilter.ActionSubnetList, pageIDs)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]*kachorepo.SubnetRecord, 0, len(visibleIDs))
	for _, s := range subs {
		if _, ok := visible[s.ID]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// ListOperationsUseCase — операции, относящиеся к конкретному subnet-id.
// NB: без repo.Get-precondition — операции должны быть доступны и после Delete
// (история операций; rows в `operations` не имеют FK cascade).
type ListOperationsUseCase struct {
	opsRepo operations.Repo
}

// NewListOperationsUseCase создает ListOperationsUseCase.
func NewListOperationsUseCase(opsRepo operations.Repo) *ListOperationsUseCase {
	return &ListOperationsUseCase{opsRepo: opsRepo}
}

// Execute — id-валидация + list.
func (u *ListOperationsUseCase) Execute(ctx context.Context, subnetID string, p Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, subnetID); err != nil {
		return nil, "", err
	}
	return u.opsRepo.List(ctx, operations.ListFilter{
		ResourceID: subnetID,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
