// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consolelintproducer_test.go — объявленная пакетом консоли команда линта обязана
// иметь производителя, и объявленные цепочки обязаны совпадать между пакетами.
//
// # Предмет
//
// Десять пакетов консоли объявляли скрипт `lint`, а звал его НИКТО (задача #1800):
//
//	производителей `lint` в дереве ................. 0
//	контроль тем же предикатом: `typecheck` ........ 1
//	контроль тем же предикатом: `format:check` ..... 1
//
// Это третий экземпляр одного класса подряд: `format:check` без производителя
// (#1653, расхождение набрало 22 файла в семи пакетах) и `typecheck` у пакета
// `e2e` (латентный, снят соседним гейтом). Здесь цена измерена так же, а не
// предположена: команда, которой пользуется разработчик, красна в `dashboard` —
// лишнее приведение типа в пробе, — и об этом не говорил ни один вердикт.
//
// # Почему `lint` НЕ покрывался соседями
//
// Цепочка `lint` = `lint:js && lint:css && typecheck`, и покрытие у трёх её
// частей РАЗНОЕ. Это надо сказать прямо, иначе гейт прочтут как дубль соседнего:
//
//	typecheck  — производитель ЕСТЬ (корневой скрипт + своя job)
//	lint:js    — ЧАСТИЧНО: scripts/check-lint-coverage.mjs доказывает, что команда
//	             ИСПОЛНИМА и покрывает файлы, но находки самого линта не судит —
//	             это сказано его же шапкой
//	lint:css   — производителей НОЛЬ, stylelint не запускался НИ РАЗУ
//
// То есть до этого гейта находки eslint по девяти приложениям и stylelint по всем
// не читал никто.
//
// # Гейт читает ИСПОЛНЯЕМУЮ часть конвейера, а не текст
//
// Предикат по подстроке здесь врёт дважды, и обе формы встречались:
//
//	«npm run lint:js» содержит «npm run lint» ....... ложное СОВПАДЕНИЕ
//	упоминание в комментарии шага .................. ложное совпадение
//
// Замер на дереве в день заведения: сырой текст ui.yml даёт два совпадения
// `npm run lint:js`, и ОБА — комментарии. Поэтому шаги разбираются как YAML, из
// тела шага снимаются строки-комментарии оболочки (общий `shellExecutablePart`),
// и совпадение требует ГРАНИЦЫ имени — иначе `lint` неотличим от `lint:js`.
//
// # Второй судья того же скрипта: цепочки обязаны совпадать
//
// Производитель отвечает на вопрос «зовут ли», и не отвечает на вопрос «одно ли
// зовут». Девять цепочек сегодня совпадают побайтово, но правка ОДНОГО
// объявления разведёт их молча — тот же класс, что измерен по копиям консоли
// (`ui.md` §«Незакрытый форк»), и та же форма, что у соседнего
// consoleformatterversion_test.go, сводящего судей формата к одному.
//
// Перечень сравниваемых ВЫВОДИТСЯ: цепочкой считается `lint` у пакета,
// объявляющего ОБЕ её части (`lint:js` и `lint:css`). Поэтому `e2e`, у которого
// нет ни одной из них и `lint` которого по природе другой (`eslint .`), в
// сравнение не попадает by construction — а не по выписанному исключению,
// которое разошлось бы с деревом молча.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: оба судьи печатают
// объём осмотренного. Пустой обход — провал.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// consoleLintScript — имя объявляемой пакетом команды линта.
const consoleLintScript = "lint"

// consoleLintJSScript / consoleLintCSSScript — части цепочки. Их наличие и есть
// признак «пакет объявляет ОБЩУЮ цепочку», по которому выводится перечень
// сравниваемых.
const (
	consoleLintJSScript  = "lint:js"
	consoleLintCSSScript = "lint:css"
)

