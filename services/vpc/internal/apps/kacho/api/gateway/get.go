// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// GetGatewayUseCase — простой read через CQRS Reader. Возвращает
// `*kacho.GatewayRecord` (repo-leaf entity).
//
// Открывает read-only TX через `repo.Reader(ctx)`; закрытие читателя — defer
// `rd.Close()` (no-op rollback на read-only TX, освобождает соединение).
//
// # AuthZ
//
// Видимость единичного чтения энфорсит per-RPC authz-interceptor ПРЯМЫМ
// per-object Check'ом (`vpc_gateway:<id>` / relation `v_get`, permission_map),
// ровно как для Update/Delete. Deny на СУЩЕСТВУЮЩЕМ объекте interceptor
// превращает в NotFound (existence-hiding, ErrHideExistence), deny на
// отсутствующем — в passthrough, и handler отдаёт дословный NotFound из БД. Поэтому
// use-case никакой собственной authz-проверки не делает и не знает про фильтр.
//
// Здесь ранее стоял второй гейт `enforceGetVisible`, который спрашивал
// «перечисли ВСЕ gateway'и, которые subject'у можно» и искал id в ответе. Он был
// (а) избыточен — тот же вопрос уже задан interceptor'ом точнее и дешевле, и
// (б) НЕВЕРЕН: перечисление упирается в жёсткий предел OpenFGA ListObjects
// (default 1000, без continuation-token'а), поэтому на долгоживущем сторе
// собственный gateway тенанта выпадал за префикс и Get отдавал 404 при
// существующей строке и существующем гранте. Подробности — package-doc
// `internal/authzfilter`.
type GetGatewayUseCase struct {
	repo Repo
}

// NewGetGatewayUseCase создает GetGatewayUseCase.
func NewGetGatewayUseCase(r Repo) *GetGatewayUseCase {
	return &GetGatewayUseCase{repo: r}
}

// Execute возвращает repo-entity Gateway. NotFound → mapRepoErr → gRPC NotFound.
func (u *GetGatewayUseCase) Execute(ctx context.Context, id string) (*kacho.GatewayRecord, error) {
	if err := corevalidate.ResourceID("gateway", ids.PrefixGateway, id); err != nil {
		return nil, err
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()

	g, err := rd.Gateways().Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	return g, nil
}
