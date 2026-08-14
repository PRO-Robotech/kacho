// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// diagnosticcollector_test.go — два свойства КАЖДОГО коллектора диагностической
// поверхности, оба проверяются по дереву.
//
// # 1. Словарь меток закрыт
//
// Значение метки, взятое из данных запроса, превращает счётчик в перечень
// арендаторов: число серий растёт с числом обслуженных, память процесса растёт
// вместе с ним, а на проводе оказывается то, чему там быть не положено
// (`security.md` §«Инфра-чувствительные данные»). Потолок кардинальности и этот
// запрет — один запрет с двух сторон.
//
// # 2. Коллектор не ходит наружу в момент сбора
//
// Диагностика, которая дозванивается до соседа за своими числами, гаснет ровно
// тогда, когда нужна: сосед недоступен ⇒ сбор виснет ⇒ величин нет ни у одного
// семейства, включая те, что лежат в самом процессе. Величины обязаны читаться
// ИЗ ПРОЦЕССА.
//
// # Почему гейт по дереву, а не проба каждого коллектора
//
// Оба свойства теряются ТИХО: метка из чужих данных собирается и отдаётся, вызов
// наружу компилируется. Ни одна проба самого коллектора при этом не краснеет —
// она читает то, что коллектор отдал, а не то, откуда он это взял. Свойство
// принадлежит дереву, и держать его может только обход дерева. Пятый коллектор
// заведут копированием четвёртого.

// collectorMethod — одна реализация сбора.
type collectorMethod struct {
	where string
	recv  string
	// recvVar — ИМЯ переменной получателя (`c` в `func (c *authzCollector)`).
	//
	// Нужно ради одного различия, без которого гейт краснеет на исправном коде:
	// `c.read()` и `http.Get()` синтаксически одинаковы — точка между двумя
	// именами. Первое — чтение СВОЕГО поля, принесённого при сборке; наружу оно
	// не ходит по построению.
	recvVar string
	fn      *ast.FuncDecl
	pkg     *ast.File
}

// collectDiagnosticCollectors переписывает не-тестовое дерево и возвращает все
// методы `Collect(ch chan<- prometheus.Metric)`.
func collectDiagnosticCollectors(t *testing.T) (methods []collectorMethod, scanned int) {
	t.Helper()
	root := repoRoot(t)
	walkOwnerRegisterGoFiles(t, root, []string{"services", "gateway", "pkg"}, func(rel string, body []byte) {
		scanned++
		if !strings.Contains(string(body), "chan<- prometheus.Metric") {
			return
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Collect" || fn.Recv == nil || fn.Body == nil {
				continue
			}
			methods = append(methods, collectorMethod{
				where:   rel + ":" + fmtLine(fset, fn.Pos()),
				recv:    recvTypeName(fn),
				recvVar: recvVarName(fn),
				fn:      fn,
				pkg:     file,
			})
		}
	})
	return methods, scanned
}

// recvTypeName — имя типа получателя, для текста находки.
func recvTypeName(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return "?"
	}
	switch tp := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := tp.X.(*ast.Ident); ok {
			return "*" + id.Name
		}
	case *ast.Ident:
		return tp.Name
	}
	return "?"
}

// recvVarName — имя переменной получателя; пусто у безымянного получателя.
func recvVarName(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

// packageConsts — имена констант, объявленных в файле.
//
// Файла, а не пакета: разбор идёт пофайлово, и коллектор со своим закрытым
// словарём держит его рядом с собой — это и есть та форма, которую гейт
// поощряет.
func packageConsts(file *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				out[name.Name] = true
			}
		}
	}
	return out
}

// closedVocabularyNames — имена, связанные обходом ЛИТЕРАЛЬНОГО набора внутри
// тела: `for outcome := range map[string]uint64{Const1: …, Const2: …}`.
//
// Такой обход — тот же закрытый словарь, только записанный таблицей; требовать
// от него разворота в перечень вызовов значило бы требовать формы, а не
// свойства.
func closedVocabularyNames(body *ast.BlockStmt, consts map[string]bool) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		rng, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		lit, ok := rng.X.(*ast.CompositeLit)
		if !ok {
			return true
		}
		// Все ключи набора обязаны быть закрытыми: литерал либо константа.
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			switch key := kv.Key.(type) {
			case *ast.BasicLit:
				if key.Kind != token.STRING {
					return true
				}
			case *ast.Ident:
				if !consts[key.Name] {
					return true
				}
			default:
				return true
			}
		}
		// Именем цикла может быть только ключ (первая переменная).
		if id, ok := rng.Key.(*ast.Ident); ok {
			out[id.Name] = true
		}
		// Переменные набора элементов (значения) закрытыми НЕ считаются: там
		// лежат числа, и меткой они быть не должны.
		return true
	})
	return out
}

