// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
)

// intentResourceName — как ресурс намерения называется в отказах. Тексты
// отказов — часть контракта, поэтому имя одно на все три формы отказа и не
// собирается на месте.
const intentResourceName = "dataplane intent"

// ReportAppliedUseCase — приём подтверждения применения от исполнителя.
//
// # Что здесь решается, кроме записи
//
// Подтверждение — единственное, что платформа знает о фактическом состоянии
// датаплейна, поэтому у него ровно три исхода, и каждый назван:
//
//   - невыразимая форма (нет идентификатора, ревизия неположительна, исход не
//     назван, класс причины не согласован с исходом) — `INVALID_ARGUMENT`
//     СИНХРОННО, до обращения к хранилищу;
//   - объекта нет или ревизия ему не выдавалась — `NOT_FOUND` и
//     `FAILED_PRECONDITION` соответственно: первое про СВОЁ отсутствие, второе
//     про состояние;
//   - подтверждение старее уже записанного — НЕ отказ: гонка «применил N,
//     платформа успела выдать N+1» штатна. Оно объявляется незасчитанным
//     (`Recorded=false`) и попадает в наблюдаемость, иначе отличить его от
//     свежего нечем.
type ReportAppliedUseCase struct {
	recorder ApplyRecorder
	obs      *Observer
}

// NewReportAppliedUseCase собирает use-case.
func NewReportAppliedUseCase(recorder ApplyRecorder, obs *Observer) *ReportAppliedUseCase {
	return &ReportAppliedUseCase{recorder: recorder, obs: obs}
}

// Record записывает подтверждение.
func (u *ReportAppliedUseCase) Record(ctx context.Context, rep ApplyReport) (ApplyRecord, error) {
	if err := validateReport(rep); err != nil {
		return ApplyRecord{}, err
	}
	got, err := u.recorder.Record(ctx, rep)
	switch {
	case errors.Is(err, ErrIntentUnknown):
		// Своя база, своя строка, строки нет — это «своё отсутствует».
		return ApplyRecord{}, status.Errorf(codes.NotFound,
			"Dataplane intent %s not found", rep.ResourceID)
	case errors.Is(err, ErrRevisionNotIssued):
		// Объект есть, а названной ревизии платформа ему не выдавала: состояние
		// не позволяет принять отчёт, потому что принять его значило бы записать
		// применение того, чего не объявляли.
		return ApplyRecord{}, status.Errorf(codes.FailedPrecondition,
			"Dataplane intent %s was never issued revision %d", rep.ResourceID, rep.Revision)
	case err != nil:
		return ApplyRecord{}, err
	}
	if !got.Recorded {
		u.obs.ReportStale(rep.ResourceID, rep.Revision, got.CurrentRevision)
	}
	return got, nil
}

// validateReport отвергает формы, в которых подтверждение не имеет смысла.
//
// Порядок проверок читается сверху вниз и совпадает с порядком, в котором о
// полях говорит контракт: сначала идентичность, потом ревизия, потом исход и
// его причина. Каждая называет ПОЛЕ — отказ без имени поля заставляет
// вызывающего гадать, какое из четырёх он прислал не так.
//
// Словарь классов отказа проверяется ЗДЕСЬ, а не только CHECK-ограничением
// базы: значение вне словаря обязано получить `INVALID_ARGUMENT` с именем поля,
// а не отказ целостности, преобразованный в непрозрачный INTERNAL. База
// остаётся вторым рубежом — она отвечает КАЖДОМУ писателю, а не только этому
// пути.
//
// Сам словарь живёт в ОДНОМ месте ([KnownFailureReasons]) и здесь не
// переписывается: приём подтверждения и публичная проекция обязаны говорить об
// одном и том же наборе классов. Две копии разошлись бы молча — и разошлись бы
// именно там, где расхождение не видно: класс, принятый у исполнителя и
// неизвестный проекции, не доехал бы до арендатора.
func validateReport(rep ApplyReport) error {
	// Формат идентификатора — первым стейтментом, до всего остального.
	// `corevalidate.ResourceID` пустую строку ПРОПУСКАЕТ (это записано в её
	// godoc), поэтому обязательность — отдельная проверка здесь, а не надежда на
	// неё.
	if rep.ResourceID == "" {
		return status.Error(codes.InvalidArgument, "resource_id: required")
	}
	if err := corevalidate.ResourceID(intentResourceName, "", rep.ResourceID); err != nil {
		return err
	}
	if rep.Revision <= 0 {
		return status.Error(codes.InvalidArgument, "Illegal argument revision")
	}
	switch rep.Outcome {
	case OutcomeApplied:
		// У успеха причины неуспеха нет. Принять её и выбросить значило бы
		// вернуть исполнителю успех на параметр, который никуда не поехал.
		if rep.Reason != ReasonNone {
			return status.Error(codes.InvalidArgument, "Illegal argument reason")
		}
	case OutcomeFailed:
		if _, ok := knownReasonSet[rep.Reason]; !ok {
			return status.Error(codes.InvalidArgument, "Illegal argument reason")
		}
	default:
		return status.Error(codes.InvalidArgument, "Illegal argument outcome")
	}
	return nil
}
