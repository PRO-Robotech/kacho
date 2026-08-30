// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// Отказ учёта числа ресурсов на пути наружу.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-3 и DoD S4 п.1.
//
// ПОЧЕМУ ЭТИ ДВА ИСХОДА РАЗБИРАЮТСЯ ОТДЕЛЬНО ОТ ОСТАЛЬНЫХ SENTINEL'ОВ. Общая
// классификация отвечает только за КОД. Учёту этого мало: клиент обязан
// различать полосы МАШИННО — по `reason`-токену в `google.rpc.ErrorInfo`, а не
// разбором прозы (`api-conventions.md` §By-lane code-split).

const (
	// reasonQuotaExceeded — место кончилось: потолок назван и выбран.
	// Администратору требуется ПОДНЯТЬ предел.
	reasonQuotaExceeded = "QUOTA_EXCEEDED"

	// reasonQuotaNotProvisioned — потолок не назван ни на одной области.
	// Администратору требуется ЗАВЕСТИ предел.
	//
	// Отдельный признак, а не оттенок предыдущего: сведи их в один, и читающий
	// «место кончилось» пойдёт искать, что понизить, там, где ничего не
	// назначено.
	reasonQuotaNotProvisioned = "QUOTA_NOT_PROVISIONED"
)

// quotaReasonDomain — источник отказа в `ErrorInfo.domain`, как его видит клиент.
const quotaReasonDomain = "registry.kacho.cloud"

// quotaRefusal собирает статус отказа учёта, если err им является; ok=false
// означает «это не отказ учёта» и передаёт разбор общей классификации.
//
// Текст производителя (`kacho_quota_refuse`, миграция 0015) выносится наружу
// ДОСЛОВНО: он и есть контракт — называет носителя, предел и вид. Пересказать
// его здесь значило бы завести второе место об одном предмете, а именно от этого
// обе полосы и защищены единственным производителем.
func quotaRefusal(err error) (error, bool) {
	var (
		code   codes.Code
		reason string
		sent   error
	)
	switch {
	case errors.Is(err, regerrors.ErrQuotaExceeded):
		code, reason, sent = codes.ResourceExhausted, reasonQuotaExceeded, regerrors.ErrQuotaExceeded
	case errors.Is(err, regerrors.ErrQuotaNotProvisioned):
		// FAILED_PRECONDITION, а не INVALID_ARGUMENT: ввод арендатора корректен,
		// не выполнено предусловие ПЛАТФОРМЫ. INVALID_ARGUMENT обвинил бы
		// вызывающего в том, чего он не присылал.
		code, reason, sent = codes.FailedPrecondition, reasonQuotaNotProvisioned, regerrors.ErrQuotaNotProvisioned
	default:
		return nil, false
	}

	st := status.New(code, strip(err, sent))
	// Величины производителя — в штатное поле `metadata`, а не в прозу. Контракт
	// от этого не меняется: `ErrorInfo.metadata` существует ровно под такие
	// величины, и клиент, читавший только признак, продолжает работать
	// (задача продукта #1605). Величин нет — поле остаётся пустым; выдумывать их
	// нечем, а ноль есть законная величина занятого и молчанием быть не может.
	withDetails, derr := st.WithDetails(&errdetails.ErrorInfo{
		Reason:   reason,
		Domain:   quotaReasonDomain,
		Metadata: quotadetail.MetadataFromError(err),
	})
	if derr != nil {
		// Деталь не прикрепилась — код и текст важнее признака.
		return st.Err(), true
	}
	return withDetails.Err(), true
}
