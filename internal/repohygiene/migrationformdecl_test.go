// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationformdecl_test.go — гейт: объявление формы номера миграции в дереве
// согласно с тем, что требует проверка.
//
// Предмет, роли мест и граница разобраны в шапке migrationformdecl.go — здесь
// они не пересказываются, чтобы не завести двух мест об одном предмете.
//
// Доказательство способности упасть и смолчать — в
// migrationformdecl_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

func TestMigrationFormIsDeclaredInOnePlace(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}

	// КАНОН читается из гейта, который его требует, а не выписывается здесь.
	gateSrc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(enforcingMigrationFormGate)))
	if err != nil {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: гейт %s не прочитан (%v) — брать канон неоткуда, "+
			"и любое «ноль находок» ниже ничего не значит. Если гейт переехал, поправь "+
			"enforcingMigrationFormGate тем же изменением", enforcingMigrationFormGate, err)
	}
	form, ferr := readCanonicalMigrationForm(string(gateSrc))
	if ferr != nil {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: канон из %s не извлечён: %v",
			enforcingMigrationFormGate, ferr)
	}

	docs := collectMigrationFormDocs(t, tree)
	findings, census := migrationFormFindings(docs, form)

	t.Logf("перепись: файлов в дереве %d, документов прочитано %d, из них называют форму %d; "+
		"объявлений формы найдено %d (действующей %d, отвергнутой %d); "+
		"канон — %s (%d цифр), прочитан из %s; объявлен в %s",
		tree.Count(), census.FilesRead, census.FilesWithForm,
		census.Canonical+census.Legacy, census.Canonical, census.Legacy,
		form.Token, form.Digits, enforcingMigrationFormGate, canonicalMigrationFormDoc)

	// ПРЕДПОСЫЛКИ. Каждая — факт, который может измениться и сделать запрет
	// беспредметным; поэтому гейт проверяет их сам, а не подразумевает.
	if tree.Count() == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: индекс дерева пуст — прочитано ноль файлов")
	}
	if census.FilesRead == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: не прочитано ни одного документа — обход сломан " +
			"либо соглашение о расширениях сменилось")
	}
	if form.Token == "" || form.Digits == 0 {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: канон извлечён пустым (запись %q, ширина %d). "+
			"Гейт %s перестал называть форму либо переименовал принимающую регулярку",
			form.Token, form.Digits, enforcingMigrationFormGate)
	}
	if len(form.Token) != form.Digits {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: гейт %s ТРЕБУЕТ %d цифр, а НАЗЫВАЕТ читателю запись "+
			"%q длиной %d. Требование и его объяснение разошлись в самом гейте — пока это "+
			"так, сверять документы не с чем",
			enforcingMigrationFormGate, form.Digits, form.Token, len(form.Token))
	}
	if !tree.HasFile(canonicalMigrationFormDoc) {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: канонического документа %s в дереве нет — "+
			"места, объявляющего форму, не существует, и ссылаться остальным не на что",
			canonicalMigrationFormDoc)
	}
	if census.Canonical+census.Legacy == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: во всём дереве не найдено ни одного объявления формы — " +
			"ни действующего, ни отвергнутого. Так выглядит переставший узнавать записи " +
			"предикат, а не дерево без расхождений")
	}

	for _, f := range findings {
		t.Error(f)
	}
}

// collectMigrationFormDocs — корпус: отслеживаемые тексты дерева, в которых
// форма может быть названа.
//
// Берётся ИНДЕКС git (свойство коммита, а не рабочего каталога), поэтому
// неотслеживаемый черновик корпус не расширяет и вердикт не меняет.
func collectMigrationFormDocs(t *testing.T, tree *treecorpus.Tree) []migrationFormDoc {
	t.Helper()

	var docs []migrationFormDoc
	for _, rel := range tree.SortedFiles() {
		switch {
		case strings.HasSuffix(rel, ".md"), strings.HasSuffix(rel, ".mdx"),
			strings.HasSuffix(rel, ".go"):
		default:
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			// Файл в индексе есть, на диске нет — состояние рабочего каталога, а
			// не дерева. Пропустить молча нельзя: он выпал бы из-под надзора.
			t.Fatalf("%s: числится в индексе, но не прочитан: %v", rel, err)
		}
		docs = append(docs, migrationFormDoc{Rel: rel, Body: string(body)})
	}
	return docs
}
