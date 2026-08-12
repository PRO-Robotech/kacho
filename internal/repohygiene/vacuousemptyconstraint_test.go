// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// vacuousemptyconstraint_test.go — гейт против проверки, которая ПРИНИМАЕТ, потому
// что пуст её собственный источник ограничения.
//
// # Предмет
//
// Отвергающая проверка устроена одинаково: есть ПРЕДМЕТ (то, что принесли на вход
// и что можно отвергнуть) и есть ИСТОЧНИК ОГРАНИЧЕНИЯ (то, относительно чего
// предмет судится). Цикл по предмету спрашивает у источника и отвергает элемент,
// который источнику не соответствует.
//
// Ранний выход `if len(<источник>) == 0 { return nil }` превращает такую проверку в
// тождественно-истинную: источника нет — значит не отвергается НИЧЕГО, и при этом
// вызывающий получает успех. Форма проверки остаётся на месте, содержание уходит.
// Хуже всего, что состояние «источник пуст» обычно не редкость, а штатное: поле,
// из которого источник берётся, не обязательно, поэтому ограничение не действует
// не на краю, а на целом классе входов.
//
// Тот же ранний выход по ПРЕДМЕТУ — `if len(<предмет>) == 0 { return nil }` —
// законен и остаётся: отвергать нечего, потому что ничего не просили. Именно этим
// два случая и различаются, а не формой записи: она у них одна.
//
// # Что гейт считает каждой из двух ролей (это и есть его объём)
//
//   - ПРЕДМЕТ — выражение, по которому идёт отвергающий цикл (`for … range X`,
//     в теле которого есть возврат ненулевой ошибки);
//   - ИСТОЧНИК — выражение, до которого дотягивается решение этого цикла: оно либо
//     читается прямо в теле цикла, либо от него по присваиваниям происходит то, что
//     читается в теле.
//
// Роли различаются ЦЕПОЧКОЙ ПРОИСХОЖДЕНИЯ, а не формой записи — она у них одна.
// Охрана законна, когда охраняемое выражение стоит с предметом цикла в одной
// цепочке: это тот же самый предмет, только названный раньше (`if len(batch) == 0`
// перед циклом по разложенному из `batch`) или позже (`if len(targets) == 0`, где
// `targets` собран из `rules`, по которым цикл и идёт). В обоих случаях пусто
// именно то, что принесли, — отвергать нечего.
//
// Охрана на выражении, которого нет в цепочке предмета, но до которого решение
// цикла дотягивается, — находка: пуст ИСТОЧНИК, а предмет на месте. Охрана на
// постороннем выражении (ни то, ни другое: «работы не заказывали») гейта не
// касается: решение цикла от него не зависит.
//
// # Почему по синтаксическому дереву, а не по тексту
//
// Слово «skip» и слово «пустой» встречаются ровно в тех комментариях, которые эту
// самую защиту описывают, — текстовый поиск зеленел бы тем увереннее, чем лучше
// место задокументировано. Разбор идёт по дереву, комментарии в него не входят.
//
// # Область
//
// Функции, последний результат которых — `error`: у них «принять» и «отвергнуть»
// записаны одним общепринятым способом (`return nil` против возврата ненулевой
// ошибки), и роль раннего выхода читается без догадок. Проверки, возвращающие
// `bool`, сюда не входят намеренно — у них нет такого разделителя, и гейт вместо
// свойства ловил бы форму. Это ОБЪЁМ гейта, а не послабление: форма вне объёма им
// не покрыта, и здесь это сказано прямо.
//
// # Списка исключений нет
//
// Намеренно. Сработавшее место имеет ровно три исхода: (1) охрана стоит на
// источнике — снять её, пусть проверка отвергает; (2) охрана стоит на предмете, а
// гейт спутал роли — уточнить распознавание ролей ниже, а не заводить запись;
// (3) функция вообще не проверка — то же самое, сузить триггер.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// emptyConstraintScanRoots — где ищем прод-код.
var emptyConstraintScanRoots = []string{"services", "gateway", "pkg", "internal"}

