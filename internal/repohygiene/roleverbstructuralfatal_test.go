// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// roleverbstructuralfatal_test.go — IAM-RV-1-08, ЗАГРУЗОЧНАЯ ПОЛОВИНА:
// СТРУКТУРНАЯ полоса пересчёта проекции роли РОНЯЕТ старт.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО ГЕЙТ ДЕРЕВА, А НЕ ПРОБА — и почему довод шире, чем его применили
//
// У сценария две половины, как и у IAM-RV-1-07. Первая — что досев ВОЗВРАЩАЕТ
// перепись, по которой полосы различимы, — держится интеграционной пробой рядом
// с кодом досева (`TestIAMRV108_StructuralFailureNamesBothQuantities`). Вторая —
// что структурная полоса действительно РОНЯЕТ старт — живёт в композиционном
// корне, то есть в `package main`, и ни одна проба Go до неё не дотягивается by
// construction.
//
// Довод здесь ровно тот же, каким обоснован соседний гейт полосы старта, но
// применён он был только к УРОВНЮ записи в журнал и не применён к ПАДЕНИЮ.
// Замер (снятие блока из корня, сборка, прогон всех гейтов полосы): сборка
// проходит, все четыре гейта зелены. То есть решение «повтор даст то же самое,
// старт продолжать нельзя» снималось шестью строками, и не краснело НИЧТО.
//
// Отдельно названо, чего НЕ покрывает соседняя интеграционная проба: её помощник
// возвращает ошибку самого досева и предиката `Structural` не читает вовсе —
// поэтому она утверждает о ТЕКСТЕ переписи, а не о том, что кто-то по этому
// тексту останавливает старт. Предикат имел ОДНОГО вызывающего в дереве и НОЛЬ
// в пробах и гейтах.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО ТРЕБУЕТСЯ
//
//  1. перепись досева ПРИВЯЗАНА к имени в корне — иначе судить не о чем;
//  2. после вызова досева в том же блоке стоит ветка, чьё УСЛОВИЕ спрашивает
//     структурную полосу у ЭТОЙ переписи;
//  3. тело этой ветки ПРЕРЫВАЕТ старт — возвращает ненулевую ошибку либо
//     завершает процесс. Ветка, которая только пишет в журнал, структурную полосу
//     не исполняет: «повтори позже» на ней есть ложь, а служба продолжает
//     работать с неизвестным состоянием вердикта.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА
//
// Структурная полоса опознаётся по ВЫЗОВУ метода `Structural` НА ИМЕНИ, которому
// присвоена перепись досева. Привязка к имени существенна: она отделяет эту
// полосу от одноимённого метода в других предметах дерева и от соседней,
// ТРАНЗИЕНТНОЙ ветки (`if verr != nil`), которая ронять старт НЕ обязана и не
// должна.
//
// Предикат, записанный иначе — раскрытый в условие (`Examined > 0 && Reseeded == 0`)
// или переименованный, — гейт не найдёт. Это НЕ тихая слепая зона: ноль
// найденных структурных веток есть НАХОДКА (п. 2), а не молчание. Гейт судит узлы
// разобранного дерева, а не текст: слово `Structural` встречается в комментариях
// этого же корня, и проверка по подстроке краснела бы на собственном объяснении.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// structuralPredicateName — метод переписи, спрашивающий структурную полосу.
const structuralPredicateName = "Structural"

// startAbortingCalls — вызовы, которыми старт прерывается помимо `return`.
// Корень сегодня возвращает ошибку, но завершение процесса — столь же законная
// форма прерывания, и распознаватель, знающий одну, оставил бы вторую вне
// наблюдения.
var startAbortingCalls = map[string]bool{
	"Fatal": true, "Fatalf": true, "Fatalln": true, "Exit": true,
}

