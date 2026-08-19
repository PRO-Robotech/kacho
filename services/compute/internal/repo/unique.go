// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/pkg/validate/nameform"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// isUniqueViolation — Postgres unique-constraint violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	s := err.Error()
	return strings.Contains(s, "23505") || strings.Contains(s, "duplicate key value")
}

// isFKViolation — Postgres foreign_key_violation (SQLSTATE 23503). Маппится в
// gRPC FailedPrecondition ("The <resource> is being used").
func isFKViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	s := err.Error()
	return strings.Contains(s, "23503") || strings.Contains(s, "violates foreign key")
}

// isCheckViolation — Postgres check_violation (SQLSTATE 23514). Легитимный
// bad-input по user-reachable CHECK-constraint → gRPC InvalidArgument
// (data-integrity.md SQLSTATE→gRPC table).
func isCheckViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23514"
	}
	return strings.Contains(err.Error(), "23514")
}

// isExclusionViolation — Postgres exclusion_violation (SQLSTATE 23P01). Состояние
// ресурса не позволяет (пересечение EXCLUDE-range) → gRPC FailedPrecondition
// (data-integrity.md SQLSTATE→gRPC table).
func isExclusionViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23P01"
	}
	return strings.Contains(err.Error(), "23P01")
}

// wrapPgErr классифицирует pgx-ошибку и возвращает sentinel-ошибку из
// service-пакета. НЕ leak'ает raw PG-сообщение клиенту: неизвестные классы → ErrInternal.
//
// kind/id — для NotFound сообщений (имя ресурса + id).
func wrapPgErr(err error, kind, id string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if id != "" {
			return fmt.Errorf("%w: %s %s not found", ports.ErrNotFound, kind, id)
		}
		return ports.ErrNotFound
	}
	// Отказ учёта квоты — ПЕРЕД общими классами. Его SQLSTATE'ы (KQ001/KQ002)
	// ни одному из них не принадлежат, поэтому без этой ветки они дошли бы до
	// `ErrInternal` — то есть арендатор, упёршийся в потолок, увидел бы
	// «что-то сломалось» вместо «место кончилось», а администратор не отличил бы
	// исчерпание от неназначенного предела.
	if qerr := classifyQuotaErr(err); qerr != err {
		return qerr
	}
	if isUniqueViolation(err) {
		return ports.ErrAlreadyExists
	}
	if isFKViolation(err) {
		return fmt.Errorf("%w: The %s %s is being used", ports.ErrFailedPrecondition, strings.ToLower(kind), id)
	}
	if isCheckViolation(err) {
		return wrapCheckViolation(err, kind, id)
	}
	if isExclusionViolation(err) {
		return ports.ErrFailedPrecondition
	}
	return ports.ErrInternal
}

// wrapCheckViolation разбирает 23514 на две полосы по вопросу «чьё это
// значение» — тот же разбор, что в vpc, nlb и storage: чинится класс, а не
// экземпляр.
//
// Форму имени compute проверяет сам — `corevalidate.Name` / `NameOrDefault` на
// всех четырёх ресурсах, несущих ограничение формы (машина, тип машины, группа
// размещения, гостевой ключ). Значит ограничение таблицы есть защита последнего
// рубежа, и его срабатывание означает, что негодное значение прошло МИМО
// проверки: дефект сервиса, а не ввода.
//
// Текст наружу и раньше не пересказывал СУБД — здесь чинится не тон, а СМЫСЛ
// кода ответа: `INVALID_ARGUMENT` на нашем дефекте обвиняет вызывающего и не
// даёт ему ничего, что можно исправить. Имя ограничения идёт в журнал (ERROR для
// нашего дефекта, WARN для ввода) — иначе о срабатывании не знает никто.
func wrapCheckViolation(err error, kind, id string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if nameform.IsConstraint(pgErr.TableName, pgErr.ConstraintName) {
			slog.Error("name form backstop fired: service admitted a name it validates itself",
				"sqlstate", pgErr.Code, "table", pgErr.TableName,
				"constraint", pgErr.ConstraintName, "kind", kind, "id", id)
			return ports.ErrInternal
		}
		slog.Warn("check constraint rejected caller input",
			"sqlstate", pgErr.Code, "table", pgErr.TableName,
			"constraint", pgErr.ConstraintName, "kind", kind, "id", id)
	}
	return ports.ErrInvalidArg
}
