// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/serviceerr"
)

// internalMapErr — admin/Internal-handler error mapper. Гарантирует что raw
// pgx-text (хранит hostname/db/query) не уходит в response даже на
// cluster-internal listener (:9091). Зеркалит kacho-vpc/internal/handler/internal_maperr.go.
//
// Порядок веток — сначала pass-through, потом sentinel-switch (форма kacho-nlb).
// Он несущий, а не косметический: pkg/validate кладёт имя поля ТОЛЬКО в
// google.rpc.BadRequest-details, сообщение остаётся общим «invalid argument».
// Пересборка статуса в sentinel-ветке (`status.Error(code, sentinel.Error())`)
// детали теряет, поэтому ошибка, обёрнутая через `%w` на serviceerr.Err*, обязана
// пройти pass-through ПЕРВОЙ. Строгая текстовая политика касается sentinel-ветвей,
// а не уже-курированного статуса, который и раньше проходил насквозь — просто ниже.
func internalMapErr(tag string, err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	switch {
	case errors.Is(err, serviceerr.ErrNotFound):
		return status.Error(codes.NotFound, serviceerr.ErrNotFound.Error())
	case errors.Is(err, serviceerr.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, serviceerr.ErrAlreadyExists.Error())
	case errors.Is(err, serviceerr.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, serviceerr.ErrFailedPrecondition.Error())
	case errors.Is(err, serviceerr.ErrInvalidArg):
		return status.Error(codes.InvalidArgument, serviceerr.ErrInvalidArg.Error())
	}
	if tag == "" {
		tag = "internal error"
	}
	return status.Error(codes.Internal, tag)
}
