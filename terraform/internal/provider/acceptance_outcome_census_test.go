// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Перепись двери приёмочных проб (kacho#2771): классификация исхода работает, только
// если через неё проходят ВСЕ. Проба, позвавшая resource.UnitTest мимо accUnitTest, снова
// печатала бы «провайдер не поднялся» красным; проба с «Acceptance» в имени, не учтённая
// accTrack, лишила бы код выхода 3 основания. Обе вещи судятся по узлам разбора пакета.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// accDirectCycleCalls — вызовы цикла terraform-plugin-testing мимо двери.
func accDirectCycleCalls(dir string) ([]string, int, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, 0, err
	}
	var out []string
	for _, p := range paths {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, 0, err
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Name.Name == "accRunUnitTest" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := c.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "resource" &&
					(sel.Sel.Name == "UnitTest" || sel.Sel.Name == "Test" || sel.Sel.Name == "ParallelTest") {
					out = append(out, filepath.Base(p)+":"+fd.Name.Name)
				}
				return true
			})
		}
	}
	return out, len(paths), nil
}

func TestAcceptanceHarnessIsTheOnlyDoor(t *testing.T) {
	accTrack(t)
	direct, files, err := accDirectCycleCalls(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range direct {
		t.Errorf("%s: цикл terraform зовётся мимо accUnitTest — «провайдер не поднялся» здесь "+
			"снова неотличимо от красного", at)
	}
	tests, tracked, err := accPackageTests(".")
	if err != nil {
		t.Fatal(err)
	}
	acceptance, untracked := 0, 0
	for _, name := range tests {
		if !strings.Contains(name, "Acceptance") {
			continue
		}
		acceptance++
		if !tracked[name] {
			untracked++
			t.Errorf("%s: приёмочная проба не учтена accTrack — код выхода 3 для её прогона "+
				"не имеет основания", name)
		}
	}
	t.Logf("осмотрено файлов %d · проб пакета %d · приёмочных %d (не учтено %d) · вызовов цикла мимо двери %d",
		files, len(tests), acceptance, untracked, len(direct))
	if acceptance == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: приёмочных проб в пакете не найдено — перепись не видит своего предмета")
	}
}
