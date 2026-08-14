// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Ссылочная целостность внутри сервиса живёт в СХЕМЕ — `FK ... ON DELETE
// RESTRICT` (data-integrity.md, ban #10). Решение «удалить нельзя» принимает
// БД, и только она: предпроверка use-case'а читает свой снапшот в ОТДЕЛЬНОЙ
// читающей транзакции, а строку удаляет worker позже, поэтому ссылка,
// закоммиченная в это окно, предпроверке не видна by construction.
//
// Отсюда — предмет этого файла. Раз отказ выносит БД, то и контрактный ТЕКСТ,
// называющий блокирующие строки, обязан приходить с этого же пути. Пока текст
// с именами умела только предпроверка, у одного факта было два клиентских
// сообщения, и то из них, которое держится под гонкой, блокирующих не
// называло: вызывающему нечего было чинить, а утверждение приёмки, написанное
// на текст предпроверки, на этом пути не выполнялось.
//
// Перечисление НЕ является решением и не может его подменить: оно выполняется
// ПОСЛЕ отказа, полученного от БД, и только чтобы этот отказ назвать. Именно
// поэтому удаление идёт в SAVEPOINT'е — откат до точки сохранения оставляет
// транзакцию живой, и перечислить блокирующие строки можно тем же соединением.

// restrictBlockerCap — потолок ПЕРЕЧНЯ в сообщении. Текст отказа — диагностика
// для человека, а не выгрузка: перечень свыше потолка усекается, и усечение
// НАЗЫВАЕТСЯ («+N more»), чтобы «названы все» было отличимо от «названы первые».
//
// Потолок относится только к перечню. ЧИСЛО блокирующих строк считается
// отдельным запросом и НЕ усечено: иначе сообщение «has 21 target(s)» на группе
// со ста целями было бы ложью, поданной как измерение.
const restrictBlockerCap = 20

// RestrictBlockers — то, что БД назвала блокирующим: усечённый перечень плюс
// истинное число.
type RestrictBlockers struct {
	// IDs — не более restrictBlockerCap идентификаторов, по возрастанию.
	IDs []string
	// Total — истинное число блокирующих строк (не усечено).
	Total int
}

// Truncated — перечень короче истинного числа.
func (b RestrictBlockers) Truncated() bool { return b.Total > len(b.IDs) }

// List — перечень для сообщения; усечение названо явно.
func (b RestrictBlockers) List() string {
	if !b.Truncated() {
		return strings.Join(b.IDs, ", ")
	}
	return strings.Join(b.IDs, ", ") + fmt.Sprintf(", +%d more", b.Total-len(b.IDs))
}

// RestrictFKContract — контракт одной ссылки, удерживаемой `ON DELETE RESTRICT`.
type RestrictFKContract struct {
	// ParentKind — kind родителя в терминах mapPgErr (`TargetGroup`,
	// `NetworkLoadBalancer`): по нему различаются направления одного и того же
	// ограничения (удаление родителя против вставки ребёнка).
	ParentKind string
	// Blockers — перечисление строк, удержавших удаление. Читается ПОСЛЕ отказа
	// БД, в той же транзакции, откатанной до точки сохранения.
	Blockers func(ctx context.Context, tx pgx.Tx, parentID string) (RestrictBlockers, error)
	// Render — контрактный текст. Обязан совпадать с текстом предпроверки того
	// же факта: два текста об одном отказе — это два места об одном предмете, из
	// которых верно одно.
	Render func(parentID string, blockers RestrictBlockers) string
}

// Запросы перечисления — по одному на ссылку; каждый используется И для
// перечня (`LIMIT $2`), И для счёта, поэтому «сколько» и «кто» не могут
// разойтись, отвечая на разные предикаты.
const (
	qListenersByTargetGroup = `FROM kacho_nlb.listeners WHERE default_target_group_id = $1`
	qTargetsByTargetGroup   = `FROM kacho_nlb.targets WHERE target_group_id = $1`
	qListenersByLB          = `FROM kacho_nlb.listeners WHERE load_balancer_id = $1`
)

