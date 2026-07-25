// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// GetNetworkInterfaceUseCase — простой read через CQRS Reader.
//
// Открывает reader-TX через CQRS-iface; Reader идет на master-pool, а при наличии
// slave-реплики kacho.Repository.Reader будет роутить туда.
//
// # AuthZ
//
// Видимость единичного чтения энфорсит per-RPC authz-interceptor ПРЯМЫМ
// per-object Check'ом (`vpc_network_interface:<id>` / relation `v_get`,
// permission_map), ровно как для Update/Delete. Deny на СУЩЕСТВУЮЩЕМ объекте
// interceptor превращает в NotFound (existence-hiding, ErrHideExistence), deny на
// отсутствующем — в passthrough, и handler отдаёт дословный NotFound из БД. Поэтому
// use-case никакой собственной authz-проверки не делает и не знает про фильтр.
//
// Здесь ранее стоял второй гейт `enforceGetVisible`, который спрашивал
// «перечисли ВСЕ NIC'и, которые subject'у можно» и искал id в ответе. Он был
// (а) избыточен — тот же вопрос уже задан interceptor'ом точнее и дешевле, и
// (б) НЕВЕРЕН: перечисление упирается в жёсткий предел OpenFGA ListObjects
// (default 1000, без continuation-token'а), поэтому на долгоживущем сторе
// собственный NIC тенанта выпадал за префикс и Get отдавал 404 при существующей
// строке и существующем гранте. Подробности — package-doc `internal/authzfilter`.
type GetNetworkInterfaceUseCase struct {
	repo Repo
}

// NewGetNetworkInterfaceUseCase создает GetNetworkInterfaceUseCase.
func NewGetNetworkInterfaceUseCase(r Repo) *GetNetworkInterfaceUseCase {
	return &GetNetworkInterfaceUseCase{repo: r}
}

// Execute возвращает repo-entity NIC. NotFound → mapRepoErr → gRPC NotFound.
func (u *GetNetworkInterfaceUseCase) Execute(ctx context.Context, id string) (*kachorepo.NetworkInterfaceRecord, error) {
	if err := niResourceID(id); err != nil {
		return nil, err
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()
	got, err := rd.NetworkInterfaces().Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	return got, nil
}
