// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestNlbDocumentedRoutesAreProducedByTheContract — всякий маршрут, показанный
// клиенту документацией nlb, обязан производиться контрактом.
//
// Предмет и радиус — в шапке clienttruth_nlb_documented_routes.go. Способность
// падать и молчать доказана инъекцией:
// clienttruth_nlb_documented_routes_injection_test.go.
func TestNlbDocumentedRoutesAreProducedByTheContract(t *testing.T) {
	root := repoRoot(t)

	// Состав дерева — ИНДЕКС git, а не обход диска: под services/ и proto/ на
	// машине, где поднимали стенд, лежат игнорируемые каталоги, и вердикт,
	// собранный обходом файловой системы, стал бы свойством рабочего каталога,
	// а не коммита.
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}

	c, err := collectNlbDocumentedRoutes(tree)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	uniq := map[string]struct{}{}
	for _, cl := range c.Claims {
		uniq[cl.norm] = struct{}{}
	}
	routes := make([]string, 0, len(c.ContractRoutes))
	for r := range c.ContractRoutes {
		routes = append(routes, r)
	}
	sort.Strings(routes)
	t.Logf("перепись: контрактов %d · страниц %d · маршрутов контракта %d · "+
		"показано вхождений %d (различных %s)",
		c.ProtoFiles, c.DocFiles, len(c.ContractRoutes), len(c.Claims), strconv.Itoa(len(uniq)))
	t.Logf("маршруты контракта: %s", strings.Join(routes, " "))

	// Проверка собственной предпосылки: пустой обход обесценивает вердикт.
	if c.ProtoFiles == 0 || c.DocFiles == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: контрактов %d, страниц %d",
			c.ProtoFiles, c.DocFiles)
	}
	if len(c.ContractRoutes) == 0 {
		t.Fatal("из контракта не выведено ни одного маршрута — сверять не с чем")
	}
	if len(c.Claims) == 0 {
		t.Fatal("в документации не найдено ни одного маршрута — распознаватель " +
			"перестал видеть предмет (см. п.7 §«Гейт на класс»)")
	}

	for _, f := range nlbDocumentedRouteFindings(c) {
		t.Errorf("неисполнимый маршрут в документации: %s", f)
	}
}
