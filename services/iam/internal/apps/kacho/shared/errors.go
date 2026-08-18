// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package shared — errors.go: единый sentinel → gRPC status mapping для всех
// api-слайсов (account / project / user / service_account / group / role /
// access_binding).
//
// Заменяет 7+ копий per-resource `mapRepoErr` (account/helpers.go,
// project/helpers.go, …). Все вызывающие должны
// маппить sentinel-ошибки именно через эти функции — единственный
// authoritative point of translation между internal-sentinels и gRPC-кодами,
// чтобы (а) не дрейфил mapping per-package, (б) добавление нового sentinel'а
// требовало правки одного места.
package shared

import (
	stderrors "errors"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamerr "github.com/PRO-Robotech/kacho/services/iam/internal/errors"
)

// MapRepoErr — sentinel → gRPC status. Возвращает nil на nil-input.
//
// Полное покрытие 8 sentinel'ов (включая ErrPermissionDenied /
// ErrUnauthenticated, которых не было в per-resource копиях — leak'ало
// `codes.Internal` клиенту до этой консолидации).
//
// Fallback'и:
//   - если err уже несет gRPC status (не codes.Unknown) — пропускаем через;
//   - если err-текст начинается с "Illegal argument" — YC-style InvalidArgument
//     (parity с verbatim-формой error-сообщений);
//   - иначе — Internal с переданным err-текстом (StripSentinel снимает
//     sentinel-prefix чтобы клиент не увидел "not found: ...").
//
// Порядок веток — сначала pass-through, потом sentinel-switch (форма kacho-nlb).
// Он несущий, а не косметический: pkg/validate кладёт имя поля ТОЛЬКО в
// google.rpc.BadRequest-details, сообщение остаётся общим «invalid argument».
// Пересборка статуса в sentinel-ветке (`status.Error(code, StripSentinel(err))`)
// детали теряет, поэтому ошибка, обёрнутая через `%w` на iamerr.Err*, обязана
// пройти pass-through ПЕРВОЙ. status с codes.Unknown под pass-through НЕ попадает
// (guard `!= Unknown`) — он падает в sentinel-switch и дальше в фиксированный
// INTERNAL, без leak'а.
func MapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	// Отказ учёта — ДО общего switch'а: он несёт не только код, но и признак
	// полосы в `google.rpc.ErrorInfo`, а sentinel-ветка ниже пересобирает статус
	// голым `status.Error(code, text)` и признак потеряла бы.
	if refusal, ok := quotaRefusal(err); ok {
		return refusal
	}
	switch {
	case stderrors.Is(err, iamerr.ErrNotFound):
		return status.Error(codes.NotFound, iamerr.StripSentinel(err))
	case stderrors.Is(err, iamerr.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, iamerr.StripSentinel(err))
	case stderrors.Is(err, iamerr.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, iamerr.StripSentinel(err))
	case stderrors.Is(err, iamerr.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, iamerr.StripSentinel(err))
	case stderrors.Is(err, iamerr.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, iamerr.StripSentinel(err))
	case stderrors.Is(err, iamerr.ErrInvalidArg):
		return status.Error(codes.InvalidArgument, iamerr.StripSentinel(err))
	case stderrors.Is(err, iamerr.ErrAborted):
		return status.Error(codes.Aborted, iamerr.StripSentinel(err))
	case stderrors.Is(err, iamerr.ErrUnavailable):
		return status.Error(codes.Unavailable, iamerr.StripSentinel(err))
	case stderrors.Is(err, iamerr.ErrInternal):
		// hardening-invariant #1: INTERNAL carries a FIXED opaque text, never the
		// wrapped detail (a wrapped ErrInternal may embed subject/principal ids,
		// row-counts or pgx/SQL text). Detail stays in the error chain for logs —
		// и ЭТО ОБЕЩАНИЕ ТЕПЕРЬ ИСПОЛНЯЕТСЯ (#666): подробность называется строкой
		// ниже, иначе она умирает здесь и причину отказа назвать нечем.
		nameDiscardedDetail(err, "sentinel")
		return status.Error(codes.Internal, "internal error")
	}
	if strings.HasPrefix(err.Error(), "Illegal argument") {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	// Defense-in-depth: an unexpected non-sentinel error must never surface its
	// raw text (could carry pgx/SQL detail) as the gRPC INTERNAL message
	// (api-conventions.md: INTERNAL = fixed text, no leak). The detail stays in
	// the error chain for server-side logging — и называется здесь.
	nameDiscardedDetail(err, "unclassified")
	return status.Error(codes.Internal, "internal error")
}

// sqlStater — то, что умеет назвать свой SQLSTATE.
//
// Интерфейс, а не импорт `pgconn`: этот пакет импортируют ~40 файлов слоя
// use-case, и тянуть в их граф сборки драйвер значило бы нарушить правило
// зависимостей ради одной строки журнала. `*pgconn.PgError` этому интерфейсу
// удовлетворяет, и разбор от версии драйвера не зависит.
type sqlStater interface{ SQLState() string }

// nameDiscardedDetail называет подробность отказа В ТОТ МОМЕНТ, когда она
// перестаёт существовать.
//
// # Зачем это здесь, а не у вызывающего
//
// Это ЕДИНСТВЕННАЯ воронка, где подробность теряется: наружу уезжает
// фиксированный непротекающий текст, и после возврата причины нет ни у кого.
// Два комментария на этом пути обещали журнал («Detail stays in the error chain
// for logging») — журнала не было, и обещание читалось как факт. Цена измерена:
// тянущий величины отказывал на каждом подъёме каждого домена, дважды подряд, и
// назвать причину было нечем — соответствующей строки в журнале не существовало
// ни одной.
//
// # Что именно пишется, и почему НЕ текст ошибки
//
// Пишутся SQLSTATE и ТИП корневой ошибки — то есть ровно то, что отвечает на
// вопрос «почему», и ровно то, что не может оказаться персональными данными.
// Свободный текст сюда не идёт: обёрнутая ошибка вправе нести значения полей
// арендатора, а `security.md` запрещает их в журнале — и запрет не имеет
// исключения «на отладку».
func nameDiscardedDetail(err error, lane string) {
	attrs := []any{
		slog.String("lane", lane),
		slog.String("error_type", fmt.Sprintf("%T", stderrors.Unwrap(err))),
	}
	var st sqlStater
	if stderrors.As(err, &st) {
		attrs = append(attrs, slog.String("sqlstate", st.SQLState()))
	}
	slog.Default().Error("repo error surfaced as INTERNAL", attrs...)
}

// MapValidationErr — обертка для результатов `domain.<Type>.Validate()`
// (cumulative multierr). Все sync-handler'ы вызывают ее на validation-stage
// перед эмитом Operation, чтобы InvalidArgument имел единую форму.
func MapValidationErr(err error) error {
	if err == nil {
		return nil
	}
	return status.Error(codes.InvalidArgument, err.Error())
}
