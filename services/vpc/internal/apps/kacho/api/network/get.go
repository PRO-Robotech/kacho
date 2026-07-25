// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// GetNetworkUseCase — простой read через CQRS Reader. Возвращает
// `*kacho.NetworkRecord` (repo-leaf entity).
//
// Reader-TX открывается явно через `repo.Reader(ctx)` — routing на slave-реплику
// станет automatic, когда та появится; пока на той же мастер-pool.
//
// # AuthZ
//
// Видимость единичного чтения энфорсит per-RPC authz-interceptor ПРЯМЫМ
// per-object Check'ом (`vpc_network:<id>` / relation `v_get`, permission_map),
// ровно как для Update/Delete/AddCidrBlocks. Deny на СУЩЕСТВУЮЩЕМ объекте
// interceptor превращает в NotFound (existence-hiding, ErrHideExistence), deny на
// отсутствующем — в passthrough, и handler отдаёт дословный NotFound из БД. Поэтому
// use-case никакой собственной authz-проверки не делает и не знает про фильтр.
//
// Здесь ранее стоял второй гейт `enforceGetVisible`, который спрашивал
// «перечисли ВСЕ networks, которые subject'у можно» и искал id в ответе. Он был
// (а) избыточен — тот же вопрос уже задан interceptor'ом точнее и дешевле, и
// (б) НЕВЕРЕН: перечисление упирается в жёсткий предел OpenFGA ListObjects
// (default 1000, без continuation-token'а), поэтому на долгоживущем сторе
// собственный network тенанта выпадал за префикс и Get отдавал 404 при
// существующей строке и существующем гранте. Подробности — package-doc
// `internal/authzfilter`.
type GetNetworkUseCase struct {
	repo Repo
}

// NewGetNetworkUseCase создает GetNetworkUseCase.
func NewGetNetworkUseCase(r Repo) *GetNetworkUseCase {
	return &GetNetworkUseCase{repo: r}
}

// Execute возвращает repo-entity Network. NotFound → mapRepoErr → gRPC NotFound.
func (u *GetNetworkUseCase) Execute(ctx context.Context, id string) (*kachorepo.NetworkRecord, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, id); err != nil {
		return nil, err
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()
	n, err := r.Networks().Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	return n, nil
}
