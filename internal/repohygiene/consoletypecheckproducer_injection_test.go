// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «у проверки типов каждого пакета есть
// производитель» СПОСОБЕН упасть.
//
// # Здесь инъекция — ЕДИНСТВЕННЫЙ источник красного, и это надо сказать прямо
//
// Предмет гейта латентный: на сегодняшнем дереве числа сходятся (одиннадцать
// пакетов с `tsconfig.json`, одиннадцать `--prefix` в корневом скрипте), поэтому
// прогон по стволу молчит by construction. Пара RED→GREEN у такого гейта живёт
// не в дереве, а здесь: дефект вносится настоящей формой и обязан покраснеть,
// законный близнец обязан промолчать.
//
// # Прогонов ТРИ, а не два
//
//	контроль          — всё цело: молчат ОБА судьи (новый и существующий);
//	инъекция нового   — снято РОВНО новое свойство: краснеет ТОЛЬКО новый;
//	инъекция старого  — снято старое: краснеет ТОЛЬКО существующий.
//
// Третий прогон обязателен: без него молчание существующего контроля неотличимо
// от его мёртвости.
//
// Обе половины гоняют ТЕ ЖЕ функции (`judgeConsoleTypecheckProducers`,
// `judgeConsoleFormatProducers`), что и прогон по дереву.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические входы — настоящий состав дерева, а не выдумка: одиннадцать
// пакетов со своим tsconfig, из них девять несут ещё и `format:check`.
// ─────────────────────────────────────────────────────────────────────────────

// consoleTypecheckedPkgs — пакеты со своим tsconfig.json и скриптом typecheck.
var consoleTypecheckedPkgs = []string{
	"compute", "dashboard", "e2e", "host", "iam", "nlb",
	"registry", "shared", "storage", "system", "vpc",
}

// consoleFormattedPkgs — те же минус `shared` и `e2e`: форматтер судит девять.
// Расхождение двух перечней намеренное и служит контролем — гейт формата и гейт
// типов обязаны судить РАЗНЫЕ множества, иначе один из них лишний.
var consoleFormattedPkgs = []string{
	"compute", "dashboard", "host", "iam", "nlb", "registry", "storage", "system", "vpc",
}

// TestConsoleTypecheckGateControl — ПРОГОН 1: всё цело, молчат оба судьи.
func TestConsoleTypecheckGateControl(t *testing.T) {
	if f := judgeConsoleTypecheckProducers(
		consoleTypecheckedPkgs, consoleTypecheckedPkgs, consoleTypecheckedPkgs); len(f) != 0 {
		t.Errorf("НОВЫЙ судья краснеет на целом входе — он ловит форму, а не существо: %v", f)
	}
	if f := judgeConsoleFormatProducers(consoleFormattedPkgs, consoleFormattedPkgs); len(f) != 0 {
		t.Errorf("СУЩЕСТВУЮЩИЙ судья краснеет на целом входе: %v", f)
	}
}

// TestConsoleTypecheckGateInjectsTheNewProperty — ПРОГОН 2: снято РОВНО новое
// свойство, старое цело.
//
// Заведён двенадцатый пакет со своим `tsconfig.json` — тот самый день, ради
// которого гейт стоит, — и в корневую цепочку он не попал. Его `format:check`
// при этом в порядке, поэтому существующий гейт обязан молчать.
func TestConsoleTypecheckGateInjectsTheNewProperty(t *testing.T) {
	withTSConfig := append(append([]string{}, consoleTypecheckedPkgs...), "quota")
	declaring := append(append([]string{}, consoleTypecheckedPkgs...), "quota")

	findings := judgeConsoleTypecheckProducers(withTSConfig, declaring, consoleTypecheckedPkgs)
	if len(findings) == 0 {
		t.Fatal("НОВЫЙ судья промолчал на пакете, не попавшем в корневую цепочку, — " +
			"условие инъекции выглядит созданным и не создано")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "ui-future/quota") {
		t.Errorf("находка не называет координату, за которой идти: %v", findings)
	}

	// Существующий контроль обязан МОЛЧАТЬ: перечень format:check не тронут.
	if f := judgeConsoleFormatProducers(consoleFormattedPkgs, consoleFormattedPkgs); len(f) != 0 {
		t.Errorf("существующий гейт покраснел на инъекции, его не касающейся: %v", f)
	}
}

