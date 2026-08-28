// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// AddCidrBlocksUseCase — расширение состава набора.
//
// Идемпотентен: добавление уже присутствующего члена — успех без изменения.
// Причина названа в контракте и повторена здесь, потому что она объясняет форму
// кода: глагол асинхронный, значит вызывающий обязан уметь повторить операцию
// после неопределённого исхода, а неидемпотентный глагол превращал бы штатный
// повтор в ложный отказ.
//
// Потолок и отсутствие затирания держит writer репозитория одним условным
// обновлением счётчика под блокировкой строки — здесь остаётся только граница
// стоимости ОДНОГО запроса.
type AddCidrBlocksUseCase struct {
	repo    Repo
	opsRepo operations.Repo
}

// NewAddCidrBlocksUseCase создаёт AddCidrBlocksUseCase.
func NewAddCidrBlocksUseCase(r Repo, opsRepo operations.Repo) *AddCidrBlocksUseCase {
	return &AddCidrBlocksUseCase{repo: r, opsRepo: opsRepo}
}

// Execute — sync-валидация id и членов + Operation + запись в worker'е.
func (u *AddCidrBlocksUseCase) Execute(ctx context.Context, id string, v4, v6 []string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("cidr_group", ids.PrefixCidrGroupHyphen, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, serviceerr.InvalidArg("v4_cidr_blocks", "v4_cidr_blocks or v6_cidr_blocks is required")
	}
	if err := validateCidrGroupCardinality(v4, v6); err != nil {
		return nil, err
	}
	normV4, err := normalizeCidrBlocks("v4_cidr_blocks", v4, true)
	if err != nil {
		return nil, err
	}
	normV6, err := normalizeCidrBlocks("v6_cidr_blocks", v6, false)
	if err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Add CIDR blocks to cidr group %s", id),
		&vpcv1.UpdateCidrGroupMetadata{CidrGroupId: id},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, u.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		w, werr := u.repo.Writer(ctx)
		if werr != nil {
			return nil, serviceerr.MapRepoErr(werr)
		}
		defer w.Abort()

		updated, aerr := w.CidrGroups().AddBlocks(ctx, id, normV4, normV6)
		if aerr != nil {
			return nil, serviceerr.MapRepoErr(aerr)
		}
		if err := w.Outbox().Emit(ctx, "CidrGroup", updated.ID, updated.ProjectID, "UPDATED", helpers.DomainToMap(updated)); err != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
		}
		if err := w.Commit(); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
		return marshalCidrGroupRecord(updated)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}
