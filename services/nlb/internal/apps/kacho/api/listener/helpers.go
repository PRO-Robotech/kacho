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

// Строитель нагрузки вида `nlb_listener` живёт НЕ ЗДЕСЬ, а в repo-leaf —
// `kachorepo.ListenerStatePayload`, рядом со своим читателем.
//
// # Почему он оттуда, а не отсюда
//
// Точки эмиссии этого вида лежат в ДВУХ пакетах use-case: правку и снятие
// эмитит этот, а каскадный переезд — пакет балансировщика, который правит проект
// слушателей вместе со своим (#1549). Строитель, спрятанный здесь, второму
// пакету недоступен, и второй завёл бы свой — вторую форму нагрузки того же вида.
// Контракт единой формы разрешает читать непустое состояние как ПОЛНОЕ, поэтому
// одна частичная точка делает ложным ВЕСЬ вид, и делает тихо.
//
// Держит это разбор ДЕРЕВА use-case'ов
// (`subscriptionjournal.TestEveryEmissionOfAStatefulKindBuildsTheSamePayload`), а
// не внимание.

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
