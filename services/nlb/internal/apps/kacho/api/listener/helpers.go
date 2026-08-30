// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// listenerRecordToPb — repo-record → proto.Listener via DTO registry
// (registered in `internal/dto/type2pb/listener.go`).
func listenerRecordToPb(rec *kachorepo.ListenerRecord) (*lbv1.Listener, error) {
	if rec == nil {
		return nil, status.Error(codes.Internal, "nil listener record")
	}
	var dst *lbv1.Listener
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, mapDomainErr(err)
	}
	return dst, nil
}

// operationToProto — прослойка к общему слою: перевод строки операции в контракт
// объявлен в дереве ОДИН раз (`pkg/operations/operationspb`).
//
// Здесь стояло «делегатор к единому `shared.OperationToProto`» — звено, снятое
// выпрямлением цепочки: комментарий пережил свой предмет ровно на одну правку.
func operationToProto(op *operations.Operation) *operationpb.Operation {
	return operationspb.ToProto(op)
}

// mapDomainErr — translate domain-sentinel error → gRPC status. Делегирует
// единому мапперу `shared.MapDomainErr` (один источник истины для всех use-case
// пакетов kacho-nlb):
//
//	ErrNotFound            → NOT_FOUND
//	ErrAlreadyExists       → ALREADY_EXISTS
//	ErrFailedPrecondition  → FAILED_PRECONDITION
//	ErrInvalidArg          → INVALID_ARGUMENT
//	ErrUnavailable         → UNAVAILABLE
//	ErrInternal / other    → INTERNAL (no leak)
func mapDomainErr(err error) error {
	return shared.MapDomainErr(err)
}

// marshalListener — anypb.New(Listener) wrapper used as Operation.response on
// successful Create/Update worker completion. Returns gRPC Internal on
// marshal-failure (should be impossible — Listener is a proto message).
func marshalListener(rec *kachorepo.ListenerRecord) (*anypb.Any, error) {
	pb, err := listenerRecordToPb(rec)
	if err != nil {
		return nil, err
	}
	any, err := anypb.New(pb)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	return any, nil
}

// listenerPayloadMap — нагрузка строки `nlb_outbox` для слушателя: ЗАПИСЬ
// ЦЕЛИКОМ, под конвертом полного состояния.
//
// # Читатель у неё ЕСТЬ, и он один
//
// Объявление журнала подписки (`internal/subscriptionjournal`) собирает из этой
// нагрузки состояние события вида `nlb_listener`. Отсюда два требования, и оба
// строгие.
//
// ПЕРВОЕ: запись кладётся целиком, а не пересобирается по полям. Пересборка
// завела бы вторую проекцию ресурса рядом с той, которой отвечает `Get`, и
// расходились бы они молча.
//
// ВТОРОЕ: строитель у вида ОДИН. Контракт единой формы разрешает подписчику
// читать непустое состояние как ПОЛНОЕ, поэтому одна точка эмиссии с частичным
// снимком делает ложным весь вид — и делает тихо. Держит это разбор пакета
// (`TestEveryListenerEmissionBuildsTheSamePayload`), а не внимание.
//
// # Чего здесь БОЛЬШЕ НЕТ
//
// Прежняя нагрузка была минимальным снимком из словаря
// `kachorepo.LifecyclePayload` (идентификатор, родитель, проект, регион, имя,
// состояние, протокол, порт) и читателя не имела вовсе. Словарь остаётся: им
// по-прежнему собирают нагрузку балансировщика и целевой группы, у которых
// состояния нет (задача #1381 обогатила вид слушателя).
func listenerPayloadMap(rec *kachorepo.ListenerRecord) map[string]any {
	if rec == nil {
		return nil
	}
	return kachorepo.StateEnvelope(rec)
}

// lbUpdatedPayloadMap — нагрузка перекрёстного эмита правки
// `nlb_load_balancer:<lb_id> UPDATED` после Listener.Create / .Delete.
// Минимальная; читателя у неё сегодня нет (см. listenerPayloadMap).
func lbUpdatedPayloadMap(lbID, projectID, regionID, trigger string) map[string]any {
	return kachorepo.LifecyclePayload{
		ID:        lbID,
		ProjectID: projectID,
		RegionID:  regionID,
		Trigger:   trigger,
	}.Map()
}

// loggerOrDiscard — defensive accessor для nil-loggers. Возвращает global
// default slog (через slog.Default) если переданный logger == nil; иначе
// возвращает его. Use-case helpers могут безопасно вызывать `loggerOrDiscard(u.logger).Info`.
func loggerOrDiscard(l *slog.Logger) *slog.Logger {
	if l != nil {
		return l
	}
	return slog.Default()
}