// TestEveryConsoleLintHasAProducer — вердикт на настоящем дереве.
func TestEveryConsoleLintHasAProducer(t *testing.T) {
	root := repoRoot(t)
	uiRoot := filepath.Join(root, "ui-future")

	facts := scanConsolePackages(t, uiRoot)
	if len(facts) == 0 {
		t.Fatal("обход ui-future не прочитал НИ ОДНОГО package.json — перепись " +
			"беспредметна, а молчание такого гейта неотличимо от согласия")
	}

	var declaring []string
	for _, f := range facts {
		if _, ok := f.Scripts[consoleLintScript]; ok {
			declaring = append(declaring, f.Name)
		}
	}
	if len(declaring) == 0 {
		t.Fatal("ни один пакет консоли не объявляет " + consoleLintScript +
			" — либо обход сломан, либо скрипт переименовали, не тронув гейт")
	}

	rootScript := consoleRootScript(t, uiRoot, consoleLintScript)
	if rootScript == "" {
		t.Errorf("корневой ui-future/package.json не объявляет скрипт %q — у линта "+
			"нет производителя вовсе: пакеты его объявляют, а зовёт его никто",
			consoleLintScript)
	}

	var called []string
	for _, m := range consolePrefixCallRe.FindAllStringSubmatch(rootScript, -1) {
		called = append(called, m[1])
	}
	for _, f := range judgeConsoleLintProducers(declaring, called) {
		t.Errorf("%s", f)
	}

	exec := consoleUIWorkflowExecutable(t, root)
	if !consoleWorkflowCallsScript(exec, consoleLintScript) {
		t.Errorf("исполняемая часть ui.yml не зовёт %q — корневой скрипт есть, а "+
			"конвейер его не зовёт: проверка объявлена дважды и не исполняется ни разу",
			"npm run "+consoleLintScript)
	}

	t.Logf("перепись: пакетов осмотрено — %d, объявляют %s — %d, названо производителем — %d, "+
		"исполняемых шагов ui.yml прочитано — %d",
		len(facts), consoleLintScript, len(declaring), len(called),
		strings.Count(exec, consoleWorkflowStepSep))
}

// judgeConsoleLintProducers — решающая часть, вынесенная из вердикта, чтобы её
// можно было проверить подставными входами, а не только зелёным деревом.
func judgeConsoleLintProducers(declaring, called []string) []string {
	isCalled := map[string]bool{}
	for _, p := range called {
		isCalled[p] = true
	}
	declares := map[string]bool{}
	for _, p := range declaring {
		declares[p] = true
	}

	var findings []string
	for _, p := range declaring {
		if !isCalled[p] {
			findings = append(findings,
				"пакет ui-future/"+p+" объявляет "+consoleLintScript+", но корневой скрипт "+
					"его не зовёт — проверка объявлена и не исполняется, а её краснота "+
					"обнаружится только тем, кто позовёт команду руками")
		}
	}
	for _, p := range called {
		if !declares[p] {
			findings = append(findings,
				"корневой скрипт зовёт "+consoleLintScript+" у ui-future/"+p+
					", но такого скрипта пакет не объявляет — вызов отвалится отказом npm, "+
					"и падать это будет не там, где сломано")
		}
	}
	sort.Strings(findings)
	return findings
}

// TestConsoleLintChainsAgree — второй судья того же скрипта: одно ли зовут.
func TestConsoleLintChainsAgree(t *testing.T) {
	root := repoRoot(t)
	facts := scanConsolePackages(t, filepath.Join(root, "ui-future"))
	if len(facts) == 0 {
		t.Fatal("обход ui-future не прочитал НИ ОДНОГО package.json — перепись " +
			"беспредметна, а молчание такого гейта неотличимо от согласия")
	}

	chains := map[string]string{}
	for _, f := range facts {
		_, hasJS := f.Scripts[consoleLintJSScript]
		_, hasCSS := f.Scripts[consoleLintCSSScript]
		chain, hasChain := f.Scripts[consoleLintScript]
		if hasJS && hasCSS && hasChain {
			chains[f.Name] = chain
		}
	}
	if len(chains) == 0 {
		t.Fatal("ни один пакет консоли не объявляет обе части цепочки линта — " +
			"сравнивать нечего, а молчание такого гейта неотличимо от согласия")
	}

	for _, f := range judgeConsoleLintChains(chains) {
		t.Errorf("%s", f)
	}

	distinct := map[string]bool{}
	for _, c := range chains {
		distinct[c] = true
	}
	t.Logf("перепись: пакетов осмотрено — %d, объявляют общую цепочку — %d, "+
		"различных цепочек — %d", len(facts), len(chains), len(distinct))
}

