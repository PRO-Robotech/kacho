// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

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

// ListNetworksUseCase — list networks с пагинацией; project_id обязателен.
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
type ListNetworksUseCase struct {
	repo   Repo
	filter ListFilter
}

// NewListNetworksUseCase создает ListNetworksUseCase. filter может быть nil
// (list-filter disabled / dev) → unfiltered passthrough.
func NewListNetworksUseCase(r Repo, filter ListFilter) *ListNetworksUseCase {
	return &ListNetworksUseCase{repo: r, filter: filter}
}

// Execute — проверяет project_id, читает страницу и фильтрует её per-object.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
//
// Параметры:
//   - subjectID: FGA-subject ("user:usr_xxx"). Пустой при включенном фильтре →
//     fail-closed (no-leak). Сквозного пропуска фильтра нет ни для какого значения
//     субъекта — см. authzfilter/actions.go.
func (u *ListNetworksUseCase) Execute(ctx context.Context, subjectID string, f NetworkFilter, p Pagination) ([]*kachorepo.NetworkRecord, string, error) {
	if f.ProjectID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "project_id required")
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()

	if subjectID == "" && u.filter != nil {
		// identity не извлечен (anon / gateway не проставил principal) при
		// включенном фильтре → fail-closed (пустой список), НЕ unfiltered
		// passthrough: «не знаю, кто ты» ≠ «доверенный system-вызов» (no-leak).
		return nil, "", nil
	}
	// repo.List валидирует page_token/page_size — вызывается ВСЕГДА и ДО любого
	// authz-решения, поэтому malformed-token даёт InvalidArgument независимо от
	// grant-state (api-conventions.md: валидация пагинации до authz-short-circuit).
	nets, next, lerr := r.Networks().List(ctx, f, p)
	if lerr != nil {
		return nil, "", lerr
	}
	if u.filter == nil || len(nets) == 0 {
		return nets, next, nil
	}
	visible, ferr := filterVisibleNetworks(ctx, u.filter, subjectID, nets)
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, next, nil
}

// filterVisibleNetworks оставляет из страницы только видимые subject'у строки,
// сохраняя порядок курсора.
func filterVisibleNetworks(ctx context.Context, filter ListFilter, subjectID string, nets []*kachorepo.NetworkRecord) ([]*kachorepo.NetworkRecord, error) {
	pageIDs := make([]string, 0, len(nets))
	for _, n := range nets {
		pageIDs = append(pageIDs, n.ID)
	}
	visibleIDs, err := filter.FilterVisibleIDs(ctx, subjectID,
		authzfilter.ResourceTypeNetwork, authzfilter.ActionNetworkList, pageIDs)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]*kachorepo.NetworkRecord, 0, len(visibleIDs))
	for _, n := range nets {
		if _, ok := visible[n.ID]; ok {
			out = append(out, n)
		}
	}
	return out, nil
}

// ListSubnetsUseCase — список Subnets конкретной Network. Network-existence-check
// идет через CQRS-Reader; SubnetReader — узкий read-порт Subnet.
type ListSubnetsUseCase struct {
	repo         Repo
	subnetReader SubnetReader
}

// NewListSubnetsUseCase создает ListSubnetsUseCase.
func NewListSubnetsUseCase(r Repo, subnetReader SubnetReader) *ListSubnetsUseCase {
	return &ListSubnetsUseCase{repo: r, subnetReader: subnetReader}
}

// Execute — id validate → existence check → list subnets.
func (u *ListSubnetsUseCase) Execute(ctx context.Context, networkID string, p Pagination) ([]*kachorepo.SubnetRecord, string, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, networkID); err != nil {
		return nil, "", err
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()
	if _, err := rd.Networks().Get(ctx, networkID); err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	if u.subnetReader == nil {
		return nil, "", nil
	}
	return u.subnetReader.List(ctx, SubnetFilter{NetworkID: networkID}, p)
}

// ListSecurityGroupsUseCase — список SG, привязанных к Network.
type ListSecurityGroupsUseCase struct {
	repo   Repo
	sgRepo SecurityGroupRepo
}

// NewListSecurityGroupsUseCase создает ListSecurityGroupsUseCase.
func NewListSecurityGroupsUseCase(r Repo, sgRepo SecurityGroupRepo) *ListSecurityGroupsUseCase {
	return &ListSecurityGroupsUseCase{repo: r, sgRepo: sgRepo}
}

// Execute — id validate → existence check → list SG.
func (u *ListSecurityGroupsUseCase) Execute(ctx context.Context, networkID string, p Pagination) ([]*kachorepo.SecurityGroupRecord, string, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, networkID); err != nil {
		return nil, "", err
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()
	if _, err := rd.Networks().Get(ctx, networkID); err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	if u.sgRepo == nil {
		return nil, "", nil
	}
	return u.sgRepo.List(ctx, SecurityGroupFilter{NetworkID: networkID}, p)
}

// ListRouteTablesUseCase — список RT в Network.
type ListRouteTablesUseCase struct {
	repo           Repo
	routeTableRead RouteTableReader
}

// NewListRouteTablesUseCase создает ListRouteTablesUseCase.
func NewListRouteTablesUseCase(r Repo, routeTableRead RouteTableReader) *ListRouteTablesUseCase {
	return &ListRouteTablesUseCase{repo: r, routeTableRead: routeTableRead}
}

// Execute — id validate → existence check → list RT.
func (u *ListRouteTablesUseCase) Execute(ctx context.Context, networkID string, p Pagination) ([]*kachorepo.RouteTableRecord, string, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, networkID); err != nil {
		return nil, "", err
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()
	if _, err := rd.Networks().Get(ctx, networkID); err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	if u.routeTableRead == nil {
		return nil, "", nil
	}
	return u.routeTableRead.List(ctx, RouteTableFilter{NetworkID: networkID}, p)
}

// ListOperationsUseCase — операции, относящиеся к конкретному network-id.
// NB: без repo.Get-precondition — операции должны быть доступны и после Delete
// (история).
type ListOperationsUseCase struct {
	opsRepo operations.Repo
}

// NewListOperationsUseCase создает ListOperationsUseCase.
func NewListOperationsUseCase(opsRepo operations.Repo) *ListOperationsUseCase {
	return &ListOperationsUseCase{opsRepo: opsRepo}
}

// Execute — id-валидация (любой prefix принимается; ListOperations используется
// и сразу после Delete, поэтому existence-check не делаем) + list.
func (u *ListOperationsUseCase) Execute(ctx context.Context, networkID string, p Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, networkID); err != nil {
		return nil, "", err
	}
	return u.opsRepo.List(ctx, operations.ListFilter{
		ResourceID: networkID,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
