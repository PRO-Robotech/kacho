// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package manifestcheckrun_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// singleproducer_test.go — у проверки манифестов ОДНА композиция и тонкие
// вызывающие (задача #1036).
//
// # Предмет
//
// Проверку зовут двое: сборочная цель `module-manifest-check` (её зовёт
// конвейер) и действие `iamctl validate` (его зовёт человек и pre-commit).
// Вторая композиция тех же стадий разошлась бы с первой МОЛЧА — и разошлась бы
// там, где расхождение не видно: обе дают «годно» на честном дереве, а
// разъезжаются на негодном, то есть ровно тогда, когда на них полагаются.
//
// Держится это ДВУМЯ утверждениями, и второе без первого бесполезно:
//
//  1. у чистой функции обхода ровно один прод-вызывающий на каждую точку входа —
//     иначе композиция завелась бы второй раз в обход этого пакета;
//  2. у самой композиции вызывающих ровно двое, и оба — команды: третий
//     вызывающий законен, но обязан быть замечен, потому что он и есть место,
//     где стадии начинают выбирать по-своему.
//
// # Распознаватель знает ОБЕ законные формы обращения
//
// Функция бывает не только ПОЗВАНА, но и ПЕРЕДАНА значением: композиционный
// корень инструмента вносит её портом (`Validate: manifestcheckrun.Run`), и это
// такое же обращение, как вызов, — просто исполняется оно позже. Распознаватель,
// знающий только узел вызова, второго вызывающего не увидел бы ВОВСЕ: не
// нарушением, а невидимостью. Замерено при заведении гейта: по узлу вызова
// находился один из двух.
//
// Поэтому судится узел СЕЛЕКТОРА — он один и тот же у обеих форм, и двойного
// счёта у вызова не даёт.
//
// # Почему разбор, а не поиск по образцу
//
// Имена этих функций встречаются в комментариях этого дерева десятками — в том
// числе в объяснении самого правила. Проверка по подстроке краснела бы на
// собственной шапке; поэтому судится УЗЕЛ вызова, а не строка.
//
// # Объём осмотренного печатается всегда
//
// «Ноль лишних вызывающих» обязано быть отличимо от «ноль прочитанных файлов»:
// пустой обход — находка, а не успех.

// entryPoints — точки входа, у каждой из которых прод-вызывающий ровно один.
var entryPoints = map[string]string{
	"CheckTree":              "services/iam/internal/manifestcheckrun",
	"CheckTreeForGeneration": "services/iam/internal/authzmapgen",
}

// composition — сама композиция и те, кому позволено её звать.
const compositionCall = "Run"

var compositionCallers = []string{
	"services/iam/tools/modulemanifestcheck",
	"services/iam/cmd/iamctl",
}

type callSite struct {
	pkgDir string
	file   string
	line   int
}

// walkCalls обходит прод-дерево и собирает места вызова названных селекторов.
//
// Возвращает вдобавок ЧИСЛО прочитанных файлов: без него вердикт обхода
// неотличим от вердикта обхода, не прочитавшего ничего.
func walkCalls(t *testing.T, root string, want map[string]string) (map[string][]callSite, int) {
	t.Helper()
	found := make(map[string][]callSite)
	filesRead := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "vendor":
				return filepath.SkipDir
			}
			// Сгенерённые стабы пишет генератор — судить его нечего.
			if strings.HasSuffix(filepath.ToSlash(path), "pkg/api") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// Неразобранный файл — «не прочитано», а не «находок нет».
			t.Fatalf("обход НЕ ИСПОЛНЕН: файл %s не разобран: %v", path, perr)
		}
		filesRead++
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			name := sel.Sel.Name
			if _, wanted := want[name]; !wanted {
				return true
			}
			// Селектор судится вместе с квалификатором: одноимённый метод
			// чужого типа вызовом этой функции не является.
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch name {
			case compositionCall:
				if ident.Name != "manifestcheckrun" {
					return true
				}
			default:
				if ident.Name != "manifest" {
					return true
				}
			}
			found[name] = append(found[name], callSite{
				pkgDir: filepath.ToSlash(rel),
				file:   filepath.ToSlash(path),
				line:   fset.Position(sel.Pos()).Line,
			})
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обход НЕ ИСПОЛНЕН: %v", err)
	}
	return found, filesRead
}

func repoRoot(t *testing.T) string {
	t.Helper()
	// Пакет лежит на четыре уровня ниже корня монорепо.
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatalf("обход НЕ ИСПОЛНЕН: корень не разрешён: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("обход НЕ ИСПОЛНЕН: корень %s не похож на монорепо: %v", root, err)
	}
	return root
}

// auditCallers — сами утверждения, отдающие НАХОДКИ, а не роняющие пробу.
//
// Вынесены из пробы именно затем, чтобы гейт можно было подать синтетическому
// дереву: проверка, которую нельзя позвать на подготовленном входе, свою
// способность упасть не доказывает ничем.
func auditCallers(t *testing.T, root string) (findings []string, filesRead int) {
	t.Helper()
	want := map[string]string{compositionCall: ""}
	for name, dir := range entryPoints {
		want[name] = dir
	}
	found, filesRead := walkCalls(t, root, want)

	for _, name := range sortedKeys(entryPoints) {
		wantDir := entryPoints[name]
		sites := found[name]
		if len(sites) != 1 {
			findings = append(findings, fmt.Sprintf(
				"у точки входа %s прод-вызывающих %d, а обязан быть ОДИН: %v — "+
					"второй вызывающий заводит ВТОРУЮ композицию стадий, и разойдётся она молча",
				name, len(sites), describe(sites)))
			continue
		}
		if sites[0].pkgDir != wantDir {
			findings = append(findings, fmt.Sprintf(
				"точку входа %s зовёт %s, а обязан %s (%s:%d)",
				name, sites[0].pkgDir, wantDir, sites[0].file, sites[0].line))
		}
	}

	gotCallers := make([]string, 0, len(found[compositionCall]))
	for _, s := range found[compositionCall] {
		gotCallers = append(gotCallers, s.pkgDir)
	}
	sort.Strings(gotCallers)
	wantCallers := append([]string(nil), compositionCallers...)
	sort.Strings(wantCallers)
	if strings.Join(gotCallers, ",") != strings.Join(wantCallers, ",") {
		findings = append(findings, fmt.Sprintf(
			"композицию зовут %v, а объявлено %v — вызывающий вне перечня законен, "+
				"но обязан быть ЗАМЕЧЕН: он и есть место, где стадии начинают выбирать "+
				"по-своему; внесите его сюда осознанно",
			gotCallers, wantCallers))
	}
	return findings, filesRead
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestManifestCheckKeepsOneCompositionAndTwoThinCallers(t *testing.T) {
	findings, filesRead := auditCallers(t, repoRoot(t))

	t.Logf("перепись: прочитано файлов Go %d · точек входа %d · находок %d",
		filesRead, len(entryPoints), len(findings))
	if filesRead == 0 {
		t.Fatal("обход прочитал НОЛЬ файлов — вердикт беспредметен")
	}
	for _, f := range findings {
		t.Errorf("НАХОДКА: %s", f)
	}
}

func describe(sites []callSite) string {
	out := make([]string, 0, len(sites))
	for _, s := range sites {
		out = append(out, fmt.Sprintf("%s:%d", s.file, s.line))
	}
	return strings.Join(out, " · ")
}
