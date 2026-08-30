// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"
	"fmt"
	"strings"

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

// RemoveCidrBlocksUseCase — атомарное удаление CIDR-блоков из подсети.
//
// Правила:
//   - удаление primary-anchor (blocks[0] = ipv4CidrPrimary/ipv6CidrPrimary,
//     immutable после Create) → InvalidArgument (нельзя сменить placement-якорь).
//   - CIDR не присутствует в подсети → FailedPrecondition.
//   - удаление последнего CIDR → FailedPrecondition (subnet не может быть пустой).
//   - в снимаемом диапазоне живут адреса подсети → FailedPrecondition
//     "subnet CIDR <cidr> has allocated addresses" (проверка в той же
//     writer-TX, после row-lock подсети).
//
// Get + SetCidrBlocks + outbox-emit UPDATED атомарны в одной writer-TX.
type RemoveCidrBlocksUseCase struct {
	repo    Repo
	opsRepo operations.Repo
}

// NewRemoveCidrBlocksUseCase создает RemoveCidrBlocksUseCase.
func NewRemoveCidrBlocksUseCase(r Repo, opsRepo operations.Repo) *RemoveCidrBlocksUseCase {
	return &RemoveCidrBlocksUseCase{repo: r, opsRepo: opsRepo}
}

// Execute — sync-валидация id + Operation + async-вычитание в worker'е.
func (u *RemoveCidrBlocksUseCase) Execute(ctx context.Context, id string, v4, v6 []string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, serviceerr.InvalidArg(blocksCidrFields.v4,
			blocksCidrFields.v4+" or "+blocksCidrFields.v6+" is required")
	}
	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Remove CIDR blocks from subnet %s", id),
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
		// подсети — lost-update исключен.
		sub, gerr := w.Subnets().GetForUpdate(ctx, id)
		if gerr != nil {
			return nil, serviceerr.MapRepoErr(gerr)
		}
		// F7 (immutable anchor): blocks[0] каждого семейства — это placement-anchor
		// ipv4CidrPrimary / ipv6CidrPrimary, immutable после Create. Удаление primary
		// молча промотировало бы следующий блок в anchor (смена placement-якоря) →
		// reject конвенционным immutable-тоном (паритет с Subnet.Update immutable-switch).
		if len(sub.V4CidrBlocks) > 0 {
			for _, c := range v4 {
				if c == sub.V4CidrBlocks[0] {
					return nil, serviceerr.InvalidArg("ipv4_cidr_primary", "ipv4_cidr_primary is immutable after Subnet.Create")
				}
			}
		}
		if len(sub.V6CidrBlocks) > 0 {
			for _, c := range v6 {
				if c == sub.V6CidrBlocks[0] {
					return nil, serviceerr.InvalidArg("ipv6_cidr_primary", "ipv6_cidr_primary is immutable after Subnet.Create")
				}
			}
		}
		remainingV4, removedV4 := subtractCIDRs(sub.V4CidrBlocks, v4)
		remainingV6, removedV6 := subtractCIDRs(sub.V6CidrBlocks, v6)
		if removedV4 != len(v4) || removedV6 != len(v6) {
			return nil, status.Errorf(codes.FailedPrecondition, "one or more CIDR blocks not found in subnet")
		}
		if len(remainingV4) == 0 && len(remainingV6) == 0 {
			return nil, status.Errorf(codes.FailedPrecondition, "cannot remove last CIDR block from subnet")
		}
		// Снятие диапазона удаляет строку subnet_cidr_blocks, которая держит
		// запрет пересечения диапазонов внутри сети. Если в диапазоне живут
		// адреса, его получит другая подсеть той же сети и выдаст те же адреса
		// заново — уникальность внутреннего адреса ключуется подсетью, поэтому
		// база такой дубль уже не поймает. Счёт идёт в той же writer-TX ПОСЛЕ
		// row-lock подсети (GetForUpdate выше). Конкурентная выдача внутреннего
		// адреса читает набор диапазонов под FOR SHARE на той же строке
		// (allocateInternal*IntoTx), поэтому один из двух путей ждёт другого:
		// либо адрес уже записан и попадает в счёт, либо выдача идёт после
		// снятия и видит уже суженный набор. Полагаться на неявную блокировку
		// внешнего ключа нельзя — она берётся только когда меняется колонка
		// подсети, а выдача адреса её не меняет.
		removed := append(append([]string{}, v4...), v6...)
		occupied, cerr := w.Subnets().OccupiedCidrs(ctx, id, removed)
		if cerr != nil {
			return nil, serviceerr.MapRepoErr(cerr)
		}
		if len(occupied) > 0 {
			// Названы ЗАНЯТЫЕ диапазоны, а не весь снимаемый набор: иначе отказ
			// утверждал бы занятость и про чистые диапазоны запроса.
			return nil, status.Errorf(codes.FailedPrecondition,
				"subnet CIDR %s has allocated addresses", strings.Join(occupied, ", "))
		}
		updated, uerr := w.Subnets().SetCidrBlocks(ctx, id, remainingV4, remainingV6)
		if uerr != nil {
			return nil, serviceerr.MapRepoErr(uerr)
		}
		if err := w.Outbox().Emit(ctx, "Subnet", updated.ID, updated.ProjectID, "UPDATED", helpers.DomainToMap(updated)); err != nil {
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
