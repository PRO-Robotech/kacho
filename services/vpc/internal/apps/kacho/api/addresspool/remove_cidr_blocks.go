// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package addresspool

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// RemoveCidrBlocksUseCase — admin-only удаление CIDR-блоков из пула
// (по форме как Subnet :removeCidrBlocks). Sync (нет Operation).
//
// Гарантии:
//   - CIDR-блок отсутствует в пуле → FailedPrecondition.
//   - В удаляемом CIDR есть выделенные external-IP ЛЮБОЙ семьи →
//     FailedPrecondition "address pool CIDR <cidr> has allocated addresses"
//     (use-check).
//   - Удаление опустошит пул (v4 ∪ v6 = ∅) → InvalidArgument.
//   - Чисто → убрать CIDR из пула + удалить соответствующие free_ips.
//
// Атомарность (см. notes на DeleteFreelistForCidrs в repo): в одной writer-TX
// сперва DELETE free_ips удаляемых CIDR (берет row-lock, блокирует/сериализует
// конкурентный alloc на тех же строках), затем use-check count по addresses.
// Если конкурентный alloc успел закоммитить IP в addresses — count > 0 →
// FailedPrecondition → TX abort → DELETE откатывается. Иначе — free_ips удалены,
// pool обновлен, новые аллокации из удаленных CIDR невозможны (free_ips нет).
type RemoveCidrBlocksUseCase struct {
	repo Repo
}

// NewRemoveCidrBlocksUseCase собирает use-case.
func NewRemoveCidrBlocksUseCase(r Repo) *RemoveCidrBlocksUseCase {
	return &RemoveCidrBlocksUseCase{repo: r}
}

// Execute удаляет v4/v6 CIDR-блоки. Возвращает обновленный AddressPool.
func (u *RemoveCidrBlocksUseCase) Execute(ctx context.Context, id string, v4, v6 []string) (*kachorepo.AddressPoolRecord, error) {
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "address_pool_id required")
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"v4_cidr_blocks or v6_cidr_blocks is required")
	}

	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, err
	}
	defer w.Abort()

	// GetForUpdate (row-lock), НЕ plain Get: v4_cidr_blocks/v6_cidr_blocks — set
	// read-modify-write (subtract → Update). DeleteFreelistForCidrs берёт row-lock
	// только на free_ips УДАЛЯЕМОГО CIDR — он НЕ сериализует конкурентную мутацию
	// массива пула (add[X]/remove[Y] на disjoint блоки). Без FOR UPDATE на пуле
	// второй UPDATE тихо затирает первый (project-rule #10 / data-integrity.md).
	// Row-lock сериализует мутаторов набора; parity с add_cidr_blocks/update.
	curRec, err := w.AddressPools().GetForUpdate(ctx, id)
	if err != nil {
		return nil, err
	}
	cur := curRec.AddressPool

	remainingV4, removedV4 := subtractCIDRSet(cur.V4CIDRBlocks, v4)
	remainingV6, removedV6 := subtractCIDRSet(cur.V6CIDRBlocks, v6)
	if removedV4 != len(v4) || removedV6 != len(v6) {
		return nil, status.Error(codes.FailedPrecondition,
			"one or more CIDR blocks not found in address pool")
	}
	// Пул не может стать пустым.
	if len(remainingV4) == 0 && len(remainingV6) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"v4_cidr_blocks and v6_cidr_blocks must not be both empty after removal")
	}

	// Атомарность: DELETE free_ips первым (row-lock сериализует против
	// конкурентной выдачи v4), потом use-check по обеим семьям. v6-CIDR в
	// DeleteFreelistForCidrs безвредны (free_ips всегда v4). Выдача v6
	// сериализуется иначе — row-lock самого пула (GetForUpdate выше против
	// FOR SHARE в аллокаторе), поэтому счёт по книге учёта v6 не гоночный.
	removed := append(append([]string{}, v4...), v6...)
	if err := w.AddressPools().DeleteFreelistForCidrs(ctx, id, v4); err != nil {
		return nil, err
	}
	allocated, err := w.AddressPools().CountAllocatedInCidrs(ctx, id, removed)
	if err != nil {
		return nil, err
	}
	if allocated > 0 {
		// verbatim-стиль (как Subnet "network is not empty").
		return nil, status.Error(codes.FailedPrecondition,
			fmt.Sprintf("address pool CIDR %s has allocated addresses", strings.Join(removed, ", ")))
	}

	cur.V4CIDRBlocks = remainingV4
	cur.V6CIDRBlocks = remainingV6

	updated, err := w.AddressPools().Update(ctx, &cur)
	if err != nil {
		return nil, err
	}
	// Удаляем снятые блоки из address_pool_cidrs — освобождаем CIDR-диапазон
	// для будущих пулов (EXCLUDE больше не блокирует их). В той же writer-TX →
	// согласовано с use-check выше.
	if err := w.AddressPools().DeleteCidrBlocks(ctx, id, v4, v6); err != nil {
		return nil, err
	}
	if err := w.Outbox().Emit(ctx, "AddressPool", updated.ID, "UPDATED",
		helpers.AddressPoolDomainPayload(&updated.AddressPool)); err != nil {
		return nil, fmt.Errorf("%w: outbox emit: %v", serviceerr.ErrInternal, err)
	}
	if err := w.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}
