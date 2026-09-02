// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что оба судьи гейта stylelint СПОСОБНЫ упасть и смолчать.
//
// # Прогонов ТРИ, а не два
//
//	контроль          — судья один и объявлен всеми: молчат ОБА судьи;
//	инъекция нового   — внесён ВТОРОЙ вариант: краснеет ТОЛЬКО судья единственности;
//	инъекция старого  — снят предмет послабления: краснеет ТОЛЬКО судья истечения.
//
// Третий прогон обязателен: без него молчание судьи истечения неотличимо от его
// мёртвости (`testing.md` §«Гейт на класс», п. 2в).
//
// # Инъекция вносится НАСТОЯЩЕЙ формой
//
// Второй вариант — не выдумка: это дословно тот судья, который до #1801 несли
// `host` и `dashboard` (без послабления под at-правила фреймворка и с тремя
// невыключенными правилами). Дефект, внесённый формой, которой в дереве не
// бывает, доказывал бы способность падать на том, чего не случается.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические входы — настоящий состав дерева и настоящие два варианта судьи.
// ─────────────────────────────────────────────────────────────────────────────

// consoleStyledPkgs — девять модулей, объявляющих линт стилей.
var consoleStyledPkgs = []string{
	"compute", "dashboard", "host", "iam", "nlb", "registry", "storage", "system", "vpc",
}

// consoleUnifiedStylelint — сведённый судья: строгий плюс послабление под
// at-правила фреймворка.
const consoleUnifiedStylelint = `{
  "extends": ["stylelint-config-standard"],
  "rules": {
    "alpha-value-notation": "number",
    "at-rule-no-unknown": [true, { "ignoreAtRules": ["apply", "config", "layer", "screen", "tailwind"] }],
    "color-function-notation": "modern",
    "declaration-empty-line-before": null,
    "selector-class-pattern": null
  }
}`

// consoleLegacyStrictStylelint — судья, который до #1801 несли host и dashboard.
const consoleLegacyStrictStylelint = `{
  "extends": ["stylelint-config-standard"],
  "rules": {
    "alpha-value-notation": "number",
    "color-function-notation": "modern",
    "declaration-empty-line-before": null,
    "selector-class-pattern": null
  }
}`

// consoleUniformStylelintConfigs — девять согласных судей.
func consoleUniformStylelintConfigs() map[string]string {
	m := map[string]string{}
	for _, p := range consoleStyledPkgs {
		m[p] = consoleUnifiedStylelint
	}
	return m
}

// TestConsoleStylelintGateControl — ПРОГОН 1: всё цело, молчат оба судьи.
func TestConsoleStylelintGateControl(t *testing.T) {
	if f := judgeConsoleStylelintConfigs(consoleStyledPkgs, consoleUniformStylelintConfigs()); len(f) != 0 {
		t.Errorf("судья ЕДИНСТВЕННОСТИ краснеет на целом входе — он ловит форму, "+
			"а не существо: %v", f)
	}
	if !consoleStylelintIgnoresTailwind(t, "control", []byte(consoleUnifiedStylelint)) {
		t.Error("распознаватель послабления НЕ видит его в сведённом судье — судья " +
			"истечения молчал бы всегда, то есть был бы мёртв")
	}
}

// TestConsoleStylelintGateFailsOnASecondJudge — ПРОГОН 2: внесён второй вариант.
func TestConsoleStylelintGateFailsOnASecondJudge(t *testing.T) {
	cfgs := consoleUniformStylelintConfigs()
	cfgs["host"] = consoleLegacyStrictStylelint
	cfgs["dashboard"] = consoleLegacyStrictStylelint

	f := judgeConsoleStylelintConfigs(consoleStyledPkgs, cfgs)
	if len(f) != 1 {
		t.Fatalf("судья обязан дать РОВНО одну находку на одном отклонившемся "+
			"варианте, дал %d: %v", len(f), f)
	}
	for _, want := range []string{"ui-future/dashboard", "ui-future/host"} {
		if !strings.Contains(f[0], want) {
			t.Errorf("находка не называет %s — читатель пойдёт править не тот файл: %s", want, f[0])
		}
	}
	if !strings.Contains(f[0], "vpc") {
		t.Errorf("находка не называет большинство, с которым сверяться: %s", f[0])
	}

	// Инъекция обязана ронять ТОЛЬКО проверяемое: у послабления предмет на месте.
	if !consoleStylelintIgnoresTailwind(t, "vpc", []byte(consoleUnifiedStylelint)) {
		t.Error("судья ИСТЕЧЕНИЯ задет инъекцией в единственность — красное пришло бы " +
			"от соседа")
	}
}