// emptyConstraintHit — одна находка: координата, функция, охраняемое выражение и
// предметы отвергающих циклов этой функции (чтобы отчёт называл обе роли).
type emptyConstraintHit struct {
	line     int
	fn       string
	guarded  string
	subjects []string
}

// emptyConstraintScan — результат разбора одного файла плюс перепись осмотренного.
type emptyConstraintScan struct {
	errFuncs       int // функций, последний результат которых error
	rejectingLoops int // отвергающих циклов в них
	acceptGuards   int // ранних выходов формы if len(X)==0 { return … nil }
	lawfulGuards   int // из них — стоящих перед отвергающим циклом и признанных законными
	hits           []emptyConstraintHit
}

// TestCheckNeverAcceptsBecauseItsConstraintIsEmpty — ни одна отвергающая проверка
// не принимает вход только потому, что пуст её источник ограничения.
func TestCheckNeverAcceptsBecauseItsConstraintIsEmpty(t *testing.T) {
	root := repoRoot(t)

	var hits []string
	scannedFiles := 0
	total := emptyConstraintScan{}

	forEachProductionGoFileForEmptyConstraint(t, root, func(rel string, body []byte) {
		scannedFiles++
		res := analyzeEmptyConstraintAccept(t, rel, body)
		total.errFuncs += res.errFuncs
		total.rejectingLoops += res.rejectingLoops
		total.acceptGuards += res.acceptGuards
		total.lawfulGuards += res.lawfulGuards
		for _, h := range res.hits {
			hits = append(hits, rel+":"+strconv.Itoa(h.line)+" ("+h.fn+
				": охрана на `"+h.guarded+"`, предмет цикла — "+strings.Join(h.subjects, ", ")+")")
		}
	})

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного» И от «ноль
	// распознанного»: сломанный обход и сломанное распознавание ролей дают
	// одинаково зелёный гейт, если не утверждать объём осмотренного.
	if scannedFiles == 0 {
		t.Fatalf("гейт не прочитал ни одного файла в %v — предпосылка обхода сломана, "+
			"молчание ничего не доказывает", emptyConstraintScanRoots)
	}
	if total.errFuncs == 0 {
		t.Fatalf("гейт не нашёл ни одной функции с последним результатом error в %d прод-файлах — "+
			"распознавание области сломано, молчание ничего не доказывает", scannedFiles)
	}
	if total.rejectingLoops == 0 {
		t.Fatalf("гейт не нашёл ни одного отвергающего цикла в %d функциях — распознавание "+
			"ПРЕДМЕТА сломано, молчание ничего не доказывает", total.errFuncs)
	}
	if total.acceptGuards == 0 {
		t.Fatalf("гейт не распознал ни одного раннего выхода формы `if len(X) == 0 { return … nil }` "+
			"в %d прод-файлах — распознавание охраны сломано, а именно её гейт и судит; "+
			"молчание ничего не доказывает", scannedFiles)
	}
	// Предпосылка МОЛЧАНИЯ, а не обхода: весь гейт держится на различении двух
	// ролей одной и той же записи. Если в дереве не осталось ни одной охраны,
	// признанной ЗАКОННОЙ, то различитель на живых данных не исполняется вовсе —
	// и «зелено» перестаёт отличаться от «различитель всё пропускает». Синтетическая
	// инъекция ниже это тоже ловит, но только на своём входе; здесь — на дереве.
	if total.lawfulGuards == 0 {
		t.Fatalf("гейт не признал законной ни одной охраны перед отвергающим циклом (%d охран всего): "+
			"различитель ролей не исполнился ни разу на живом дереве, поэтому его молчание "+
			"ничего не доказывает", total.acceptGuards)
	}
	t.Logf("осмотрено прод-файлов: %d; функций с результатом error: %d; отвергающих циклов: %d; "+
		"ранних выходов формы `if len(X) == 0 { return … nil }`: %d, из них перед отвергающим "+
		"циклом и признанных законными (охрана на предмете): %d",
		scannedFiles, total.errFuncs, total.rejectingLoops, total.acceptGuards, total.lawfulGuards)

	if len(hits) > 0 {
		sort.Strings(hits)
		t.Errorf("найдено %d проверок, принимающих вход из-за пустоты СВОЕГО источника ограничения:\n  %s\n\n"+
			"Следствие: проверка отвечает «да» всегда, когда источник пуст, — а пустой он обычно не "+
			"на краю, а на целом классе входов, потому что поле, из которого он берётся, не обязательно. "+
			"Форма проверки на месте, содержания нет.\n\n"+
			"Исход — один из трёх, четвёртого нет: (1) снять охрану, пусть проверка отвергает и назовёт "+
			"причину; (2) охрана стоит на ПРЕДМЕТЕ, а гейт спутал роли — уточнить распознавание ролей "+
			"в analyzeEmptyConstraintAccept; (3) функция не проверка — сузить триггер там же. "+
			"Списка исключений у гейта нет намеренно.",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// ---- разбор ----

// analyzeEmptyConstraintAccept — разбор одного исходника.
func analyzeEmptyConstraintAccept(t *testing.T, rel string, body []byte) emptyConstraintScan {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: разбор не удался: %v — файл не может быть ни засчитан в перепись, "+
			"ни молча пропущен", rel, err)
	}

	out := emptyConstraintScan{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		// Тело функции и тела её замыканий разбираются по отдельности: у каждого
		// свои роли, и смешивать их значит судить о цикле по чужой охране.
		for _, b := range bodiesOf(fd) {
			if !lastResultIsError(b.results) {
				continue
			}
			out.errFuncs++
			res := analyzeEmptyConstraintBody(fset, b.name, b.block)
			out.rejectingLoops += res.rejectingLoops
			out.acceptGuards += res.acceptGuards
			out.lawfulGuards += res.lawfulGuards
			out.hits = append(out.hits, res.hits...)
		}
	}
	return out
}

type namedBody struct {
	name    string
	block   *ast.BlockStmt
	results *ast.FieldList
}

// bodiesOf — тело функции плюс тела её литералов-замыканий (у worker-fn мутаций
// вся отвергающая логика живёт именно в литерале).
func bodiesOf(fd *ast.FuncDecl) []namedBody {
	out := []namedBody{{name: fd.Name.Name, block: fd.Body, results: fd.Type.Results}}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		fl, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		out = append(out, namedBody{name: fd.Name.Name + ".func", block: fl.Body, results: fl.Type.Results})
		return true
	})
	return out
}