// TestConsoleTypecheckGateInjectsTheExistingProperty — ПРОГОН 3: снято СТАРОЕ
// свойство, новое цело.
//
// Проверка типов у всех одиннадцати на месте, но `registry` выпал из корневой
// цепочки `format:check`. Красное обязано прийти ТОЛЬКО от существующего гейта.
func TestConsoleTypecheckGateInjectsTheExistingProperty(t *testing.T) {
	var called []string
	for _, p := range consoleFormattedPkgs {
		if p == "registry" {
			continue
		}
		called = append(called, p)
	}

	findings := judgeConsoleFormatProducers(consoleFormattedPkgs, called)
	if len(findings) == 0 {
		t.Fatal("СУЩЕСТВУЮЩИЙ гейт промолчал на пакете, выпавшем из цепочки, — " +
			"значит его молчание в прогонах 1 и 2 ничего не доказывало")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "ui-future/registry") {
		t.Errorf("находка существующего гейта не называет координату: %v", findings)
	}

	// Новый гейт обязан МОЛЧАТЬ: оси типов инъекция не касалась.
	if f := judgeConsoleTypecheckProducers(
		consoleTypecheckedPkgs, consoleTypecheckedPkgs, consoleTypecheckedPkgs); len(f) != 0 {
		t.Errorf("новый гейт покраснел на инъекции, его не касающейся: %v", f)
	}
}

// TestConsoleTypecheckGateJudgesAllThreeSeams — каждый из трёх швов падает
// порознь, и законный близнец у каждого молчит.
//
// Односторонняя проба зеленела бы на ПУСТОМ перечне, а пустой перечень означает
// «типы не проверяет никто» — ровно то, ради чего гейт стоит.
func TestConsoleTypecheckGateJudgesAllThreeSeams(t *testing.T) {
	cases := []struct {
		name         string
		withTSConfig []string
		declaring    []string
		called       []string
		wantRed      bool
		wantSays     string
	}{
		{
			// Настоящий случай `e2e` до того, как у пакета завели tsconfig:
			// конфигурация есть, запускать её некому, и первым читателем кода
			// оказывается браузер.
			name:         "ДЕФЕКТ: tsconfig есть, скрипта typecheck нет",
			withTSConfig: consoleTypecheckedPkgs,
			declaring:    consoleWithout(consoleTypecheckedPkgs, "e2e"),
			called:       consoleWithout(consoleTypecheckedPkgs, "e2e"),
			wantRed:      true,
			wantSays:     "несёт свой tsconfig.json, но не объявляет скрипт typecheck",
		},
		{
			name:         "ДЕФЕКТ: скрипт объявлен, корневая цепочка его не зовёт",
			withTSConfig: consoleTypecheckedPkgs,
			declaring:    consoleTypecheckedPkgs,
			called:       consoleWithout(consoleTypecheckedPkgs, "shared"),
			wantRed:      true,
			wantSays:     "ui-future/shared объявляет typecheck, но корневой скрипт его не зовёт",
		},
		{
			// Тише и потому хуже: npm отвечает отказом, и падать это будет не
			// там, где сломано.
			name:         "ДЕФЕКТ: цепочка зовёт пакет, которого в дереве нет",
			withTSConfig: consoleTypecheckedPkgs,
			declaring:    consoleTypecheckedPkgs,
			called:       append(append([]string{}, consoleTypecheckedPkgs...), "quota"),
			wantRed:      true,
			wantSays:     "такого скрипта пакет не объявляет",
		},
		{
			name:         "БЛИЗНЕЦ: каталог без tsconfig и без скрипта — deploy, docs, scripts",
			withTSConfig: consoleTypecheckedPkgs,
			declaring:    consoleTypecheckedPkgs,
			called:       consoleTypecheckedPkgs,
			wantRed:      false,
		},
		{
			// Порядок в цепочке пишут вручную; перестановка не должна выводить
			// пакет из-под наблюдения.
			name:         "БЛИЗНЕЦ: та же цепочка в другом порядке",
			withTSConfig: consoleTypecheckedPkgs,
			declaring:    consoleTypecheckedPkgs,
			called:       consoleReversed(consoleTypecheckedPkgs),
			wantRed:      false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := judgeConsoleTypecheckProducers(c.withTSConfig, c.declaring, c.called)
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

// consoleWithout — перечень без названного элемента.
func consoleWithout(all []string, drop string) []string {
	var out []string
	for _, p := range all {
		if p != drop {
			out = append(out, p)
		}
	}
	return out
}

// consoleReversed — тот же перечень в обратном порядке.
func consoleReversed(all []string) []string {
	out := make([]string, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		out = append(out, all[i])
	}
	return out
}
