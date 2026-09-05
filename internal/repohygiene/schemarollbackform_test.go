// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// schemarollbackform_test.go — ГЕЙТ: миграция, отнимающая у прежнего образа
// колонку, обязана сказать об этом машинно.
//
// Предмет, механизм, граница с `dropguard` и форма признака — в шапке
// schemarollbackform.go; здесь они не пересказываются, чтобы два места об одном
// предмете не разошлись.
//
// Что делать при отказе — говорит сам текст находки: он называет файл, форму,
// число вхождений и обе законные формы объявления.
//
// Доказательство способности упасть и смолчать — schemarollbackform_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// readMigrationSources — исходники миграций ИЗ ИНДЕКСА git: вердикт обязан быть
// свойством коммита, а не рабочего каталога. Файл, лежащий на диске и не
// добавленный в индекс, гейтом не судится — и это правильно: судить надо то,
// что уезжает.
func readMigrationSources(t testing.TB, root string, rel []string) []schemaRollbackSource {
	t.Helper()
	out := make([]schemaRollbackSource, 0, len(rel))
	for _, r := range rel {
		if !strings.HasSuffix(r, ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r)))
		if err != nil {
			// Файл в индексе, но не на диске — состояние, о котором вердикт
			// выносить нельзя: обход неполон, и «ноль находок» ниже
			// относилось бы к меньшему, чем дерево.
			t.Fatalf("файл %s есть в индексе и не читается с диска: %v", r, err)
		}
		out = append(out, schemaRollbackSource{Rel: r, Body: string(b)})
	}
	return out
}

// TestMigrationsRemovingAColumnDeclareThePointOfNoReturn — после такой миграции
// прежний образ схему обслуживать не может, и сегодня об этом не говорит ничто:
// единственный машинный распознаватель необратимости (`dropguard`) знает
// `DROP TABLE` и о снятии колонки не спрашивает.
func TestMigrationsRemovingAColumnDeclareThePointOfNoReturn(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}

	srcs := readMigrationSources(t, root, tree.SortedFiles())

	var baselineText string
	if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(schemaRollbackBaselineFile))); err == nil {
		baselineText = string(b)
	}
	baseline, malformed := parseSchemaRollbackBaseline(baselineText)

	census, findings := findSchemaRollbackFindings(srcs, baseline)

	t.Logf("перепись: файлов в дереве %d · %s · записей ведомости %d · находок %d",
		tree.Count(), census, len(baseline), len(findings))

	// ПРЕДПОСЫЛКИ. Каждая — факт о дереве, который может измениться и сделать
	// «ноль находок» бессмысленным; поэтому гейт проверяет их сам.
	if tree.Count() == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: индекс дерева пуст — прочитано ноль файлов, " +
			"и любое «ноль находок» ниже ничего не значит")
	}
	if census.Files == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: в дереве не нашлось ни одного файла миграции " +
			"вида <номер>_<что>.sql — обход пуст, гейт не проверяет ничего")
	}
	if census.WithForm == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: ни одна Up-секция дерева не несёт ни одной из " +
			"судимых форм. Либо распознаватель перестал их видеть, либо формы ушли " +
			"из дерева; в обоих случаях «ноль находок» ничего не означает")
	}

	for _, m := range malformed {
		t.Errorf("%s: %s", schemaRollbackBaselineFile, m)
	}
	for _, f := range findings {
		t.Error(f.String())
	}
}
