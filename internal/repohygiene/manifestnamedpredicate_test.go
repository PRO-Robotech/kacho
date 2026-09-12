// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// manifestnamedpredicate_test.go — держатель разбора «предикат, названный
// манифестом домена, исполним в этом дереве» (задача #1110, п. 4 действующего
// предиката).
//
// Предмет, пять осей, довод про разбор по позициям аргументов и перечень того,
// чего гейт НЕ покрывает, — в шапке manifestnamedpredicate.go. Здесь только
// обход, перепись и вердикт.
//
// Способность упасть и смолчать доказана инъекцией —
// manifestnamedpredicate_injection_test.go.
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

	"github.com/PRO-Robotech/corelib/modulemanifest"
)

// buildManifestTreeView — вид дерева, собранный ПО ИНДЕКСУ git.
//
// Обход диска прочитал бы игнорируемые каталоги — рабочие копии полос,
// распакованные чарты, каталоги сборки консоли, — и вердикт перестал бы быть
// свойством коммита. Цели `Makefile` и имена функций читаются ЛЕНИВО: только у
// тех координат, которые манифесты действительно назвали.
func buildManifestTreeView(t *testing.T, root string, preds []manifestNamedPredicate) manifestTreeView {
	t.Helper()
	tt := newTrackedTree(t, root)

	view := manifestTreeView{
		Files:       map[string]bool{},
		Dirs:        map[string]bool{".": true},
		MakeTargets: map[string]map[string]bool{},
		GoDirs:      map[string]bool{},
		GoFuncs:     map[string]map[string]bool{},
	}
	for rel := range tt.files {
		view.Files[rel] = true
		dir := rel
		for {
			idx := strings.LastIndex(dir, "/")
			if idx < 0 {
				break
			}
			dir = dir[:idx]
			view.Dirs[dir] = true
		}
		if strings.HasSuffix(rel, ".go") {
			d := "."
			if idx := strings.LastIndex(rel, "/"); idx >= 0 {
				d = rel[:idx]
			}
			view.GoDirs[d] = true
		}
	}

	for _, p := range preds {
		switch p.Kind {
		case manifestPredicateKindMake:
			mf := "Makefile"
			if p.Dir != "" && p.Dir != "." {
				mf = p.Dir + "/Makefile"
			}
			if !view.Files[mf] || view.MakeTargets[mf] != nil {
				continue
			}
			// #nosec G304 -- путь из индекса git.
			raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(mf)))
			if err != nil {
				t.Fatalf("%s не прочитан: %v — вердикт о названной цели был бы беспредметен", mf, err)
			}
			view.MakeTargets[mf] = parseManifestMakeTargets(string(raw))
		case manifestPredicateKindGoTest:
			if p.RunName == "" {
				continue
			}
			dir := manifestPredicatePackageDir(p)
			if !view.GoDirs[dir] || view.GoFuncs[dir] != nil {
				continue
			}
			view.GoFuncs[dir] = manifestPackageFuncNames(t, root, dir, view.Files)
		}
	}
	return view
}

// manifestPackageFuncNames — имена функций, ОБЪЯВЛЕННЫХ каталогом.
//
// Читается разбором, а не образцом по тексту: имя пробы встречается в шапках,
// в `t.Run` и в прозе соседей, и поиск по вхождению признал бы объявленной
// функцию, которой нет.
func manifestPackageFuncNames(t *testing.T, root, dir string, files map[string]bool) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	var rels []string
	for rel := range files {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		d := "."
		if idx := strings.LastIndex(rel, "/"); idx >= 0 {
			d = rel[:idx]
		}
		if d == dir {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	fset := token.NewFileSet()
	for _, rel := range rels {
		// #nosec G304 -- путь из индекса git.
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s не прочитан: %v", rel, err)
		}
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s не разобран: %v — «функции нет» означало бы «файл не прочитан»", rel, err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil {
				continue
			}
			out[fd.Name.Name] = true
		}
	}
	return out
}

// TestManifestNamedPredicateIsRunnableInThisTree — сам гейт.
func TestManifestNamedPredicateIsRunnableInThisTree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		parts := strings.Split(rel, "/")
		if len(parts) != 3 || parts[0] != manifestServicesDir || parts[2] != modulemanifest.FileName {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		preds        []manifestNamedPredicate
		commentLines int
	)
	for _, rel := range rels {
		// #nosec G304 -- путь из индекса git.
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s не прочитан: %v — «ноль держателей» означало бы «ноль прочитанного»", rel, err)
		}
		p, lines := extractManifestNamedPredicates(rel, string(raw))
		preds = append(preds, p...)
		commentLines += lines
	}

	if len(rels) == 0 {
		t.Fatalf("под %s/*/%s не найдено ни одного манифеста — обход пуст, и «ноль находок» "+
			"означало бы «ноль прочитанного»", manifestServicesDir, modulemanifest.FileName)
	}
	if commentLines == 0 {
		t.Fatalf("на %d манифестах прочитано НОЛЬ строк-комментариев — разбор шапки сломан, "+
			"и вердикт о названных держателях беспредметен", len(rels))
	}
	if len(preds) == 0 {
		t.Fatalf("на %d манифестах (%d строк-комментариев) не найдено НИ ОДНОГО названного "+
			"держателя — либо разбор по позициям сломан, либо манифесты перестали называть "+
			"свои проверки; оба случая суть находка, а не норма", len(rels), commentLines)
	}

	view := buildManifestTreeView(t, root, preds)
	found := auditManifestNamedPredicates(preds, view)
	// «Резолвится» считается от ПРОВЕРЕННЫХ: координата чужого репозитория не
	// проверялась вовсе, и зачесть её в резолвящиеся значило бы завысить
	// положительный контроль ровно на размер исключения.
	checked := len(preds) - countManifestForeignPredicates(preds)
	resolved := checked - len(found)

	t.Log(manifestNamedPredicateCensus(len(rels), commentLines, preds, resolved))

	if checked == 0 {
		t.Errorf("все %d названных держателя отнесены к ЧУЖОМУ репозиторию — проверять в "+
			"этом дереве не осталось нечего, и «ноль находок» стало бы свойством исключения, "+
			"а не дерева", len(preds))
	} else if resolved == 0 {
		t.Errorf("из %d проверенных держателей не резолвится НИ ОДИН — это либо дерево, "+
			"потерявшее всех держателей манифестов, либо сломанный резолвер; различить их "+
			"по молчанию нельзя, поэтому исход назван отдельно от находок ниже", checked)
	}
	if len(found) > 0 {
		t.Errorf("манифест домена назвал держателя, которого в этом дереве нет — %d находка(и):\n  %s\n\n"+
			"Что делать: назвать предикат, ИСПОЛНИМЫЙ здесь, либо — если держатель уехал в другой "+
			"репозиторий — назвать его ПРОЗОЙ вместе с репозиторием, а не командной строкой. "+
			"Командная строка в манифесте платформы обещает исполнимость в дереве, где документ лежит.",
			len(found), strings.Join(found, "\n  "))
	}
}