// TestDiagnosticCollectorLabelsAreAClosedVocabulary — значения меток берутся из
// закрытого словаря, а не из данных.
func TestDiagnosticCollectorLabelsAreAClosedVocabulary(t *testing.T) {
	methods, scanned := collectDiagnosticCollectors(t)
	t.Logf("осмотрено не-тестовых файлов Go: %d; коллекторов диагностической поверхности: %d",
		scanned, len(methods))
	if scanned == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if len(methods) == 0 {
		t.Fatal("в дереве нет ни одного метода Collect(ch chan<- prometheus.Metric) — предмет " +
			"гейта отпал: снимите его вместе с коллекторами либо почините форму, которой он их ищет")
	}

	var findings []string
	for _, m := range methods {
		consts := packageConsts(m.pkg)
		closed := closedVocabularyNames(m.fn.Body, consts)
		ast.Inspect(m.fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "prometheus" {
				return true
			}
			firstLabelArg, isConstMetric := firstLabelArgIndex(sel.Sel.Name)
			if !isConstMetric {
				return true
			}
			for i := firstLabelArg; i < len(call.Args); i++ {
				if closedLabelArg(call.Args[i], consts, closed) {
					continue
				}
				findings = append(findings, m.where+" ("+m.recv+
					") — значение метки не из закрытого словаря: годятся строковый литерал, "+
					"константа файла либо переменная обхода литерального набора с такими ключами. "+
					"Метка из данных превращает счётчик в перечень арендаторов: серий становится "+
					"столько же, сколько обслужено, и на проводе оказывается то, чему там не место")
			}
			return true
		})
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("метка диагностической поверхности берётся из данных:\n  %s",
			strings.Join(findings, "\n  "))
	}
}

// firstLabelArgIndex — с какого аргумента у конструктора метрики начинаются
// ЗНАЧЕНИЯ МЕТОК, и конструктор ли это вообще.
//
// Индекс зависит от вида: у счётчика перед метками стоят дескриптор, вид и
// значение; у гистограммы и сводки — дескриптор, число, сумма и набор корзин
// либо квантилей. Единый индекс читал бы набор корзин за метку и краснел бы на
// исправной гистограмме — то есть был бы снят следующим как ложный.
func firstLabelArgIndex(name string) (int, bool) {
	switch name {
	case "MustNewConstMetric", "NewConstMetric":
		return 3, true
	case "MustNewConstHistogram", "NewConstHistogram",
		"MustNewConstSummary", "NewConstSummary":
		return 4, true
	}
	return 0, false
}

// closedLabelArg — принадлежит ли аргумент закрытому словарю.
//
// Форм ровно три, и `x.y` среди них НЕТ намеренно: поле структуры синтаксически
// неотличимо от `запрос.Арендатор`, и признать его закрытым значило бы завести
// проверку, пропускающую ровно то, ради чего она написана.
func closedLabelArg(arg ast.Expr, consts, closed map[string]bool) bool {
	switch a := arg.(type) {
	case *ast.BasicLit:
		return a.Kind == token.STRING
	case *ast.Ident:
		return consts[a.Name] || closed[a.Name]
	}
	return false
}

// TestDiagnosticCollectorsDoNotDialOut — в момент сбора коллектор не зовёт
// ничего, кроме сборки метрик.
func TestDiagnosticCollectorsDoNotDialOut(t *testing.T) {
	// Пакеты, которые коллектору законно звать в момент сбора. Список УЗКИЙ
	// намеренно: всё, что не собирает метрику из уже прочитанной величины, —
	// повод объясниться, а не расширить перечень.
	allowed := map[string]bool{"prometheus": true, "float64": true}

	methods, scanned := collectDiagnosticCollectors(t)
	t.Logf("осмотрено не-тестовых файлов Go: %d; коллекторов диагностической поверхности: %d",
		scanned, len(methods))
	if len(methods) == 0 {
		t.Fatal("в дереве нет ни одного метода Collect(ch chan<- prometheus.Metric) — предмет " +
			"гейта отпал")
	}

	var findings []string
	for _, m := range methods {
		ast.Inspect(m.fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			// `c.read()` — чтение СВОЕГО поля: значение принесено при сборке
			// коллектора, наружу оно не ходит по построению. Синтаксически это
			// неотличимо от `http.Get()`, поэтому различает имя получателя.
			if m.recvVar != "" && pkgIdent.Name == m.recvVar {
				return true
			}
			if allowed[pkgIdent.Name] {
				return true
			}
			// Локальная переменная того же имени, что пакет, здесь неотличима от
			// пакета без разбора типов; такой вызов всё равно объясняется в
			// находке, а не пропускается молча.
			findings = append(findings, m.where+" ("+m.recv+") — в момент сбора зовётся "+
				pkgIdent.Name+"."+sel.Sel.Name+"(). Диагностика, которая ходит наружу за своими "+
				"числами, гаснет ровно тогда, когда нужна: величины обязаны читаться из процесса, "+
				"а всё, что требует похода к соседу, — накапливаться на его пути")
			return true
		})
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("коллектор диагностической поверхности ходит наружу:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
