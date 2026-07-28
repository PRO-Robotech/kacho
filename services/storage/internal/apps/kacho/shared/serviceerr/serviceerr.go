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

	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// ToStatus переводит ошибку use-case/repo в gRPC-статус, срезая sentinel-префикс,
// чтобы клиент видел стабильное сообщение Kachō. Уже-gRPC-статус (например,
// codes.Unimplemented из adapter-заглушки скелета или validate.PageSize)
// пробрасывается как есть. Неклассифицированная ошибка → фиксированный INTERNAL
// "internal error" (§1.7 контрактный текст; без leak'а pgx-текста).
//
// Порядок веток — сначала pass-through, потом sentinel-switch (форма kacho-nlb).
// Он несущий, а не косметический: pkg/validate кладёт имя поля ТОЛЬКО в
// google.rpc.BadRequest-details, сообщение остаётся общим «invalid argument».
// Пересборка статуса в sentinel-ветке (`status.Error(code, strip(err))`) детали
// теряет, поэтому ошибка, обёрнутая через `%w` на storageerr.Err*, обязана пройти
// pass-through ПЕРВОЙ. status с codes.Unknown под pass-through НЕ попадает
// (guard `!= Unknown`) — он падает в sentinel-switch и дальше в фиксированный
// INTERNAL, без leak'а.
func ToStatus(err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	switch {
	case errors.Is(err, storageerr.ErrNotFound):
		return status.Error(codes.NotFound, strip(err, storageerr.ErrNotFound))
	case errors.Is(err, storageerr.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, strip(err, storageerr.ErrAlreadyExists))
	case errors.Is(err, storageerr.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, strip(err, storageerr.ErrFailedPrecondition))
	case errors.Is(err, storageerr.ErrInvalidArg):
		return status.Error(codes.InvalidArgument, strip(err, storageerr.ErrInvalidArg))
	case errors.Is(err, storageerr.ErrUnimplemented):
		return status.Error(codes.Unimplemented, strip(err, storageerr.ErrUnimplemented))
	case errors.Is(err, storageerr.ErrInternal):
		return status.Error(codes.Internal, "internal error")
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
