// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratorform_test.go — гейт: форма наката миграций в дереве ОДНА, копий
// обёртки не больше потолка, решение существует и называет действующую форму.
//
// Предмет, требования и граница разобраны в шапке migratorform.go — здесь они
// не пересказываются, чтобы не завести двух мест об одном предмете.
//
// Доказательство способности упасть и смолчать — в
// migratorform_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

func TestMigratorFormIsOneAndItIsDeclared(t *testing.T) {
	root := repoRoot(t)

	paths, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".go")
	if err != nil {
		t.Fatalf("корпус дерева не построен: %v", err)
	}

	var (
		forms   []migratorForm
		census  migratorFormCensus
		wrapper = map[string]struct{}{}
	)
	for _, path := range paths {
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			t.Fatalf("путь %s не приводится к корню: %v", path, rerr)
		}
		rel = filepath.ToSlash(rel)

		// Копия обёртки считается по КАТАЛОГУ, а не по числу файлов: у vpc их
		// пять (с пробами), у iam три — счёт файлов дал бы величину, которая
		// меняется от добавления пробы и о числе копий не говорит ничего.
		if i := strings.Index(rel, wrapperImportSuffix+"/"); i >= 0 {
			wrapper[rel[:i+len(wrapperImportSuffix)]] = struct{}{}
		}

		if !strings.HasSuffix(rel, "/cmd/migrator/main.go") {
			continue
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("%s не прочитан: %v", rel, rerr)
		}
		form, cerr := classifyMigratorEntryPoint(serviceOfMigratorPath(rel), rel, string(src))
		if cerr != nil {
			t.Fatalf("%v", cerr)
		}
		forms = append(forms, form)
	}

	census.EntryPoints = len(forms)
	census.WrapperCopies = len(wrapper)
	for _, f := range forms {
		if f.Delegating {
			census.Delegating++
		}
		if f.Direct {
			census.Direct++
		}
	}

	// Предпосылка: пустой обход означает, что гейт ничего не осмотрел, а не что
	// дерево чисто. Точек наката в этом продукте не бывает ноль.
	if census.EntryPoints == 0 {
		t.Fatalf("осмотрено НОЛЬ точек наката (%s) — обход пуст, вердикт недействителен: "+
			"проверь, что корпус строится от services/", census)
	}

	// Решение обязано существовать и называть действующую форму. Документ,
	// потерявший это утверждение, равносилен отсутствию решения.
	doc, derr := os.ReadFile(filepath.Join(root, migratorFormDecisionDoc))
	if derr != nil {
		t.Fatalf("решение о форме мигратора не читается (%s): %v — "+
			"на него ссылаются сообщения этого гейта", migratorFormDecisionDoc, derr)
	}
	if !strings.Contains(string(doc), "делегирующая") {
		t.Errorf("%s не называет действующую форму: гейт ссылается на документ, "+
			"который перестал отвечать на вопрос «какая форма целевая»", migratorFormDecisionDoc)
	}

	findings := migratorFormFindings(forms, census.WrapperCopies)
	for _, f := range findings {
		t.Errorf("%s", f)
	}

	t.Logf("перепись: %s", census)
	if len(findings) == 0 {
		t.Logf("второй формы и копий обёртки нет")
	}
}
