// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
	"github.com/PRO-Robotech/kacho/pkg/validate/nameform"
)

// TestDocsNameFormMatchesTheTree — всякая форма имени ресурса, показанная
// клиенту (страницей сайта либо словарём, из которого страница берёт текст),
// совпадает с единственным объявлением формы в дереве.
//
// Предмет, три формы записи, две кодировки и границы — в шапке
// clienttruth_docs_nameform.go. Способность падать и молчать доказана инъекцией:
// clienttruth_docs_nameform_injection_test.go.
func TestDocsNameFormMatchesTheTree(t *testing.T) {
	root := repoRoot(t)

	// Состав дерева — ИНДЕКС git: под docs/ на машине, где собирали сайт, лежат
	// node_modules и .docusaurus, и обход файловой системы прочитал бы чужие
	// регулярки наравне с нашими.
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}

	c, err := collectDocsNameForm(tree)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	var matched, diverged, entity int
	for _, cl := range c.Claims {
		if cl.Entity {
			entity++
		}
		if cl.Regex == nameform.Form {
			matched++
		} else {
			diverged++
		}
	}
	t.Logf("перепись: файлов прочитано %d (страниц %d · словарей %d) · "+
		"регулярок осмотрено %d (обычной записью %d · записью HTML-сущностями %d)",
		c.Files, c.ContentDocs, c.DictFiles, c.Regexes, c.Regexes-c.RegexEntity, c.RegexEntity)
	t.Logf("перепись форм имени: признано утверждением о форме %d "+
		"(рядом таблицы %d · словарём %d · прозой %d; из них сущностями %d) · "+
		"совпало %d · разошлось %d · записей ведомости исключений %d",
		len(c.Claims),
		c.ShapeCount(docsNameFormShapeTable),
		c.ShapeCount(docsNameFormShapeDict),
		c.ShapeCount(docsNameFormShapeProse),
		entity, matched, diverged, len(docsNameFormExemptions))
	t.Logf("эталон (pkg/validate/nameform.Form): %s", nameform.Form)

	// Проверка собственной предпосылки: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	if c.Files == 0 || c.ContentDocs == 0 || c.DictFiles == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: файлов %d (страниц %d, словарей %d)",
			c.Files, c.ContentDocs, c.DictFiles)
	}
	if c.Regexes == 0 {
		t.Fatal("в клиентской документации не найдено ни одной регулярки — " +
			"распознаватель перестал видеть предмет (см. п.7 §«Гейт на класс»)")
	}
	// Положительный контроль гейта, чей предмет — ОТСУТСТВИЕ расхождения: он
	// молчит и когда расхождений нет, и когда сломан распознаватель. Различает
	// это требование, чтобы утверждения о форме имени были НАЙДЕНЫ — и найдены
	// в каждой из трёх форм записи.
	if len(c.Claims) == 0 {
		t.Fatal("ни одна регулярка не признана утверждением о форме имени — " +
			"распознаватель ослеп, и его молчание ничего не значит")
	}
	for _, s := range []docsNameFormShape{
		docsNameFormShapeTable, docsNameFormShapeDict, docsNameFormShapeProse,
	} {
		if c.ShapeCount(s) == 0 {
			t.Errorf("форма записи %q не прочитана ни разу — всё записанное в ней "+
				"вышло из-под наблюдения молча", s)
		}
	}

	for _, f := range docsNameFormFindings(c) {
		t.Errorf("форма имени в документации: %s", f)
	}
}
