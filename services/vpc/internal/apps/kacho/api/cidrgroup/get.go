// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// GetCidrGroupUseCase — синхронное чтение через CQRS Reader.
//
// # AuthZ
//
// Видимость одиночного чтения энфорсит per-RPC authz-интерсептор ПРЯМЫМ
// пообъектным Check'ом (`vpc_cidr_group:<id>` / `v_get`). Отказ на СУЩЕСТВУЮЩЕМ
// объекте интерсептор превращает в NotFound, побайтово равный настоящему
// промаху владельца; отказ на отсутствующем проходит насквозь, и handler отдаёт
// дословный NotFound из базы. Поэтому use-case собственной проверки прав не
// делает и про фильтр не знает.
type GetCidrGroupUseCase struct {
	repo Repo
}

// NewGetCidrGroupUseCase создаёт GetCidrGroupUseCase.
func NewGetCidrGroupUseCase(r Repo) *GetCidrGroupUseCase {
	return &GetCidrGroupUseCase{repo: r}
}

// Execute — формат id ПЕРВЫМ стейтментом, затем чтение.
//
// Порядок несущий: без format-check малформированный id уехал бы в repo и вернул
// NotFound — то есть утверждение об отсутствии ресурса на строку, которая
// ресурсом быть не может.
func (u *GetCidrGroupUseCase) Execute(ctx context.Context, id string) (*kachorepo.CidrGroupRecord, error) {
	if err := corevalidate.ResourceID("cidr_group", ids.PrefixCidrGroupHyphen, id); err != nil {
		return nil, err
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()
	rec, err := r.CidrGroups().Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	return rec, nil
}
