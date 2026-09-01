// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// roleverbreseedbootlane_test.go — IAM-RV-1-07, ЗАГРУЗОЧНАЯ ПОЛОВИНА: отказ
// пересчёта проекции роли ВИДЕН, а не проглочен.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО ГЕЙТ ДЕРЕВА, А НЕ ПРОБА
//
// У сценария две половины. Первая — поведение самого досева (какие роли
// закоммичены, что вернул вызов) — проверяется интеграционной пробой рядом с
// кодом досева. Вторая — уровень, которым отказ сообщается ОПЕРАТОРУ, — живёт в
// композиционном корне, то есть в `package main`, и ни одна проба Go до неё не
// дотягивается by construction.
//
// Без этого гейта у оси инъекции «заменить `Error` на `Warn`» (§7 приёмки) не
// оставалось бы держателя вовсе: правка одного слова возвращала бы проглоченный
// отказ, и ни одна проверка дерева не покраснела бы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО ТРЕБУЕТСЯ
//
//  1. у досева проекции глаголов роли есть СВОЙ вызов в композиционном корне —
//     пока он спрятан внутри чужого досева, у его отказа нет собственной полосы,
//     и различить настройку от сбоя нечем;
//  2. ветка отказа этого вызова печатает `Error`, а НЕ `Warn`/`Info`/`Debug`.
//     `Warn` в этом дереве несёт смысл «ожидаемое отклонение, ретрай штатен»;
//     проекция, не пересеянная целиком, — не отклонение, а неизвестное состояние
//     вердикта;
//  3. ветка отказа печатает ХОТЬ ЧТО-ТО: пустая ветка глотает отказ полнее любого
//     `Warn`.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА
//
// Досев опознаётся по имени вызываемой функции пакета досева: имя обязано нести
// `RoleVerb`. Досев, названный иначе, гейт не найдёт — и это НЕ тихая слепая
// зона: ноль найденных вызовов есть НАХОДКА (п. 1), а не молчание. Гейт судит
// узлы разобранного дерева, а не текст: имя `Warn` встречается в комментариях, и
// проверка по подстроке краснела бы на собственном объяснении.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// bootCompositionRoot — композиционный корень iam.
const bootCompositionRoot = "services/iam/cmd/kacho-iam/serve.go"

// seedPackageIdent — имя, под которым композиционный корень импортирует досев.
const seedPackageIdent = "seed"

// roleVerbReseedMarker — по чему опознаётся вызов досева проекции глаголов роли.
const roleVerbReseedMarker = "RoleVerb"

// swallowingLevels — уровни, которыми отказ пересчёта сообщать НЕЛЬЗЯ.
var swallowingLevels = map[string]bool{"Warn": true, "Info": true, "Debug": true}

// loggerLevels — все уровни, по которым узнаётся «ветка отказа что-то печатает».
var loggerLevels = map[string]bool{
	"Warn": true, "Info": true, "Debug": true, "Error": true, "Fatal": true,
}

// bootReseedReport — что гейт увидел в композиционном корне.
type bootReseedReport struct {
	// SeedCalls — вызовов пакета досева всего: перепись, чтобы «ноль вызовов
	// досева проекции» было отличимо от «файл не разобран».
	SeedCalls int
	// ReseedCalls — имена найденных вызовов досева проекции глаголов.
	ReseedCalls []string
	// Levels — уровни журнала в ветке отказа каждого такого вызова.
	Levels []string
	// Silent — вызовы, чья ветка отказа не печатает ничего.
	Silent []string
}

