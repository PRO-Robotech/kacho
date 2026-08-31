// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package serviceerr — единый маппинг sentinel-ошибок kacho-registry в
// gRPC-статус. Используется и тонким handler'ом (sync-возврат), и async-worker'ом
// LRO (worker сохраняет google.rpc.Status в Operation.error), поэтому доменная
// ошибка обязана конвертироваться в gRPC-код именно здесь — единообразно для
// sync- и async-веток.
//
// Тексты сообщений — часть контракта Kachō ("<Resource> %s not found" и т. п.);
// сырой pgx/SQL наружу не утекает (некатегоризированное → фиксированный INTERNAL).
package serviceerr

import (
	"errors"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// ToStatus переводит ошибку use-case/repo/clients в gRPC-статус, срезая
// sentinel-префикс. Неклассифицированное → фиксированный INTERNAL (без leak'а).
// Уже-gRPC-статус (например, validate.PageSize) пробрасывается как есть.
//
// Порядок веток — сначала pass-through, потом sentinel-switch (форма kacho-nlb).
// Он несущий, а не косметический: pkg/validate кладёт имя поля ТОЛЬКО в
// google.rpc.BadRequest-details, сообщение остаётся общим «invalid argument».
// Пересборка статуса в sentinel-ветке (`status.Error(code, strip(err))`) детали
// теряет, поэтому ошибка, обёрнутая через `%w` на regerrors.Err*, обязана пройти
// pass-through ПЕРВОЙ. status с codes.Unknown под pass-through НЕ попадает
// (guard `!= Unknown`) — он падает в sentinel-switch и дальше в фиксированный
// INTERNAL (с лог-строкой), без leak'а.
func ToStatus(err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	// Учёт числа ресурсов разбирается ПЕРВЫМ и отдельно: клиенту мало кода — он
	// обязан различать полосы машинно, по `reason`-токену, а не разбором прозы.
	if st, ok := quotaRefusal(err); ok {
		return st
	}
	switch {
	case errors.Is(err, regerrors.ErrNotFound):
		return status.Error(codes.NotFound, strip(err, regerrors.ErrNotFound))
	case errors.Is(err, regerrors.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, strip(err, regerrors.ErrAlreadyExists))
	case errors.Is(err, regerrors.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, strip(err, regerrors.ErrFailedPrecondition))
	case errors.Is(err, regerrors.ErrInvalidArg):
		return status.Error(codes.InvalidArgument, strip(err, regerrors.ErrInvalidArg))
	case errors.Is(err, regerrors.ErrUnavailable):
		return status.Error(codes.Unavailable, strip(err, regerrors.ErrUnavailable))
	case errors.Is(err, regerrors.ErrInternal):
		// ErrInternal-класс: сырой текст (если обёрнут контекстом) в лог, клиенту — фикс.
		slog.Default().Error("registry: internal error mapped to gRPC INTERNAL", "err", err.Error())
		return status.Error(codes.Internal, "internal database error")
	}
	// Неклассифицированная ошибка (напр. corelib operations `repo.Create: <raw pg>`,
	// не прошедшая через registry-adapter Wrap) — логируем сырую причину ПЕРЕД схлопом,
	// иначе живой сбой Create = «internal database error» без единой лог-строки.
	slog.Default().Error("registry: unclassified error mapped to gRPC INTERNAL", "err", err.Error())
	return status.Error(codes.Internal, "internal database error")
}

// strip убирает префикс "<sentinel>: ", чтобы клиент видел стабильное сообщение.
func strip(err, sentinel error) string {
	msg := err.Error()
	prefix := sentinel.Error() + ": "
	if rest, ok := strings.CutPrefix(msg, prefix); ok {
		// Пустой остаток — вырожденный случай: обёртка без текста
		// (`fmt.Errorf("%w: %s", sentinel, "")`). Отдать его клиенту значило бы
		// отказать БЕЗ СООБЩЕНИЯ — код без единого слова о том, что делать
		// дальше, неотличимый в журнале от потери сообщения. Замещается текстом
		// самого sentinel'а (задача продукта #1658, полоса ct2-misc).
		if rest == "" {
			return sentinel.Error()
		}
		return rest
	}
	return msg
}
