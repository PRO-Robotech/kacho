// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrProjectInstanceLimit — предел числа машин проекта исчерпан.
//
// Отдельная ошибка, а не общий отказ: вызывающий обязан отличить исчерпание
// предела от сбоя хранилища. Первое — штатный ответ, который клиент понимает и
// на который реагирует; второе требует внимания оператора.
var ErrProjectInstanceLimit = errors.New("project instance limit reached")

// quotaWithinLimitConstraint — имя ограничения, ловящего превышение.
//
// Имя названо здесь, а не собрано из строки: отображение отказа хранилища в
// ответ вызывающему обязано знать, ЧТО именно нарушено. Без имени превышение
// предела пришло бы фиксированным внутренним отказом — то есть арендатор увидел
// бы «что-то сломалось» вместо «место кончилось».
const quotaWithinLimitConstraint = "project_instance_quotas_within_limit_check"

// chargeProjectQuota списывает одну машину из предела проекта.
//
// # Почему UPDATE, а не SELECT + сравнение
//
// Строка счётчика блокируется обновлением: второй писатель ждёт коммита первого
// и видит его результат. Чтение с последующим сравнением этого не даёт — между
// ними помещается чужая запись, и оба создателя пройдут проверку, увидев одно и
// то же свободное место.
//
// # Почему превышение ловит схема, а не это условие
//
// Условие здесь есть, и оно полезно: оно даёт понятный отказ раньше, чем база
// откажет своим. Но НЕСУЩИМ является ограничение схемы: оно защищает все пути,
// включая те, что появятся позже и про эту функцию знать не будут.
//
// # Проект без строки предела
//
// Отсутствие строки означает «предел не назначен», и это законное состояние:
// платформенный домен лимитов ещё не проецировал сюда значение. Такой проект не
// ограничивается — но и не остаётся без счёта: строка заводится с назначением
// предела, и потребление начинает считаться с этого момента.
func chargeProjectQuota(ctx context.Context, tx pgx.Tx, projectID string) error {
	const q = `UPDATE project_instance_quotas
		          SET used = used + 1, updated_at = now()
		        WHERE project_id = $1`
	tag, err := tx.Exec(ctx, q, projectID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == quotaWithinLimitConstraint {
			return ErrProjectInstanceLimit
		}
		return wrapPgErr(err, "Instance", projectID)
	}
	// Ноль строк — предел проекту не назначен. Это не ошибка: см. выше.
	_ = tag
	return nil
}

// refundProjectQuota возвращает одну машину в предел проекта.
//
// Зовётся в той же транзакции, что удаление строки машины. Возврат вне
// транзакции удаления оставил бы счётчик завышенным при откате — то есть проект
// платил бы местом за машину, которой нет.
//
// Отрицательное потребление запрещено схемой: возврат того, что не списывали,
// означает удаление машины, которой не было, и ловить это надо в момент, когда
// ошибка ещё локальна.
func refundProjectQuota(ctx context.Context, tx pgx.Tx, projectID string) error {
	const q = `UPDATE project_instance_quotas
		          SET used = GREATEST(used - 1, 0), updated_at = now()
		        WHERE project_id = $1`
	if _, err := tx.Exec(ctx, q, projectID); err != nil {
		return wrapPgErr(err, "Instance", projectID)
	}
	return nil
}
