// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// poolparamrefusal.go — разбор для гейта «предикат пулового ключа один на
// дерево». Отделён от гейта затем, чтобы инъекция подавала ему синтетику, не
// трогая дерево: проверка, доказанная только на дереве, не показывает, умеет ли
// она молчать.

// poolParamLiteral — подстрока, которой пять копий стража отличали пуловый
// ключ. Записана СТРОКОВОЙ КОНСТАНТОЙ: разбор судит узел вызова
// `strings.Contains`, поэтому ни это объявление, ни проза о нём под предикат не
// подпадают. Гейт, краснеющий на собственном объяснении, снимут как непонятный.
const poolParamLiteral = "pool_"

// poolParamPredicateHome — единственный законный носитель предиката.
const poolParamPredicateHome = "pkg/db/"

// PoolParamFinding — координата копии подстрочного предиката.
type PoolParamFinding struct {
	File string
	Line int
}

// PoolParamCensus — объём осмотренного. Без него «ноль находок» неотличимо от
// «ноль прочитанного».
type PoolParamCensus struct {
	// Files — файлов Go, разобранных успешно.
	Files int
	// Unparsed — файлов, которые разобрать не удалось; они НЕ судятся, и их
	// число названо отдельно, чтобы молчание по ним не читалось как чистота.
	Unparsed int
	// Skipped — файлов дома предиката, выведенных из-под суда намеренно. Тоже
	// названы отдельно: иначе «ноль находок» скрывало бы, что дом исключён, и
	// расширение дома прошло бы незамеченным.
	Skipped int
}

// FindPoolParamSubstringChecks находит места, где пуловый ключ ищется
// ПОДСТРОКОЙ вне своего дома.
//
// Судит исполняемую часть: вызов `strings.Contains(x, "pool_")`. Комментарий,
// объясняющий запрет, и строковая константа с той же подстрокой находкой не
// являются by construction.
func FindPoolParamSubstringChecks(sources map[string]string) ([]PoolParamFinding, PoolParamCensus) {
	var (
		findings []PoolParamFinding
		census   PoolParamCensus
	)
	fset := token.NewFileSet()
	for rel, src := range sources {
		if strings.HasPrefix(rel, poolParamPredicateHome) {
			census.Skipped++
			continue
		}
		file, err := parser.ParseFile(fset, rel, src, 0)
		if err != nil {
			census.Unparsed++
			continue
		}
		census.Files++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Contains" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "strings" {
				return true
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil || val != poolParamLiteral {
				return true
			}
			findings = append(findings, PoolParamFinding{
				File: rel,
				Line: fset.Position(call.Pos()).Line,
			})
			return true
		})
	}
	return findings, census
}
