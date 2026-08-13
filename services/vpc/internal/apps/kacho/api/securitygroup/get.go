// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// GetSecurityGroupUseCase — простой read через CQRS Reader. Use-case можно было бы
// опустить, но handler-у удобнее единый шов через use-case'ы. Открывает Reader,
// читает, закрывает; read-only TX — параллельный writer не блокируется.
//
// # AuthZ
//
// Видимость единичного чтения энфорсит per-RPC authz-interceptor ПРЯМЫМ
// per-object Check'ом (`vpc_security_group:<id>` / relation `v_get`,
// permission_map), ровно как для Update/UpdateRules/Delete. Deny на СУЩЕСТВУЮЩЕМ
// объекте interceptor превращает в NotFound (existence-hiding, ErrHideExistence),
// deny на отсутствующем — в passthrough, и handler отдаёт дословный NotFound из БД.
// Поэтому use-case никакой собственной authz-проверки не делает и не знает про фильтр.
//
// Здесь ранее стоял второй гейт `enforceGetVisible`, который спрашивал
// «перечисли ВСЕ security group'ы, которые subject'у можно» и искал id в ответе.
// Он был (а) избыточен — тот же вопрос уже задан interceptor'ом точнее и дешевле,
// и (б) НЕВЕРЕН: перечисление упирается в жёсткий предел OpenFGA ListObjects
// (default 1000, без continuation-token'а), поэтому на долгоживущем сторе
// собственная SG тенанта выпадала за префикс и Get отдавал 404 при существующей
// строке и существующем гранте. Подробности — package-doc `internal/authzfilter`.
type GetSecurityGroupUseCase struct {
	repo Repo
}

// NewGetSecurityGroupUseCase создает GetSecurityGroupUseCase.
func NewGetSecurityGroupUseCase(r Repo) *GetSecurityGroupUseCase {
	return &GetSecurityGroupUseCase{repo: r}
}

// Execute возвращает repo-entity SG. NotFound → mapRepoErr → gRPC NotFound.
func (u *GetSecurityGroupUseCase) Execute(ctx context.Context, id string) (*kacho.SecurityGroupRecord, error) {
	if err := corevalidate.ResourceID("security group", ids.PrefixSecurityGroup, id); err != nil {
		return nil, err
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()
	sg, err := rd.SecurityGroups().Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if uerr := loadUsedBy(ctx, rd.SecurityGroups(), []*kacho.SecurityGroupRecord{sg}); uerr != nil {
		return nil, serviceerr.MapRepoErr(uerr)
	}
	return sg, nil
}
