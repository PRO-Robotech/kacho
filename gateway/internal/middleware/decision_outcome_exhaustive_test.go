// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// decision_outcome_exhaustive_test.go — КАЖДЫЙ разбор исхода решения о правах
// разбирает ВСЕ исходы.
//
// # Зачем, и почему именно fail-open
//
// Исход решения — перечисление, и Go не требует полноты разбора. Три разбора,
// которые решают СУДЬБУ ЗАПРОСА (нативный унарный, нативный потоковый и HTTP),
// имеют случай по умолчанию, и он ВЫЗЫВАЕТ ОБРАБОТЧИК. То есть исход, о котором
// разбор не знает, означает «пропустить» — молча и без единой записи.
//
// Так и вышло с `outcomeUnserved`: он был разобран в отображении ответов и в
// разборе HTTP, а в двух нативных — нет. Сегодня недостижимо (фаза выходит на
// пустом запросе HTTP, а нативные его не заполняют), но расстояние до
// достижимости — одна строка, и она напрашивается: рядом на той же полосе уже
// стоит отказ по маршруту с тем же предметом, и свести их в одно место —
// естественное желание.
//
// # Что держит гейт
//
// Не «есть ли default» и не «сколько случаев», а РАЗНОСТЬ: множество констант
// исхода против множества разобранных случаев в каждом разборе. Разность
// непуста — находка, названная поимённо. Новая константа исхода краснит гейт в
// каждом месте, где её забыли, и называет их все сразу.
//
// # Почему разбором, а не поиском по тексту
//
// Имя константы встречается в комментариях этого же файла десяток раз.
// Упоминание за разобранный случай не считается.
package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// outcomeSwitch — один разбор исхода, найденный в исходниках пакета.
type outcomeSwitch struct {
	fn       string
	pos      token.Position
	cases    map[string]bool
	hasDeflt bool
}

// parsePackageOutcomes собирает константы исхода и все разборы по ним.
func parsePackageOutcomes(t *testing.T) (consts []string, switches []outcomeSwitch) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("разбор пакета: %v", err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			// 1. Константы типа decisionOutcome.
			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				typed := false
				for _, spec := range gd.Specs {
					vs, isVS := spec.(*ast.ValueSpec)
					if !isVS {
						continue
					}
					if id, isID := vs.Type.(*ast.Ident); isID && id.Name == "decisionOutcome" {
						typed = true
					}
					// Значения после первого наследуют тип через iota.
					if !typed {
						continue
					}
					for _, name := range vs.Names {
						if name.Name != "_" {
							consts = append(consts, name.Name)
						}
					}
				}
			}

			// 2. Разборы по `<что-то>.outcome`.
			var currentFn string
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.FuncDecl:
					currentFn = node.Name.Name
				case *ast.SwitchStmt:
					sel, ok := node.Tag.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "outcome" {
						return true
					}
					sw := outcomeSwitch{fn: currentFn, pos: fset.Position(node.Pos()), cases: map[string]bool{}}
					for _, stmt := range node.Body.List {
						cc, isCC := stmt.(*ast.CaseClause)
						if !isCC {
							continue
						}
						if len(cc.List) == 0 {
							sw.hasDeflt = true
							continue
						}
						for _, expr := range cc.List {
							if id, isID := expr.(*ast.Ident); isID {
								sw.cases[id.Name] = true
							}
						}
					}
					switches = append(switches, sw)
				}
				return true
			})
		}
	}
	sort.Strings(consts)
	return consts, switches
}

// TestEveryDecisionOutcomeSwitchIsExhaustive — разность множеств пуста в каждом
// разборе.
func TestEveryDecisionOutcomeSwitchIsExhaustive(t *testing.T) {
	consts, switches := parsePackageOutcomes(t)

	// Премиса в обе стороны: и констант, и разборов найдено не ноль. Пустой
	// обход даёт «ноль находок» так же убедительно, как полное соответствие.
	if len(consts) == 0 {
		t.Fatal("констант исхода не найдено ни одной — разбор сломан, и молчание гейта пусто")
	}
	if len(switches) == 0 {
		t.Fatal("разборов по исходу не найдено ни одного — предмета у гейта нет")
	}

	t.Logf("перепись: констант исхода %d (%s) · разборов по исходу %d",
		len(consts), strings.Join(consts, ", "), len(switches))

	for _, sw := range switches {
		var missing []string
		for _, c := range consts {
			if !sw.cases[c] {
				missing = append(missing, c)
			}
		}
		if len(missing) == 0 {
			continue
		}
		t.Errorf("разбор в %s (%s) не разбирает %d исход(ов): %s.\n"+
			"Случай по умолчанию есть: %v. Если он ПРОПУСКАЕТ запрос, каждый неразобранный "+
			"исход означает допуск — молча и без записи.",
			sw.fn, sw.pos, len(missing), strings.Join(missing, ", "), sw.hasDeflt)
	}
}