func lastResultIsError(res *ast.FieldList) bool {
	if res == nil || len(res.List) == 0 {
		return false
	}
	last := res.List[len(res.List)-1]
	id, ok := last.Type.(*ast.Ident)
	return ok && id.Name == "error"
}

// analyzeEmptyConstraintBody — роли и находки в пределах ОДНОГО тела.
func analyzeEmptyConstraintBody(fset *token.FileSet, fnName string, block *ast.BlockStmt) emptyConstraintScan {
	out := emptyConstraintScan{}

	// 1. Отвергающие циклы: `for … range X`, в теле которого есть возврат
	//    ненулевой ошибки. Именно X и есть ПРЕДМЕТ.
	type loopInfo struct {
		pos     token.Pos
		subject string
	}
	var loops []loopInfo
	// consultedSeed — что читает решение отвергающего цикла.
	consulted := map[string]bool{}

	walkOwnBody(block, func(n ast.Node) {
		rs, ok := n.(*ast.RangeStmt)
		if !ok || rs.X == nil || rs.Body == nil || !loopRejects(rs.Body) {
			return
		}
		subj := types.ExprString(rs.X)
		loops = append(loops, loopInfo{pos: rs.Pos(), subject: subj})
		out.rejectingLoops++
		for _, name := range readNames(rs.Body, loopVars(rs)) {
			consulted[name] = true
		}
	})
	if len(loops) == 0 {
		return out
	}

	// 2. До чего решение цикла дотягивается по присваиваниям — до неподвижной
	//    точки. Без этого шага источник, собранный в отдельную переменную
	//    (`supers := …` из `supernet`), выглядел бы посторонним.
	expandConsulted(block, consulted)

	// Цепочка происхождения предмета — в ОБЕ стороны: что в него втекает и что
	// из него вытекает. Охраняемое выражение, стоящее в этой цепочке, — тот же
	// предмет под другим именем, и охрана на нём законна.
	subjectChain := map[string]bool{}
	for _, l := range loops {
		for name := range closureOf(block, l.subject) {
			subjectChain[name] = true
		}
	}

	// 3. Ранние выходы формы `if len(X) == 0 { return … nil }`.
	walkOwnBody(block, func(n ast.Node) {
		is, ok := n.(*ast.IfStmt)
		if !ok || is.Else != nil || is.Init != nil {
			return
		}
		guarded, ok := emptyLenOperand(is.Cond)
		if !ok || !blockOnlyAcceptsNil(is.Body) {
			return
		}
		out.acceptGuards++

		// Предметы циклов, стоящих ПОСЛЕ охраны: охрана раньше них и решает,
		// доживёт ли до них исполнение.
		var subjects []string
		lawful := false
		for _, l := range loops {
			if l.pos <= is.Pos() {
				continue
			}
			subjects = append(subjects, "`"+l.subject+"`")
			if l.subject == guarded || subjectChain[guarded] || closureOf(block, guarded)[l.subject] {
				lawful = true
			}
		}
		// Три роли, и порядок вопросов повторяет их: (1) охрана вообще не перед
		// отвергающим циклом — не предмет гейта; (2) охрана на предмете — законна,
		// и именно на ней различитель ролей отрабатывает; (3) охрана на выражении,
		// от которого решение цикла не зависит, — «работы не заказывали».
		if len(subjects) == 0 {
			return
		}
		if lawful {
			out.lawfulGuards++
			return
		}
		if !consulted[guarded] {
			return
		}
		out.hits = append(out.hits, emptyConstraintHit{
			line:     fset.Position(is.Pos()).Line,
			fn:       fnName,
			guarded:  guarded,
			subjects: subjects,
		})
	})
	return out
}

