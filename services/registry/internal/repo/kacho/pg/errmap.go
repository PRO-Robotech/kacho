// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/pkg/db/pgfault"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// errmap.go — pgx/SQLSTATE→sentinel трансляция. Живёт в repo-adapter (package pg),
// а НЕ в leaf-пакете internal/errors: перенос сюда убирает pgx/pgconn из
// dependency-графа use-case (internal/apps/kacho/api/registry импортит только
// голые sentinel'ы) — clean-arch dependency-rule (adapter не течёт в use-case).

// wrapPgErr транслирует ошибку pgx/pgconn в sentinel kacho-registry, прикрепляя
// стабильное сообщение без утечек. Маппинг SQLSTATE:
//
//	pgx.ErrNoRows → ErrNotFound
//	23505 UNIQUE  → ErrAlreadyExists
//	23503 FK      → ErrFailedPrecondition
//	23514 CHECK   → ErrInvalidArg
//	все остальное → ErrInternal
//
// resource — человекочитаемый ярлык ("Registry"); id — id ресурса (может быть "").
func wrapPgErr(err error, resource, id string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s %s not found", regerrors.ErrNotFound, resource, id)
	}
	// Учёт числа ресурсов классифицируется ПЕРВЫМ и отдельно от общей таблицы
	// SQLSTATE'ов: клиенту мало кода — он обязан различать полосы машинно, по
	// признаку, а не разбором прозы. Подробности — в classifyQuotaErr.
	if qerr := classifyQuotaErr(err); qerr != nil {
		return qerr
	}
	f := pgfault.Classify(err)
	if f.FromDatabase() {
		switch f.Class {
		case pgfault.Unique:
			return fmt.Errorf("%w: %s %s already exists", regerrors.ErrAlreadyExists, resource, id)
		case pgfault.ForeignKey:
			return fmt.Errorf("%w: %s %s violates a reference constraint", regerrors.ErrFailedPrecondition, resource, id)
		case pgfault.Check:
			return fmt.Errorf("%w: invalid %s", regerrors.ErrInvalidArg, resource)
		}
		slog.Default().Error("registry repo: unclassified database error",
			append([]any{"resource", resource, "resource_id", id}, f.LogAttrs()...)...)
		return regerrors.ErrInternal
	}
	slog.Default().Error("registry repo: unclassified error",
		"err", err.Error(), "resource", resource, "resource_id", id)
	return regerrors.ErrInternal
}
