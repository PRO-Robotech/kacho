// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package roleverb — ЕДИНСТВЕННОЕ место, где пишется проекция «роль → тип
// объекта × глагол» (`kacho_iam.role_verb`).
//
// # Почему один писатель, а не два
//
// Проекция — то, из чего цепь вердикта собирает ответ «разрешено ли действие».
// Пока её пишут две реализации, всякое изменение семантики обязано доехать до
// ОБЕИХ, а промах в одной МОЛЧАЛИВ: обе компилируются, у обеих есть пробы, и
// расходятся они только на входе, который ни одна проба не подаёт. Прецедент
// этого класса дереву уже стоил инцидента — досев селекторов завели, а вторую
// сторону того же правила нет, и разошлись они на годы.
//
// # Почему отдельный пакет, а не тело метода репозитория
//
// Писателя зовут два пути, и лежат они по разные стороны ребра импортов.
// Путь роли (`Role.Create`/`Role.Update`) идёт через порт записи — метод
// `roleWriter.ReplaceRoleVerbs` пакета `pg`, который теперь только отображает
// отказы и делегирует сюда. Путь досева живёт в `apps/kacho/seed`, а `pg` уже
// импортирует `seed` (адаптеры портов досева), поэтому обратный импорт есть
// цикл — проверено сборкой. Пакет без входящих зависимостей на `seed` доступен
// обоим и оставляет SQL в слое `repo/`, которому он и принадлежит.
//
// # Граница
//
// Здесь нет решения о том, КОГДА и ДЛЯ КАКИХ ролей пересчитывать проекцию — это
// решает вызывающий (путь роли или досев). Здесь — только форма строки, отказ
// на негодной паре и транзакционность в границах транзакции вызывающего.
package roleverb

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// Execer — минимальная поверхность транзакции, которой хватает писателю.
// `pgx.Tx` её удовлетворяет; пул — намеренно НЕТ: замена проекции роли обязана
// быть атомарной, и вызывающий обязан открыть транзакцию сам.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Replace заменяет проекцию одной роли в границах транзакции вызывающего.
//
// ПОЛНАЯ ЗАМЕНА, а не досыпка: проекция есть СОСТОЯНИЕ роли, и глагол, снятый
// из правил, обязан отсюда исчезнуть. Досыпка означала бы, что отзыв права не
// применяется, — причём молча, потому что добавление проходит успешно и ни одна
// проверка «строки записаны» этого не заметит.
//
// Пары приходят от вызывающего уже переведёнными: перевод «точечное разрешение →
// тип модели + глагол» — это код (закрытый каталог типов, приведение имени), и
// повторять его в SQL значило бы завести второе место, знающее соответствие.
//
// # Совпавшая проекция не переписывается
//
// Если объявленный набор побайтово совпал с записанным, строки не трогаются
// вовсе. Это не оптимизация ради скорости: досев идёт на КАЖДОМ старте, и без
// сверки каждый старт снимал и заново клал всю проекцию — таблицу, которую
// читает цепь вердикта. Пропуск при этом не ослабляет замены: набор пар
// детерминирован правилами роли, поэтому два писателя, увидевшие одно и то же
// состояние, и записали бы одно и то же.
func Replace(ctx context.Context, ex Execer, roleID string, pairs []domain.RoleVerb) error {
	// Негодная пара отвергается ДО всякой записи: она означает, что перевод у
	// вызывающего дал ничего, и записать «ничего» тихо значит потерять право,
	// которое роль объявляет. Частичная проекция хуже отсутствующей.
	desired := make(map[domain.RoleVerb]struct{}, len(pairs))
	for _, pv := range pairs {
		if pv.ObjectType == "" || pv.Verb == "" {
			return fmt.Errorf("role_verb: пустая пара (%q,%q) у роли %s",
				pv.ObjectType, pv.Verb, roleID)
		}
		desired[pv] = struct{}{}
	}

	current, err := currentPairs(ctx, ex, roleID)
	if err != nil {
		return err
	}
	if samePairs(current, desired) {
		return nil
	}

	if _, err := ex.Exec(ctx,
		`DELETE FROM kacho_iam.role_verb WHERE role_id = $1`, roleID); err != nil {
		return fmt.Errorf("role_verb: снять проекцию роли %s: %w", roleID, err)
	}
	for pv := range desired {
		if _, err := ex.Exec(ctx,
			`INSERT INTO kacho_iam.role_verb (role_id, object_type, verb)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (role_id, object_type, verb) DO NOTHING`,
			roleID, pv.ObjectType, pv.Verb,
		); err != nil {
			return fmt.Errorf("role_verb: записать пару (%s,%s) роли %s: %w",
				pv.ObjectType, pv.Verb, roleID, err)
		}
	}
	return nil
}

// currentPairs — что записано у роли сейчас.
func currentPairs(ctx context.Context, ex Execer, roleID string) (map[domain.RoleVerb]struct{}, error) {
	rows, err := ex.Query(ctx,
		`SELECT object_type, verb FROM kacho_iam.role_verb WHERE role_id = $1`, roleID)
	if err != nil {
		return nil, fmt.Errorf("role_verb: прочитать проекцию роли %s: %w", roleID, err)
	}
	defer rows.Close()
	out := map[domain.RoleVerb]struct{}{}
	for rows.Next() {
		var pv domain.RoleVerb
		if serr := rows.Scan(&pv.ObjectType, &pv.Verb); serr != nil {
			return nil, fmt.Errorf("role_verb: разобрать строку проекции роли %s: %w", roleID, serr)
		}
		out[pv] = struct{}{}
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, fmt.Errorf("role_verb: обойти проекцию роли %s: %w", roleID, rerr)
	}
	return out, nil
}

// samePairs — совпадают ли наборы по СОСТАВУ. Колонки таблицы порядка не несут,
// поэтому сравнивать последовательности было бы сравнением не того.
func samePairs(a, b map[domain.RoleVerb]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
