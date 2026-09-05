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
// Законная форма ОДНА (#1383): точка наката отдаёт работу общему накату
// [migratorrun.Runner], называя себя (Config.Service). Пропустить счёт в этой
// форме нельзя, не обойдя сам Runner.Up — он и делает вызов; предусловия
// отказывают в старте безымянной службе, поэтому «забыл назваться» тоже не
// проходит.
//
// Ветвей было ДВЕ: вторая — прямая, где `cmd/migrator/main.go` сам звал
// dropguard.Gate раньше goose.Up в той же функции. Она снята вместе со своим
// предметом: прямой формы в дереве больше нет. Распознаватель, сохранивший
// ветвь без предмета, молчал бы о её возвращении — негативное утверждение,
// которому нечего искать, не краснеет никогда.
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

// sharedApplyPkg — где живёт накат, общий на все семь точек (#1383). Координата
// ОДНА на весь класс и потому пишется: перечень «у кого где лежит» стареет со
// скоростью самого подвижного из семи, а один адрес меняется вместе с предметом,
// и его неверность немедленно видна всем гейтам, которые его читают.
var sharedApplyPkg = filepath.Join("pkg", "migratorrun")

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
	// namesItselfToRunner — пакет строит migratorrun.Config с непустым Service.
	namesItselfToRunner bool
}

// stringConsts — объявленные в пакете строковые константы (имя → значение).
//
// Нужны потому, что имя службы законно пишется ДВУМЯ формами: литералом прямо в
// конструкторе и именованной константой рядом. Распознаватель, знающий одну,
// объявил бы вторую нарушением — и объявил: все семь точек наката назвали себя
// константой, а гейт печатал «делегируют 0». Форма, о которой распознаватель не
// знает, не даёт ни красного, ни зелёного — она молчит.
func stringConsts(files []*ast.File) map[string]string {
	out := map[string]string{}
	for _, file := range files {
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
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						out[name.Name] = lit.Value
					}
				}
			}
		}
	}
	return out
}

// namesANonEmptyString — несёт ли выражение непустую строку: литералом либо
// именем объявленной здесь же константы.
func namesANonEmptyString(expr ast.Expr, consts map[string]string) bool {
	switch v := expr.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING && len(v.Value) > 2
	case *ast.Ident:
		lit, ok := consts[v.Name]
		return ok && len(lit) > 2
	}
	return false
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
		ordered := make([]*ast.File, 0, len(pkg.Files))
		for _, file := range pkg.Files {
			ordered = append(ordered, file)
		}
		consts := stringConsts(ordered)
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
				if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "migratorrun" {
					return true
				}
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Service" {
						if namesANonEmptyString(kv.Value, consts) {
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

	delegated, filesRead := 0, 0
	for _, svc := range names {
		f, files := readMigrator(t, dirs[svc])
		filesRead += files
		switch {
		case f.appliesDirectly:
			t.Errorf("%s: cmd/migrator applies the chain itself (goose.Up*) instead of delegating to "+
				"migratorrun.Runner. The tree carries ONE apply form (#1383, "+
				"docs/architecture/migrator-form.md); a second one puts the drop preflight back "+
				"into a line somebody has to remember to write (services/%s/cmd/migrator)", svc, svc)
		case f.namesItselfToRunner:
			delegated++
		default:
			t.Errorf("%s: cmd/migrator does not name itself to the shared apply "+
				"(migratorrun.Config.Service); the drop preflight has no producer here "+
				"(services/%s/cmd/migrator)", svc, svc)
		}
	}

	// Вторая половина: общий накат, которому её доверили, обязан действительно
	// звать счёт — и звать РАНЬШЕ применения. Без неё первая половина зеленела бы
	// на службе, которая назвалась, и только.
	//
	// Пакет здесь ОДИН, поэтому его отсутствие — отказ, а не «нечего проверять»:
	// перепись, не прочитавшая общего наката, утверждала бы о семи точках то,
	// чего не смотрела ни у одной.
	sharedDir := filepath.Join(root, sharedApplyPkg)
	shared, sharedFiles := readMigrator(t, sharedDir)
	if sharedFiles == 0 {
		t.Fatalf("общий накат не прочитан (%s) — эта проверка утверждала бы о счёте "+
			"перед сносом, не посмотрев ни одного его вызова", sharedDir)
	}
	if !shared.appliesDirectly {
		t.Errorf("%s: общий накат не зовёт goose.Up* — цепочку применяет кто-то ещё, "+
			"и счёт перед сносом стоит не на его пути", sharedDir)
	}
	if !shared.gateBeforeApply {
		t.Errorf("%s: общий накат применяет цепочку, не дойдя до dropguard.Gate раньше "+
			"применения. Down-миграция возвращает форму, а не строки, поэтому считать "+
			"надо пока они ещё есть", sharedDir)
	}

	// Перепись печатает ОДНО число там, где прежде печатала пару «прямых /
	// делегирующих»: форм наката в дереве одна, и пара сообщала бы о выборе,
	// которого больше нет.
	t.Logf("перепись: точек наката %d (файлов %d), делегируют общему накату %d; "+
		"общий накат прочитан (файлов %d)",
		len(names), filesRead, delegated, sharedFiles)
}