// inspectBootRoleVerbReseed разбирает композиционный корень и отвечает, есть ли у
// досева проекции роли собственная полоса отказа и каким уровнем она сообщается.
//
// Ветка отказа ищется в теле `if` — и в форме `if err := seed.X(); err != nil`,
// и в форме «присваивание, затем `if err != nil`»: обе живут в этом корне, и
// признак, знающий одну, оставил бы вторую вне наблюдения.
func inspectBootRoleVerbReseed(filename, src string) (bootReseedReport, error) {
	var rep bootReseedReport
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return rep, err
	}

	// isReseedCall — вызов `seed.<…RoleVerb…>(…)`.
	isReseedCall := func(n ast.Node) string {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return ""
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != seedPackageIdent {
			return ""
		}
		return sel.Sel.Name
	}

	// levelsInFailureBranch — уровни журнала внутри тел `if` поддерева.
	levelsInFailureBranch := func(nodes []ast.Stmt) []string {
		var out []string
		for _, n := range nodes {
			ast.Inspect(n, func(x ast.Node) bool {
				ifs, ok := x.(*ast.IfStmt)
				if !ok {
					return true
				}
				ast.Inspect(ifs.Body, func(y ast.Node) bool {
					call, ok := y.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || !loggerLevels[sel.Sel.Name] {
						return true
					}
					out = append(out, sel.Sel.Name)
					return true
				})
				return true
			})
		}
		return out
	}

	// Вызов досева приписывается САМОМУ УЗКОМУ объемлющему оператору.
	//
	// Обход посещает вложенные блоки, и один и тот же вызов попадается на каждом
	// уровне: тело функции старта несёт оператор `tasks = append(…, func(){…})`,
	// а тело замыкания — сам `if`. Приписав вызов внешнему оператору, признак
	// взял бы окном ВЕСЬ блок задачи — и уровень соседней полосы прочитался бы
	// как уровень этой. Найдено собственной инъекцией («соседняя полоса»), а не
	// чтением: ось «Warn у соседа» краснела бы всегда.
	type candidate struct {
		span  int
		block *ast.BlockStmt
		index int
	}
	best := map[token.Pos]candidate{}
	names := map[token.Pos]string{}

	ast.Inspect(file, func(n ast.Node) bool {
		if name := isReseedCall(n); name != "" {
			rep.SeedCalls++
			if strings.Contains(name, roleVerbReseedMarker) {
				names[n.Pos()] = name
			}
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, stmt := range block.List {
			for pos := range names {
				if pos < stmt.Pos() || pos >= stmt.End() {
					continue
				}
				span := int(stmt.End() - stmt.Pos())
				if cur, seen := best[pos]; !seen || span < cur.span {
					best[pos] = candidate{span: span, block: block, index: i}
				}
			}
		}
		return true
	})

	// Порядок находок — по позиции вызова, чтобы вывод не плясал между прогонами.
	positions := make([]token.Pos, 0, len(best))
	for pos := range best {
		positions = append(positions, pos)
	}
	sort.Slice(positions, func(a, b int) bool { return positions[a] < positions[b] })

	for _, pos := range positions {
		c := best[pos]
		rep.ReseedCalls = append(rep.ReseedCalls, names[pos])
		window := []ast.Stmt{c.block.List[c.index]}
		if c.index+1 < len(c.block.List) {
			window = append(window, c.block.List[c.index+1])
		}
		levels := levelsInFailureBranch(window)
		if len(levels) == 0 {
			rep.Silent = append(rep.Silent, names[pos])
		}
		rep.Levels = append(rep.Levels, levels...)
	}
	return rep, nil
}

// TestIAMRV107_BootReportsRoleVerbReseedFailureAtErrorLevel — отказ пересчёта
// проекции роли сообщается оператору уровнем `Error` из собственной полосы.
func TestIAMRV107_BootReportsRoleVerbReseedFailureAtErrorLevel(t *testing.T) {
	root := repoRoot(t)
	path := root + "/" + bootCompositionRoot
	b, err := os.ReadFile(path) // #nosec G304 -- путь из этого же дерева
	if err != nil {
		t.Fatalf("чтение %s: %v — композиционный корень не прочитан, и молчание гейта "+
			"ничего не значит", bootCompositionRoot, err)
	}
	rep, perr := inspectBootRoleVerbReseed(bootCompositionRoot, string(b))
	if perr != nil {
		t.Fatalf("разбор %s: %v", bootCompositionRoot, perr)
	}

	t.Logf("осмотрен %s: вызовов пакета досева %d; из них досева проекции глаголов %d %v; "+
		"уровней в ветке отказа %v", bootCompositionRoot, rep.SeedCalls,
		len(rep.ReseedCalls), rep.ReseedCalls, rep.Levels)

	if rep.SeedCalls == 0 {
		t.Fatalf("в %s не найдено НИ ОДНОГО вызова пакета досева — предпосылка гейта неверна: "+
			"либо корень переехал, либо досев зовётся под другим именем пакета",
			bootCompositionRoot)
	}
	if len(rep.ReseedCalls) == 0 {
		t.Errorf("у досева проекции глаголов роли НЕТ собственного вызова в %s "+
			"(искали вызов `%s.*%s*`).\n"+
			"Пока пересчёт спрятан внутри чужого досева, у его отказа нет собственной полосы: "+
			"он приезжает вызывающему обёрнутым в чужую ошибку и печатается уровнем чужой "+
			"полосы. Различить «база не ответила» и «механизм не работает» на этом входе нечем.",
			bootCompositionRoot, seedPackageIdent, roleVerbReseedMarker)
	}
	for _, name := range rep.Silent {
		t.Errorf("ветка отказа `%s.%s` не печатает НИЧЕГО — отказ проглочен полнее, чем любым "+
			"`Warn`: состояние вердикта неизвестно, и об этом никто не узнает",
			seedPackageIdent, name)
	}
	for _, lvl := range rep.Levels {
		if swallowingLevels[lvl] {
			t.Errorf("отказ пересчёта проекции роли сообщается уровнем `%s` — а обязан `Error`.\n"+
				"`Warn` в этом дереве значит «ожидаемое отклонение, ретрай штатен». Проекция, "+
				"не пересеянная целиком, — не отклонение, а НЕИЗВЕСТНОЕ состояние вердикта: "+
				"цепь ответа «разрешено ли действие» собирается из строк, которых может не быть.", lvl)
		}
	}
}
