// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что оба судьи гейта линта СПОСОБНЫ упасть, и что
// распознаватель вызова знает ВСЕ формы, в которых предмет записывается.
//
// # Прогонов ТРИ, а не два
//
//	контроль          — всё цело: молчат ОБА судьи (производитель и цепочки);
//	инъекция нового   — снято РОВНО новое свойство: краснеет ТОЛЬКО он;
//	инъекция старого  — снято существующее: краснеет ТОЛЬКО существующий судья.
//
// Третий прогон обязателен: без него молчание существующего контроля неотличимо
// от его мёртвости (`testing.md` §«Гейт на класс», п. 2в).
//
// # Отдельно — формы записи предмета (п. 7)
//
// Распознаватель вызова обязан знать все законные формы и не считать вызовом то,
// что им не является. Здесь их три, и все три встречались в дереве:
//
//	`npm run lint`     — вызов;
//	`npm run lint:js`  — НЕ вызов `lint`: подстрока совпадает, предмет другой;
//	то же в комментарии — НЕ вызов: замер в день заведения дал два таких
//	                     совпадения в ui.yml, и оба были комментариями.
//
// Форма, о которой распознаватель не знает, не даёт ни красного, ни зелёного —
// она молчит; поэтому каждая проверяется отдельным утверждением.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические входы — настоящий состав дерева, а не выдумка: десять пакетов
// объявляют `lint`, девять из них — общую цепочку (у `e2e` нет ни `lint:js`,
// ни `lint:css`, и его `lint` по природе другой).
// ─────────────────────────────────────────────────────────────────────────────

var consoleLintingPkgs = []string{
	"compute", "dashboard", "e2e", "host", "iam", "nlb",
	"registry", "storage", "system", "vpc",
}

const consoleLintChain = "npm run lint:js && npm run lint:css && npm run typecheck"

// consoleUniformChains — девять согласных цепочек.
func consoleUniformChains() map[string]string {
	m := map[string]string{}
	for _, p := range consoleLintingPkgs {
		if p == "e2e" {
			continue
		}
		m[p] = consoleLintChain
	}
	return m
}

// TestConsoleLintGateControl — ПРОГОН 1: всё цело, молчат оба судьи.
func TestConsoleLintGateControl(t *testing.T) {
	if f := judgeConsoleLintProducers(consoleLintingPkgs, consoleLintingPkgs); len(f) != 0 {
		t.Errorf("судья ПРОИЗВОДИТЕЛЯ краснеет на целом входе — он ловит форму, "+
			"а не существо: %v", f)
	}
	if f := judgeConsoleLintChains(consoleUniformChains()); len(f) != 0 {
		t.Errorf("судья ЦЕПОЧЕК краснеет на целом входе: %v", f)
	}
	// Существующий соседний контроль обязан молчать на своём целом входе — иначе
	// его молчание в прогонах ниже неотличимо от мёртвости.
	if f := judgeConsoleFormatProducers(consoleFormattedPkgs, consoleFormattedPkgs); len(f) != 0 {
		t.Errorf("СУЩЕСТВУЮЩИЙ судья формата краснеет на целом входе: %v", f)
	}
}

// TestConsoleLintProducerGateFailsOnAnUnproducedScript — ПРОГОН 2а.
func TestConsoleLintProducerGateFailsOnAnUnproducedScript(t *testing.T) {
	// Ровно то состояние ствола, ради которого гейт заведён: объявляют десять,
	// зовёт никто.
	f := judgeConsoleLintProducers(consoleLintingPkgs, nil)
	if len(f) != len(consoleLintingPkgs) {
		t.Fatalf("судья производителя обязан назвать КАЖДЫЙ непроизведённый пакет, "+
			"назвал %d из %d: %v", len(f), len(consoleLintingPkgs), f)
	}
	for _, p := range consoleLintingPkgs {
		if !strings.Contains(strings.Join(f, "\n"), "ui-future/"+p+" объявляет") {
			t.Errorf("находка не называет ui-future/%s — читатель пойдёт искать не там", p)
		}
	}
	// Инъекция обязана ронять ТОЛЬКО проверяемое: судья цепочек её не замечает.
	if g := judgeConsoleLintChains(consoleUniformChains()); len(g) != 0 {
		t.Errorf("судья ЦЕПОЧЕК покраснел на инъекции ПРОИЗВОДИТЕЛЯ — красное пришло "+
			"бы от соседа, и вакуумность нового судьи осталась бы незамеченной: %v", g)
	}
}

// TestConsoleLintProducerGateFailsOnACallWithoutADeclaration — ПРОГОН 2б.
func TestConsoleLintProducerGateFailsOnACallWithoutADeclaration(t *testing.T) {
	// Обратная сторона шва: корневой скрипт зовёт пакет, у которого скрипта нет.
	f := judgeConsoleLintProducers(consoleLintingPkgs, append(consoleLintingPkgs, "shared"))
	if len(f) != 1 || !strings.Contains(f[0], "ui-future/shared") {
		t.Fatalf("судья обязан назвать вызов без объявления, дал: %v", f)
	}
}

