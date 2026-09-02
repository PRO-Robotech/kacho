// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_step_survives_kubernetes_expansion_injection_test.go — доказательство
// того, что гейт подстановки Kubernetes СПОСОБЕН упасть и падает ТОЛЬКО на своём
// предмете.
//
// Прогонов ПЯТЬ, и последние два несущие:
//
//  1. контроль — нетронутая копия молчит;
//  2. инъекция НАСТОЯЩИМ дефектом — удвоенный знак доллара, уронивший стенд
//     задачей #1786;
//  3. инъекция ВТОРОЙ формы того же класса — ссылка `$(ИМЯ)` по ОБЪЯВЛЕННОЙ
//     переменной контейнера: её kubelet подставляет величиной;
//  4. ЗАКОННЫЙ БЛИЗНЕЦ — форма `${ИМЯ}`, которую Kubernetes не трогает;
//  5. ЗАКОННЫЙ БЛИЗНЕЦ — подстановка команды `$(…)`, чьё содержимое ИМЕНЕМ
//     объявленной переменной не является.
//
// Без четвёртого и пятого гейт было бы не отличить от запрета «никаких долларов
// в скрипте»: в шаге их десятки, и все законны. Гейт судит ИСХОД подстановки, а
// не наличие знака, — близнецы это и доказывают.
package deploy_test

import (
	"strings"
	"testing"
)

// anchorCopySrc — единственная строка копирования исходника в отдаваемый файл.
// Годна якорем: встречается в шаблоне ровно один раз.
const anchorCopySrc = `      cp "$src" "$out"`

// anchorEvalToken — присвоение величины подставляемой переменной. Тот самый
// оператор, чья форма и была предметом #1786.
const anchorEvalToken = `        eval "KACHO_SUBST_TOKEN=\${$n-}"`

func expansionFindings(t *testing.T, f stepFixture) []string {
	t.Helper()
	out, defs, _, strs := stepExpansionFindings(t, f.root)
	if defs == 0 {
		t.Fatalf("обход копии ПУСТ — вердикт беспредметен")
	}
	if strs == 0 {
		t.Fatalf("в копии не осмотрено НИ ОДНОЙ строки команд — вердикт беспредметен")
	}
	return out
}

func TestStepExpansionGateFailsOnAReturnedDefect(t *testing.T) {
	t.Run("контроль: нетронутая копия — молчание", func(t *testing.T) {
		f := newStepFixture(t)
		if got := expansionFindings(t, f); len(got) != 0 {
			t.Errorf("гейт краснеет на НЕТРОНУТОЙ копии — его находки не про инъекцию: %v", got)
		}
	})

	t.Run("инъекция: удвоенный знак доллара — тот самый дефект #1786", func(t *testing.T) {
		f := newStepFixture(t)
		// Удвоение собирается из двух половин НАМЕРЕННО: записанное здесь
		// литералом, оно попало бы в этот же файл и объясняло бы класс формой,
		// которую сам класс и запрещает.
		doubled := `        eval "KACHO_SUBST_TOKEN=\` + "$" + `$n"`
		f.replaceOnce(t, anchorEvalToken, doubled)
		got := expansionFindings(t, f)
		if len(got) == 0 {
			t.Fatalf("гейт МОЛЧИТ на возвращённом дефекте — он не способен упасть")
		}
		joined := strings.Join(got, "\n")
		if !strings.Contains(joined, "_kratos-identity.tpl") {
			t.Errorf("находка не называет КООРДИНАТУ: %v", got)
		}
		if !strings.Contains(joined, "identity-config-render") {
			t.Errorf("находка не называет КОНТЕЙНЕР: %v", got)
		}
		if !strings.Contains(joined, "KACHO_SUBST_TOKEN") {
			t.Errorf("находка не показывает саму строку — читатель пойдёт искать её сам: %v", got)
		}
	})

	t.Run("инъекция: ссылка по ОБЪЯВЛЕННОЙ переменной — вторая форма класса", func(t *testing.T) {
		f := newStepFixture(t)
		ref := "        echo \"перечень: " + "$" + "(KACHO_IDENTITY_SUBSTITUTED_VARS)\""
		f.replaceOnce(t, anchorCopySrc, anchorCopySrc+"\n"+ref)
		got := expansionFindings(t, f)
		if len(got) == 0 {
			t.Fatalf("гейт МОЛЧИТ на ссылке, которую kubelet подставляет величиной")
		}
		if !strings.Contains(strings.Join(got, "\n"), "KACHO_IDENTITY_SUBSTITUTED_VARS") {
			t.Errorf("находка не называет подставленное имя: %v", got)
		}
	})

	t.Run("близнец: форма ${ИМЯ} — Kubernetes её не трогает, молчание", func(t *testing.T) {
		f := newStepFixture(t)
		twin := "        left_over=\"${KACHO_IDENTITY_SUBSTITUTED_VARS}\"; : \"$left_over\""
		f.replaceOnce(t, anchorCopySrc, anchorCopySrc+"\n"+twin)
		if got := expansionFindings(t, f); len(got) != 0 {
			t.Errorf("гейт краснеет на ЗАКОННОЙ форме `${ИМЯ}` — он ловит знак, а не исход: %v", got)
		}
	})

	t.Run("близнец: подстановка команды — не имя переменной, молчание", func(t *testing.T) {
		f := newStepFixture(t)
		twin := "        stamp=\"" + "$" + "(date +%s)\"; : \"$stamp\""
		f.replaceOnce(t, anchorCopySrc, anchorCopySrc+"\n"+twin)
		if got := expansionFindings(t, f); len(got) != 0 {
			t.Errorf("гейт краснеет на ЗАКОННОЙ подстановке команды — он запрещал бы "+
				"оболочке её собственный синтаксис: %v", got)
		}
	})
}
