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
// true.
//
// ЗДЕСЬ СТОЯЛО «downstream FGA-catalog-tuple материализуется eventually через
// geo_outbox» — механизма с таким предметом НЕ СУЩЕСТВУЕТ, и утверждение
// расходилось с решением своего же домена. geo намеренно НЕ участвует в потоке
// регистрации владения: per-resource записей для Region/Zone нет вовсе, Check
// идёт через cluster-синглтон, а `geo_outbox` — audit-only и НЕ драйвер
// регистрации (`docs/engineering/architecture/authz-and-tuples.md`). Предикаты:
// вызовов `RegisterResource` в прод-дереве geo — ноль, дренажа над `geo_outbox` —
// ноль. Гейтить `done` было НЕ НА ЧТО, и это верно по построению, а не по
// расписанию: у синхронно завершённой мутации каталога downstream-эффекта нет.
//
// # Порядок: строка операции ДО мутации
//
// «Синхронно завершена» описывает то, что видит клиент, и НЕ означает, что оба
// стейтмента — один. Их два, и между ними процесс может умереть. Поэтому строка
// пишется ПЕРВОЙ (done=false), мутация — второй, терминальное состояние — третьим,
// атомарной сменой done по CAS.
//
// Обратный порядок (мутация → строка) вырождается тихо: закоммиченный регион, чья
// строка операции не записалась, доложен вызывающему как ОТКАЗ — «создать не
// удалось», при том что он уже в каталоге. Дальше расходятся двое: вызывающий
// повторяет и получает AlreadyExists на объект, которого по его сведениям нет.
// Реконсайлер это не лечит — он подметает строки с done=false, а здесь строки нет
// вовсе. Тот же порядок сознательно исправлен в services/iam/.../cluster/, а
// pkg/operations.MetadataFinalizer формулирует правило в собственном godoc.
//
// Цена — метаданные на Begin неполны (warnings° известны только по СОЗДАННОЙ
// строке), поэтому терминальный переход их уточняет: ровно для этого
// MetadataFinalizer и существует.
package syncop

import (
	"context"

	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Repo — порт, который требует syncop: базовый operations.Repo плюс терминальная
// запись, уточняющая metadata. Уточнение здесь не роскошь: строка создаётся ДО
// мутации, значит на Begin заведомо неизвестно то, что вычисляется по её
// результату. Конкретный pgRepo (operations.NewRepo → FullRepo) удовлетворяет порт
// целиком, поэтому требование проверяется на компиляции, а не type-assert'ом в
// рантайме.
type Repo interface {
	operations.Repo
	operations.MetadataFinalizer
}

// Begin персистит строку операции (done=false) ДО мутации. Вызывающий обязан
// завершить её ровно одним терминальным переходом — Commit либо Fail.
//
// Ошибка здесь означает «операцию зарегистрировать не удалось» и ОБЯЗАНА вернуть
// вызывающего ДО мутации: незаписанная операция — единственное состояние, в котором
// честно сказать «не сделано», потому что сделано ещё ничего не было.
func Begin(ctx context.Context, ops Repo, op operations.Operation) error {
	return ops.Create(ctx, op)
}

// Commit финализирует УСПЕХ уже начатой операции: done=true, response и metadata,
// уточнённая по результату мутации (warnings° и прочее, чего на Begin не знали).
// Возвращает ту же op в памяти уже финализированной.
//
// metadata == nil → metadata строки остаётся такой, какой её записал Begin.
func Commit(ctx context.Context, ops Repo, op operations.Operation, metadata, response *anypb.Any) (*operations.Operation, error) {
	if metadata != nil {
		if err := ops.MarkDoneWithMetadata(ctx, op.ID, metadata, response); err != nil {
			return nil, err
		}
		op.Metadata = metadata
	} else if err := ops.MarkDone(ctx, op.ID, response); err != nil {
		return nil, err
	}
	op.Done = true
	op.Response = response
	return &op, nil
}

// Fail финализирует ПРОВАЛ уже начатой операции (done=true, error).
// DB-detected ошибки мутации (FK 23503 / UNIQUE 23505 / …) приземляются в
// op.error — НЕ как sync gRPC-ошибка (sync-ошибку отдаёт только pre-write
// валидация: malformed id, coupling, name-required, countryCode, immutable).
// statusErr — уже сконвертированный gRPC-status (serviceerr.ToStatus).
func Fail(ctx context.Context, ops Repo, op operations.Operation, statusErr error) (*operations.Operation, error) {
	st := grpcstatus.Convert(statusErr).Proto()
	if err := ops.MarkError(ctx, op.ID, st); err != nil {
		return nil, err
	}
	op.Done = true
	op.Error = st
	return &op, nil
}