// judgeConsoleLintChains — чистая часть: цепочки обязаны совпадать побайтово.
func judgeConsoleLintChains(chains map[string]string) []string {
	byChain := map[string][]string{}
	for pkg, chain := range chains {
		byChain[chain] = append(byChain[chain], pkg)
	}
	if len(byChain) <= 1 {
		return nil
	}

	// Большинство названо большинством, а не порядком чтения: находка обязана
	// указывать на отклонившегося, иначе читатель пойдёт править не тот файл.
	type group struct {
		chain string
		pkgs  []string
	}
	var groups []group
	for c, p := range byChain {
		sort.Strings(p)
		groups = append(groups, group{chain: c, pkgs: p})
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].pkgs) != len(groups[j].pkgs) {
			return len(groups[i].pkgs) > len(groups[j].pkgs)
		}
		return groups[i].chain < groups[j].chain
	})

	var findings []string
	for _, g := range groups[1:] {
		findings = append(findings,
			"цепочка линта у ui-future/"+strings.Join(g.pkgs, ", ui-future/")+" — "+
				"«"+g.chain+"»"+", а у большинства ("+
				strings.Join(groups[0].pkgs, ", ")+") — "+"«"+groups[0].chain+"»"+
				": одна команда судит разное, и разойтись они могли молча")
	}
	sort.Strings(findings)
	return findings
}

// consoleWorkflowStepSep — разделитель тел шагов в склейке исполняемой части.
// Служит и единицей счёта переписи: «сколько шагов прочитано».
const consoleWorkflowStepSep = "\x00"

// consoleUIWorkflowDoc — то немногое из ui.yml, что нужно этим гейтам.
type consoleUIWorkflowDoc struct {
	Jobs map[string]struct {
		Steps []struct {
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// consoleUIWorkflowExecutable — склейка ИСПОЛНЯЕМЫХ тел шагов ui.yml.
//
// Читается разобранный YAML, а не сырой текст: имя скрипта встречается и в
// комментариях (замер в шапке — два совпадения, оба комментарии), и предикат по
// подстроке остался бы зелёным при снятом вызове. Строки-комментарии оболочки
// снимаются общим `shellExecutablePart`.
func consoleUIWorkflowExecutable(t *testing.T, root string) string {
	t.Helper()
	// #nosec G304 -- читается объявление процесса этого же репозитория.
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ui.yml"))
	if err != nil {
		t.Fatalf("не прочитан ui.yml: %v", err)
	}
	var doc consoleUIWorkflowDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("не разобран ui.yml: %v", err)
	}
	var b strings.Builder
	steps := 0
	for _, job := range doc.Jobs {
		for _, s := range job.Steps {
			if strings.TrimSpace(s.Run) == "" {
				continue
			}
			b.WriteString(shellExecutablePart(s.Run))
			b.WriteString(consoleWorkflowStepSep)
			steps++
		}
	}
	if steps == 0 {
		t.Fatal("в ui.yml не прочитано НИ ОДНОГО исполняемого шага — обход " +
			"беспредметен, а молчание такого гейта неотличимо от согласия")
	}
	return b.String()
}

// consoleWorkflowCallsScript — зовёт ли исполняемая часть ИМЕННО этот скрипт.
//
// Граница имени обязательна: без неё `npm run lint:js` считается вызовом
// `npm run lint`, то есть гейт зеленел бы ровно на том дереве, ради которого
// заведён.
func consoleWorkflowCallsScript(exec, script string) bool {
	return regexp.MustCompile(`npm run ` + regexp.QuoteMeta(script) + `([^:\w-]|$)`).
		MatchString(exec)
}