// closureOf — всё, что втекает в выражение seed по присваиваниям тела, включая сам
// seed. Тот же обход, что у expandConsulted: одна механика на оба вопроса, чтобы
// «источник» и «цепочка предмета» не разъехались молча.
func closureOf(block *ast.BlockStmt, seed string) map[string]bool {
	set := map[string]bool{seed: true}
	expandConsulted(block, set)
	return set
}

// walkOwnBody обходит тело, НЕ спускаясь в литералы-замыкания: у них своя область
// и свои роли, они разбираются отдельно.
func walkOwnBody(block *ast.BlockStmt, visit func(ast.Node)) {
	ast.Inspect(block, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		visit(n)
		return true
	})
}

// loopRejects — в теле цикла есть возврат ненулевой ошибки (последний результат
// не `nil`). Это и делает цикл отвергающим, а не просто перебирающим.
func loopRejects(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		rs, ok := n.(*ast.ReturnStmt)
		if !ok || len(rs.Results) == 0 {
			return true
		}
		last := rs.Results[len(rs.Results)-1]
		if id, ok := last.(*ast.Ident); ok && id.Name == "nil" {
			return true
		}
		found = true
		return false
	})
	return found
}

// loopVars — имена, введённые самим циклом: они не «читаются извне».
func loopVars(rs *ast.RangeStmt) map[string]bool {
	out := map[string]bool{}
	for _, e := range []ast.Expr{rs.Key, rs.Value} {
		if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
			out[id.Name] = true
		}
	}
	return out
}

