// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// droppreflight_test.go — гейт: ни один мигратор не применяет цепочку, не сосчитав
// сперва то, что она уничтожит.
//
// Соседний гейт (dropguard_test.go) требует по каждому DROP TABLE ЧИСЛО и сверяет
// его с базой, проигранной с нуля. Это утверждение о СХЕМЕ: сколько сеет наша
// собственная цепочка. У арендатора в той же таблице лежит ещё и то, что записал он,
// и никакой контейнер этого не знает — dropguard/doc.go называет этот пробел прямо,
// а манифест vpc повторяет его дословно про таблицу, чьё живое содержимое
// неограниченно.
//
// Закрывает пробел живой счёт в мигратора. Он был бы бесполезен как «шаг, который
// надо не забыть позвать»: шаг без производителя — ровно тот дефект, который здесь
// чинится. Поэтому гейт проверяет не наличие функции, а то, что КАЖДЫЙ мигратор
// дерева до неё доходит, и доходит ДО применения.
//
// Законных форм ДВЕ, и обе перечислены намеренно: распознаватель, знающий одну,
// объявил бы вторую нарушением, а третью — невидимой.
//
//	прямая      cmd/migrator/main.go сам зовёт dropguard.Gate, и зовёт его РАНЬШЕ
//	            goose.Up в той же функции;
//	через runner cmd/migrator/main.go отдаёт работу migrator.Runner, называя себя
//	            (Config.Service). Пропустить счёт в этой форме нельзя не обойдя сам
//	            Runner.Up — он и делает вызов; Config.Validate отказывает в старте
//	            безымянному сервису, поэтому «забыл назваться» тоже не проходит.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// gooseApplyFuncs — вызовы goose, которые ПРИМЕНЯЮТ цепочку вперёд. Down и Status
// сюда не входят: сносить нечего, а Status ничего не меняет.
var gooseApplyFuncs = map[string]bool{
	"Up": true, "UpContext": true, "UpTo": true, "UpToContext": true,
}

type migratorFacts struct {
	// gateBeforeApply — в какой-то функции пакета dropguard.Gate стоит раньше
	// первого применяющего вызова goose.
	gateBeforeApply bool
	// appliesDirectly — пакет сам зовёт goose.Up*.
	appliesDirectly bool
	// namesItselfToRunner — пакет строит migrator.Config с непустым Service.
	namesItselfToRunner bool
}

func readMigrator(t *testing.T, dir string) (migratorFacts, int) {
	t.Helper()
	var f migratorFacts
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	files := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			files++
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				sel, ok := lit.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Config" {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "migrator" {
					return true
				}
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Service" {
						if s, ok := kv.Value.(*ast.BasicLit); ok && len(s.Value) > 2 {
							f.namesItselfToRunner = true
						}
					}
				}
				return true
			})

			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				gateAt, applyAt := token.NoPos, token.NoPos
				ast.Inspect(fn.Body, func(n ast.Node) bool {
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
					switch {
					case pkgIdent.Name == "dropguard" && sel.Sel.Name == "Gate":
						if !gateAt.IsValid() {
							gateAt = call.Pos()
						}
					case pkgIdent.Name == "goose" && gooseApplyFuncs[sel.Sel.Name]:
						f.appliesDirectly = true
						if !applyAt.IsValid() {
							applyAt = call.Pos()
						}
					}
					return true
				})
				if gateAt.IsValid() && applyAt.IsValid() && gateAt < applyAt {
					f.gateBeforeApply = true
				}
			}
		}
	}
	return f, files
}

// TestEveryMigratorCountsBeforeItDrops — гейт по дереву.
//
// Перечень мигратров ВЫЧИСЛЯЕТСЯ обходом services/*/cmd/migrator, а не
// выписывается: сервис, заведённый завтра, попадает под гейт сам. Пустой обход —
// провал, а не «чисто»: «ноль находок» обязано быть отличимо от «ноль
// прочитанного».
func TestEveryMigratorCountsBeforeItDrops(t *testing.T) {
	root := repoRoot(t)
	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("read %s: %v", servicesDir, err)
	}

	var names []string
	dirs := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(servicesDir, e.Name(), "cmd", "migrator")
		if st, serr := os.Stat(dir); serr == nil && st.IsDir() {
			names = append(names, e.Name())
			dirs[e.Name()] = dir
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("no cmd/migrator found under %s — this gate would assert nothing", servicesDir)
	}

	direct, delegated, filesRead := 0, 0, 0
	for _, svc := range names {
		f, files := readMigrator(t, dirs[svc])
		filesRead += files
		switch {
		case f.appliesDirectly && f.gateBeforeApply:
			direct++
		case f.appliesDirectly:
			t.Errorf("%s: cmd/migrator applies the chain itself but does not reach dropguard.Gate before it. "+
				"A down migration restores the shape, not the rows, so the count has to happen while they still exist "+
				"(services/%s/cmd/migrator)", svc, svc)
		case f.namesItselfToRunner:
			delegated++
		default:
			t.Errorf("%s: cmd/migrator neither counts before applying nor names itself to a Runner that does "+
				"(migrator.Config.Service); the drop preflight has no producer here (services/%s/cmd/migrator)", svc, svc)
		}
	}

	// Вторая половина делегированной формы: Runner, которому её доверили, обязан
	// действительно звать счёт. Без этой проверки первая половина зеленела бы на
	// сервисе, который назвался — и только.
	runnersChecked := 0
	for _, svc := range names {
		runner := filepath.Join(servicesDir, svc, "internal", "apps", "migrator", "runner.go")
		raw, rerr := os.ReadFile(runner)
		if rerr != nil {
			continue // сервис применяет напрямую, своего runner-пакета не держит
		}
		runnersChecked++
		if !strings.Contains(string(raw), "dropguard.Gate") {
			t.Errorf("%s: internal/apps/migrator/runner.go carries Up but never reaches dropguard.Gate; "+
				"a migrator that delegates would then apply drops uncounted", svc)
		}
	}

	t.Logf("census: %d migrator binary(ies) read across %d file(s) — %d count inline before goose, %d delegate to a Runner; %d runner package(s) checked",
		len(names), filesRead, direct, delegated, runnersChecked)
}