// RestrictFKContracts — все ссылки схемы `kacho_nlb`, удерживаемые
// `ON DELETE RESTRICT`, по имени ограничения из `pg_constraint`.
//
// Перечень сверяется с ДЕРЕВОМ, а не с памятью: гейт
// `TestGate_EveryRestrictFKHasBlockerNamingContract` перечисляет ограничения в
// мигрированной базе и требует совпадения в ОБЕ стороны — ссылка без контракта
// и контракт без ссылки одинаково являются находкой. Поэтому запись, чей FK
// снят миграцией, не переживает свой предмет, а новый FK RESTRICT нельзя
// завести, промолчав про текст его отказа.
var RestrictFKContracts = map[string]RestrictFKContract{
	// Слушатель → группа целей (0018 существование, 0023 расширен до
	// same-project). Удаление группы, на которую ссылается слушатель, отвергается
	// с ПЕРЕЧНЕМ слушателей: порядок разбора не приходится угадывать.
	"listeners_target_group_fk": {
		ParentKind: "TargetGroup",
		Blockers:   blockersOf(qListenersByTargetGroup),
		Render: func(_ string, b RestrictBlockers) string {
			return "target group is referenced by listeners: [" + b.List() + "]"
		},
	},
	// Цель → группа целей (0001). Контрактная единица здесь — ЧИСЛО целей и
	// действие («remove them first via RemoveTargets»); эта форма уже закреплена
	// приёмкой, поэтому путь БД повторяет её, а не заводит третью.
	"targets_target_group_id_fkey": {
		ParentKind: "TargetGroup",
		Blockers:   blockersOf(qTargetsByTargetGroup),
		Render: func(_ string, b RestrictBlockers) string {
			return fmt.Sprintf("TargetGroup has %d target(s); remove them first via RemoveTargets", b.Total)
		},
	},
	// Слушатель → балансировщик (0001). Контрактная форма отказа уже объявлена
	// предпроверкой use-case'а и DB-guard'ом MarkDeleting
	// (`markDeletingBlockReason`) — здесь она повторяется дословно, чтобы у
	// одного факта остался один текст на всех трёх производителях.
	"listeners_load_balancer_id_fkey": {
		ParentKind: "NetworkLoadBalancer",
		Blockers:   blockersOf(qListenersByLB),
		Render: func(parentID string, _ RestrictBlockers) string {
			return fmt.Sprintf("NetworkLoadBalancer %s has listener(s); delete first", parentID)
		},
	},
}

// blockersOf — перечислитель по хвосту запроса «FROM … WHERE <родитель> = $1».
// Перечень берётся с `LIMIT $2` (потолок — параметром, не склейкой строки),
// число — отдельным `count(*)` по тому же хвосту.
func blockersOf(fromWhere string) func(context.Context, pgx.Tx, string) (RestrictBlockers, error) {
	listQ := "SELECT id " + fromWhere + " ORDER BY id LIMIT $2"
	countQ := "SELECT count(*) " + fromWhere
	return func(ctx context.Context, tx pgx.Tx, parentID string) (RestrictBlockers, error) {
		var out RestrictBlockers
		rows, err := tx.Query(ctx, listQ, parentID, restrictBlockerCap)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return out, err
			}
			out.IDs = append(out.IDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
		if err := tx.QueryRow(ctx, countQ, parentID).Scan(&out.Total); err != nil {
			return out, err
		}
		return out, nil
	}
}

// isFKViolation — отказ 23503 по НАЗВАННОМУ ограничению. Различать по имени
// обязательно: одна и та же ссылка срабатывает в разные стороны (снос
// родителя, вставка ребёнка, переписывание ключа), и «любой 23503» смешал бы
// их в один текст.
func isFKViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraint
}