// readNames — имена и селекторы, читаемые в узле (кроме локальных имён цикла).
func readNames(n ast.Node, skip map[string]bool) []string {
	var out []string
	ast.Inspect(n, func(x ast.Node) bool {
		switch e := x.(type) {
		case *ast.FuncLit:
			return false
		case *ast.SelectorExpr:
			s := types.ExprString(e)
			if !skip[rootIdent(e)] {
				out = append(out, s)
			}
			return true
		case *ast.Ident:
			if !skip[e.Name] {
				out = append(out, e.Name)
			}
		}
		return true
	})
	return out
}

func rootIdent(e ast.Expr) string {
	for {
		switch v := e.(type) {
		case *ast.SelectorExpr:
			e = v.X
		case *ast.IndexExpr:
			e = v.X
		case *ast.CallExpr:
			e = v.Fun
		case *ast.Ident:
			return v.Name
		default:
			return ""
		}
	}
}

// expandConsulted — до неподвижной точки: если присваивание пишет в то, что
// читает отвергающий цикл, то читаемое этим присваиванием тоже входит в решение.
func expandConsulted(block *ast.BlockStmt, consulted map[string]bool) {
	for i := 0; i < 8; i++ {
		grew := false
		add := func(names []string) {
			for _, n := range names {
				if !consulted[n] {
					consulted[n] = true
					grew = true
				}
			}
		}
		walkOwnBody(block, func(n ast.Node) {
			switch s := n.(type) {
			case *ast.AssignStmt:
				if !writesInto(s.Lhs, consulted) {
					return
				}
				for _, r := range s.Rhs {
					add(readNames(r, nil))
				}
			case *ast.ValueSpec:
				writes := false
				for _, name := range s.Names {
					if consulted[name.Name] {
						writes = true
					}
				}
				if !writes {
					return
				}
				for _, r := range s.Values {
					add(readNames(r, nil))
				}
			case *ast.RangeStmt:
				// `for _, x := range src { dst = append(dst, …) }` — источник src
				// доезжает до dst, а значит и до решения цикла, читающего dst.
				if s.Body == nil || !bodyWritesInto(s.Body, consulted) {
					return
				}
				add(readNames(s.X, nil))
			}
		})
		if !grew {
			return
		}
	}
}

func writesInto(lhs []ast.Expr, consulted map[string]bool) bool {
	for _, l := range lhs {
		if consulted[types.ExprString(l)] || consulted[rootIdent(l)] {
			return true
		}
	}
	return false
}

func bodyWritesInto(body *ast.BlockStmt, consulted map[string]bool) bool {
	found := false
	walkOwnBody(body, func(n ast.Node) {
		if found {
			return
		}
		if as, ok := n.(*ast.AssignStmt); ok && writesInto(as.Lhs, consulted) {
			found = true
		}
	})
	return found
}

// emptyLenOperand — `len(X) == 0` / `0 == len(X)` → рендер X.
func emptyLenOperand(cond ast.Expr) (string, bool) {
	be, ok := cond.(*ast.BinaryExpr)
	if !ok || be.Op != token.EQL {
		return "", false
	}
	for _, pair := range [][2]ast.Expr{{be.X, be.Y}, {be.Y, be.X}} {
		call, ok := pair[0].(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			continue
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "len" {
			continue
		}
		lit, ok := pair[1].(*ast.BasicLit)
		if !ok || lit.Kind != token.INT || lit.Value != "0" {
			continue
		}
		return types.ExprString(call.Args[0]), true
	}
	return "", false
}

// blockOnlyAcceptsNil — тело охраны состоит ровно из возврата, последний результат
// которого `nil`: «принято, ошибки нет».
func blockOnlyAcceptsNil(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) != 1 {
		return false
	}
	rs, ok := b.List[0].(*ast.ReturnStmt)
	if !ok || len(rs.Results) == 0 {
		return false
	}
	last := rs.Results[len(rs.Results)-1]
	id, ok := last.(*ast.Ident)
	return ok && id.Name == "nil"
}

// ---- обход ----

