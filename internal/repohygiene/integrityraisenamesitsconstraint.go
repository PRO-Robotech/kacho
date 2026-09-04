// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// integrityraisenamesitsconstraint.go — разбор миграций: кто поднимает класс
// `integrity_constraint_violation` и называет ли при этом связь.
//
// # Предмет
//
// Отображение отказов сервиса (`repo/kacho/pg/pgmaperr.go`) отдаёт этому классу
// `FAILED_PRECONDITION` целиком — и это верно: «состояние ресурса не позволяет»,
// а не поломка. Но ПРИЗНАК, по которому клиент машинно отличает одну полосу
// класса от прочих предусловий, выбирается ПО ИМЕНИ СВЯЗИ. Отказ, поднятый без
// `CONSTRAINT`, попадает в общую полосу с фиксированным текстом: код тот же,
// различимость потеряна, и потребитель, дочитывающий перечень мешающих выдач,
// её не получает.
//
// Заметить это чтением нельзя: обе формы `RAISE` законны, обе компилируются, и
// сервер на обеих отвечает одинаковым SQLSTATE.
//
// # Почему гейт, а не число в комментарии
//
// В комментарии отображения стояло «единственный производитель в дереве» с
// предикатом `git grep -n integrity_constraint_violation -- '*.sql'` → «одно
// попадание». Предикат давал ЧЕТЫРЕ, и одно из четырёх — его собственное
// объяснение в шапке соседней миграции, то есть счёт шёл по слову, а не по
// производителю (задача #2018). Число в комментарии не имеет владельца и
// переживает свой замер молча; поэтому число здесь не выписывается вовсе — его
// печатает перепись гейта на каждом прогоне.
//
// # Что считается ЖИВЫМ производителем
//
// Миграции переопределяют функции: `CREATE OR REPLACE FUNCTION` в более поздней
// миграции ЗАМЕЩАЕТ определение из ранней, а применённую миграцию не правят
// (ban #5). Поэтому находкой может быть только определение, которое доживает до
// боевой базы:
//
//   - учитывается ветвь `Up` — то, что после `-- +goose Down`, есть откат, и он
//     возвращает ПРЕЖНЕЕ состояние by construction;
//   - у функции живёт определение из миграции с НАИБОЛЬШЕЙ версией; версии
//     сравниваются числом, а не строкой: `472002` и `20260824010000` строкой
//     упорядочиваются наоборот;
//   - производитель вне функции живым считается всегда — замещать его нечем.
//
// Без этого разбора гейт краснел бы на истории, которую правкой не изменить, —
// то есть требовал бы нарушить ban #5.
package repohygiene

import (
	"regexp"
	"strconv"
	"strings"
)

// IntegrityRaiseSite — один `RAISE`, поднимающий класс целостности.
type IntegrityRaiseSite struct {
	// File — путь миграции от корня дерева.
	File string
	// Version — числовая версия миграции (префикс имени файла).
	Version uint64
	// Function — функция, внутри которой стоит RAISE; пусто, если вне функции.
	Function string
	// Line — строка, на которой начинается RAISE.
	Line int
	// NamesConstraint — в клаузе USING назван `CONSTRAINT`.
	NamesConstraint bool
	// InDownBranch — RAISE стоит после `-- +goose Down`.
	InDownBranch bool
}

var (
	// integrityErrcode — клауза, объявляющая класс. Ищется вместе с `ERRCODE`,
	// а не по одному имени класса: имя встречается и в прозе шапок миграций, и
	// счёт по нему считал бы объяснение производителем.
	integrityErrcode = regexp.MustCompile(`(?i)ERRCODE\s*=\s*'integrity_constraint_violation'`)
	// integrityRaiseKeyword — начало оператора.
	integrityRaiseKeyword = regexp.MustCompile(`(?i)\bRAISE\b`)
	// integrityConstraintClause — `CONSTRAINT = '<имя>'` в той же клаузе USING.
	integrityConstraintClause = regexp.MustCompile(`(?i)\bCONSTRAINT\s*=`)
	// integrityCreateFunction — объявление функции.
	integrityCreateFunction = regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+([A-Za-z0-9_.]+)`)
	// integrityGooseDown — граница ветви отката.
	integrityGooseDown = regexp.MustCompile(`(?m)^\s*--\s*\+goose\s+Down\b`)
	// integrityVersionPrefix — числовой префикс имени миграции.
	integrityVersionPrefix = regexp.MustCompile(`^(\d+)_`)
)

// MigrationVersion — числовая версия миграции по имени файла. Ноль означает
// «префикса нет»; такой файл миграцией goose не является.
func MigrationVersion(path string) uint64 {
	name := path
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	m := integrityVersionPrefix.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	v, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// IntegrityRaiseSitesIn разбирает ОДНУ миграцию.
//
// Оператор берётся целиком — от слова `RAISE` до завершающей `;`, — потому что
// `CONSTRAINT` стоит в клаузе `USING` через строку-другую после `ERRCODE`, и
// разбор по одной строке объявил бы названную связь безымянной.
func IntegrityRaiseSitesIn(path, body string) []IntegrityRaiseSite {
	version := MigrationVersion(path)
	downAt := len(body)
	if loc := integrityGooseDown.FindStringIndex(body); loc != nil {
		downAt = loc[0]
	}

	var out []IntegrityRaiseSite
	for _, loc := range integrityErrcode.FindAllStringIndex(body, -1) {
		// Начало оператора — последнее слово RAISE перед клаузой.
		starts := integrityRaiseKeyword.FindAllStringIndex(body[:loc[0]], -1)
		if len(starts) == 0 {
			continue
		}
		start := starts[len(starts)-1][0]
		// Конец оператора — первая `;` после клаузы ERRCODE.
		end := strings.IndexByte(body[loc[1]:], ';')
		if end < 0 {
			end = len(body) - loc[1]
		}
		stmt := body[start : loc[1]+end]

		out = append(out, IntegrityRaiseSite{
			File:            path,
			Version:         version,
			Function:        integrityEnclosingFunction(body[:start]),
			Line:            strings.Count(body[:start], "\n") + 1,
			NamesConstraint: integrityConstraintClause.MatchString(stmt),
			InDownBranch:    start >= downAt,
		})
	}
	return out
}

// integrityEnclosingFunction — имя функции, объявление которой ближе всего
// предшествует этому месту.
func integrityEnclosingFunction(before string) string {
	all := integrityCreateFunction.FindAllStringSubmatch(before, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1][1]
}

// LiveIntegrityRaiseSites оставляет из всех найденных те, что доживают до
// боевой базы: ветвь `Up`, и у функции — определение с наибольшей версией.
//
// Производитель вне функции живым считается всегда: замещать его нечем.
func LiveIntegrityRaiseSites(all []IntegrityRaiseSite) []IntegrityRaiseSite {
	newest := map[string]uint64{}
	for _, s := range all {
		if s.InDownBranch || s.Function == "" {
			continue
		}
		if v, ok := newest[s.Function]; !ok || s.Version > v {
			newest[s.Function] = s.Version
		}
	}
	var out []IntegrityRaiseSite
	for _, s := range all {
		if s.InDownBranch {
			continue
		}
		if s.Function == "" || newest[s.Function] == s.Version {
			out = append(out, s)
		}
	}
	return out
}
