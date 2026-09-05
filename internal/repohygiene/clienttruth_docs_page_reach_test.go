// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestDocsPagesAreReachableFromTheMenu — страница сайта достижима из меню, а
// пункт меню ведёт на существующую страницу.
//
// Предмет, разбор объявления меню и границы — в шапке
// clienttruth_docs_page_reach.go. Способность падать и молчать доказана
// инъекцией: clienttruth_docs_page_reach_injection_test.go.
func TestDocsPagesAreReachableFromTheMenu(t *testing.T) {
	root := repoRoot(t)

	// Состав дерева — ИНДЕКС git: под docs/ на машине, где собирали сайт, лежит
	// .docusaurus со своей копией страниц, и обход диска считал бы её деревом.
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}

	c, err := collectDocsPageReach(tree)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	var sites []string
	for _, s := range c.Sites {
		mark := ""
		switch {
		case !s.HasSidebar:
			mark = ",без меню"
		case s.Autogen:
			mark = ",меню автособираемое"
		}
		sites = append(sites, fmt.Sprintf("%s(страниц %d, пунктов %d%s)",
			s.Base, len(s.Pages), len(s.MenuRefs), mark))
	}
	t.Logf("перепись: сайтов %d (достижимость судится у %d · автособираемых %d) · "+
		"страниц %d · пунктов-ссылок %d",
		len(c.Sites), c.Judged, c.AutogenSkip, c.Pages, c.MenuRefs)
	t.Logf("сайты: %s", strings.Join(sites, " "))

	// Проверка собственной предпосылки: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	if len(c.Sites) == 0 || c.Pages == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: сайтов %d, страниц %d",
			len(c.Sites), c.Pages)
	}
	if c.Judged == 0 {
		t.Fatal("достижимость не судится ни у одного сайта — вердикт пуст: " +
			"либо меню нигде не объявлено, либо все они автособираемые")
	}
	// Положительный контроль разбора объявления меню: гейт, чей предмет —
	// ОТСУТСТВИЕ недостижимых страниц, молчит и когда всё подключено, и когда
	// разбор перестал видеть пункты. Различает это требование, чтобы пункты
	// БЫЛИ прочитаны.
	if c.MenuRefs == 0 {
		t.Fatal("из объявлений меню не выведено ни одного пункта-ссылки — " +
			"разбор ослеп, и его молчание ничего не значит")
	}

	for _, f := range docsPageReachFindings(c) {
		t.Errorf("достижимость страницы: %s", f)
	}
}
