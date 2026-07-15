// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package dberr — repo-adjacent adapter: трансляция ошибок pgx/pgconn в чистые
// sentinel'ы internal/errors. Зависит от pgx (Postgres-driver), поэтому вынесен
// из leaf-пакета internal/errors — так use-case/domain тянут только sentinel'ы,
// а pgx остаётся в dependency-closure одного лишь repo-слоя (ports/adapters).
package dberr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
)

// Wrap транслирует ошибку pgx/pgconn в sentinel kacho-geo, прикрепляя стабильное
// сообщение без утечек ("<Resource> <id> not found"). Маппинг SQLSTATE:
//
//	pgx.ErrNoRows            → ErrNotFound
//	23505 UNIQUE             → ErrAlreadyExists
//	23503 FK                 → ErrFailedPrecondition
//	23514 CHECK              → ErrInvalidArg
//	context.Canceled         → ErrCanceled          (client-cancel, не серверный сбой)
//	context.DeadlineExceeded → ErrDeadlineExceeded  (истёкший per-call timeout)
//	все остальное            → ErrInternal
//
// resource — человекочитаемый ярлык ("Region" / "Zone"); id — id ресурса (может быть "").
func Wrap(err error, resource, id string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s %s not found", geoerrors.ErrNotFound, resource, id)
	}
	// Отмена/дедлайн вызывающей стороны (client-cancel, истёкший per-call timeout) —
	// НЕ серверный сбой: коллапс в ErrInternal раздул бы server-error budget и залил
	// бы ERROR-лог ложным «uncategorized» именно во время latency/timeout-инцидентов.
	// Отдаём выделенные sentinel'ы (→ codes.Canceled / codes.DeadlineExceeded) и не
	// логируем на ERROR (нормальный исход, не root-cause для operator-trail).
	if errors.Is(err, context.Canceled) {
		return geoerrors.ErrCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return geoerrors.ErrDeadlineExceeded
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s %s already exists", geoerrors.ErrAlreadyExists, resource, id)
		case "23503": // foreign_key_violation — direction-neutral: 23503 летит и на
			// parent-delete (Region.Delete с зонами), и на child-insert/update (Zone
			// с несуществующим region_id). Текст не привязан к направлению.
			return fmt.Errorf("%w: %s %s violates a reference constraint", geoerrors.ErrFailedPrecondition, resource, id)
		case "23514": // check_violation
			return fmt.Errorf("%w: invalid %s", geoerrors.ErrInvalidArg, resource)
		}
		// Некатегоризированный SQLSTATE (deadlock 40P01, serialization 40001,
		// insufficient_privilege 42501, …). Клиенту отдаём фиксированный sentinel
		// (без leak'а pgx-текста), НО SQLSTATE логируем на repo-границе — иначе
		// root cause выбрасывается без следа (CWE-390) и оператор при разборе
		// инцидента не имеет привязки к реальной причине БД.
		slog.Error("uncategorized postgres error mapped to internal",
			"sqlstate", pgErr.Code,
			"pg_message", pgErr.Message,
			"resource", resource,
			"id", id)
		return geoerrors.ErrInternal
	}
	// Не-pg ошибка (context deadline, conn reset, pool-exhaustion). Так же:
	// клиенту — sentinel, но оригинал логируем для operator-trail.
	slog.Error("uncategorized db error mapped to internal",
		"err", err.Error(),
		"resource", resource,
		"id", id)
	return geoerrors.ErrInternal
}
