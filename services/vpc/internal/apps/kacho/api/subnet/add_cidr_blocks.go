// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

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

// AddCidrBlocksUseCase — атомарное добавление CIDR-блоков к подсети.
// Возвращает Operation; внутри worker'а:
//   - Get subnet (FOR UPDATE) → если не найден → NotFound.
//   - Validate каждого CIDR (host-bits=0).
//   - Get parent Network → каждый добавляемый блок ⊆ супернета сети (F7/VPC-1-34);
//     блок вне супернета → InvalidArgument.
//   - Проверка overlap внутри новой объединенной коллекции (v4 + v6).
//   - SetCidrBlocks (DB UPDATE) — внутри него child-таблица subnet_cidr_blocks
//     пересобирается и ее EXCLUDE gist ловит пересечение ЛЮБОГО блока (primary и
//     вторичного) с блоками других подсетей той же сети.
//
// Cross-subnet non-overlap гарантируется только на DB-уровне (network-scoped
// EXCLUDE): GetForUpdate сериализует лишь операции над ОДНОЙ подсетью, поэтому
// пересечение блоков РАЗНЫХ подсетей одной сети ловит declarative-инвариант,
// а не software-проверка (она была бы TOCTOU-prone между подсетями).
//
// Get + SetCidrBlocks + outbox-emit UPDATED атомарны в одной writer-TX.
type AddCidrBlocksUseCase struct {
	repo    Repo
	opsRepo operations.Repo
}

// NewAddCidrBlocksUseCase создает AddCidrBlocksUseCase.
func NewAddCidrBlocksUseCase(r Repo, opsRepo operations.Repo) *AddCidrBlocksUseCase {
	return &AddCidrBlocksUseCase{repo: r, opsRepo: opsRepo}
}

// Execute — sync-валидация id/CIDR-формата + Operation + async-merge в worker'е.
func (u *AddCidrBlocksUseCase) Execute(ctx context.Context, id string, v4, v6 []string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, serviceerr.InvalidArg("v4_cidr_blocks", "v4_cidr_blocks or v6_cidr_blocks is required")
	}
	for i, c := range v4 {
		if err := validateSubnetV4CIDR(fmt.Sprintf("v4_cidr_blocks[%d]", i), c); err != nil {
			return nil, err
		}
	}
	for i, c := range v6 {
		if err := validateSubnetV6CIDR(fmt.Sprintf("v6_cidr_blocks[%d]", i), c); err != nil {
			return nil, err
		}
	}
	// Disjointness внутри переданного v6-списка (sync; mirror v4 — для v4 это
	// проверяется ниже на merged-наборе, что покрывает и intra-request).
	if err := checkCIDRDisjoint("v6_cidr_blocks", v6); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Add CIDR blocks to subnet %s", id),
		&vpcv1.UpdateSubnetMetadata{SubnetId: id},
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

		// FOR UPDATE: сериализует конкурентные Add/RemoveCidrBlocks на этой
		// подсети — закрывает lost-update.
		sub, gerr := w.Subnets().GetForUpdate(ctx, id)
		if gerr != nil {
			return nil, serviceerr.MapRepoErr(gerr)
		}
		// F7 (VPC-1-34): добавляемый диапазон обязан лежать ВНУТРИ объявленного
		// супернета родительской сети (within-service, та же БД). Фетчим network в
		// той же writer-TX и валидируем containment каждого добавляемого блока ⊆
		// одного из network CIDR-блоков соответствующего семейства. Пустой супернет
		// (legacy-сеть) → skip (back-compat, как в Subnet.Create). Блок вне супернета
		// → InvalidArgument "subnet CIDR <X> is not within any network CIDR block".
		parentNet, nerr := w.Networks().Get(ctx, sub.NetworkID)
		if nerr != nil {
			return nil, serviceerr.MapRepoErr(nerr)
		}
		if verr := validateSubnetWithinSupernet(parentNet.IPv4CidrBlocks, parentNet.IPv6CidrBlocks, v4, v6); verr != nil {
			return nil, verr
		}
		mergedV4 := append([]string{}, sub.V4CidrBlocks...)
		mergedV4 = append(mergedV4, v4...)
		// Проверка пересечений внутри объединенного набора (sync, host-bits уже OK).
		// Покрывает overlap нового блока с уже существующим в этой же подсети.
		if err := checkCIDRDisjoint("v4_cidr_blocks", mergedV4); err != nil {
			return nil, err
		}
		// v6: то же самое.
		mergedV6 := append([]string{}, sub.V6CidrBlocks...)
		mergedV6 = appendDedup(mergedV6, v6)
		if err := checkCIDRDisjoint("v6_cidr_blocks", mergedV6); err != nil {
			return nil, err
		}
		updated, uerr := w.Subnets().SetCidrBlocks(ctx, id, mergedV4, mergedV6)
		if uerr != nil {
			return nil, serviceerr.MapRepoErr(uerr)
		}
		if err := w.Outbox().Emit(ctx, "Subnet", updated.ID, "UPDATED", helpers.DomainToMap(updated)); err != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
		}
		if err := w.Commit(); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
		return marshalSubnetRecord(updated)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}
