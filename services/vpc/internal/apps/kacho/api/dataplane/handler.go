// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// Handler — тонкий транспорт `kacho.cloud.vpc.v1.InternalDataplaneService`.
//
// Разбор запроса, перевод словарей контракта в словари домена, счёт подписок —
// и ничего больше: решения принимают use-case'ы.
//
// # Чего здесь НЕТ и почему
//
// Проверки прав здесь нет. Оба метода несут аннотации права, требуемого
// отношения и области, и звено решения о доступе стоит в ЦЕПОЧКЕ ОБОИХ
// слушателей — той же самой, что у обычных вызовов (`pkg/servicehost`,
// `serverPair` строит одну пару цепочек на два сервера). Собственная проверка
// здесь была бы вторым решением об одном предмете: она не выдаётся, не
// ограничивается областью, не отзывается и не видна в аудите.
type Handler struct {
	vpcv1.UnimplementedInternalDataplaneServiceServer

	watch  *WatchIntentUseCase
	report *ReportAppliedUseCase
	obs    *Observer

	// slots — сколько подписок обслуживается одновременно. Ёмкость канала И
	// ЕСТЬ предел; счётчика, который можно разойтись с действительностью, нет.
	slots chan struct{}
}

// NewHandler собирает транспорт.
func NewHandler(watch *WatchIntentUseCase, report *ReportAppliedUseCase, obs *Observer) *Handler {
	return &Handler{
		watch:  watch,
		report: report,
		obs:    obs,
		slots:  make(chan struct{}, MaxConcurrentStreams),
	}
}

var _ vpcv1.InternalDataplaneServiceServer = (*Handler)(nil)

// WatchIntent отдаёт поток намерения.
func (h *Handler) WatchIntent(req *vpcv1.WatchIntentRequest, stream vpcv1.InternalDataplaneService_WatchIntentServer) error {
	known := req.GetKnownRevision()
	// Отрицательной позиции не существует: ревизии выдаются последовательностью
	// с единицы. Отказ синхронный и до занятия слота — ввод, который валидным не
	// станет никогда, не должен стоить ни ресурса, ни ожидания.
	if known < 0 {
		h.obs.StreamRefused("negative known_revision")
		return status.Error(codes.InvalidArgument, "Illegal argument known_revision")
	}

	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		// Отказ, а не очередь: очередь ожидающих подписок — то же неограниченное
		// накопление, только этажом выше.
		h.obs.StreamRefused("concurrent stream limit reached")
		return status.Error(codes.ResourceExhausted,
			"too many concurrent dataplane intent streams")
	}

	return h.watch.Run(stream.Context(), known, stream)
}

// ReportIntentApplied принимает подтверждение применения.
func (h *Handler) ReportIntentApplied(ctx context.Context, req *vpcv1.ReportIntentAppliedRequest) (*vpcv1.ReportIntentAppliedResponse, error) {
	got, err := h.report.Record(ctx, ApplyReport{
		ResourceID: req.GetResourceId(),
		Revision:   req.GetRevision(),
		Outcome:    outcomeFromProto(req.GetOutcome()),
		Reason:     reasonFromProto(req.GetReason()),
	})
	if err != nil {
		return nil, err
	}
	return &vpcv1.ReportIntentAppliedResponse{
		Recorded:        got.Recorded,
		CurrentRevision: got.CurrentRevision,
	}, nil
}

// outcomeToDomain — словарь исходов контракта в словарь домена.
//
// Значение, которого в словаре нет (в том числе «не названо»), переводится в
// ПУСТОЙ исход, а не в какой-нибудь по умолчанию: пустой отвергается проверкой
// формы с именем поля, а умолчание записало бы применение, о котором никто не
// сообщал.
var outcomeToDomain = map[vpcv1.ApplyOutcome]ApplyOutcome{
	vpcv1.ApplyOutcome_APPLY_OUTCOME_APPLIED: OutcomeApplied,
	vpcv1.ApplyOutcome_APPLY_OUTCOME_FAILED:  OutcomeFailed,
}

func outcomeFromProto(in vpcv1.ApplyOutcome) ApplyOutcome { return outcomeToDomain[in] }

// reasonToDomain — словарь классов отказа контракта в словарь домена.
var reasonToDomain = map[vpcv1.ApplyFailureReason]FailureReason{
	vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_CAPACITY:             ReasonCapacity,
	vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_CONFLICT:             ReasonConflict,
	vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSUPPORTED:          ReasonUnsupported,
	vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_DEPENDENCY_NOT_READY: ReasonDependencyNotReady,
	vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_TRANSIENT:            ReasonTransient,
	vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_EXECUTOR_INTERNAL:    ReasonExecutorInternal,
}

func reasonFromProto(in vpcv1.ApplyFailureReason) FailureReason { return reasonToDomain[in] }
