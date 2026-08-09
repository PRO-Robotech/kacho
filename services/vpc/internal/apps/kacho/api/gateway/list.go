// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

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
	repo     Repo
	narrower *listnarrow.Narrower
}

// NewListGatewaysUseCase создает ListGatewaysUseCase. filter может быть nil.
func NewListGatewaysUseCase(r Repo, n *listnarrow.Narrower) *ListGatewaysUseCase {
	return &ListGatewaysUseCase{repo: r, narrower: n}
}

// Execute — проверяет project_id, читает страницу и фильтрует её per-object.
// iam недоступен → fail-closed Unavailable (страница НЕ отдается нефильтрованной).
func (u *ListGatewaysUseCase) Execute(ctx context.Context, f GatewayFilter, p Pagination) ([]*kacho.GatewayRecord, string, error) {
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
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()

	// Формат page_token/page_size уже проверен выше — repo.List повторяет обе
	// проверки как авторитетный backstop служимого пути.
	gws, nextToken, lerr := rd.Gateways().List(ctx, f, p)
	if lerr != nil {
		return nil, "", serviceerr.MapRepoErr(lerr)
	}
	if len(gws) == 0 {
		return gws, nextToken, nil
	}
	visible, ferr := listnarrow.Page(ctx, u.narrower,
		authzfilter.ResourceTypeGateway, authzfilter.ActionGatewayList, gws,
		func(rec *kacho.GatewayRecord) string { return rec.ID })
	if ferr != nil {
		return nil, "", ferr
	}
	return visible, nextToken, nil
}

// ListOperationsUseCase — операции, относящиеся к конкретному gateway-id.
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
func (u *ListOperationsUseCase) Execute(ctx context.Context, gwID string, p Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID("gateway", ids.PrefixGateway, gwID); err != nil {
		return nil, "", err
	}
	return operations.ListForCaller(ctx, u.opsRepo, operations.ListFilter{
		ResourceID: gwID,
		PageSize:   p.PageSize,
		PageToken:  p.PageToken,
	})
}
