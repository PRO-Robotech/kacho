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

// AddCidrBlocksUseCase — атомарное расширение declared-супернета сети (F2/VPC-1-08).
// Supernet immutable через Update — растёт ТОЛЬКО этим verb-pair'ом. Внутри worker'а:
//   - GetForUpdate(network) (row-lock) → сериализует конкурентные Add/Remove на сети.
//   - merge существующих + новых блоков (дедуп по canonical-строке — идемпотентно).
//   - SetCidrBlocks (узкий UPDATE ipv4/ipv6_cidr_blocks) + outbox UPDATED — одна writer-TX.
//
// Op-in-response (statusless): Operation возвращается done:true с полным телом
// Network в .response.
type AddCidrBlocksUseCase struct {
	repo    Repo
	opsRepo operations.Repo
}

// NewAddCidrBlocksUseCase создаёт AddCidrBlocksUseCase.
func NewAddCidrBlocksUseCase(r Repo, opsRepo operations.Repo) *AddCidrBlocksUseCase {
	return &AddCidrBlocksUseCase{repo: r, opsRepo: opsRepo}
}

// Execute — sync-валидация id/CIDR-формата + Operation + синхронный merge в worker'е.
func (u *AddCidrBlocksUseCase) Execute(ctx context.Context, id string, v4, v6 []string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, serviceerr.InvalidArg("ipv4_cidr_blocks", "ipv4_cidr_blocks or ipv6_cidr_blocks is required")
	}
	// Format-класс (host-bits=0, family match) — ДО создания Operation,
	// конвенционный тон "invalid CIDR block '<X>'".
	if err := validateNetworkSupernet(v4, v6); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Add CIDR blocks to network %s", id),
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
		mergedV4 := mergeCidrBlocks(n.IPv4CidrBlocks, v4)
		mergedV6 := mergeCidrBlocks(n.IPv6CidrBlocks, v6)
		updated, uerr := w.Networks().SetCidrBlocks(ctx, id, mergedV4, mergedV6)
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