func forEachProductionGoFileForEmptyConstraint(t *testing.T, root string, fn func(rel string, body []byte)) {
	t.Helper()
	for _, sub := range emptyConstraintScanRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			t.Fatalf("каталог %s не найден (%v) — область обхода гейта сломана", sub, err)
		}
		err := rootedWalk(base, func(rel string) bool {
			if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
				return false
			}
			switch {
			case strings.HasPrefix(rel, "api/") && sub == "pkg": // сгенерённые стабы
				return false
			case strings.Contains(rel, "/testdata/"), strings.HasPrefix(rel, "testdata/"):
				return false
			case strings.Contains(rel, "mock"):
				return false
			}
			return true
		}, func(abs string, body []byte) error {
			rel, relErr := filepath.Rel(root, abs)
			if relErr != nil {
				return relErr
			}
			fn(filepath.ToSlash(rel), body)
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", sub, err)
		}
	}
}

// ---- инъекция в обе стороны ----
//
// Гейт без этой пары ловит форму, а не существо: молчание на законной конструкции
// той же формы надо доказать так же, как срабатывание на дефекте. Обе стороны взяты
// НЕ выдуманными — это два места одного дерева, различающиеся ровно ролью
// охраняемого выражения. Источники синтетические, поэтому исход не зависит от
// состояния дерева и пара остаётся доказательной после того, как дерево починено.

// TestEmptyConstraintGateRedOnInjectedDefect — возвращённый дефект краснит гейт И
// называет координату.
//
// Вход — обе половины ловушки, снятые из vpc: охрана по объявленному набору
// (первая, снята коммитом отказа) и охрана по разобранному набору (вторая, снята
// здесь). Форма записи у них та же, что у законной охраны ниже; различает их
// только роль.
func TestEmptyConstraintGateRedOnInjectedDefect(t *testing.T) {
	const src = `package x

func eachWithinSupernet(supernet, blocks []string, family, field string) error {
	if len(supernet) == 0 {
		return nil
	}
	supers := make([]prefixT, 0, len(supernet))
	for _, s := range supernet {
		p, perr := parse(s)
		if perr != nil {
			continue
		}
		supers = append(supers, p)
	}
	if len(supers) == 0 {
		return nil
	}
	for _, b := range blocks {
		inner, perr := parse(b)
		if perr != nil {
			continue
		}
		if !withinAny(inner, supers) {
			return errorf("subnet CIDR %s is not within any network CIDR block", b)
		}
	}
	return nil
}
`
	got := analyzeEmptyConstraintAccept(t, "injected.go", []byte(src))

	if got.rejectingLoops != 1 {
		t.Fatalf("отвергающий цикл не распознан: rejectingLoops=%d", got.rejectingLoops)
	}
	if got.acceptGuards != 2 {
		t.Fatalf("охраны не распознаны: acceptGuards=%d, ожидалось 2", got.acceptGuards)
	}
	if len(got.hits) != 2 {
		t.Fatalf("гейт обязан найти ОБЕ половины ловушки, найдено %d: %+v", len(got.hits), got.hits)
	}
	lines := map[int]string{}
	for _, h := range got.hits {
		lines[h.line] = h.guarded
		if h.fn != "eachWithinSupernet" {
			t.Errorf("гейт обязан назвать функцию, получено %q", h.fn)
		}
	}
	if lines[4] != "supernet" {
		t.Errorf("координата первой половины не названа: ожидалась строка 4 с охраной на `supernet`, "+
			"получено %v", lines)
	}
	if lines[15] != "supers" {
		t.Errorf("координата второй половины не названа: ожидалась строка 15 с охраной на `supers`, "+
			"получено %v", lines)
	}
}

