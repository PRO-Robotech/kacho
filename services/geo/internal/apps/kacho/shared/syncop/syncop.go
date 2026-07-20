// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package syncop — synchronously-completed Operation helper для config-INSERT
// каталога kacho-geo. Топология — cluster-scoped config в одной БД без
// provision-саги (один INSERT), поэтому admin-мутации Region/Zone возвращают
// Operation{done:true} НЕМЕДЛЕННО (module-geo rule 4; unified §1 conv-2): нет
// async-worker'а, нет поллинга. Клиент разворачивает `.response` (полное тело
// public-ресурса) — звать OperationService.Get не требуется.
//
// Инвариант «любая мутация Kachō → Operation» держится буквально (ban #9): op
// персистится в per-service operations-таблице (corelib), pollable, но done уже
// true. Downstream FGA-catalog-tuple материализуется eventually через geo_outbox —
// done на его видимость НЕ гейтится.
package syncop

import (
	"context"

	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Commit персистит синхронно-завершённую УСПЕШНУЮ операцию (done=true, response)
// и возвращает её же в памяти уже финализированной. metadata (в т.ч. warnings°)
// вшита в op ДО вызова (Create-time; MarkDone metadata не трогает).
func Commit(ctx context.Context, ops operations.Repo, op operations.Operation, response *anypb.Any) (*operations.Operation, error) {
	if err := ops.Create(ctx, op); err != nil {
		return nil, err
	}
	if err := ops.MarkDone(ctx, op.ID, response); err != nil {
		return nil, err
	}
	op.Done = true
	op.Response = response
	return &op, nil
}

// Fail персистит синхронно-завершённую ПРОВАЛЕННУЮ операцию (done=true, error).
// DB-detected ошибки мутации (FK 23503 / UNIQUE 23505 / …) приземляются в
// op.error — НЕ как sync gRPC-ошибка (sync-ошибку отдаёт только pre-write
// валидация: malformed id, coupling, name-required, countryCode, immutable).
// statusErr — уже сконвертированный gRPC-status (serviceerr.ToStatus).
func Fail(ctx context.Context, ops operations.Repo, op operations.Operation, statusErr error) (*operations.Operation, error) {
	st := grpcstatus.Convert(statusErr).Proto()
	if err := ops.Create(ctx, op); err != nil {
		return nil, err
	}
	if err := ops.MarkError(ctx, op.ID, st); err != nil {
		return nil, err
	}
	op.Done = true
	op.Error = st
	return &op, nil
}
