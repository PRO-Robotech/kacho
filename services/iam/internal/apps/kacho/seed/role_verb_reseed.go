// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package seed

// role_verb_reseed.go — досев проекции «роль → тип объекта × глагол» для
// системных ролей.
//
// # Зачем досев вообще
//
// Правило роли читают с ДВУХ сторон: селекторы отвечают «подходит ли объект»,
// проекция глаголов — «разрешено ли действие». Обе стороны пишет путь
// ПОЛЬЗОВАТЕЛЬСКОЙ роли (`Role.Create`/`Role.Update`), а системная роль
// заводится сырым SQL миграции и этим путём не проходит НИКОГДА. Роль с одной
// стороной адресует объект и не разрешает на нём ничего: вердикт по её выдаче —
// отказ, причём МОЛЧАЛИВЫЙ (пустое соединение не отличается от честного «права
// нет»).
//
// # Зернистость — ОДНА ТРАНЗАКЦИЯ НА РОЛЬ
//
// Прежде пересчёт шёл по всем ролям в одной транзакции, общей с досевом
// владельческих выдач. Отсюда два следствия, оба наблюдаемых: отказ на одной
// паре откатывал работу, к проекции отношения не имеющую, и откатывал пересчёт
// ВСЕХ прочих ролей — то есть оставлял проекцию неизвестной целиком, при том что
// самолечение обещано по ролям.
//
// Цена размена названа прямо: пересчёт перестаёт быть атомарным МЕЖДУ ролями,
// и состояние «часть ролей пересчитана, часть нет» становится наблюдаемым. Это
// законно и не ново — то же состояние наблюдается между двумя стартами и между
// правками двух разных ролей: проекция роли самодостаточна, и вердикт по роли A
// не читает строк роли B. Инвариант, который обязан удержаться, — ВНУТРИ роли,
// и его держит транзакция вокруг замены (снять всё, положить текущее).
//
// # Полос отказа ДВЕ, и смешивать их нельзя
//
// Транзиентная — база не ответила, часть ролей пересеяна: следующий старт
// пересчитает, и старт ронять не за что. Структурная — системные роли есть, а
// пересеяно НОЛЬ: механизм не работает, и повтор даст то же; «повтори позже»
// здесь есть ложь. Отличает их ПЕРЕПИСЬ, поэтому она возвращается вызывающему
// и попадает в текст отказа: без обеих величин «пересеяно ноль» неотличимо от
// «пересеяно не всё».
//
// # Своего SQL здесь НЕТ
//
// Писатель проекции в дереве один и лежит в слое репозитория
// (`repo/kacho/pg/roleverb`). Досев решает лишь КОГДА и ДЛЯ КАКИХ ролей
// пересчитывать; решение «как писать» ему не принадлежит.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmap"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg/roleverb"
)

// Исходы пересчёта одной роли — закрытый набор, приходит из констант этого
// пакета и никогда из запроса, поэтому кардинальность метки не растёт с
// трафиком.
const (
	// RoleVerbReseedOutcomeReseeded — транзакция роли закоммичена.
	RoleVerbReseedOutcomeReseeded = "reseeded"
	// RoleVerbReseedOutcomeFailed — транзакция роли откачена.
	RoleVerbReseedOutcomeFailed = "failed"
)

// RoleVerbReseedObserver — куда досев сообщает исход по каждой роли.
//
// Успехи считаются наравне с отказами: счётчик одних отказов не отличает
// «отказов не было» от «пересчёта не было вовсе», и путь, умерший целиком,
// выглядел бы здоровее всех.
type RoleVerbReseedObserver interface {
	IncRoleVerbReseed(outcome string)
}

// RoleVerbReseedCensus — перепись одного прогона досева.
//
// «Пересеяно» считает ЗАКОММИЧЕННЫЕ роли, а не начатые: при зернистости по роли
// транзакция роли либо закоммичена, либо нет, и называть надо первое. Иначе
// после отката печаталось бы число, которого в базе нет, — ровно тот класс, ради
// которого перепись и заводится.
type RoleVerbReseedCensus struct {
	// Examined — системных ролей с непустыми правилами прочитано.
	Examined int
	// Reseeded — ролей, чья транзакция закоммичена.
	Reseeded int
	// Pairs — пар, объявленных правилами закоммиченных ролей.
	Pairs int
	// Failed — ролей, чья транзакция откачена.
	Failed int
}

