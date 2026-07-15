// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package serviceerr — единый маппинг sentinel-ошибок use-case/repo-слоя
// kacho-storage в gRPC-статус. Используется тонким handler'ом (sync-возврат) и
// async-worker'ом LRO (worker сохраняет google.rpc.Status в Operation.error).
//
// Тексты сообщений — часть контракта Kachō ("<Resource> %s not found" и т. п.);
// сырой pgx/SQL наружу не утекает (некатегоризированное → фиксированный INTERNAL).
package serviceerr

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
)

// ToStatus переводит ошибку use-case/repo в gRPC-статус, срезая sentinel-префикс,
// чтобы клиент видел стабильное сообщение Kachō. Уже-gRPC-статус (например,
// codes.Unimplemented из adapter-заглушки скелета или validate.PageSize)
// пробрасывается как есть. Неклассифицированная ошибка → фиксированный INTERNAL
// "internal error" (§1.7 контрактный текст; без leak'а pgx-текста).
func ToStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return status.Error(codes.NotFound, strip(err, ports.ErrNotFound))
	case errors.Is(err, ports.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, strip(err, ports.ErrAlreadyExists))
	case errors.Is(err, ports.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, strip(err, ports.ErrFailedPrecondition))
	case errors.Is(err, ports.ErrInvalidArg):
		return status.Error(codes.InvalidArgument, strip(err, ports.ErrInvalidArg))
	case errors.Is(err, ports.ErrUnimplemented):
		return status.Error(codes.Unimplemented, strip(err, ports.ErrUnimplemented))
	case errors.Is(err, ports.ErrInternal):
		return status.Error(codes.Internal, "internal error")
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
