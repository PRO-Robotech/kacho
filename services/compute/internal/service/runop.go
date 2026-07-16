// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"context"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// runOp — единая обёртка async-LRO dispatch: operations.New → opsRepo.Create →
// operations.Run(worker) → возврат синхронного snapshot'а Operation (done=false).
// Устраняет дублирование этой 6-строчной обвязки в каждом мутирующем RPC (Create/
// Update/Delete/Restart/Attach/Detach/UpdateMetadata/Relocate/SimulateMaintenance/
// lifecycle) — изменение контракта диспетчеризации (audit-tag, per-op deadline,
// metric) правится в ОДНОМ месте, а не в скопированных блоках. Мандатный
// async-Operation-паттерн (ban 9) и wire-контракт (LRO envelope, metadata-типы,
// error-mapping, outbox-emit) сохранены дословно — централизуется только
// hand-copied обвязка. desc/meta/worker — единственная per-site вариация; синхронную
// pre-валидацию (guard'ы id/req) call-site выполняет ДО вызова runOp.
func runOp(ctx context.Context, opsRepo operations.Repo, desc string, meta proto.Message,
	fn func(context.Context) (*anypb.Any, error)) (*operations.Operation, error) {
	// non-owner-tuple мутации (Update/Delete/Restart/Attach/…) — без confirm-gate:
	// нового owner-tuple они не создают, поэтому read-after-register окна нет
	// (OTG-16). Делегируем в runOpWithConfirm с confirm=nil (эквивалент прежнего
	// operations.Run).
	return runOpWithConfirm(ctx, opsRepo, desc, meta, fn, nil)
}

// runOpWithConfirm — owner-tuple opgate вариант runOp: тот же async-LRO dispatch,
// но с read-after-register confirm-gate поверх worker-механизма (P1
// operations.RunWithConfirm). При non-nil confirm Create-Operation достигает
// success-`done` ТОЛЬКО после confirmed=true; иначе fail-closed по
// confirmation-deadline → op.error(codes.Unavailable, "owner-tuple registration not
// confirmed"), success-done без confirm НЕ выставляется никогда (acceptance
// owner-tuple-opgate, OTG-03/-05). confirm==nil → сегодняшнее поведение
// (fn success → сразу MarkDone). Resource-ref в op.metadata (CreateInstanceMetadata/
// CreateDiskMetadata) durable на ВСЕХ терминалах, включая error (MarkError сохраняет
// metadata — FIX-3, OTG-05b): metadata пишется opsRepo.Create ДО исполнения fn.
func runOpWithConfirm(ctx context.Context, opsRepo operations.Repo, desc string, meta proto.Message,
	fn func(context.Context) (*anypb.Any, error), confirm operations.ConfirmFunc) (*operations.Operation, error) {
	op, err := operations.New(ids.PrefixOperationCompute, desc, meta)
	if err != nil {
		return nil, err
	}
	if err := opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.RunWithConfirm(ctx, opsRepo, op.ID, fn, confirm)
	return &op, nil
}
