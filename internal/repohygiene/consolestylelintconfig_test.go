// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consolestylelintconfig_test.go — судья стилей у консоли ОДИН, и его послабление
// под фреймворк истекает вместе со своим предметом.
//
// # Предмет
//
// Конфигураций stylelint было ДВЕ на девять модулей (задача #1801), и решения об
// этом не принимал никто: обе приехали одним коммитом переезда в монорепо
// (`78b0c50f0`), то есть расхождение унаследовано, а не выбрано.
//
//	различных .stylelintrc.json .................... 2
//	строгий вариант несли ....................... host, dashboard
//	послабленный вариант несли .......... остальные семь
//
// Различие было НЕ случайным по форме — послабленный вариант ровно у тех семи,
// что настраивают Tailwind, — но случайным по последствиям: следующий, кто
// заводит модуль, копировал бы ОДИН ИЗ ДВУХ наугад, и какой именно, не решало
// ничто.
//
// # Почему сведено к строгому, а не к послабленному
//
// Свести к послабленному значило бы СНЯТЬ у двух модулей три действующих правила
// (`no-descending-specificity`, `rule-empty-line-before`,
// `declaration-block-single-line-max-declarations`) — то есть заплатить за
// единообразие проверкой. Свели к строгому: он никому ничего не снимает, а семи
// модулям правила ДОБАВЛЯЕТ.
//
// Цена измерена, а не предположена. Строгий вариант прогнан по CSS всех девяти
// модулей на живом stylelint: красным стал ОДИН файл на ОДНОМ правиле
// (`registry/src/index.css:37`, `rule-empty-line-before`, автопочинимо) — он и
// починен тем же изменением. Остальные восемь зелены.
//
// # Что осталось послаблением и почему это НЕ дрейф
//
// В сведённом судье оставлено ровно одно отступление от стандарта —
// `at-rule-no-unknown` с перечнем at-правил Tailwind. Оно оставлено ПЛАТФОРМЕННО,
// включая два модуля, которые Tailwind не настраивают: общий лист стилей
// (`shared/src/index.css`) написан на его at-правилах, модули импортируют его, и
// правило, запрещающее `@apply` в одном модуле и разрешающее в соседнем, — это
// ровно та развилка, которую запись и снимает.
//
// # Послабление ОБЯЗАНО истекать само
//
// Отступление живёт, пока у него есть предмет (`testing.md` §«Гейт на класс»,
// п. 5). Предмет здесь — Tailwind в дереве консоли. Второй судья ниже краснеет,
// когда судья перечисляет at-правила Tailwind, а Tailwind не настраивает НИ ОДИН
// модуль: тогда перечень прощает то, чего больше нет, и его снимают, а не носят
// вечно.
//
// Обоснование и условие пересмотра — `ui-future/docs/architecture/known-divergences.md`,
// §«Один судья стилей на девять модулей». Здесь оно не пересказывается: два
// места об одном предмете расходятся молча.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: оба судьи печатают
// объём осмотренного. Пустой обход — провал.
package repohygiene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// consoleStylelintConfigFile — имя файла судьи стилей.
const consoleStylelintConfigFile = ".stylelintrc.json"

// consoleTailwindAtRules — at-правила, ради которых держится послабление. Перечень
// — предпосылка второго судьи, поэтому стоит рядом с ним, а не выведен из файла.
var consoleTailwindAtRules = []string{"apply", "tailwind"}

// TestConsoleStylelintConfigIsSingle — судья стилей один на все модули.
func TestConsoleStylelintConfigIsSingle(t *testing.T) {
	root := repoRoot(t)
	uiRoot := filepath.Join(root, "ui-future")

	facts := scanConsolePackages(t, uiRoot)
	if len(facts) == 0 {
		t.Fatal("обход ui-future не прочитал НИ ОДНОГО package.json — перепись " +
			"беспредметна, а молчание такого гейта неотличимо от согласия")
	}

	var declaring []string
	configs := map[string]string{}
	for _, f := range facts {
		if _, ok := f.Scripts[consoleLintCSSScript]; ok {
			declaring = append(declaring, f.Name)
		}
		// #nosec G304 -- читается конфигурация пакета этого же репозитория.
		raw, err := os.ReadFile(filepath.Join(uiRoot, f.Name, consoleStylelintConfigFile))
		if err == nil {
			configs[f.Name] = string(raw)
		}
	}
	if len(declaring) == 0 {
		t.Fatal("ни один пакет консоли не объявляет " + consoleLintCSSScript +
			" — либо обход сломан, либо линт стилей сняли, не сняв этот гейт")
	}
	if len(configs) == 0 {
		t.Fatal("ни один пакет консоли не несёт " + consoleStylelintConfigFile +
			" — сравнивать нечего, а молчание такого гейта неотличимо от согласия")
	}

	for _, f := range judgeConsoleStylelintConfigs(declaring, configs) {
		t.Errorf("%s", f)
	}

	distinct := map[string]bool{}
	for _, c := range configs {
		distinct[c] = true
	}
	t.Logf("перепись: пакетов осмотрено — %d, объявляют %s — %d, несут %s — %d, "+
		"различных судей — %d", len(facts), consoleLintCSSScript, len(declaring),
		consoleStylelintConfigFile, len(configs), len(distinct))
}

