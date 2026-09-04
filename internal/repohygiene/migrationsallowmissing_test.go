// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestEveryMigrationRunnerAdmitsANonChronologicalNumber — накат обязан принимать
// миграцию, чей номер МЕНЬШЕ уже применённого.
//
// # Почему это свойство, а не придирка
//
// Номер миграции у нас — «задача × 1000 + порядок», и он НЕ хронологичен by
// construction: задача #708 закрывается после #800, и файл `708001` появляется в
// дереве позже, чем `800001`. База, накатившая `800001` раньше, при следующем
// обновлении видит «пропущенную миграцию перед текущей версией» и отказывает —
// служба не стартует.
//
// Замер на момент заведения гейта (обход истории добавления файлов миграций,
// сравнение номера с максимумом на тот момент): iam — 8 случаев, storage — 5,
// vpc — 3, registry — 3, compute/nlb/geo — по 1. Итого 22, все семь сервисов.
//
// # Почему гейт по дереву, а не проба одного накатчика
//
// Накат сведён в общий пакет (#1383), и вызов goose в дереве сегодня ОДИН. Гейт
// от этого не становится лишним: он судит по синтаксису вызова, поэтому требует
// свойство и от накатчика, которого ещё нет, — а второй такой вызов заводится
// решением, а не молча.
//
// # Корней обхода ДВА, и это со-правка сведения
//
// Накат переехал из `services/*/internal/apps/migrator` в общий пакет
// ([sharedApplyPkg]), и обход одного лишь `services/` увёл бы ВСЕ вызовы из
// корпуса: счётчик упал бы в ноль, а гейт уронил бы прогон собственным стражем
// предпосылки. Отказ был бы верным (fail-closed), но о переезде, а не о свойстве.
//
// Второй корень — именно общий пакет, а не `internal/` целиком: под ним живут
// оснастка проб и харнессы, которые проигрывают цепочку в ПУСТУЮ базу, где
// пропущенной миграции не бывает by construction. Требовать от них опцию значило
// бы требовать свойство там, где у него нет предмета.
//
// # Чего гейт НЕ утверждает
//
// Что порядок ВНУТРИ одной задачи сохраняется (`NNN001` до `NNN002`) — это
// свойство самого goose, и оно от опции не зависит. И что миграция корректна:
// приём пропущенной означает «применить», а не «пропустить».
func TestEveryMigrationRunnerAdmitsANonChronologicalNumber(t *testing.T) {
	root := repoRoot(t)
	var paths []string
	for _, sub := range []string{"services", sharedApplyPkg} {
		found, cerr := treecorpus.UnderWithSuffix(filepath.Join(root, sub), ".go")
		if cerr != nil {
			t.Fatalf("корпус дерева под %s не построен: %v", sub, cerr)
		}
		paths = append(paths, found...)
	}

	var (
		filesRead  int
		callsFound int
		missingOpt []string
	)
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("%s: чтение не удалось: %v", path, rerr)
		}
		src := string(raw)
		if !strings.Contains(src, "goose.Up") {
			continue
		}
		filesRead++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("%s: разбор не удался: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "goose" {
				return true
			}
			// Предмет — только накат вверх: Down/Status/Version про пропущенные
			// ничего не решают.
			if !strings.HasPrefix(sel.Sel.Name, "Up") {
				return true
			}
			callsFound++
			if !callAdmitsMissing(call) {
				missingOpt = append(missingOpt,
					fset.Position(call.Pos()).String()+" — goose."+sel.Sel.Name)
			}
			return true
		})
	}

	t.Logf("перепись: файлов с накатом прочитано %d, вызовов наката найдено %d, "+
		"без приёма пропущенных %d", filesRead, callsFound, len(missingOpt))

	if callsFound == 0 {
		t.Fatal("вызовов наката не найдено ни одного — гейт ничего не осмотрел, и его " +
			"молчание неотличимо от исправности. Сменился пакет миграций либо предикат " +
			"перестал их узнавать")
	}
	if len(missingOpt) != 0 {
		t.Errorf("накат отказывает на миграции с номером МЕНЬШЕ применённого (%d мест):\n  %s\n\n"+
			"Номер у нас не хронологичен by construction: задача закрывается не по порядку "+
			"номеров, поэтому файл с меньшим номером появляется позже. База, накатившая "+
			"больший номер раньше, при обновлении не стартует.\n"+
			"Чинится опцией goose.WithAllowMissing() в этом вызове.",
			len(missingOpt), strings.Join(missingOpt, "\n  "))
	}
}

// callAdmitsMissing — несёт ли вызов наката опцию приёма пропущенных.
//
// Опция может стоять любым по счёту аргументом (сигнатура вариативная), поэтому
// ищется по ВСЕМ аргументам, а не по позиции.
func callAdmitsMissing(call *ast.CallExpr) bool {
	for _, a := range call.Args {
		inner, ok := a.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := inner.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if sel.Sel.Name == "WithAllowMissing" {
			return true
		}
	}
	return false
}
