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

	"github.com/PRO-Robotech/kacho/pkg/db/pgfault"
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
	f := pgfault.Classify(err)
	if f.FromDatabase() {
		switch f.Class {
		case pgfault.Unique:
			return fmt.Errorf("%w: %s %s already exists", geoerrors.ErrAlreadyExists, resource, id)
		case pgfault.ForeignKey: // direction-neutral: 23503 летит и на
			// parent-delete (Region.Delete с зонами), и на child-insert/update (Zone
			// с несуществующим region_id). Текст не привязан к направлению.
			return fmt.Errorf("%w: %s %s violates a reference constraint", geoerrors.ErrFailedPrecondition, resource, id)
		case pgfault.Check:
			return checkViolation(f, resource, id)
		}
		// Некатегоризированный SQLSTATE (deadlock 40P01, serialization 40001,
		// insufficient_privilege 42501, …). Клиенту отдаём фиксированный sentinel
		// (без leak'а pgx-текста), НО SQLSTATE логируем на repo-границе — иначе
		// root cause выбрасывается без следа (CWE-390) и оператор при разборе
		// инцидента не имеет привязки к реальной причине БД.
		slog.Error("uncategorized postgres error mapped to internal",
			append([]any{"resource", resource, "id", id}, f.LogAttrs()...)...)
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

// Здесь жили `nameConstraintSuffix` и `WrapUnique` — разбор 23505 по имени
// ограничения, чтобы отличить конфликт по ИМЕНИ от конфликта по id.
//
// Предмета у них больше нет (#716): у каталога размещения снято поле-дубль, и
// вместе с ним ушла глобальная `UNIQUE (name)`. Единственный уникальный ключ
// обеих таблиц — первичный, поэтому 23505 говорит ровно об id, и разбирать
// нечего. Сняты вместе с вызовами, а не оставлены «на всякий случай»: ветка,
// которая не может сработать, читается следующим как действующая маршрутизация.

// checkViolation отображает 23514 в отказ по вводу.
//
// Полосы «наш дефект против ввода вызывающего» здесь БОЛЬШЕ НЕТ, и это следствие
// #716, а не упрощение. Разделяла полосы принадлежность ограничения форме имени
// (`nameform.IsConstraint`), а формы имени у каталога размещения не осталось: поле
// снято, `<t>_name_check` снят вместе с ним миграцией 716001. Ветка, которую
// нельзя достичь ни при каком входе, снята вместе со своим предметом — оставленная,
// она читалась бы следующим как действующая маршрутизация (у vpc/compute/storage/
// nlb предмет на месте, и разбор полос там остаётся).
//
// Имя ограничения наружу не идёт: оно идёт в журнал, чтобы «ограничение ловит ввод
// регулярно» было счётно.
func checkViolation(f pgfault.Fault, resource, id string) error {
	slog.Warn("check constraint rejected caller input",
		append([]any{"resource", resource, "id", id}, f.LogAttrs()...)...)
	return fmt.Errorf("%w: invalid %s", geoerrors.ErrInvalidArg, resource)
}
