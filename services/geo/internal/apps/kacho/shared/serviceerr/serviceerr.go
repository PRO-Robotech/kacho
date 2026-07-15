// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package serviceerr — единый маппинг sentinel-ошибок repo-слоя kacho-geo в
// gRPC-статус. Используется и тонким handler'ом (sync-возврат InvalidArgument на
// malformed id), и async-worker'ом LRO: worker сохраняет результирующий
// google.rpc.Status в Operation.error, поэтому доменная ошибка обязана быть
// сконвертирована в gRPC-код именно здесь, до записи в operations-строку.
//
// Тексты сообщений — часть контракта Kachō ("<Resource> %s not found" и т. п.);
// сырой pgx/SQL наружу не утекает (некатегоризированное → фиксированный INTERNAL).
package serviceerr

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
)

// ToStatus переводит ошибку use-case/repo в gRPC-статус, срезая sentinel-префикс,
// чтобы клиент видел стабильное сообщение Kachō. Неклассифицированная ошибка →
// фиксированный INTERNAL "internal database error" (без leak'а pgx-текста).
// Уже-gRPC-статус (например, validate.PageSize) пробрасывается как есть.
func ToStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, geoerrors.ErrNotFound):
		return status.Error(codes.NotFound, strip(err, geoerrors.ErrNotFound))
	case errors.Is(err, geoerrors.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, strip(err, geoerrors.ErrAlreadyExists))
	case errors.Is(err, geoerrors.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, strip(err, geoerrors.ErrFailedPrecondition))
	case errors.Is(err, geoerrors.ErrInvalidArg):
		return status.Error(codes.InvalidArgument, strip(err, geoerrors.ErrInvalidArg))
	case errors.Is(err, geoerrors.ErrCanceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, geoerrors.ErrDeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	case errors.Is(err, geoerrors.ErrInternal):
		return status.Error(codes.Internal, "internal database error")
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	return status.Error(codes.Internal, "internal database error")
}

// strip убирает префикс "<sentinel>: ", чтобы клиент видел стабильное сообщение.
func strip(err, sentinel error) string {
	msg := err.Error()
	prefix := sentinel.Error() + ": "
	if rest, ok := strings.CutPrefix(msg, prefix); ok {
		return rest
	}
	return msg
}