// structuralFatalReport — что гейт увидел в композиционном корне.
type structuralFatalReport struct {
	// SeedCalls — вызовов пакета досева всего: перепись, чтобы «ноль структурных
	// веток» было отличимо от «файл не разобран».
	SeedCalls int
	// ReseedCensusIdents — имена, которым присвоена перепись досева проекции.
	ReseedCensusIdents []string
	// StructuralBranches — имена переписей, у которых структурная ветка НАЙДЕНА.
	StructuralBranches []string
	// NonAborting — структурные ветки, чьё тело старт НЕ прерывает.
	NonAborting []string
	// SiblingBranches — прочих веток после вызова досева (транзиентная полоса и
	// соседи). Перепись: она показывает, что обход дошёл до тела и что молчание
	// гейта на них — решение, а не слепота.
	SiblingBranches int
}

// abortsTheStart — тело ветки прерывает старт: возвращает ненулевую ошибку либо
// завершает процесс.
func abortsTheStart(body *ast.BlockStmt) bool {
	aborts := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ReturnStmt:
			for _, r := range x.Results {
				if id, ok := r.(*ast.Ident); ok && id.Name == "nil" {
					continue
				}
				aborts = true
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && startAbortingCalls[sel.Sel.Name] {
				aborts = true
			}
		}
		return true
	})
	return aborts
}

// callsStructuralOn — условие спрашивает структурную полосу у названной переписи.
func callsStructuralOn(cond ast.Expr, censusIdent string) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != structuralPredicateName {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if ok && recv.Name == censusIdent {
			found = true
		}
		return true
	})
	return found
}

// inspectStructuralFatal разбирает композиционный корень и отвечает, роняет ли
// структурная полоса пересчёта проекции старт.
//
// Перепись досева привязывается к ЛЕВОЙ части присваивания, содержащего вызов:
// без этой привязки «структурная полоса» была бы любым вызовом одноимённого
// метода, а с ней — именно полосой этого досева.
func inspectStructuralFatal(filename, src string) (structuralFatalReport, error) {
	var rep structuralFatalReport
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return rep, err
	}

	// Присваивания, чья правая часть зовёт `seed.<…RoleVerb…>(…)`.
	type binding struct {
		ident string
		pos   token.Pos
		end   token.Pos
	}
	var bindings []binding
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != seedPackageIdent {
			return true
		}
		rep.SeedCalls++
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) == 0 {
			return true
		}
		named := ""
		for _, rhs := range as.Rhs {
			ast.Inspect(rhs, func(x ast.Node) bool {
				// Внутрь литерала функции НЕ спускаемся: `tasks = append(tasks,
				// func() error { … seed.Reseed…(…) … })` содержит вызов ТЕКСТУАЛЬНО,
				// но переписи не получает — её получает имя ВНУТРИ замыкания.
				// Найдено собственной инъекцией, а не чтением: без этой границы гейт
				// требовал структурную ветку от `tasks` и краснел на исправном дереве.
				if _, isLit := x.(*ast.FuncLit); isLit {
					return false
				}
				call, ok := x.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !strings.Contains(sel.Sel.Name, roleVerbReseedMarker) {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == seedPackageIdent {
					named = sel.Sel.Name
				}
				return true
			})
		}
		if named == "" {
			return true
		}
		id, ok := as.Lhs[0].(*ast.Ident)
		if !ok || id.Name == "_" {
			return true
		}
		bindings = append(bindings, binding{ident: id.Name, pos: as.Pos(), end: as.End()})
		return true
	})

	sort.Slice(bindings, func(a, b int) bool { return bindings[a].pos < bindings[b].pos })
	for _, b := range bindings {
		rep.ReseedCensusIdents = append(rep.ReseedCensusIdents, b.ident)
	}

	// Ветки ПОСЛЕ присваивания в ТОМ ЖЕ блоке: структурное решение принимается
	// там, где перепись видна, и обход ограничен этим блоком намеренно — ветка из
	// чужого блока о ней ничего не знает.
	ast.Inspect(file, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for _, b := range bindings {
			idx := -1
			for i, stmt := range block.List {
				if stmt.Pos() == b.pos {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue
			}
			for _, stmt := range block.List[idx+1:] {
				ifs, ok := stmt.(*ast.IfStmt)
				if !ok {
					continue
				}
				if !callsStructuralOn(ifs.Cond, b.ident) {
					rep.SiblingBranches++
					continue
				}
				rep.StructuralBranches = append(rep.StructuralBranches, b.ident)
				if !abortsTheStart(ifs.Body) {
					rep.NonAborting = append(rep.NonAborting, b.ident)
				}
			}
		}
		return true
	})
	return rep, nil
}

