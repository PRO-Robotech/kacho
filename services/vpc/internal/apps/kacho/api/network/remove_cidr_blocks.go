// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

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

// RemoveCidrBlocksUseCase — атомарное сужение declared-супернета сети (F2/VPC-1-10).
// Внутри worker'а:
//   - GetForUpdate(network) (row-lock) сериализует конкурентные Add/Remove.
//   - ∉-guard: блок, всё ещё покрывающий CIDR живой подсети (не покрытый ни одним
//     remaining-блоком), удалить нельзя → FAILED_PRECONDITION (иначе subnet.primary
//     осиротел бы вне супернета). Проверка — single-query по subnet_cidr_blocks
//     (SupernetBlockCoveringSubnet) под network row-lock в той же writer-TX: учитывает
//     ВСЕ подсети сети (любое их число), не только первую страницу — не software-TOCTOU.
//   - SetCidrBlocks(remaining) + outbox UPDATED — одна writer-TX.
//
// Op-in-response: reject приходит embedded в Operation.error (op-in-response), не
// как return-ошибка (worker-fn возвращает status.Error → RunSync кладёт в op).
type RemoveCidrBlocksUseCase struct {
	repo    Repo
	opsRepo operations.Repo
}

// NewRemoveCidrBlocksUseCase создаёт RemoveCidrBlocksUseCase.
func NewRemoveCidrBlocksUseCase(r Repo, opsRepo operations.Repo) *RemoveCidrBlocksUseCase {
	return &RemoveCidrBlocksUseCase{repo: r, opsRepo: opsRepo}
}

// Execute — sync-валидация id/CIDR-формата + Operation + синхронный subtract в worker'е.
func (u *RemoveCidrBlocksUseCase) Execute(ctx context.Context, id string, v4, v6 []string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, serviceerr.InvalidArg("ipv4_cidr_blocks", "ipv4_cidr_blocks or ipv6_cidr_blocks is required")
	}
	if err := validateNetworkSupernet(v4, v6); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Remove CIDR blocks from network %s", id),
		&vpcv1.UpdateNetworkMetadata{NetworkId: id},
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

		n, gerr := w.Networks().GetForUpdate(ctx, id)
		if gerr != nil {
			return nil, serviceerr.MapRepoErr(gerr)
		}
		remainingV4 := subtractCidrBlocks(n.IPv4CidrBlocks, v4)
		remainingV6 := subtractCidrBlocks(n.IPv6CidrBlocks, v6)

		// ∉-guard: под network row-lock single-query проверяем, что ни один удаляемый
		// блок не осиротит живую подсеть (её CIDR ⊆ удаляемого блока и НЕ покрыт ни
		// одним из остающихся). Один indexed-запрос по subnet_cidr_blocks — корректно
		// при ЛЮБОМ числе подсетей (без окна пагинации; прежний List первой страницы
		// пропускал подсети со 2-й страницы у сетей с >50 подсетями). candidate/retained
		// смешивают семейства (v4+v6): cidr `>>=` не пересекает разные семейства.
		candidate := append(append([]string{}, v4...), v6...)
		retained := append(append([]string{}, remainingV4...), remainingV6...)
		if b, cerr := w.Subnets().SupernetBlockCoveringSubnet(ctx, id, candidate, retained); cerr != nil {
			return nil, serviceerr.MapRepoErr(cerr)
		} else if b != "" {
			return nil, status.Errorf(codes.FailedPrecondition, "network CIDR block %s still contains subnets", b)
		}

		updated, uerr := w.Networks().SetCidrBlocks(ctx, id, remainingV4, remainingV6)
		if uerr != nil {
			return nil, serviceerr.MapRepoErr(uerr)
		}
		if err := w.Outbox().Emit(ctx, "Network", updated.ID, "UPDATED", helpers.DomainToMap(updated)); err != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
		}
		if err := w.Commit(); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
		return marshalNetworkRecord(updated)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}
