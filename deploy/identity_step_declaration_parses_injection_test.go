// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_step_declaration_parses_injection_test.go — доказательство того, что
// гейт разбора объявления шага СПОСОБЕН упасть и падает ТОЛЬКО на своём предмете.
//
// Прогонов ЧЕТЫРЕ:
//
//  1. контроль — сведённое дерево молчит;
//  2. инъекция НАСТОЯЩИМ дефектом — та самая склейка, что пришла слиянием;
//  3. инъекция второй половины предмета — имя, объявленное дважды;
//  4. ЗАКОННЫЙ БЛИЗНЕЦ — те же символы `- name:` внутри ЗАКАВЫЧЕННОГО значения.
//
// Четвёртый прогон несущий. Без него гейт было бы не отличить от поиска
// подстроки: подстрока `true    - name:` есть в обоих случаях, а YAML в одном
// из них законен. Гейт судит РАЗБОР, и близнец это и доказывает.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stepFixture — копия зонтичного чарта под инъекцию. Правится КОПИЯ: писать в
// дерево, из которого запущен прогон, запрещено (`multi-agent-flow.md` §13).
type stepFixture struct{ root string }

func newStepFixture(t *testing.T) stepFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "umbrella")
	copyTree(t, umbrellaDir, root)
	return stepFixture{root: root}
}

// stepTemplate — координата шаблона в КОПИИ выводится тем же признаком, что и
// популяция гейта: перечня имён здесь нет.
func (f stepFixture) stepTemplate(t *testing.T) string {
	t.Helper()
	defs := containerDefines(t, f.root)
	if len(defs) != 1 {
		t.Fatalf("в копии ожидалось РОВНО ОДНО объявление-контейнер, найдено %d — "+
			"инъекция сядет не туда, и зелёный прогон означал бы «дефект не воспроизведён»", len(defs))
	}
	for coord := range defs {
		return filepath.Join(f.root, strings.SplitN(coord, ":", 2)[0])
	}
	return ""
}

// replaceOnce правит по ЕДИНСТВЕННОМУ якорю: правка по неоднозначному якорю
// сядет в первое вхождение и условия инъекции может не создать.
func (f stepFixture) replaceOnce(t *testing.T, anchor, replacement string) {
	t.Helper()
	p := f.stepTemplate(t)
	b, err := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственной копии
	if err != nil {
		t.Fatalf("чтение копии: %v", err)
	}
	txt := string(b)
	if n := strings.Count(txt, anchor); n != 1 {
		t.Fatalf("якорь %q встречается %d раз — инъекция недостоверна", anchor, n)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(txt, anchor, replacement, 1)), 0o600); err != nil {
		t.Fatalf("запись копии: %v", err)
	}
}

func (f stepFixture) findings(t *testing.T) []string {
	t.Helper()
	out, examined, _ := stepDeclarationFindings(t, f.root)
	if examined == 0 {
		t.Fatalf("обход копии ПУСТ — вердикт беспредметен")
	}
	return out
}

// anchorOptional — хвост объявления переменной почтовой полосы. Он единственный
// в файле и потому годен якорем.
const anchorOptional = "          key: smtpConnectionURI\n          optional: true"

func TestStepDeclarationGateFailsOnAReturnedDefect(t *testing.T) {
	t.Run("контроль: сведённое дерево — молчание", func(t *testing.T) {
		f := newStepFixture(t)
		if got := f.findings(t); len(got) != 0 {
			t.Errorf("гейт краснеет на НЕТРОНУТОЙ копии — его находки не про инъекцию: %v", got)
		}
	})

	t.Run("инъекция: та самая склейка, пришедшая слиянием", func(t *testing.T) {
		f := newStepFixture(t)
		f.replaceOnce(t, anchorOptional,
			"          key: smtpConnectionURI\n"+
				"          optional: true    - name: KACHO_IAM_HOOK_TOKEN\n"+
				"      valueFrom:\n"+
				"        secretKeyRef:\n"+
				"          name: kacho-iam-hook-token\n"+
				"          key: token\n"+
				"          optional: true")
		got := f.findings(t)
		if len(got) == 0 {
			t.Fatalf("гейт МОЛЧИТ на возвращённом дефекте — он не способен упасть")
		}
		if !strings.Contains(strings.Join(got, "\n"), "НЕ РАЗБИРАЕТСЯ") {
			t.Errorf("находка не называет ПРИЧИНУ (разбор), а значит посылает читателя "+
				"искать не там: %v", got)
		}
		if !strings.Contains(strings.Join(got, "\n"), "_kratos-identity.tpl") {
			t.Errorf("находка не называет КООРДИНАТУ: %v", got)
		}
	})

	t.Run("инъекция: имя объявлено дважды", func(t *testing.T) {
		f := newStepFixture(t)
		f.replaceOnce(t, anchorOptional,
			"          key: smtpConnectionURI\n"+
				"          optional: true\n"+
				"    - name: KACHO_IAM_HOOK_TOKEN\n"+
				"      value: \"второе объявление того же имени\"")
		got := f.findings(t)
		if len(got) == 0 {
			t.Fatalf("гейт МОЛЧИТ на имени, объявленном дважды")
		}
		if !strings.Contains(strings.Join(got, "\n"), "объявлена 2 раза") {
			t.Errorf("находка не называет предмет (повтор имени): %v", got)
		}
	})

	t.Run("близнец: те же символы внутри ЗАКАВЫЧЕННОГО значения — молчание", func(t *testing.T) {
		f := newStepFixture(t)
		f.replaceOnce(t, anchorOptional,
			"          key: smtpConnectionURI\n"+
				"          optional: true\n"+
				"    - name: KACHO_IDENTITY_TWIN\n"+
				"      value: \"true    - name: KACHO_IAM_HOOK_TOKEN\"")
		if got := f.findings(t); len(got) != 0 {
			t.Errorf("гейт краснеет на ЗАКОННОМ близнеце — он ловит подстроку, а не разбор: %v", got)
		}
	})
}
