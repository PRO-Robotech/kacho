// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

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
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ListAddressesUseCase — список адресов с пагинацией; project_id обязателен —
// чтобы закрыть cross-project enumeration. Читает СТРАНИЦУ через CQRS Reader
// (курсор, project-scoped) и оставляет из неё только видимые subject'у строки —
// per-object, через `AuthorizeService.BatchCheck` (viewer ∪ v_list; read==enforce).
// При filter==nil или пустом subject — passthrough без обращения к FGA.
//
// Порядок принципиален: страница берётся из БД ПЕРВОЙ, права проверяются на её
// идентификаторах. Обратный порядок («перечисли все разрешённые id → сузь ими SQL»)
// упирался в жёсткий предел OpenFGA ListObjects (1000) и делал собственные ресурсы
// тенанта невидимыми — см. package-doc `internal/authzfilter`. Побочный эффект
// нового порядка: страница может вернуться НЕПОЛНОЙ (часть строк отфильтрована) —
// это нормально для cursor-пагинации, next_page_token берётся от последней
// ПРОСМОТРЕННОЙ строки, поэтому обхода без пропусков это не ломает.
type ListAddressesUseCase struct {
	repo   Repo
	filter ListFilter
}

// NewListAddressesUseCase создает ListAddressesUseCase. filter может быть nil.
func NewListAddressesUseCase(r Repo, filter ListFilter) *ListAddressesUseCase {
	return &ListAddressesUseCase{repo: r, filter: filter}
}

// Execute — project_id required + per-object фильтр видимости + load UsedBy.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
func (u *ListAddressesUseCase) Execute(ctx context.Context, subjectID string, f AddressFilter, p Pagination) ([]*kachorepo.AddressRecord, string, error) {
	if f.ProjectID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "project_id required")
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()

	addrs, nextToken, err := u.listFiltered(ctx, r, subjectID, f, p)
	if err != nil {
		return nil, "", err
	}
	loadUsedBy(ctx, r.Addresses(), addrs)
	return addrs, nextToken, nil
}

// listFiltered читает страницу и оставляет из неё только видимые subject'у строки.
func (u *ListAddressesUseCase) listFiltered(ctx context.Context, r Reader, subjectID string, f AddressFilter, p Pagination) ([]*kachorepo.AddressRecord, string, error) {
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
	if subjectID == "" && u.filter != nil {
		// identity не извлечен (anon) при включенном фильтре → fail-closed (no-leak).
		return nil, "", nil
	}
	// Формат page_token/page_size уже проверен выше — repo.List повторяет обе
	// проверки как авторитетный backstop служимого пути.
	addrs, next, lerr := r.Addresses().List(ctx, f, p)
	if lerr != nil {
		return nil, "", serviceerr.MapRepoErr(lerr)
	}
	if u.filter == nil || len(addrs) == 0 {
		return addrs, next, nil
	}
	visible, ferr := filterVisibleAddresses(ctx, u.filter, subjectID, addrs)
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, next, nil
}

// filterVisibleAddresses оставляет из страницы только видимые subject'у строки,
// сохраняя порядок курсора.
func filterVisibleAddresses(ctx context.Context, filter ListFilter, subjectID string, addrs []*kachorepo.AddressRecord) ([]*kachorepo.AddressRecord, error) {
	pageIDs := make([]string, 0, len(addrs))
	for _, a := range addrs {
		pageIDs = append(pageIDs, a.ID)
	}
	visibleIDs, err := filter.FilterVisibleIDs(ctx, subjectID,
		authzfilter.ResourceTypeAddress, authzfilter.ActionAddressList, pageIDs)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]*kachorepo.AddressRecord, 0, len(visibleIDs))
	for _, a := range addrs {
		if _, ok := visible[a.ID]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

// ListBySubnetUseCase — child-list адресов конкретной подсети. Использует
// SubnetReader.AddressesBySubnet (join через internal_ipv4.subnet_id ИЛИ
// internal_ipv6.subnet_id).
type ListBySubnetUseCase struct {
	repo         Repo
	subnetReader SubnetReader
}

// NewListBySubnetUseCase создает ListBySubnetUseCase.
func NewListBySubnetUseCase(r Repo, subnetReader SubnetReader) *ListBySubnetUseCase {
	return &ListBySubnetUseCase{repo: r, subnetReader: subnetReader}
}

// Execute — id-валидация → existence-check (Subnet) → AddressesBySubnet → UsedBy.
func (u *ListBySubnetUseCase) Execute(ctx context.Context, subnetID string, p Pagination) ([]*kachorepo.AddressRecord, string, error) {
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, subnetID); err != nil {
		return nil, "", err
	}
	if subnetID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if _, err := u.subnetReader.Get(ctx, subnetID); err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	addrs, nextToken, err := u.subnetReader.AddressesBySubnet(ctx, subnetID, p)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return addrs, nextToken, nil
	}
	defer func() { _ = r.Close() }()
	loadUsedBy(ctx, r.Addresses(), addrs)
	return addrs, nextToken, nil
}

// ListOperationsUseCase — операции, относящиеся к конкретному address-id.
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

// Execute — id-валидация (любой prefix принимается; ListOperations используется
// и сразу после Delete, поэтому existence-check не делаем) + list.
func (u *ListOperationsUseCase) Execute(ctx context.Context, addressID string, p Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID("address", ids.PrefixAddress, addressID); err != nil {
		return nil, "", err
	}
	return operations.ListForCaller(ctx, u.opsRepo, operations.ListFilter{
		ResourceID: addressID,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
