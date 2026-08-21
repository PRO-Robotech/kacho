// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

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

// ListSubnetsUseCase — list subnets с пагинацией; project_id обязателен (закрывает
// cross-project enumeration). Читает СТРАНИЦУ через CQRS Reader (курсор,
// project-scoped) и оставляет из неё только видимые subject'у строки — per-object,
// через `AuthorizeService.BatchCheck` (viewer ∪ v_list; read==enforce). При
// filter==nil или пустом subject — passthrough без обращения к FGA.
//
// Порядок принципиален: страница берётся из БД ПЕРВОЙ, права проверяются на её
// идентификаторах. Обратный порядок («перечисли все разрешённые id → сузь ими SQL»)
// упирался в жёсткий предел перечисления у прежнего движка прав (1000, без
// продолжения) и делал собственные ресурсы
// тенанта невидимыми — см. package-doc `internal/authzfilter`. Побочный эффект
// нового порядка: страница может вернуться НЕПОЛНОЙ (часть строк отфильтрована) —
// это нормально для cursor-пагинации, next_page_token берётся от последней
// ПРОСМОТРЕННОЙ строки, поэтому обхода без пропусков это не ломает.
type ListSubnetsUseCase struct {
	repo     Repo
	narrower *listnarrow.Narrower
}

// NewListSubnetsUseCase создает ListSubnetsUseCase. filter может быть nil
// (list-filter disabled / dev) → unfiltered passthrough.
func NewListSubnetsUseCase(r Repo, n *listnarrow.Narrower) *ListSubnetsUseCase {
	return &ListSubnetsUseCase{repo: r, narrower: n}
}

// Execute — проверяет project_id, читает страницу и фильтрует её per-object.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
func (u *ListSubnetsUseCase) Execute(ctx context.Context, f SubnetFilter, p Pagination) ([]*kachorepo.SubnetRecord, string, error) {
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

	// Формат page_token/page_size уже проверен выше — repo.List повторяет обе
	// проверки как авторитетный backstop служимого пути.
	subs, next, lerr := r.Subnets().List(ctx, f, p)
	if lerr != nil {
		return nil, "", lerr
	}
	if len(subs) == 0 {
		return subs, next, nil
	}
	visible, ferr := listnarrow.Page(ctx, u.narrower,
		authzfilter.ResourceTypeSubnet, authzfilter.ActionSubnetList, subs,
		func(rec *kachorepo.SubnetRecord) string { return rec.ID })
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, next, nil
}

// ListOperationsUseCase — операции, относящиеся к конкретному subnet-id.
// NB: без repo.Get-precondition — операции должны быть доступны и после Delete
// (история операций; rows в `operations` не имеют FK cascade).
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
func (u *ListOperationsUseCase) Execute(ctx context.Context, subnetID string, p Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, subnetID); err != nil {
		return nil, "", err
	}
	return operations.ListForCaller(ctx, u.opsRepo, operations.ListFilter{
		ResourceID: subnetID,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