// TestConsoleLintChainGateFailsOnADivergedChain — ПРОГОН 2в.
func TestConsoleLintChainGateFailsOnADivergedChain(t *testing.T) {
	chains := consoleUniformChains()
	// Правка ОДНОГО объявления — та самая форма, которой цепочки разошлись бы молча.
	chains["nlb"] = "npm run lint:js && npm run typecheck"

	f := judgeConsoleLintChains(chains)
	if len(f) != 1 {
		t.Fatalf("судья цепочек обязан дать РОВНО одну находку на одном отклонившемся, "+
			"дал %d: %v", len(f), f)
	}
	if !strings.Contains(f[0], "ui-future/nlb") {
		t.Errorf("находка не называет отклонившийся пакет: %s", f[0])
	}
	if !strings.Contains(f[0], "host") {
		t.Errorf("находка не называет большинство, с которым сверяться: %s", f[0])
	}
	// Тот же контроль в обратную сторону: судья производителя молчит.
	if g := judgeConsoleLintProducers(consoleLintingPkgs, consoleLintingPkgs); len(g) != 0 {
		t.Errorf("судья ПРОИЗВОДИТЕЛЯ покраснел на инъекции ЦЕПОЧЕК: %v", g)
	}
}

// TestConsoleLintGateLeavesTheExistingControlAlone — ПРОГОН 3.
//
// Снимаем СУЩЕСТВУЮЩЕЕ свойство (производителя формата) и требуем, чтобы
// покраснел ТОЛЬКО существующий судья. Без этого прогона его молчание выше
// неотличимо от того, что он мёртв.
func TestConsoleLintGateLeavesTheExistingControlAlone(t *testing.T) {
	if f := judgeConsoleFormatProducers(consoleFormattedPkgs, nil); len(f) == 0 {
		t.Fatal("СУЩЕСТВУЮЩИЙ судья формата молчит на снятом производителе — он мёртв, " +
			"и его молчание в прогонах выше ничего не доказывало")
	}
	if g := judgeConsoleLintProducers(consoleLintingPkgs, consoleLintingPkgs); len(g) != 0 {
		t.Errorf("новый судья покраснел на инъекции в СОСЕДНЕЕ свойство: %v", g)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Формы записи предмета (п. 7): распознаватель вызова.
// ─────────────────────────────────────────────────────────────────────────────

// TestConsoleWorkflowCallDetectorKnowsEveryForm — все три формы, каждая отдельно.
func TestConsoleWorkflowCallDetectorKnowsEveryForm(t *testing.T) {
	for _, c := range []struct {
		name   string
		body   string
		script string
		want   bool
	}{
		{"голый вызов", "npm run lint\n", "lint", true},
		{"вызов с аргументами", "npm run lint --prefix host\n", "lint", true},
		{"часть цепочки — НЕ вызов цепочки", "npm run lint:js\n", "lint", false},
		{"вторая часть — НЕ вызов цепочки", "npm run lint:css\n", "lint", false},
		{"дефис в имени соседа", "npm run lint-staged\n", "lint", false},
		{"скрипт с двоеточием находится", "npm run format:check\n", "format:check", true},
		{"соседний скрипт находится", "npm run typecheck\n", "typecheck", true},
	} {
		if got := consoleWorkflowCallsScript(c.body, c.script); got != c.want {
			t.Errorf("%s: распознаватель дал %v на теле %q для скрипта %q, ожидалось %v — "+
				"форма, о которой он не знает, не даёт ни красного, ни зелёного",
				c.name, got, c.body, c.script, c.want)
		}
	}
}

// TestConsoleWorkflowCommentIsNotACall — комментарий вызовом не является.
//
// Именно эта форма делала предикат по сырому тексту ложным: в день заведения
// ui.yml нёс два совпадения `npm run lint:js`, и оба были комментариями.
func TestConsoleWorkflowCommentIsNotACall(t *testing.T) {
	commented := "# здесь объясняется, зачем нужен npm run lint\n" +
		"        # и почему его звал бы npm run lint\n"
	if consoleWorkflowCallsScript(shellExecutablePart(commented), "lint") {
		t.Error("комментарий засчитан вызовом — гейт остался бы зелёным при снятом " +
			"шаге, то есть ровно на том дереве, ради которого заведён")
	}

	// Законный близнец: тот же комментарий рядом с НАСТОЯЩИМ вызовом обязан
	// оставлять гейт зелёным — иначе он ловил бы форму, а не существо.
	withCall := commented + "        npm run lint\n"
	if !consoleWorkflowCallsScript(shellExecutablePart(withCall), "lint") {
		t.Error("настоящий вызов рядом с комментарием не распознан — гейт краснел бы " +
			"на исправном дереве, и его сняли бы первым")
	}
}
