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
		mark := ""
		switch {
		case p.SpeaksOfWindow && p.NamesKnob:
			mark = "окно+ручка"
		case p.SpeaksOfWindow:
			mark = "окно без ручки"
		default:
			mark = "ручка"
		}
		named = append(named, p.Rel+" ("+mark+")")
	}
	t.Logf("перепись: страниц края прочитано %d · относящихся к окну %d · "+
		"говорят об окне %d · называют ручку %d · величина совпала %d · умолчание %q\n         %s",
		c.PagesRead, len(c.Pages), c.SpeakingOf, c.NamingKnob, c.Agreeing,
		c.DefaultValue, strings.Join(named, "; "))

	// ── ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Пустой обход обесценивает вердикт.
	if c.PagesRead == 0 {
		t.Fatal("страниц края не прочитано ни одной — вердикт беспредметен")
	}
	if c.DefaultValue == "" {
		t.Fatalf("умолчание %s не выведено из %s — сверять названное не с чем",
			ct2AuthzCacheKnob, ct2GatewayConfigFile)
	}
	if len(c.Pages) == 0 {
		t.Fatal("ни одна страница края не говорит об окне authz-видимости и не " +
			"называет ручку — распознаватель перестал видеть предмет " +
			"(см. п.7 §«Гейт на класс»)")
	}
	if c.SpeakingOf == 0 {
		t.Fatal("ни одна страница края не отправляет клиента ждать authz-видимость — " +
			"маркеры распознавания пережили свой предмет")
	}

	for _, f := range authzWindowFindings(c) {
		t.Errorf("окно authz-видимости на странице края: %s", f)
	}
}
