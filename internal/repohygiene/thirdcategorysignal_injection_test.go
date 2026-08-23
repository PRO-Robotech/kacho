// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// thirdcategorysignal_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) оборви любое из трёх звеньев — гейт краснеет и НАЗЫВАЕТ оборванное;
// (б) целая провязка — гейт молчит.
//
// Звеньев именно три, и каждое правится отдельно, поэтому проверяется каждое, а
// не только «хоть что-то на месте». Гейт, требующий целостности лишь целиком,
// заметил бы обрыв ровно тогда, когда оборвано всё.
package repohygiene

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestThirdCategorySignal_ProvenByInjection(t *testing.T) {
	full := thirdCategoryWiring{
		VerdictStepFound: true, EmitsUnmetOutput: true, NamesCodeThree: true,
		CategoryFound: true, PassesFlag: true, ReadsVerdictStep: true,
	}

	t.Run("законный близнец: провязка цела — гейт молчит", func(t *testing.T) {
		if found := adjudicateThirdCategorySignal(full); len(found) != 0 {
			t.Fatalf("ложное срабатывание на целой провязке: %v", found)
		}
	})

	for _, tc := range []struct {
		name   string
		break_ func(*thirdCategoryWiring)
		want   string
	}{
		{"шаг вердикта не пишет выход", func(w *thirdCategoryWiring) { w.EmitsUnmetOutput = false }, "не пишет выход `unmet`"},
		{"код 3 не различается", func(w *thirdCategoryWiring) { w.NamesCodeThree = false }, "не различает код 3"},
		{"флаг не передаётся", func(w *thirdCategoryWiring) { w.PassesFlag = false }, "не передаёт `--verdict-unmet`"},
		{"свидетельство берётся не оттуда", func(w *thirdCategoryWiring) { w.ReadsVerdictStep = false }, "берётся не из выхода"},
	} {
		t.Run(tc.name+" — краснеет и называет звено", func(t *testing.T) {
			broken := full
			tc.break_(&broken)
			found := adjudicateThirdCategorySignal(broken)
			if len(found) != 1 {
				t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
			}
			if !strings.Contains(found[0], tc.want) {
				t.Fatalf("находка не называет оборванное звено (%q):\n%s", tc.want, found[0])
			}
		})
	}

	t.Run("шага вердикта нет вовсе — одна находка о нём, а не четыре о звеньях", func(t *testing.T) {
		found := adjudicateThirdCategorySignal(thirdCategoryWiring{CategoryFound: true})
		if len(found) != 1 || !strings.Contains(found[0], "report-gate") {
			t.Fatalf("ожидалась одна находка про отсутствующий шаг, получено: %v", found)
		}
	})

	// ПРЕДИКАТЫ ЧТЕНИЯ НАХОДЯТ СВОЙ ПРЕДМЕТ. Без этого гейт мог бы молчать
	// оттого, что разошёлся с формой файла, — и тогда он не измеряет ничего,
	// продолжая выглядеть исправным.
	t.Run("чтение находит провязку в синтетике той же формы", func(t *testing.T) {
		var doc signalDoc
		src := `
jobs:
  probes:
    steps:
      - id: report-gate
        run: |
          rc=$?
          if [ "$rc" = 3 ]; then echo "unmet=true" >> "$GITHUB_OUTPUT"; fi
      - id: category
        env:
          VERDICT_UNMET: ${{ steps.report-gate.outputs.unmet }}
        run: python3 x.py --verdict-unmet "$VERDICT_UNMET"
`
		if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
			t.Fatalf("синтетика не разобрана: %v", err)
		}
		w, steps := readThirdCategoryWiring(doc)
		if steps != 2 {
			t.Fatalf("шагов прочитано %d, ждали 2", steps)
		}
		if found := adjudicateThirdCategorySignal(w); len(found) != 0 {
			t.Fatalf("чтение не нашло целую провязку: %v", found)
		}
	})

	t.Run("чтение НЕ засчитывает провязку, которой нет", func(t *testing.T) {
		var doc signalDoc
		// Тот же шаг, но выход не пишется и флаг не передаётся: близнец обязан
		// быть распознан как оборванный, иначе предикат принимает что угодно.
		src := `
jobs:
  probes:
    steps:
      - id: report-gate
        run: python3 verdict.py
      - id: category
        run: python3 x.py --steps s.json
`
		if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
			t.Fatalf("синтетика не разобрана: %v", err)
		}
		w, _ := readThirdCategoryWiring(doc)
		found := adjudicateThirdCategorySignal(w)
		if len(found) != 4 {
			t.Fatalf("оборванная провязка дала %d находок, ждали 4: %v", len(found), found)
		}
	})
}
