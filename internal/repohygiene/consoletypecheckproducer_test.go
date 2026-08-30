// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoletypecheckproducer_test.go — у проверки типов каждого пакета консоли
// обязан быть производитель, и перечень производимого выводится ИЗ ДЕРЕВА.
//
// # Предмет — ЛАТЕНТНЫЙ, и это сказано первым
//
// Сегодня сломанного в дереве нет: пакетов со своим `tsconfig.json` одиннадцать,
// корневой скрипт `typecheck` называет одиннадцать `--prefix`, числа сходятся.
// Гейт заводится не против сегодняшнего расхождения, а против дня, когда заведут
// двенадцатый пакет: он не попадёт в цепочку, `tsc` по нему не пойдёт, и ни одна
// проверка этого не скажет — пакет окажется вне наблюдения МОЛЧА.
//
// Что это стоит, известно не по рассуждению: у пакета `e2e` не было `tsconfig`
// вовсе, поэтому его типы не проверял НИКТО. Линт снимает `no-undef` на файлах
// TypeScript, полагаясь на `tsc`, а `tsc` по пакету не ходил. Переименование
// помощника обновило два вызова из трёх, третий уехал в ствол, и первым читателем
// кода оказался браузер: проба умерла на `ReferenceError`, сожгла предел времени и
// вернула вердикт о продукте, которого не спрашивала.
//
// # Три шва, а не один
//
//	tsconfig.json в пакете  →  пакет объявляет `typecheck`  →  корневой скрипт зовёт  →  ui.yml зовёт корневой
//
// Первый шов — тот самый случай `e2e`: конфигурация проверки типов есть, а
// запускать её некому. Второй и третий — форма соседнего гейта `format:check`
// (consoleformatproducer_test.go), взятая целиком: предмет тот же, ось другая.
//
// # Сверка идёт в ОБЕ стороны намеренно
//
// Пакет, объявивший проверку и не названный корневым скриптом, выдаёт непроверку
// за проверку. Обратное — корневой скрипт зовёт пакет, у которого объявления
// нет — тише и потому хуже: `npm run` на несуществующем скрипте отвечает отказом,
// и падать это будет не там, где сломано. Односторонний гейт вдобавок зеленел бы
// на ПУСТОМ перечне, а пустой перечень означает «никого не проверяем».
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает, сколько
// каталогов осмотрел, у скольких свой `tsconfig.json`, сколько объявляют скрипт и
// сколько названо производителем. Пустой обход — провал.
package repohygiene

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// consoleTypecheckScript — имя объявляемого пакетом скрипта проверки типов.
const consoleTypecheckScript = "typecheck"

// consoleTypecheckCIInvocation — строка, которой конвейер зовёт корневой скрипт.
const consoleTypecheckCIInvocation = "npm run typecheck"

// TestEveryConsoleTypecheckHasAProducer — вердикт на настоящем дереве.
func TestEveryConsoleTypecheckHasAProducer(t *testing.T) {
	root := repoRoot(t)
	uiRoot := filepath.Join(root, "ui-future")

	facts := scanConsolePackages(t, uiRoot)
	if len(facts) == 0 {
		t.Fatal("обход ui-future не прочитал НИ ОДНОГО package.json — перепись " +
			"беспредметна, а молчание такого гейта неотличимо от согласия")
	}

	var withTSConfig, declaring []string
	for _, f := range facts {
		if f.HasTSConfig {
			withTSConfig = append(withTSConfig, f.Name)
		}
		if _, ok := f.Scripts[consoleTypecheckScript]; ok {
			declaring = append(declaring, f.Name)
		}
	}
	if len(withTSConfig) == 0 {
		t.Fatal("ни один пакет консоли не несёт своего tsconfig.json — либо обход " +
			"сломан, либо проверку типов сняли, не сняв этот гейт")
	}
	if len(declaring) == 0 {
		t.Fatal("ни один пакет консоли не объявляет " + consoleTypecheckScript +
			" — либо обход сломан, либо скрипт переименовали, не тронув гейт")
	}

	rootScript := consoleRootScript(t, uiRoot, consoleTypecheckScript)
	if rootScript == "" {
		t.Errorf("корневой ui-future/package.json не объявляет скрипт %q — у проверки "+
			"типов нет производителя вовсе: пакеты её объявляют, а зовёт её никто",
			consoleTypecheckScript)
	}

	var called []string
	for _, m := range consolePrefixCallRe.FindAllStringSubmatch(rootScript, -1) {
		called = append(called, m[1])
	}
	for _, f := range judgeConsoleTypecheckProducers(withTSConfig, declaring, called) {
		t.Errorf("%s", f)
	}

	if wf := consoleUIWorkflow(t, root); !strings.Contains(wf, consoleTypecheckCIInvocation) {
		t.Errorf("ui.yml не содержит %q — корневой скрипт есть, а конвейер его не зовёт: "+
			"проверка объявлена дважды и не исполняется ни разу", consoleTypecheckCIInvocation)
	}

	t.Logf("перепись: каталогов осмотрено — %d, со своим tsconfig.json — %d, "+
		"объявляют %s — %d, названо производителем — %d",
		len(facts), len(withTSConfig), consoleTypecheckScript, len(declaring), len(called))
}

// judgeConsoleTypecheckProducers — решающая часть, вынесенная из вердикта, чтобы
// её можно было проверить подставными входами, а не только зелёным деревом. На
// сегодняшнем дереве она молчит by construction (предмет латентный), поэтому
// способность падать доказывается инъекцией, а не прогоном по стволу.
func judgeConsoleTypecheckProducers(withTSConfig, declaring, called []string) []string {
	isCalled := map[string]bool{}
	for _, p := range called {
		isCalled[p] = true
	}
	declares := map[string]bool{}
	for _, p := range declaring {
		declares[p] = true
	}

	var findings []string
	for _, p := range withTSConfig {
		if !declares[p] {
			findings = append(findings,
				"пакет ui-future/"+p+" несёт свой tsconfig.json, но не объявляет скрипт "+
					consoleTypecheckScript+" — конфигурация проверки типов есть, а запускать "+
					"её некому: первым читателем такого кода окажется браузер")
		}
	}
	for _, p := range declaring {
		if !isCalled[p] {
			findings = append(findings,
				"пакет ui-future/"+p+" объявляет "+consoleTypecheckScript+", но корневой "+
					"скрипт его не зовёт — проверка объявлена и не исполняется, а её "+
					"краснота обнаружится только тем, кто позовёт команду руками")
		}
	}
	for _, p := range called {
		if !declares[p] {
			findings = append(findings,
				"корневой скрипт зовёт "+consoleTypecheckScript+" у ui-future/"+p+
					", но такого скрипта пакет не объявляет — вызов отвалится отказом npm, "+
					"и падать это будет не там, где сломано")
		}
	}
	sort.Strings(findings)
	return findings
}
