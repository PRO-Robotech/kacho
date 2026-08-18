// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// GetRouteTableUseCase — простой read через CQRS Reader. Возвращает
// `*kacho.RouteTableRecord` (repo-leaf entity).
//
// # AuthZ
//
// Видимость единичного чтения энфорсит per-RPC authz-interceptor ПРЯМЫМ
// per-object Check'ом (`vpc_route_table:<id>` / relation `v_get`, permission_map),
// ровно как для Update и Delete. Deny на СУЩЕСТВУЮЩЕМ объекте
// interceptor превращает в NotFound (existence-hiding, ErrHideExistence), deny на
// отсутствующем — в passthrough, и handler отдаёт дословный NotFound из БД. Поэтому
// use-case никакой собственной authz-проверки не делает и не знает про фильтр.
//
// Здесь ранее стоял второй гейт `enforceGetVisible`, который спрашивал
// «перечисли ВСЕ route table'ы, которые subject'у можно» и искал id в ответе. Он
// был (а) избыточен — тот же вопрос уже задан interceptor'ом точнее и дешевле, и
// (б) НЕВЕРЕН: перечисление упирается в жёсткий предел OpenFGA ListObjects
// (default 1000, без continuation-token'а), поэтому на долгоживущем сторе
// собственный route table тенанта выпадал за префикс и Get отдавал 404 при
// существующей строке и существующем гранте. Подробности — package-doc
// `internal/authzfilter`.
type GetRouteTableUseCase struct {
	repo Repo
}

// NewGetRouteTableUseCase создает GetRouteTableUseCase.
func NewGetRouteTableUseCase(r Repo) *GetRouteTableUseCase {
	return &GetRouteTableUseCase{repo: r}
}

// Execute возвращает repo-entity RouteTable.
func (u *GetRouteTableUseCase) Execute(ctx context.Context, id string) (*kacho.RouteTableRecord, error) {
	if err := corevalidate.ResourceID("route table", ids.PrefixRouteTable, id); err != nil {
		return nil, err
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()
	rt, gerr := rd.RouteTables().Get(ctx, id)
	if gerr != nil {
		return nil, serviceerr.MapRepoErr(gerr)
	}
	return rt, nil
}
