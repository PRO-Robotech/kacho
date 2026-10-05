// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journalinitiatorfault.go — отказ журнала по инициатору решается ДО класса
// 23514 в каждом маппере ошибок модуля, ведущего журнал с инициатором
// (kacho#2918, условие ревью схемы S1A1-c3).
//
// # Предмет
//
// Ограничение формы инициатора `<таблица журнала>_initiator_form` — 23514, и
// маппер, решающий класс 23514, отнёс бы его к вводу вызывающего
// (`INVALID_ARGUMENT`), хотя значение производит помощник транзакции записи и
// вызывающему исправлять нечего. Опознание одно — `pkg/journalfault`; этот
// разбор судит, что каждый маппер его спрашивает и спрашивает РАНЬШЕ, чем
// решает класс 23514.
//
// # Что такое «маппер, решающий класс 23514»
//
// Объявление функции не-тестового файла модуля, в теле которого есть
//   - ветка `case`, перечисляющая `pgfault.Check`, либо
//   - вызов предиката класса `isCheckViolation` / `IsCheckViolation`
//     (сам предикат — определение класса, а не решение о нём, и предметом не
//     является).
//
// Охрана — вызов `journalfault.Report` либо `journalfault.Initiator` в том же
// теле, стоящий по позиции раньше первого решения о 23514.
//
// # Чего разбор не видит — названо
//
//  1. Решение о 23514 через `pgfault.Classify(err).Is(pgfault.Check)` прямо в
//     условии (без предиката и без ветки) — сегодня в модулях его нет; перепись
//     печатает число осмотренных функций, а не только находок.
//  2. Охрану, вынесенную в вызывающего: разбор судит тело, а не граф вызовов.
//     Маппер, который полагается на охрану вызывающего, краснеет — это
//     намеренно: второй вызывающий её бы не унаследовал.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
)

// JournalFaultMapper — функция, решающая класс 23514, и её охрана.
type JournalFaultMapper struct {
	File     string
	Func     string
	Line     int  // первое решение о 23514
	Guarded  bool // охрана стоит раньше первого решения
	GuardPos int  // строка охраны либо 0
}

// JournalFaultCensus — объём осмотренного.
type JournalFaultCensus struct {
	Funcs int // объявлений функций прочитано
}

// isCheckPredicateName — имя предиката класса 23514.
func isCheckPredicateName(name string) bool {
	return name == "isCheckViolation" || name == "IsCheckViolation"
}

// ScanJournalFaultMappers находит функции, решающие класс 23514, и судит охрану.
func ScanJournalFaultMappers(path string, src []byte) ([]JournalFaultMapper, JournalFaultCensus, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, JournalFaultCensus{}, err
	}
	var (
		out    []JournalFaultMapper
		census JournalFaultCensus
	)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		census.Funcs++
		if isCheckPredicateName(fn.Name.Name) {
			continue
		}
		decide, guard := token.NoPos, token.NoPos
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CaseClause:
				for _, e := range x.List {
					if isPkgSelector(e, "pgfault", "Check") && (decide == token.NoPos || x.Pos() < decide) {
						decide = x.Pos()
					}
				}
			case *ast.CallExpr:
				switch f := x.Fun.(type) {
				case *ast.Ident:
					if isCheckPredicateName(f.Name) && (decide == token.NoPos || x.Pos() < decide) {
						decide = x.Pos()
					}
				case *ast.SelectorExpr:
					if isCheckPredicateName(f.Sel.Name) && (decide == token.NoPos || x.Pos() < decide) {
						decide = x.Pos()
					}
					if (isPkgSelector(f, "journalfault", "Report") || isPkgSelector(f, "journalfault", "Initiator")) &&
						(guard == token.NoPos || x.Pos() < guard) {
						guard = x.Pos()
					}
				}
			}
			return true
		})
		if decide == token.NoPos {
			continue
		}
		m := JournalFaultMapper{File: path, Func: fn.Name.Name, Line: fset.Position(decide).Line}
		if guard != token.NoPos {
			m.GuardPos = fset.Position(guard).Line
			m.Guarded = guard < decide
		}
		out = append(out, m)
	}
	return out, census, nil
}

// isPkgSelector — выражение `pkg.Name`.
func isPkgSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// journalInitiatorConstraintRe — объявление колонки инициатора с ограничением
// формы в миграции журнала: таблица ALTER TABLE и имя ограничения.
var journalInitiatorConstraintRe = regexp.MustCompile(
	`(?s)ALTER TABLE\s+(?:[a-z_]+\.)?([a-z_]+)\s+ADD COLUMN\s+initiator\b.*?CONSTRAINT\s+([a-z_]+)`)

// JournalInitiatorConstraint — объявление ограничения формы в миграции.
type JournalInitiatorConstraint struct {
	Table, Constraint string
}

// ScanJournalInitiatorConstraints — объявления колонки инициатора с
// ограничением формы в тексте миграции; комментарии SQL вырезаются до разбора
// (шапки миграций журнала называют тот же оператор прозой).
func ScanJournalInitiatorConstraints(sql string) []JournalInitiatorConstraint {
	var out []JournalInitiatorConstraint
	for _, m := range journalInitiatorConstraintRe.FindAllStringSubmatch(stripSQLComments(sql), -1) {
		out = append(out, JournalInitiatorConstraint{Table: m[1], Constraint: m[2]})
	}
	return out
}