// tgMoveBlockedByListeners — контрактный текст отказа ПЕРЕНОСА группы целей.
//
// Форма ОТЛИЧАЕТСЯ от отказа удаления намеренно: предмет другой — не «сначала
// удали», а «сначала перенацель», — и приёмка ключуется именно на неё. Одна
// форма на всех производителей этого факта: предпроверку use-case'а, guard
// `NOT EXISTS` и отказ композитного FK при переписывании ключа.
//
// Счёт не читается либо равен нулю — отказ всё равно остаётся отказом (БД его
// вынесла), но числа в тексте нет: писать «0 listener(s)» значило бы подать
// невычитанное как измеренное. Достижимо только в окне «ссылка исчезла между
// отказом и счётом».
func tgMoveBlockedByListeners(ctx context.Context, tx pgx.Tx, tgID string) error {
	b, err := blockersOf(qListenersByTargetGroup)(ctx, tx, tgID)
	if err != nil || b.Total == 0 {
		return fmt.Errorf("%w: target group is referenced by listeners; repoint them before moving",
			kacho.ErrFailedPrecondition)
	}
	return fmt.Errorf("%w: target group is referenced by %d listener(s); repoint them before moving",
		kacho.ErrFailedPrecondition, b.Total)
}

// deleteParentRow — удаление строки-родителя, чей снос сторожат `ON DELETE
// RESTRICT`-ссылки.
//
// Решение принимает БД: строка исчезает ровно тогда, когда ни одно ограничение
// не сработало. SAVEPOINT здесь нужен НЕ для повторной попытки, а чтобы
// пережившая отказ транзакция могла ПЕРЕЧИСЛИТЬ то, что БД назвала блокирующим:
// после 23503 без точки сохранения соединение остаётся в отменённой транзакции
// и не отвечает ни на один запрос.
func deleteParentRow(ctx context.Context, tx pgx.Tx, kind, id, deleteSQL string) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return mapPgErr(err, kind, id)
	}
	tag, execErr := sp.Exec(ctx, deleteSQL, id)
	if execErr != nil {
		// ROLLBACK TO SAVEPOINT — транзакция снова пригодна для чтения.
		_ = sp.Rollback(ctx)
		return mapRestrictBlocked(ctx, tx, kind, id, execErr)
	}
	if tag.RowsAffected() == 0 {
		_ = sp.Rollback(ctx)
		return fmt.Errorf("%w: %s %s not found", kacho.ErrNotFound, kind, id)
	}
	// RELEASE SAVEPOINT — удаление остаётся в объемлющей транзакции.
	if err := sp.Commit(ctx); err != nil {
		return mapPgErr(err, kind, id)
	}
	return nil
}

// mapRestrictBlocked — SQLSTATE 23503 → код и КОНТРАКТНЫЙ ТЕКСТ с перечнем
// блокирующих строк.
//
// Перечень пуст либо не читается — отказ всё равно остаётся отказом: БД его
// уже вынесла, и подменять его успехом нельзя. Тогда текст даёт mapPgErr:
// у ссылки слушателя на группу целей его ветка начинается ТОЙ ЖЕ строкой, что
// и перечисляющая форма («target group is referenced by listeners»), поэтому
// клиент, ключующийся на начало сообщения, читает обе как один контракт; у
// остальных ссылок это общий тон `<Kind> has dependent resources` — он
// достижим только в окне «блокирующая строка исчезла между отказом БД и
// перечислением», и заявлять про него большее было бы неправдой.
func mapRestrictBlocked(ctx context.Context, tx pgx.Tx, kind, id string, execErr error) error {
	var pgErr *pgconn.PgError
	if !errors.As(execErr, &pgErr) || pgErr.Code != "23503" {
		return mapPgErr(execErr, kind, id)
	}
	c, ok := RestrictFKContracts[pgErr.ConstraintName]
	if !ok || c.ParentKind != kind {
		return mapPgErr(execErr, kind, id)
	}
	blockers, err := c.Blockers(ctx, tx, id)
	if err != nil || blockers.Total == 0 {
		return mapPgErr(execErr, kind, id)
	}
	return fmt.Errorf("%w: %s", kacho.ErrFailedPrecondition, c.Render(id, blockers))
}
