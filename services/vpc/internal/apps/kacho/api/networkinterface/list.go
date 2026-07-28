// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ListNetworkInterfacesUseCase — список NIC'ов; project_id обязателен. Читает
// СТРАНИЦУ через reader-TX CQRS-интерфейса (курсор, project-scoped) и оставляет
// из неё только видимые subject'у строки — per-object, через
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
type ListNetworkInterfacesUseCase struct {
	repo   Repo
	filter ListFilter
}

// NewListNetworkInterfacesUseCase создает ListNetworkInterfacesUseCase. filter==nil OK.
func NewListNetworkInterfacesUseCase(r Repo, filter ListFilter) *ListNetworkInterfacesUseCase {
	return &ListNetworkInterfacesUseCase{repo: r, filter: filter}
}

// Execute — проверяет project_id, читает страницу и фильтрует её per-object.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
func (u *ListNetworkInterfacesUseCase) Execute(ctx context.Context, subjectID string, f NetworkInterfaceFilter, p Pagination) ([]*kachorepo.NetworkInterfaceRecord, string, error) {
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
	out, next, lerr := rd.NetworkInterfaces().List(ctx, f, p)
	if lerr != nil {
		return nil, "", serviceerr.MapRepoErr(lerr)
	}
	if u.filter == nil || len(out) == 0 {
		return out, next, nil
	}
	visible, ferr := filterVisibleNetworkInterfaces(ctx, u.filter, subjectID, out)
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, next, nil
}

// filterVisibleNetworkInterfaces оставляет из страницы только видимые subject'у
// строки, сохраняя порядок курсора.
func filterVisibleNetworkInterfaces(ctx context.Context, filter ListFilter, subjectID string, nics []*kachorepo.NetworkInterfaceRecord) ([]*kachorepo.NetworkInterfaceRecord, error) {
	pageIDs := make([]string, 0, len(nics))
	for _, n := range nics {
		pageIDs = append(pageIDs, n.ID)
	}
	visibleIDs, err := filter.FilterVisibleIDs(ctx, subjectID,
		authzfilter.ResourceTypeNetworkInterface, authzfilter.ActionNetworkInterfaceList, pageIDs)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]*kachorepo.NetworkInterfaceRecord, 0, len(visibleIDs))
	for _, n := range nics {
		if _, ok := visible[n.ID]; ok {
			out = append(out, n)
		}
	}
	return out, nil
}

// ListOperationsUseCase — операции, относящиеся к конкретному NIC.
type ListOperationsUseCase struct {
	opsRepo operations.Repo
}

// NewListOperationsUseCase создает ListOperationsUseCase.
func NewListOperationsUseCase(opsRepo operations.Repo) *ListOperationsUseCase {
	return &ListOperationsUseCase{opsRepo: opsRepo}
}

// Execute — валидирует id и отдает список операций. Прекондишена repo.Get здесь
// нет специально: история операций должна оставаться доступной и после удаления
// ресурса (строки operations не привязаны FK-каскадом).
func (u *ListOperationsUseCase) Execute(ctx context.Context, niID string, p Pagination) ([]operations.Operation, string, error) {
	if err := niResourceID(niID); err != nil {
		return nil, "", err
	}
	return u.opsRepo.List(ctx, operations.ListFilter{
		ResourceID: niID,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