// Structural — полоса, на которой повтор даст то же самое: роли есть, пересеяна
// ни одна. Пустое множество ролей структурной полосой НЕ является — это свежая
// база, и роняя на ней старт, проверка краснела бы на достижении цели.
func (c RoleVerbReseedCensus) Structural() bool {
	return c.Examined > 0 && c.Reseeded == 0
}

// ReseedSystemRoleVerbs пересчитывает проекцию глаголов КАЖДОЙ системной роли,
// каждую роль — в собственной транзакции.
//
// Отказ на одной роли не прерывает обход: остальные обязаны быть пересеяны, и
// именно это отличает «база моргнула на одной строке» от «механизм не работает».
// Возвращается перепись (всегда, в том числе при отказе) и объединённая ошибка,
// чей текст несёт ОБЕ величины переписи.
//
// Наблюдатель необязателен: `nil` означает «исходы никуда не считать» — так его
// зовут операционные точки входа, у которых реестра метрик нет.
func ReseedSystemRoleVerbs(
	ctx context.Context,
	pool *pgxpool.Pool,
	obs RoleVerbReseedObserver,
) (RoleVerbReseedCensus, error) {
	var census RoleVerbReseedCensus

	roles, err := listMaterializingSystemRoles(ctx, pool)
	if err != nil {
		return census, err
	}
	census.Examined = len(roles)

	var failures []error
	for _, rr := range roles {
		pairs := authzmap.RoleVerbsFromSelectors(rr.rules.MaterializingSelectors())
		if rerr := replaceRoleVerbsInOwnTx(ctx, pool, rr.id, pairs); rerr != nil {
			census.Failed++
			failures = append(failures, rerr)
			if obs != nil {
				obs.IncRoleVerbReseed(RoleVerbReseedOutcomeFailed)
			}
			continue
		}
		census.Reseeded++
		census.Pairs += len(pairs)
		if obs != nil {
			obs.IncRoleVerbReseed(RoleVerbReseedOutcomeReseeded)
		}
	}
	if len(failures) == 0 {
		return census, nil
	}
	return census, fmt.Errorf(
		"пересчёт проекции глаголов роли: осмотрено %d, пересеяно %d, отказало %d: %w",
		census.Examined, census.Reseeded, census.Failed, errors.Join(failures...))
}

// replaceRoleVerbsInOwnTx — одна роль, одна транзакция. Замена «снять всё,
// положить текущее» обязана быть атомарной: читатель цепи вердикта не вправе
// увидеть пустой промежуток, потому что в этом окне всякий вердикт по роли
// отказывает, и отказывает молча.
func replaceRoleVerbsInOwnTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	roleID string,
	pairs []domain.RoleVerb,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("пересчёт проекции роли %s: открыть транзакцию: %w", roleID, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if rerr := roleverb.Replace(ctx, tx, roleID, pairs); rerr != nil {
		return fmt.Errorf("пересчёт проекции роли %s: %w", roleID, rerr)
	}
	if cerr := tx.Commit(ctx); cerr != nil {
		return fmt.Errorf("пересчёт проекции роли %s: зафиксировать: %w", roleID, cerr)
	}
	committed = true
	return nil
}

// listMaterializingSystemRoles читает системные роли с непустыми правилами —
// то же множество, по которому идёт досев селекторов. Обход полностью вычитан
// ДО первой транзакции: строки роли открываются по одной, и держать живой курсор
// на том же соединении, что и запись, нельзя.
func listMaterializingSystemRoles(ctx context.Context, pool *pgxpool.Pool) ([]systemRoleRules, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, rules FROM kacho_iam.roles
		  WHERE is_system = true
		    AND rules IS NOT NULL
		    AND jsonb_typeof(rules) = 'array'
		    AND jsonb_array_length(rules) > 0`)
	if err != nil {
		return nil, fmt.Errorf("пересчёт проекции глаголов роли: перечислить системные роли: %w", err)
	}
	defer rows.Close()
	var out []systemRoleRules
	for rows.Next() {
		var (
			id  string
			raw []byte
		)
		if serr := rows.Scan(&id, &raw); serr != nil {
			return nil, fmt.Errorf("пересчёт проекции глаголов роли: разобрать строку роли: %w", serr)
		}
		parsed, derr := domain.DecodeRules(raw)
		if derr != nil {
			return nil, fmt.Errorf("пересчёт проекции глаголов роли: разобрать правила %s: %w", id, derr)
		}
		out = append(out, systemRoleRules{id: id, rules: parsed})
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, fmt.Errorf("пересчёт проекции глаголов роли: обойти системные роли: %w", rerr)
	}
	return out, nil
}
