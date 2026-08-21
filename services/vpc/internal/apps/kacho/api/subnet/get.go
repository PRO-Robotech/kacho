// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// GetSubnetUseCase — простой read через CQRS Reader. Возвращает
// `*kacho.SubnetRecord` (repo-leaf entity).
//
// Открывает Reader-TX явно через `repo.Reader(ctx)` — routing на slave-реплику
// станет автоматическим, когда та появится; пока на той же мастер-pool.
//
// # AuthZ
//
// Видимость единичного чтения энфорсит per-RPC authz-interceptor ПРЯМЫМ
// per-object Check'ом (`vpc_subnet:<id>` / relation `v_get`, permission_map),
// ровно как для Update/Delete/AddCidrBlocks. Deny на СУЩЕСТВУЮЩЕМ объекте
// interceptor превращает в NotFound (existence-hiding, ErrHideExistence), deny на
// отсутствующем — в passthrough, и handler отдаёт дословный NotFound из БД. Поэтому
// use-case никакой собственной authz-проверки не делает и не знает про фильтр.
//
// Здесь ранее стоял второй гейт `enforceGetVisible`, который спрашивал
// «перечисли ВСЕ subnet'ы, которые subject'у можно» и искал id в ответе. Он был
// (а) избыточен — тот же вопрос уже задан interceptor'ом точнее и дешевле, и
// (б) НЕВЕРЕН: перечисление упиралось в жёсткий предел прежнего движка прав
// (1000 по умолчанию, без продолжения), поэтому на долгоживущем сторе
// собственная подсеть тенанта выпадала за префикс и Get отдавал 404 при
// существующей строке и существующем гранте. Подробности — package-doc
// `internal/authzfilter`.
type GetSubnetUseCase struct {
	repo Repo
}

// NewGetSubnetUseCase создает GetSubnetUseCase.
func NewGetSubnetUseCase(r Repo) *GetSubnetUseCase {
	return &GetSubnetUseCase{repo: r}
}

// Execute возвращает repo-entity Subnet. NotFound → mapRepoErr → gRPC NotFound.
func (u *GetSubnetUseCase) Execute(ctx context.Context, id string) (*kachorepo.SubnetRecord, error) {
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, id); err != nil {
		return nil, err
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()
	s, err := r.Subnets().Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	return s, nil
}
