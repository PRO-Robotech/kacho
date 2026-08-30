// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «формат консоли судит ОДНА версия» СПОСОБЕН
// упасть — и что падает он на существе, а не на форме.
//
// # Прогонов ТРИ, а не два
//
//	контроль          — дерево цело: молчат ОБА судьи (новый и существующий);
//	инъекция нового   — у пакета снята ТОЧНОСТЬ версии, всё остальное на месте:
//	                    краснеет ТОЛЬКО новый;
//	инъекция старого  — версии в порядке, но пакет не назван производителем:
//	                    краснеет ТОЛЬКО существующий.
//
// Третий прогон обязателен. Без него молчание существующего контроля в первых двух
// неотличимо от его мёртвости: гейт, потерявший способность краснеть, на целом
// входе выглядит ровно так же, как исправный.
//
// # Почему инъекция снимает НОВОЕ свойство, а не заводит новый элемент
//
// Инъекция вида «добавить ещё один пакет» нарушает всё, что требуется от пакетов
// вообще, — и красное пришло бы от соседа, а новый гейт мог бы оказаться
// вакуумным, не показав этого ничем. Поэтому каждая инъекция берёт элемент,
// у которого СТАРОЕ свойство цело, и снимает у него РОВНО ОДНО новое.
//
// Обе половины гоняют ТЕ ЖЕ функции (`judgeConsoleFormatterVersions`,
// `judgeConsoleFormatProducers`), что и прогон по дереву: проба, повторяющая
// логику гейта своей копией, доказывала бы свойство копии.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические входы. Значения — настоящие формы из этого дерева, а не выдумка:
// девять пакетов, объявлявших `prettier`, и три версии, которые их замки
// закрепляли до сведения (3.8.3 · 3.9.4 · 3.9.6).
// ─────────────────────────────────────────────────────────────────────────────

// consoleFormatterJudgedPkgs — пакеты консоли, объявляющие форматтер.
var consoleFormatterJudgedPkgs = []string{
	"compute", "dashboard", "host", "iam", "nlb", "registry", "storage", "system", "vpc",
}

// consoleOneJudgeDeclared — канон: все девять называют одну точную версию.
func consoleOneJudgeDeclared() map[string]string {
	out := map[string]string{}
	for _, p := range consoleFormatterJudgedPkgs {
		out[p] = "3.9.6"
	}
	return out
}

// consoleOneJudgeLocked — канон: каждый замок закрепляет её же. Корневой замок
// назван точкой — он судит членов workspace, своего замка у тех не бывает.
func consoleOneJudgeLocked() map[string][]string {
	return map[string][]string{
		".":         {"3.9.6"},
		"host":      {"3.9.6"},
		"dashboard": {"3.9.6"},
		"compute":   {"3.9.6"},
		"storage":   {"3.9.6"},
		"nlb":       {"3.9.6"},
		"registry":  {"3.9.6"},
	}
}

// TestConsoleFormatterVersionGateControl — ПРОГОН 1: всё цело, молчат оба судьи.
//
// Без этого прогона краснота двух следующих ничего не значила бы: гейт,
// краснеющий на любом входе, свойства не измеряет.
func TestConsoleFormatterVersionGateControl(t *testing.T) {
	if f := judgeConsoleFormatterVersions(consoleOneJudgeDeclared(), consoleOneJudgeLocked()); len(f) != 0 {
		t.Errorf("НОВЫЙ судья краснеет на целом входе — он ловит форму, а не существо: %v", f)
	}
	if f := judgeConsoleFormatProducers(consoleFormatterJudgedPkgs, consoleFormatterJudgedPkgs); len(f) != 0 {
		t.Errorf("СУЩЕСТВУЮЩИЙ судья краснеет на целом входе: %v", f)
	}
}

// TestConsoleFormatterVersionGateInjectsTheNewProperty — ПРОГОН 2: снято РОВНО
// новое свойство, старое цело.
//
// У пакета `host` версия возвращена к диапазону `^3.8.3` — ровно к тому, что
// стояло в дереве до сведения, — а производителем он по-прежнему назван. Красное
// обязано прийти ТОЛЬКО от нового гейта.
func TestConsoleFormatterVersionGateInjectsTheNewProperty(t *testing.T) {
	declared := consoleOneJudgeDeclared()
	declared["host"] = "^3.8.3"

	findings := judgeConsoleFormatterVersions(declared, consoleOneJudgeLocked())
	if len(findings) == 0 {
		t.Fatal("НОВЫЙ судья промолчал на диапазоне вместо точной версии — условие " +
			"инъекции выглядит созданным и не создано")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "ui-future/host") {
		t.Errorf("находка не называет координату, за которой идти: %v", findings)
	}

	// Существующий контроль обязан МОЛЧАТЬ: его предмет не тронут.
	if f := judgeConsoleFormatProducers(consoleFormatterJudgedPkgs, consoleFormatterJudgedPkgs); len(f) != 0 {
		t.Errorf("существующий гейт покраснел на инъекции, его не касающейся, — "+
			"красное пришло бы от соседа, и новый гейт мог бы оказаться вакуумным: %v", f)
	}
}