// TestIAMRV108_BootAbortsOnTheStructuralBand — структурная полоса пересчёта
// проекции роли роняет старт, а не сообщается и забывается.
func TestIAMRV108_BootAbortsOnTheStructuralBand(t *testing.T) {
	root := repoRoot(t)
	path := root + "/" + bootCompositionRoot
	b, err := os.ReadFile(path) // #nosec G304 -- путь из этого же дерева
	if err != nil {
		t.Fatalf("чтение %s: %v — композиционный корень не прочитан, и молчание гейта "+
			"ничего не значит", bootCompositionRoot, err)
	}
	rep, perr := inspectStructuralFatal(bootCompositionRoot, string(b))
	if perr != nil {
		t.Fatalf("разбор %s: %v", bootCompositionRoot, perr)
	}

	t.Logf("осмотрен %s: вызовов пакета досева %d; переписей досева проекции %d %v; "+
		"структурных веток %d %v; прочих веток после вызова %d",
		bootCompositionRoot, rep.SeedCalls, len(rep.ReseedCensusIdents), rep.ReseedCensusIdents,
		len(rep.StructuralBranches), rep.StructuralBranches, rep.SiblingBranches)

	if rep.SeedCalls == 0 {
		t.Fatalf("в %s не найдено НИ ОДНОГО вызова пакета досева — предпосылка гейта неверна: "+
			"либо корень переехал, либо досев зовётся под другим именем пакета",
			bootCompositionRoot)
	}
	if len(rep.ReseedCensusIdents) == 0 {
		t.Errorf("в %s перепись досева проекции глаголов роли НИ ЧЕМУ не присвоена "+
			"(искали присваивание из вызова `%s.*%s*`).\n"+
			"Судить о структурной полосе нечем: решение «повтор даст то же самое» "+
			"принимается ПО ПЕРЕПИСИ, и без неё его не существует.",
			bootCompositionRoot, seedPackageIdent, roleVerbReseedMarker)
	}
	for _, ident := range rep.ReseedCensusIdents {
		found := false
		for _, got := range rep.StructuralBranches {
			if got == ident {
				found = true
			}
		}
		if !found {
			t.Errorf("в %s у переписи `%s` НЕТ ветки структурной полосы "+
				"(искали `if %s.%s()`).\n"+
				"Структурная полоса — «системные роли есть, пересеяно НОЛЬ»: повтор даст то же "+
				"самое, и цепь вердикта собирает ответ «разрешено ли действие» из строк, которых "+
				"нет. Без этой ветки служба поднимается с НЕИЗВЕСТНЫМ состоянием вердикта, "+
				"сообщив об этом одной строкой журнала, — а снимается решение шестью строками, "+
				"и не краснеет ничто.", bootCompositionRoot, ident, ident, structuralPredicateName)
		}
	}
	for _, ident := range rep.NonAborting {
		t.Errorf("в %s ветка `if %s.%s()` старт НЕ ПРЕРЫВАЕТ — ни `return` ненулевой ошибки, "+
			"ни завершения процесса.\n"+
			"Полоса, которая только сообщает, структурной не является: она неотличима от "+
			"транзиентной, а различие между ними и есть предмет — «повтори позже» на структурной "+
			"полосе есть ложь.", bootCompositionRoot, ident, structuralPredicateName)
	}
}