// TestConsoleStylelintGateFailsOnAJudgeWithoutACommand — обратная сторона шва.
func TestConsoleStylelintGateFailsOnAJudgeWithoutACommand(t *testing.T) {
	// Судья есть, команды нет: стили не читает ничто, а на вид проверка стоит.
	cfgs := consoleUniformStylelintConfigs()
	cfgs["shared"] = consoleUnifiedStylelint
	f := judgeConsoleStylelintConfigs(consoleStyledPkgs, cfgs)
	if len(f) != 1 || !strings.Contains(f[0], "ui-future/shared") {
		t.Fatalf("судья обязан назвать конфигурацию без объявленной команды, дал: %v", f)
	}

	// Команда есть, судьи нет: stylelint отвалится отказом.
	cfgs = consoleUniformStylelintConfigs()
	delete(cfgs, "nlb")
	f = judgeConsoleStylelintConfigs(consoleStyledPkgs, cfgs)
	if len(f) != 1 || !strings.Contains(f[0], "ui-future/nlb") {
		t.Fatalf("судья обязан назвать команду без конфигурации, дал: %v", f)
	}
}

// TestConsoleStylelintRelaxationDetectorSeesBothSides — ПРОГОН 3, форма предмета.
func TestConsoleStylelintRelaxationDetectorSeesBothSides(t *testing.T) {
	// Послабления нет — судья истечения не имеет к нему претензий by construction.
	if consoleStylelintIgnoresTailwind(t, "legacy", []byte(consoleLegacyStrictStylelint)) {
		t.Error("распознаватель нашёл послабление там, где его нет — судья истечения " +
			"краснел бы на дереве без предмета")
	}
	// Правило есть, но перечня at-правил фреймворка в нём нет — тоже не послабление.
	partial := `{"rules": {"at-rule-no-unknown": [true, {"ignoreAtRules": ["media"]}]}}`
	if consoleStylelintIgnoresTailwind(t, "partial", []byte(partial)) {
		t.Error("чужой перечень at-правил принят за послабление под фреймворк — " +
			"распознаватель судит форму, а не предмет")
	}
}

// TestConsoleTailwindSubjectDetectorReadsTheTree — предмет читается ИЗ ДЕРЕВА.
//
// Двумя независимыми признаками: файл настройки фреймворка и объявленная
// зависимость. Выписанный перечень модулей разошёлся бы с деревом молча.
func TestConsoleTailwindSubjectDetectorReadsTheTree(t *testing.T) {
	bare := t.TempDir()
	if consolePackageUsesTailwind(bare, consolePackageFacts{Name: "bare"}) {
		t.Error("предмет найден у пакета, который фреймворк не настраивает — " +
			"послабление никогда бы не истекло")
	}

	byDep := t.TempDir()
	if !consolePackageUsesTailwind(byDep, consolePackageFacts{
		Name: "byDep", DependencyNames: []string{"react", "tailwindcss"},
	}) {
		t.Error("объявленная зависимость не признана предметом — признак читается " +
			"только одним способом, и второй молчал бы")
	}

	byConfig := t.TempDir()
	if err := os.WriteFile(filepath.Join(byConfig, "tailwind.config.js"), []byte("export default {};\n"), 0o600); err != nil {
		t.Fatalf("не записана синтетическая настройка: %v", err)
	}
	if !consolePackageUsesTailwind(byConfig, consolePackageFacts{Name: "byConfig"}) {
		t.Error("файл настройки фреймворка не признан предметом")
	}
}
