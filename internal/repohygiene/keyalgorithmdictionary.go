// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// keyalgorithmdictionary.go — разбор словаря зарегистрированного алгоритма в
// СХЕМЕ (приёмка F2, §9.4).
//
// # Предмет
//
// Словарь алгоритмов ключа объявлен дважды: ограничением схемы и закрытым
// перечнем кода. Совпадение двух объявлений держится ГЕЙТОМ, а не совпадением
// формулировок: разойдясь, они дают либо строку, которую схема примет, а
// проверяющий не признает, либо алгоритм, который проверяющий считает
// допустимым, а вставить его нельзя.
//
// # Пустое значение — ЗАКОННЫЙ вход, и означает оно «ключа нет»
//
// Ограничение допускает пустую строку, и это не расхождение с кодом: пустое
// значение означает «ключа нет», а не «любой алгоритм». Клиент с пустым
// зарегистрированным алгоритмом аутентифицироваться утверждением не может — и
// это свойство держится проверкой на непустоту, а не словарём. Разбор поэтому
// выносит пустое значение отдельно, а не считает его алгоритмом.
//
// # Что здесь считается ДЕЙСТВУЮЩИМ объявлением
//
// Миграции применяются по порядку и не правятся (ban #5), поэтому действует
// ПОСЛЕДНЕЕ объявление ограничения с этим именем, а снятое `DROP CONSTRAINT`
// не действует вовсе. Разбор читает файлы в порядке их номера — том же, в
// котором их применяет накат.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **ограничение, наложенное функцией или триггером**, а не выражением `IN`.
//     Форма другая, и предмет у неё другой.
//  2. **словарь, собранный из значений другой таблицы** (внешний ключ на
//     справочник): тогда объявления в тексте миграции нет вовсе, и сверять
//     нечего — счётчик найденных ограничений это покажет.
//  3. **значение, записанное иначе, чем строковым литералом** — приведение
//     типа, конкатенация.
package repohygiene

import (
	"sort"
	"strings"
)

// AlgorithmConstraint — объявление словаря алгоритма ограничением схемы.
type AlgorithmConstraint struct {
	File string
	Line int
	// Name — имя ограничения: им же оно снимается, и по нему разбор понимает,
	// какое объявление какое переобъявляет.
	Name string
	// Values — значения словаря в том виде, в каком они записаны, включая
	// пустое.
	Values []string
}

// AlgorithmDictionaryCensus — объём осмотренного.
type AlgorithmDictionaryCensus struct {
	// Files — файлов миграций прочитано.
	Files int
	// Statements — объявлений ограничения с этим столбцом найдено (включая
	// переобъявленные позднее).
	Statements int
	// Drops — снятий ограничения найдено.
	Drops int
}

// ScanKeyAlgorithmConstraints разбирает один файл миграции.
//
// column — имя столбца, чей словарь стережётся.
func ScanKeyAlgorithmConstraints(path, body, column string) (
	found []AlgorithmConstraint, dropped []string, census AlgorithmDictionaryCensus,
) {
	up := migrationUpSection(body)
	needle := column + " IN ("
	lower := strings.ToLower(up)
	lowerNeedle := strings.ToLower(needle)

	for idx := 0; ; {
		rel := strings.Index(lower[idx:], lowerNeedle)
		if rel < 0 {
			break
		}
		at := idx + rel
		open := at + len(needle) - 1
		values, end := sqlQuotedListAt(up, open)
		if end < 0 {
			idx = at + len(needle)
			continue
		}
		census.Statements++
		found = append(found, AlgorithmConstraint{
			File:   path,
			Line:   1 + strings.Count(up[:at], "\n"),
			Name:   sqlConstraintNameBefore(up, at),
			Values: values,
		})
		idx = end
	}

	for idx := 0; ; {
		rel := strings.Index(lower[idx:], "drop constraint")
		if rel < 0 {
			break
		}
		at := idx + rel + len("drop constraint")
		name := sqlIdentifierAt(up, at)
		if name != "" {
			census.Drops++
			dropped = append(dropped, name)
		}
		idx = at
	}
	return found, dropped, census
}

// sqlQuotedListAt читает список строковых литералов из скобки, открытой в
// позиции open. Возвращает значения и позицию сразу за закрывающей скобкой.
func sqlQuotedListAt(s string, open int) ([]string, int) {
	if open >= len(s) || s[open] != '(' {
		return nil, -1
	}
	var (
		values []string
		depth  int
	)
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return values, i + 1
			}
		case '\'':
			// Строковый литерал Postgres: удвоенная кавычка внутри означает саму
			// кавычку, а не конец литерала.
			var sb strings.Builder
			i++
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						sb.WriteByte('\'')
						i += 2
						continue
					}
					break
				}
				sb.WriteByte(s[i])
				i++
			}
			values = append(values, sb.String())
		}
	}
	return nil, -1
}

// sqlConstraintNameBefore — имя ближайшего слева объявления ограничения.
func sqlConstraintNameBefore(s string, at int) string {
	lower := strings.ToLower(s[:at])
	i := strings.LastIndex(lower, "constraint")
	if i < 0 {
		return ""
	}
	return sqlIdentifierAt(s, i+len("constraint"))
}

// sqlIdentifierAt — первый идентификатор начиная с позиции i.
func sqlIdentifierAt(s string, i int) string {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	start := i
	for i < len(s) && (s[i] == '_' || s[i] == '.' ||
		(s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') ||
		(s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	return s[start:i]
}

// SplitAlgorithmValues делит значения словаря на алгоритмы и пустое.
//
// Пустое выносится отдельно намеренно: оно означает «ключа нет», а не «любой
// алгоритм», и складывать его с алгоритмами значило бы объявить отсутствие
// ключа одним из них.
func SplitAlgorithmValues(values []string) (algorithms []string, hasEmpty bool) {
	seen := map[string]bool{}
	for _, v := range values {
		if v == "" {
			hasEmpty = true
			continue
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		algorithms = append(algorithms, v)
	}
	sort.Strings(algorithms)
	return algorithms, hasEmpty
}