// judgeConsoleStylelintConfigs — чистая часть: сверка идёт в ОБЕ стороны.
//
// Пакет, объявивший линт стилей без судьи, выдаёт непроверку за проверку: stylelint
// без конфигурации отвечает отказом, и падать это будет не там, где сломано. Судья
// без объявленной команды тише и потому хуже — он не исполняется никогда, а на вид
// стоит на месте.
func judgeConsoleStylelintConfigs(declaring []string, configs map[string]string) []string {
	declares := map[string]bool{}
	for _, p := range declaring {
		declares[p] = true
	}

	var findings []string
	for _, p := range declaring {
		if _, ok := configs[p]; !ok {
			findings = append(findings,
				"пакет ui-future/"+p+" объявляет "+consoleLintCSSScript+", но не несёт "+
					consoleStylelintConfigFile+" — команда объявлена и отвалится отказом "+
					"stylelint, а падать это будет не там, где сломано")
		}
	}
	byContent := map[string][]string{}
	for p, c := range configs {
		if !declares[p] {
			findings = append(findings,
				"пакет ui-future/"+p+" несёт "+consoleStylelintConfigFile+", но не объявляет "+
					consoleLintCSSScript+" — судья есть, и его не зовёт никто: стили этого "+
					"пакета не читает ничто, а на вид проверка стоит на месте")
		}
		byContent[c] = append(byContent[c], p)
	}

	if len(byContent) > 1 {
		type group struct {
			pkgs []string
		}
		var groups []group
		for _, p := range byContent {
			sort.Strings(p)
			groups = append(groups, group{pkgs: p})
		}
		sort.Slice(groups, func(i, j int) bool {
			if len(groups[i].pkgs) != len(groups[j].pkgs) {
				return len(groups[i].pkgs) > len(groups[j].pkgs)
			}
			return groups[i].pkgs[0] < groups[j].pkgs[0]
		})
		for _, g := range groups[1:] {
			findings = append(findings,
				"судья стилей у ui-future/"+strings.Join(g.pkgs, ", ui-future/")+
					" отличается от судьи большинства ("+strings.Join(groups[0].pkgs, ", ")+
					") — модули судятся РАЗНЫМ, и следующий, кто заведёт модуль, скопирует "+
					"один из вариантов наугад; решение о различии, если оно есть, обязано "+
					"стоять в ui-future/docs/architecture/known-divergences.md")
		}
	}
	sort.Strings(findings)
	return findings
}

// TestConsoleStylelintTailwindRelaxationHasASubject — послабление истекает само.
func TestConsoleStylelintTailwindRelaxationHasASubject(t *testing.T) {
	root := repoRoot(t)
	uiRoot := filepath.Join(root, "ui-future")

	facts := scanConsolePackages(t, uiRoot)
	if len(facts) == 0 {
		t.Fatal("обход ui-future не прочитал НИ ОДНОГО package.json — перепись " +
			"беспредметна, а молчание такого гейта неотличимо от согласия")
	}

	relaxing, withTailwind, read := 0, 0, 0
	for _, f := range facts {
		dir := filepath.Join(uiRoot, f.Name)
		// #nosec G304 -- читается конфигурация пакета этого же репозитория.
		raw, err := os.ReadFile(filepath.Join(dir, consoleStylelintConfigFile))
		if err == nil {
			read++
			if consoleStylelintIgnoresTailwind(t, f.Name, raw) {
				relaxing++
			}
		}
		if consolePackageUsesTailwind(dir, f) {
			withTailwind++
		}
	}
	if read == 0 {
		t.Fatal("ни один " + consoleStylelintConfigFile + " не прочитан — обход " +
			"беспредметен, а молчание такого гейта неотличимо от согласия")
	}

	if relaxing > 0 && withTailwind == 0 {
		t.Errorf("судья стилей прощает at-правила Tailwind (%d конфигураций), а Tailwind "+
			"не настраивает НИ ОДИН модуль консоли — послабление пережило свой предмет: "+
			"снимите перечень ignoreAtRules, а не носите его вечно", relaxing)
	}

	t.Logf("перепись: пакетов осмотрено — %d, конфигураций прочитано — %d, "+
		"прощают at-правила Tailwind — %d, настраивают Tailwind — %d",
		len(facts), read, relaxing, withTailwind)
}

// consoleStylelintIgnoresTailwind — прощает ли судья at-правила фреймворка.
func consoleStylelintIgnoresTailwind(t *testing.T, pkg string, raw []byte) bool {
	t.Helper()
	var cfg struct {
		Rules map[string]any `json:"rules"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatalf("не разобран %s пакета %s", consoleStylelintConfigFile, pkg)
	}
	opts, ok := cfg.Rules["at-rule-no-unknown"].([]any)
	if !ok || len(opts) < 2 {
		return false
	}
	obj, ok := opts[1].(map[string]any)
	if !ok {
		return false
	}
	listed, ok := obj["ignoreAtRules"].([]any)
	if !ok {
		return false
	}
	seen := map[string]bool{}
	for _, v := range listed {
		if s, ok := v.(string); ok {
			seen[s] = true
		}
	}
	for _, want := range consoleTailwindAtRules {
		if !seen[want] {
			return false
		}
	}
	return true
}

// consolePackageUsesTailwind — настраивает ли пакет фреймворк. Признак читается ИЗ
// ДЕРЕВА двумя независимыми способами: выписанный перечень модулей разошёлся бы с
// деревом молча.
func consolePackageUsesTailwind(dir string, f consolePackageFacts) bool {
	if _, err := os.Stat(filepath.Join(dir, "tailwind.config.js")); err == nil {
		return true
	}
	for _, name := range f.DependencyNames {
		if strings.HasPrefix(name, "tailwind") {
			return true
		}
	}
	return false
}