// TestConsoleFormatterVersionGateInjectsTheExistingProperty — ПРОГОН 3: снято
// СТАРОЕ свойство, новое цело.
//
// Версии сведены и точны, но `host` выпал из корневой цепочки `format:check`.
// Красное обязано прийти ТОЛЬКО от существующего гейта — иначе его молчание в
// прогонах 1 и 2 доказывало бы не исправность, а смерть.
func TestConsoleFormatterVersionGateInjectsTheExistingProperty(t *testing.T) {
	var called []string
	for _, p := range consoleFormatterJudgedPkgs {
		if p == "host" {
			continue
		}
		called = append(called, p)
	}

	findings := judgeConsoleFormatProducers(consoleFormatterJudgedPkgs, called)
	if len(findings) == 0 {
		t.Fatal("СУЩЕСТВУЮЩИЙ гейт промолчал на пакете, выпавшем из цепочки, — " +
			"значит его молчание в прогонах 1 и 2 ничего не доказывало")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "ui-future/host") {
		t.Errorf("находка существующего гейта не называет координату: %v", findings)
	}

	// Новый гейт обязан МОЛЧАТЬ: версии не тронуты.
	if f := judgeConsoleFormatterVersions(consoleOneJudgeDeclared(), consoleOneJudgeLocked()); len(f) != 0 {
		t.Errorf("новый гейт покраснел на инъекции, его не касающейся: %v", f)
	}
}

// TestConsoleFormatterVersionGateSeparatesTheTwoSides — обе стороны шва падают
// порознь, и законный близнец у каждой молчит.
//
// Гейт судит ДВЕ вещи: объявление (точное и одно) и замок (разрешает объявленное).
// Проба, проверившая одну, оставила бы вторую недоказанной — а именно вторая
// ловит сегодняшнее расхождение дерева.
func TestConsoleFormatterVersionGateSeparatesTheTwoSides(t *testing.T) {
	cases := []struct {
		name     string
		declared map[string]string
		locked   map[string][]string
		wantRed  bool
		wantSays string
	}{
		{
			name:     "ДЕФЕКТ: замок разошёлся с объявлением — судит замок",
			declared: consoleOneJudgeDeclared(),
			locked: func() map[string][]string {
				l := consoleOneJudgeLocked()
				l["compute"] = []string{"3.9.4"}
				return l
			}(),
			wantRed:  true,
			wantSays: "ui-future/compute/package-lock.json",
		},
		{
			name: "ДЕФЕКТ: две точные версии — судей столько же, сколько версий",
			declared: func() map[string]string {
				d := consoleOneJudgeDeclared()
				d["dashboard"] = "3.8.3"
				return d
			}(),
			locked:   consoleOneJudgeLocked(),
			wantRed:  true,
			wantSays: "3.8.3, 3.9.6",
		},
		{
			name:     "ДЕФЕКТ: корневой замок несёт чужую версию — четыре пакета вне наблюдения",
			declared: consoleOneJudgeDeclared(),
			locked: func() map[string][]string {
				l := consoleOneJudgeLocked()
				l["."] = []string{"3.9.4"}
				return l
			}(),
			wantRed:  true,
			wantSays: "ui-future/package-lock.json",
		},
		{
			name:     "БЛИЗНЕЦ: предрелизная версия — точная, а не диапазон",
			declared: consolePinnedAt("3.9.6-rc.1"),
			locked:   map[string][]string{".": {"3.9.6-rc.1"}},
			wantRed:  false,
		},
		{
			name:     "БЛИЗНЕЦ: замок без форматтера вовсе — у e2e его нет by construction",
			declared: consoleOneJudgeDeclared(),
			locked:   map[string][]string{".": {"3.9.6"}},
			wantRed:  false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := judgeConsoleFormatterVersions(c.declared, c.locked)
			if c.wantRed && len(got) == 0 {
				t.Fatal("гейт промолчал на дефекте — условие инъекции не создано")
			}
			if !c.wantRed && len(got) != 0 {
				t.Fatalf("гейт покраснел на законной форме: %v", got)
			}
			if c.wantSays != "" && !strings.Contains(strings.Join(got, "\n"), c.wantSays) {
				t.Errorf("находка не называет %q: %v", c.wantSays, got)
			}
		})
	}
}

// consolePinnedAt — девять пакетов, закреплённых на одной названной версии.
func consolePinnedAt(version string) map[string]string {
	out := map[string]string{}
	for _, p := range consoleFormatterJudgedPkgs {
		out[p] = version
	}
	return out
}
