// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// TestEdgePagesNameTheirOwnHalfOfTheAuthzVisibilityWindow — страница края,
// отправляющая клиента ждать authz-видимость, называет ручку своего кэша
// решений, и названное умолчание совпадает с объявленным в конфигурации.
//
// Предмет, обе половины и границы — в шапке ct2_misc_authz_window.go.
// Способность гейта упасть и смолчать доказана инъекцией в обе стороны:
// ct2_misc_authz_window_injection_test.go.
func TestEdgePagesNameTheirOwnHalfOfTheAuthzVisibilityWindow(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	c, err := collectAuthzWindow(tree)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	// ── ПЕРЕПИСЬ. Печатается ВСЕГДА и несёт ДВЕ величины: сколько страниц
	// осмотрено и сколько из них соответствует. Одно число скрывает ровно тот
	// случай, ради которого гейт заведён.
	var named []string
	for _, p := range c.Pages {
		mark := "ручка"
		switch {
		case p.SpeaksOfWindow && p.NamesKnob:
			mark = "окно+ручка"
		case p.SpeaksOfWindow:
			mark = "окно без ручки"
		}
		named = append(named, p.Owner+" "+p.Rel+" ("+mark+")")
	}
	var defaults []string
	for _, o := range c.Owners {
		defaults = append(defaults, o.Name+"="+c.Defaults[o.Name]+"с")
	}
	t.Logf("перепись: владельцев окна %d (умолчания: %s) · страниц прочитано %d · "+
		"относящихся к окну %d\n         говорят об окне %d · называют ручку %d · "+
		"величина совпала %d\n         %s",
		len(c.Owners), strings.Join(defaults, ", "), c.PagesRead, len(c.Pages),
		c.SpeakingOf, c.NamingKnob, c.Agreeing, strings.Join(named, "; "))

	// ── ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Пустой обход обесценивает вердикт.
	if len(c.Owners) == 0 {
		t.Fatal("владельцев окна не объявлено ни одного — предмета у гейта нет")
	}
	if c.PagesRead == 0 {
		t.Fatal("страниц владельцев не прочитано ни одной — вердикт беспредметен")
	}
	if len(c.Pages) == 0 {
		t.Fatal("ни одна страница не говорит об окне authz-видимости и не называет " +
			"ручку — распознаватель перестал видеть предмет (п.7 §«Гейт на класс»)")
	}
	if c.SpeakingOf == 0 {
		t.Fatal("ни одна страница не отправляет клиента ждать authz-видимость — " +
			"маркеры распознавания пережили свой предмет")
	}
	// Каждый объявленный владелец обязан быть ПРЕДСТАВЛЕН в обходе: владелец без
	// единой прочитанной страницы — слепая зона, неотличимая от исправного.
	seen := map[string]bool{}
	for _, p := range c.Pages {
		seen[p.Owner] = true
	}
	for _, o := range c.Owners {
		if !seen[o.Name] {
			t.Errorf("у владельца %s не найдено ни одной страницы об окне — "+
				"каталог %s либо пуст, либо маркеры его формы записи неизвестны",
				o.Name, o.DocsDir)
		}
	}

	for _, f := range authzWindowFindings(c) {
		t.Errorf("окно authz-видимости: %s", f)
	}
}
