// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// MapDomainErr — единый sentinel-error→gRPC-status маппер для всех use-case
// пакетов kacho-nlb (loadbalancer / listener / targetgroup). Раньше был
// продублирован в трёх местах и успел разойтись (pass-through
// guard без codes.Unknown-проверки в loadbalancer, две несовместимые сигнатуры
// stripSentinel). Здесь — один источник истины.
//
// Транслирует sentinel-ошибки `domain` (kacho-repo re-export'ит их через
// live-alias — errors.Is даёт одинаковый результат) и peer-client ошибки в
// gRPC-status. Sentinel-prefix убирается через StripSentinel — тем самым
// sentinel'ом, который распознала ветвь, — чтобы чистый по конвенции Kachō
// текст доходил до клиента.
//
// Если err уже gRPC-status с известным кодом (code != Unknown) — пробрасываем
// как есть (sync corelib/errors, typed peer-status). status с codes.Unknown НЕ
// пробрасываем — он падает в sentinel-switch и превращается в Internal без
// leak'а (иначе один и тот же error давал бы разный код в разных ресурсах).
//
//	ErrNotFound            → NOT_FOUND
//	ErrAlreadyExists       → ALREADY_EXISTS
//	ErrFailedPrecondition  → FAILED_PRECONDITION
//	ErrInvalidArg          → INVALID_ARGUMENT
//	ErrUnavailable         → UNAVAILABLE
//	ErrInternal / прочее   → INTERNAL (no leak)
func MapDomainErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
		// already gRPC-shaped with a meaningful code
		return err
	}
	// Учёт числа ресурсов разбирается ПЕРВЫМ и отдельно: клиенту мало кода — он
	// обязан различать полосы машинно, по `reason`-токену, а не разбором прозы.
	if st, ok := quotaRefusal(err); ok {
		return st
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, StripSentinel(err, domain.ErrNotFound))
	case errors.Is(err, domain.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, StripSentinel(err, domain.ErrAlreadyExists))
	case errors.Is(err, domain.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, StripSentinel(err, domain.ErrFailedPrecondition))
	case errors.Is(err, domain.ErrInvalidArg):
		return status.Error(codes.InvalidArgument, StripSentinel(err, domain.ErrInvalidArg))
	case errors.Is(err, domain.ErrUnavailable):
		return status.Error(codes.Unavailable, StripSentinel(err, domain.ErrUnavailable))
	case errors.Is(err, domain.ErrInternal):
		// Internal: НЕ leak'аем raw pgx text — отдаём константную фразу.
		return status.Error(codes.Internal, "internal database error")
	}
	// Default: Internal без leak'а текста.
	return status.Error(codes.Internal, "internal error")
}

// StripSentinel возвращает КЛИЕНТСКУЮ часть сообщения: то, что осталось от
// `err` после снятия префикса того sentinel'а, который распознал вызывающий.
//
// ПОЧЕМУ ПРЕФИКС ВЫВОДИТСЯ ИЗ SENTINEL'А, А НЕ БЕРЁТСЯ ИЗ СПИСКА
// (задача продукта #1658). Здесь стоял закрытый перечень из шести префиксов,
// выписанных строками. Перечень — ВТОРОЕ место о sentinel'ах домена, и оно
// разошлось с первым молча: две полосы учёта числа ресурсов завелись позже, в
// перечень не попали, и их отказ уезжал клиенту с приклеенным именем
// внутреннего sentinel'а — при том что пять остальных владельцев учёта отдают
// то же предложение производителя чистым. Расхождение видно только если
// положить шесть текстов рядом, чего не делает ни обзор изменения, ни сборка.
//
// Вывод префикса из переданного sentinel'а закрывает КЛАСС, а не экземпляр:
// седьмой sentinel добавить в обход нечего — вызывающий обязан назвать тот,
// который распознал, и снимается ровно его префикс. Та же форма, что у пяти
// остальных владельцев учёта.
//
// ВТОРОЕ, ЧТО ЭТО ЧИНИТ: прежняя подпись принимала `fallback string`, и
// вызывающий полосы учёта передавал в неё `sent.Error()` — значение, которое
// возвращалось ТОЛЬКО при `err == nil`, то есть на этом пути вычислялось и
// выбрасывалось. Ban «принято-и-проигнорировано» на уровне функции: вызывающий
// уверен, что параметр применён, а он не читался ни при каком входе. Теперь
// параметр читается всегда — он и задаёт снимаемый префикс.
//
// Вырожденные входы: `sentinel == nil` (звать так нечем — вызывающий распознал
// sentinel, иначе он сюда не попал) и пустое сообщение отдают текст самого
// sentinel'а. Прежние вызывающие передавали фиксированной строкой ровно его —
// поэтому смены поведения на них нет.
func StripSentinel(err error, sentinel error) string {
	fallback := ""
	if sentinel != nil {
		fallback = sentinel.Error()
	}
	if err == nil {
		return fallback
	}
	msg := err.Error()
	if fallback != "" {
		if rest, ok := strings.CutPrefix(msg, fallback+": "); ok {
			return rest
		}
	}
	if msg == "" {
		return fallback
	}
	return msg
}
