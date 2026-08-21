// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientexpiryimmutable.go — разбор операторов правки, разложенных по столбцам
// (приёмка F2, §9.4, решение §2.10).
//
// # Предмет
//
// Срок клиента неизменяем после создания. На этой предпосылке стоит структурная
// гарантия: срок выданного токена не превышает остатка срока клиента, и
// проверять это на пути запроса не нужно ровно потому, что срок не двигается.
// Сдвинь его — и гарантия, заменившая рантаймовый контроль, держится ничем.
//
// Выразить это ограничением схемы нельзя без новой миграции, а применённые не
// правятся (ban #5). Приёмка называет допустимую замену прямо: ГЕЙТ ДЕРЕВА с
// переписью и инъекцией. Он слабее ограничения схемы — правка, обошедшая
// разбор, до базы доедет, — и это сказано, а не спрятано.
//
// # Что здесь считается ПРАВКОЙ СТОЛБЦА
//
//	UPDATE t SET expires_at = $2 WHERE id = $1   ← правка: столбец назван в SET
//	INSERT INTO t (…, expires_at) VALUES (…)     ← СОЗДАНИЕ: срок назначается
//	SELECT expires_at FROM t WHERE id = $1       ← чтение
//
// Разбор берёт список столбцов ИЗ SET, а не всякое упоминание имени: создание и
// чтение неизменяемости не нарушают, и гейт, считающий упоминания, запретил бы
// срок заводить и читать.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **оператор, собранный из частей** во время выполнения (склейка строк,
//     построитель запросов). Разбор читает строковые литералы; счётчик
//     литералов показывает, сколько их вообще было.
//  2. **правка через функцию базы или триггер**, а не оператором `UPDATE`.
//  3. **`UPDATE` без имени таблицы в том же литерале** — имя, приехавшее
//     подстановкой.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// SQLUpdate — оператор правки, найденный в литерале.
type SQLUpdate struct {
	File string
	Line int
	// Func — функция, в которой лежит литерал.
	Func string
	// Table — таблица без имени схемы.
	Table string
	// Columns — столбцы из SET, по возрастанию.
	Columns []string
}

// SQLUpdateCensus — объём осмотренного одним файлом.
type SQLUpdateCensus struct {
	// StringLiterals — строковых литералов осмотрено.
	StringLiterals int
	// SQLLiterals — из них выглядящих оператором SQL.
	SQLLiterals int
	// Updates — из них операторов правки.
	Updates int
	// UpdatesWithoutColumns — правок, у которых список столбцов не разобран.
	// Печатается отдельно: молчание при ненулевом счётчике сказано о меньшем.
	UpdatesWithoutColumns int
}

// sqlStatementHeads — начала операторов; по ним литерал признаётся SQL.
var sqlStatementHeads = []string{"select", "insert", "update", "delete", "with"}

// ScanSQLUpdates разбирает один файл и собирает операторы правки названных
// таблиц.
func ScanSQLUpdates(path string, src []byte, tables []string) ([]SQLUpdate, SQLUpdateCensus, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, SQLUpdateCensus{}, err
	}
	want := map[string]bool{}
	for _, t := range tables {
		want[strings.ToLower(t)] = true
	}

	var (
		out    []SQLUpdate
		census SQLUpdateCensus
	)
	for _, decl := range f.Decls {
		enclosing := "уровень пакета"
		if fn, ok := decl.(*ast.FuncDecl); ok {
			enclosing = functionQualifiedName(fn)
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			census.StringLiterals++
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			head := strings.ToLower(strings.TrimSpace(text))
			isSQL := false
			for _, h := range sqlStatementHeads {
				if strings.HasPrefix(head, h) {
					isSQL = true
					break
				}
			}
			if !isSQL {
				return true
			}
			census.SQLLiterals++
			for _, u := range parseSQLUpdates(text) {
				if !want[strings.ToLower(u.Table)] {
					continue
				}
				census.Updates++
				if len(u.Columns) == 0 {
					census.UpdatesWithoutColumns++
				}
				u.File = path
				u.Line = fset.Position(lit.Pos()).Line
				u.Func = enclosing
				out = append(out, u)
			}
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, census, nil
}

// parseSQLUpdates — операторы правки одного литерала.
func parseSQLUpdates(text string) []SQLUpdate {
	lower := strings.ToLower(text)
	var out []SQLUpdate
	for idx := 0; ; {
		rel := strings.Index(lower[idx:], "update ")
		if rel < 0 {
			break
		}
		at := idx + rel + len("update ")
		table := sqlIdentifierAt(text, at)
		idx = at
		if table == "" {
			continue
		}
		if i := strings.LastIndex(table, "."); i >= 0 {
			table = table[i+1:]
		}
		setAt := strings.Index(lower[at:], " set ")
		if setAt < 0 {
			out = append(out, SQLUpdate{Table: table})
			continue
		}
		start := at + setAt + len(" set ")
		end := len(text)
		for _, stop := range []string{" where ", " returning ", " from ", ";"} {
			if i := strings.Index(lower[start:], stop); i >= 0 && start+i < end {
				end = start + i
			}
		}
		out = append(out, SQLUpdate{Table: table, Columns: sqlSetColumns(text[start:end])})
		idx = start
	}
	return out
}

// sqlSetColumns — столбцы из списка присваиваний SET.
func sqlSetColumns(clause string) []string {
	var (
		out   []string
		depth int
		start int
	)
	flush := func(part string) {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		eq := strings.Index(part, "=")
		if eq < 0 {
			return
		}
		name := strings.TrimSpace(part[:eq])
		name = strings.Trim(name, "\"`")
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if name != "" {
			out = append(out, strings.ToLower(name))
		}
	}
	for i := 0; i < len(clause); i++ {
		switch clause[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				flush(clause[start:i])
				start = i + 1
			}
		}
	}
	flush(clause[start:])
	sort.Strings(out)
	return out
}

// SQLCreateTableBody — тело объявления таблицы: то, чем проверяется, что столбец
// у неё вообще есть.
//
// Возвращает пустую строку, если объявления нет: предпосылка гейта тогда не
// выполнена, и сказать об этом обязан он сам.
func SQLCreateTableBody(sqlText, table string) string {
	up := migrationUpSection(sqlText)
	lower := strings.ToLower(up)
	needle := strings.ToLower(table)
	for idx := 0; ; {
		rel := strings.Index(lower[idx:], "create table")
		if rel < 0 {
			return ""
		}
		at := idx + rel + len("create table")
		name := sqlIdentifierAt(up, at)
		idx = at
		short := name
		if i := strings.LastIndex(short, "."); i >= 0 {
			short = short[i+1:]
		}
		if !strings.EqualFold(short, needle) {
			continue
		}
		open := strings.Index(up[at:], "(")
		if open < 0 {
			return ""
		}
		open += at
		depth := 0
		for i := open; i < len(up); i++ {
			switch up[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return up[open+1 : i]
				}
			}
		}
		return ""
	}
}
