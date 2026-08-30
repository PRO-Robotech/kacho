// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestNlbNameUniquenessScopeIsStatedAsTheDatabaseEnforcesIt — контракт и
// документация nlb обязаны называть ТУ область уникальности имени, которую
// держит уникальный индекс.
//
// Предмет, признак и цена — в шапке clienttruth_nlb_name_scope.go. Способность
// этого гейта упасть и смолчать доказана инъекцией в обе стороны:
// clienttruth_nlb_name_scope_injection_test.go.
func TestNlbNameUniquenessScopeIsStatedAsTheDatabaseEnforcesIt(t *testing.T) {
	root := repoRoot(t)

	c, err := collectNlbNameScope(root)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	// ── ПЕРЕПИСЬ. Печатается ВСЕГДА: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	kinds := make([]string, 0, len(c.ClaimsByKind))
	for k := range c.ClaimsByKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	var byKind []string
	for _, k := range kinds {
		byKind = append(byKind, k+"="+strconv.Itoa(c.ClaimsByKind[k]))
	}
	tables := make([]string, 0, len(c.IndexesDerived))
	for tbl, col := range c.IndexesDerived {
		tables = append(tables, tbl+"→"+col)
	}
	sort.Strings(tables)
	t.Logf("перепись: миграций %d · контрактов %d · страниц %d · областей из базы %d (%s) · утверждений %d (%s)",
		c.MigrationFiles, c.ProtoFiles, c.DocFiles,
		len(c.IndexesDerived), strings.Join(tables, ", "),
		len(c.Claims), strings.Join(byKind, ", "))

	// ── ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Пустой обход обесценивает вердикт:
	// «нарушений нет» и «нечего было читать» печатаются одинаково.
	if c.MigrationFiles == 0 || c.ProtoFiles == 0 || c.DocFiles == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: миграций %d, контрактов %d, страниц %d",
			c.MigrationFiles, c.ProtoFiles, c.DocFiles)
	}
	if len(c.IndexesDerived) == 0 {
		t.Fatal("из миграций не выведено ни одной области уникальности имени — " +
			"авторитета нет, сверять не с чем")
	}
	// Область слушателя — предмет, ради которого гейт заведён (#1597).
	if _, ok := c.IndexesDerived["listeners"]; !ok {
		t.Fatal("область уникальности имени слушателя из миграций не выведена")
	}
	if len(c.Claims) == 0 {
		t.Fatal("утверждений об области уникальности имени не найдено ни одного — " +
			"распознаватель перестал видеть предмет (см. п.7 §«Гейт на класс»)")
	}

	for _, f := range nlbNameScopeFindings(c) {
		t.Errorf("расхождение об области уникальности имени: %s", f)
	}
}
