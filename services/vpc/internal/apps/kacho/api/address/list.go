// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"net/netip"

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
	repo     Repo
	narrower *listnarrow.Narrower
}

// NewListAddressesUseCase создает ListAddressesUseCase. filter может быть nil.
func NewListAddressesUseCase(r Repo, n *listnarrow.Narrower) *ListAddressesUseCase {
	return &ListAddressesUseCase{repo: r, narrower: n}
}

// Execute — project_id required + per-object фильтр видимости + load UsedBy.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
func (u *ListAddressesUseCase) Execute(ctx context.Context, f AddressFilter, p Pagination) ([]*kachorepo.AddressRecord, string, error) {
	// Формат пагинации — ПЕРВЫМ стейтментом, до всего остального.
	//
	// Ниже стоит замыкание по личности вызывающего: безымянный запрос до чтения
	// страницы не доходит вовсе. Пока формат курсора проверял только репозиторий,
	// один и тот же мусорный page_token получал разный ответ в зависимости от того,
	// опознан ли вызывающий, — то есть проверка ввода зависела от прав. Проверка
	// формата решением о доступе не является; репозиторий остаётся авторитетным на
	// служимом пути.
	if err := listpage.ValidatePagination(p.PageToken, p.PageSize); err != nil {
		return nil, "", err
	}
	if f.ProjectID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "project_id required")
	}
	// Форма ссылки на подсеть — тоже проверка ввода, и она стоит здесь, рядом с
	// пагинацией, а не уезжает в SQL. Подсеть принадлежит этому же сервису, значит
	// малформед — синхронный `InvalidArgument`, а не пустая страница: пустая
	// страница утверждала бы «в этой подсети адресов нет» про строку, которая
	// подсетью быть не может. Пустое значение `corevalidate.ResourceID`
	// пропускает — это «без сужения», и так его и читает репозиторий.
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, f.SubnetID); err != nil {
		return nil, "", err
	}
	// Форма ЗНАЧЕНИЯ — по тому же правилу и по той же причине, что форма ссылки
	// выше. `ip_address` отвечает на вопрос «чей это адрес?»; у строки, которая
	// адресом быть не может, ответа нет ни при каких данных, поэтому пустая
	// страница означала бы «такого адреса ни у кого нет» — ложное утверждение об
	// отсутствии, а вызывающий не отличил бы его от «значение никому не
	// принадлежит». Различать эти два исхода и есть назначение поля.
	//
	// Это поле и `subnet_id` — сужения ОДНОГО списка, оба заведены одной волной как
	// замена снятым методам. Проверку тогда получило только второе; здесь класс
	// закрывается у обоих. Снятый поиск по значению сняли в том числе за ложное
	// «не найдено» на запрос без предмета — замена не наследует это послабление.
	//
	// Пустое значение пропускается: это «без сужения», и так его читает репозиторий.
	if err := validateNarrowValue(f.IPAddress); err != nil {
		return nil, "", err
	}
	// Предусловие сужения — ДО чтения страницы из БД.
	//
	// Оно стоит здесь, а не внутри сужателя, потому что между решением и сужением
	// лежит курсорный запрос к своей базе: безымянный запрос оплачивал бы его, чтобы
	// получить отказ на прочитанном. Проверка формата остаётся ПЕРВОЙ и выше —
	// ответ на некорректный ввод не должен зависеть от того, что вызывающему выдано.
	if err := listnarrow.Precheck(ctx, u.narrower); err != nil {
		return nil, "", err
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()

	addrs, nextToken, err := u.listFiltered(ctx, r, f, p)
	if err != nil {
		return nil, "", err
	}
	loadUsedBy(ctx, r.Addresses(), addrs)
	return addrs, nextToken, nil
}

// validateNarrowValue — форма значения, по которому сужается список адресов.
//
// Предикат — «это вообще адрес», а не «это адрес известного нам вида»: значение
// хранится в четырёх формах владения (внутренний и внешний, два семейства), и
// проверка, принимающая только часть из них, отвергала бы законный вопрос.
//
// Зона интерфейса отвергается отдельно: `netip.ParseAddr` её разбирает, но хранимое
// значение — голый адрес, зоны в нём нет никогда, поэтому зона-квалифицированная
// строка не совпадёт ни с одной строкой и даёт то же ложное «ни у кого нет».
// Успешность разбора сама по себе класс не закрывает.
//
// Пустая строка — «без сужения», не ошибка (симметрично `corevalidate.ResourceID`).
func validateNarrowValue(v string) error {
	if v == "" {
		return nil
	}
	addr, err := netip.ParseAddr(v)
	if err != nil || addr.Zone() != "" {
		return status.Error(codes.InvalidArgument, "Illegal argument ip_address")
	}
	return nil
}

// listFiltered читает страницу и оставляет из неё только видимые subject'у строки.
func (u *ListAddressesUseCase) listFiltered(ctx context.Context, r Reader, f AddressFilter, p Pagination) ([]*kachorepo.AddressRecord, string, error) {
	// Формат page_token/page_size проверен вызывающим (Execute) — repo.List повторяет обе
	// проверки как авторитетный backstop служимого пути.
	addrs, next, lerr := r.Addresses().List(ctx, f, p)
	if lerr != nil {
		return nil, "", serviceerr.MapRepoErr(lerr)
	}
	if len(addrs) == 0 {
		return addrs, next, nil
	}
	visible, ferr := listnarrow.Page(ctx, u.narrower,
		authzfilter.ResourceTypeAddress, authzfilter.ActionAddressList, addrs,
		func(rec *kachorepo.AddressRecord) string { return rec.ID })
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, next, nil
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
