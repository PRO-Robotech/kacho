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

// RemoveCidrBlocksUseCase — сужение состава набора.
//
// Идемпотентен: снятие отсутствующего члена — успех без изменения (та же причина,
// что у парного глагола).
//
// Потолком НЕ гейтится: сужение обязано проходить всегда, иначе набор, каким-то
// образом оказавшийся над потолком, стало бы нечем починить.
type RemoveCidrBlocksUseCase struct {
	repo    Repo
	opsRepo operations.Repo
}

// NewRemoveCidrBlocksUseCase создаёт RemoveCidrBlocksUseCase.
func NewRemoveCidrBlocksUseCase(r Repo, opsRepo operations.Repo) *RemoveCidrBlocksUseCase {
	return &RemoveCidrBlocksUseCase{repo: r, opsRepo: opsRepo}
}

// Execute — sync-валидация id и членов + Operation + запись в worker'е.
func (u *RemoveCidrBlocksUseCase) Execute(ctx context.Context, id string, v4, v6 []string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("cidr_group", ids.PrefixCidrGroupHyphen, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, serviceerr.InvalidArg("v4_cidr_blocks", "v4_cidr_blocks or v6_cidr_blocks is required")
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
		fmt.Sprintf("Remove CIDR blocks from cidr group %s", id),
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

		updated, rerr := w.CidrGroups().RemoveBlocks(ctx, id, normV4, normV6)
		if rerr != nil {
			return nil, serviceerr.MapRepoErr(rerr)
		}
		// Опустошить набор, на который ссылается живое правило, — отказ.
		//
		// Пустой набор в ссылке правила либо заставляет правило выпасть целиком,
		// либо заставляет фильтр молча исчезнуть из него: первое сужает разрешение
		// незаметно, второе — расширяет. Оба исхода суть защита с формой и без
		// содержания, поэтому состояние не допускается вовсе.
		//
		// Проверка стоит ЗДЕСЬ, после снятия членов и ВНУТРИ той же транзакции,
		// а не перед ним: `RemoveBlocks` уже взял строку набора строгой
		// блокировкой, конфликтующей с проверкой внешнего ключа при вставке
		// ссылки правила. Значит правило, создаваемое одновременно, либо уже
		// видно нам здесь, либо ждёт нашего коммита и получит отказ по ключу.
		// Проверка ДО снятия отвечала бы по снимку, который конкурент уже
		// переписывает.
		//
		// Потребители спрашиваются ОТДЕЛЬНЫМ вызовом, а не читаются из записи,
		// которую вернул писатель: он их не заполняет. Первая редакция читала
		// `updated.UsedBy` — поле, у которого на этом пути нет производителя, —
		// и запрет выглядел исполненным, ничего не запрещая. Проба
		// TestCidrGroup_EmptyingAReferencedSetIsRefused написана раньше фикса и
		// поймала ровно это.
		if updated.CidrGroupBlockCount() == 0 {
			refs, rerr := w.CidrGroups().ReferrersFor(ctx, []string{id})
			if rerr != nil {
				return nil, serviceerr.MapRepoErr(rerr)
			}
			if held := refs[id]; len(held) > 0 {
				return nil, status.Errorf(codes.FailedPrecondition,
					"CidrGroup %s is in use (%s)", id, blockersText(held))
			}
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