// TestEmptyConstraintGateSilentOnLawfulSameShape — законная конструкция ТОЙ ЖЕ
// формы гейта не задевает.
//
// Три способа, которыми охрана оказывается на ПРЕДМЕТЕ, и все три сняты с живого
// дерева, а не выдуманы:
//
//   - охрана прямо на том, по чему идёт цикл (`blocks` — второй ранний выход
//     того же vpc-хелпера, он остался и обязан остаться);
//   - охрана на том, ИЗ ЧЕГО предмет собран (батч событий разложен по типам,
//     цикл идёт по разложенному);
//   - охрана на том, ЧТО собрано из предмета (цели вынуты из правил, цикл идёт по
//     правилам).
//
// Без этой стороны гейт запрещал бы ранний выход как таковой — и был бы снят
// первым же ложным срабатыванием.
func TestEmptyConstraintGateSilentOnLawfulSameShape(t *testing.T) {
	const src = `package x

func guardOnSubjectItself(supernet, blocks []string) error {
	if len(blocks) == 0 {
		return nil
	}
	for _, b := range blocks {
		if !withinAny(b, supernet) {
			return errorf("subnet CIDR %s is not within any network CIDR block", b)
		}
	}
	return nil
}

func guardOnWhatTheSubjectIsBuiltFrom(batch []eventT) error {
	if len(batch) == 0 {
		return nil
	}
	byKind := make(map[string][]string, len(batch))
	for _, ev := range batch {
		byKind[ev.Kind] = append(byKind[ev.Kind], ev.ID)
	}
	for kind, ids := range byKind {
		if err := ask(kind, ids); err != nil {
			return err
		}
	}
	return nil
}

func guardOnWhatIsBuiltFromTheSubject(rules []ruleT, owner string) error {
	targets := make([]string, 0, len(rules))
	for _, r := range rules {
		if r.TargetID != "" {
			targets = append(targets, r.TargetID)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	found, err := getMany(targets)
	if err != nil {
		return err
	}
	for i, r := range rules {
		if r.TargetID == "" {
			continue
		}
		if found[r.TargetID].Owner != owner {
			return invalidArg(i, "cross-network")
		}
	}
	return nil
}
`
	got := analyzeEmptyConstraintAccept(t, "lawful.go", []byte(src))

	// Предпосылка молчания: все три формы обязаны быть РАЗОБРАНЫ, а не пропущены
	// мимо распознавания — иначе «гейт молчит» означало бы «гейт не смотрел».
	if got.rejectingLoops != 3 {
		t.Fatalf("отвергающие циклы не распознаны: rejectingLoops=%d, ожидалось 3 — "+
			"молчание на неразобранном входе ничего не доказывает", got.rejectingLoops)
	}
	if got.acceptGuards != 3 {
		t.Fatalf("охраны не распознаны: acceptGuards=%d, ожидалось 3", got.acceptGuards)
	}
	if got.lawfulGuards != 3 {
		t.Fatalf("законными признаны не все охраны: lawfulGuards=%d, ожидалось 3 — "+
			"различитель ролей не отработал на всех трёх формах", got.lawfulGuards)
	}
	if len(got.hits) != 0 {
		t.Fatalf("гейт сработал на законной конструкции той же формы: %+v — он ловит форму, "+
			"а не существо, и будет снят первым же ложным срабатыванием", got.hits)
	}
}

// TestEmptyConstraintGateIgnoresUnrelatedGuard — третья роль: охрана на выражении,
// от которого решение цикла НЕ зависит («работы не заказывали»).
//
// Такая охрана вне предмета гейта, и это записано, чтобы следующий читатель не
// расширил триггер до неё: пустой список заданий законно означает «делать нечего»,
// и требовать от него отказа было бы запретом про то, чего не спрашивали.
func TestEmptyConstraintGateIgnoresUnrelatedGuard(t *testing.T) {
	const src = `package x

func applyAll(updates []updateT, targets []targetT) error {
	if len(updates) == 0 {
		return nil
	}
	for _, tg := range targets {
		if !tg.OK {
			return errorf("target %s is not ready", tg.ID)
		}
	}
	return nil
}
`
	got := analyzeEmptyConstraintAccept(t, "unrelated.go", []byte(src))

	if got.rejectingLoops != 1 || got.acceptGuards != 1 {
		t.Fatalf("вход не разобран: rejectingLoops=%d acceptGuards=%d", got.rejectingLoops, got.acceptGuards)
	}
	if len(got.hits) != 0 {
		t.Fatalf("гейт сработал на охране, от которой решение цикла не зависит: %+v", got.hits)
	}
}
