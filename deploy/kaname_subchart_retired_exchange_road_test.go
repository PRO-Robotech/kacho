// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_subchart_retired_exchange_road_test.go — ПОДЧАРТ СЛУЖБЫ ДОСТУПА НЕ
// ВЫПУСКАЕТ ДОРОГИ ОБМЕНА К ПРЕЖНЕМУ ИЗДАТЕЛЮ, А ПРОФИЛЬ, ВСЁ ЕЩЁ ЕЁ
// ОБЪЯВЛЯЮЩИЙ, ПОЛУЧАЕТ ОТКАЗ РЕНДЕРА (kacho#2936).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба доступа с kaname#494 (ветка эпика 357, слияние волны-5) не строит
// дороги обмена подписанного утверждения у прежнего издателя: ни его адреса,
// ни якоря доверия к нему, ни его издателя она не читает. Подчарт выпускал всё
// три — двумя переменными процесса и ключом карты настроек — из трёх ручек
// значений. Судятся два утверждения:
//
//  1. рендер подчарта значениями КАЖДОГО стека deploy/stacks.txt и его
//     умолчаниями не несёт семейства дороги ни в одной строке — ни переменной
//     процесса, ни ключом карты, ни ручкой, ни прозой;
//  2. профиль, объявивший любую из трёх ручек — значением или пустой строкой, —
//     получает отказ рендера с именем ручки, а не значение, которого шаблон
//     больше не читает. Близнец — те же умолчания без ручки — рендерится.
//
// ─────────────────────────────────────────────────────────────────────────────
// КОНТРОЛЬ ПЕРЕПИСИ — НАСТОЯЩИМ ВХОДОМ ДЕРЕВА
//
// «Семейства ноль» на всех рендерах неотличимо от переписи, которая не читает
// пода. Поэтому тот же рендер умолчаний получает переменную семейства через
// сквозной перечень `env` подчарта (вход, который в дереве ЕСТЬ), и перепись
// обязана её найти. Отказ на самой ручке контролем быть не может: после
// снятия она не рендерится вовсе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Она не судит сквозной перечень `env` как ручку: это сырой вход оператора, и
// отказа на нём не заводится. Она не судит подчарты поставщика и иные
// провязки поставщика в подчарте — их держит
// kaname_subchart_retired_identity_wiring_test.go (kacho#2818).
package deploy_test

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// retiredExchangeRoadKnobs — ручки дороги обмена в значениях подчарта так, как
// их писал профиль: адрес token-эндпоинта, якорь доверия хопа, издатель.
// Литералы, а не ссылки на объявление: проба судит, что отвергается ИМЕННО то,
// что оператор мог написать.
var retiredExchangeRoadKnobs = []string{
	"platform.iam.hydraTokenURL",
	"platform.iam.hydraTokenCaFile",
	"config.authn.hydraIssuer",
}

// exchangeRoadFamily — семейство дороги в тексте рендера: переменные процесса
// (`*_HYDRA_TOKEN_*`, `*_HYDRA_ISSUER`), ключ карты настроек (`hydra-issuer`),
// имена ручек (`hydraToken*`, `hydraIssuer`). Без учёта регистра.
var exchangeRoadFamily = regexp.MustCompile(`(?i)hydra[_-]?(token|issuer)`)

// exchangeRoadControlSet — переменная семейства, поданная сквозным перечнем
// `env` подчарта: контроль переписи настоящим входом дерева.
const exchangeRoadControlSet = "env.KANAME_HYDRA_TOKEN_URL=https://control.invalid/token"

// exchangeRoadLines — строки рендера, несущие семейство дороги, с номером строки.
func exchangeRoadLines(rendered string) []string {
	var out []string
	for i, line := range strings.Split(rendered, "\n") {
		if exchangeRoadFamily.MatchString(line) {
			out = append(out, fmt.Sprintf("строка %d: %s", i+1, strings.TrimSpace(line)))
		}
	}
	return out
}

// TestKanameSubchartRendersNoExchangeRoadOnEveryStack — утверждение 1.
func TestKanameSubchartRendersNoExchangeRoadOnEveryStack(t *testing.T) {
	all := deployStacks(t)
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)

	type render struct{ where, text string }
	var renders []render
	for _, name := range names {
		out, err := renderStackSubchart(t, name, stackIdentityValues(t, all[name]))
		if err != nil {
			t.Fatalf("стек %s: рендер подчарта службы не удался — вердикта НЕТ: %v\n%s", name, err, out)
		}
		renders = append(renders, render{"стек " + name, out})
	}
	defaults, err := renderIdentitySubchart(t, nil)
	if err != nil {
		t.Fatalf("умолчания подчарта не рендерятся — вердикта НЕТ: %v\n%s", err, defaults)
	}
	renders = append(renders, render{"умолчания подчарта", defaults})

	lines := 0
	var census []string
	for _, r := range renders {
		if podOf(renderedDocs(t, r.text)) == nil {
			t.Fatalf("%s: в рендере нет пода службы (Deployment kaname) — «семейства ноль» здесь "+
				"неотличимо от «рендера нет»", r.where)
		}
		n := strings.Count(r.text, "\n") + 1
		lines += n
		found := exchangeRoadLines(r.text)
		for _, f := range found {
			t.Errorf("%s: рендер несёт дорогу обмена к прежнему издателю — %s", r.where, f)
		}
		census = append(census, fmt.Sprintf("%s: строк %d, находок %d", r.where, n, len(found)))
	}

	control, err := renderIdentitySubchart(t, nil, exchangeRoadControlSet)
	if err != nil {
		t.Fatalf("контроль переписи (%s) не рендерится — контроля НЕТ: %v\n%s", exchangeRoadControlSet, err, control)
	}
	seen := len(exchangeRoadLines(control))
	t.Logf("перепись: рендеров %d (стеков %d) · строк %d · находок в контроле %d\n  %s",
		len(renders), len(names), lines, seen, strings.Join(census, "\n  "))
	if seen == 0 {
		t.Fatalf("контроль переписи: переменная семейства подана подчарту (%s), а перепись её не "+
			"нашла — перепись слепа, и «семейства ноль» ничего не доказывает", exchangeRoadControlSet)
	}
}

// TestKanameSubchartRefusesTheRetiredExchangeRoadKnobs — утверждение 2.
func TestKanameSubchartRefusesTheRetiredExchangeRoadKnobs(t *testing.T) {
	if out, err := renderIdentitySubchart(t, nil); err != nil {
		t.Fatalf("близнец: подчарт на своих умолчаниях обязан рендериться: %v\n%s", err, out)
	}
	judged := 0
	for _, knob := range retiredExchangeRoadKnobs {
		for _, v := range []string{"https://retired.invalid/x", `""`} {
			judged++
			out, err := renderIdentitySubchart(t, nil, knob+"="+v)
			if err == nil {
				t.Errorf("профиль объявил `%s=%s`, и подчарт отрендерился: снятая ручка принята "+
					"(строк семейства в рендере %d) — оператор читает её как действующую дорогу",
					knob, v, len(exchangeRoadLines(out)))
				continue
			}
			for _, want := range []string{knob, "снята", "Уберите"} {
				if !strings.Contains(out, want) {
					t.Errorf("отказ рендера на `%s=%s` не называет %q — оператор не узнает, что убрать:\n%s",
						knob, v, want, out)
				}
			}
		}
	}
	t.Logf("перепись: ручек %d · объявлений осуждено %d", len(retiredExchangeRoadKnobs), judged)
}

// TestExchangeRoadInjection_FamilyLineRedsAndTwinIsSilent — способность
// переписи упасть: каждая форма семейства — находка, законный близнец,
// отличающийся одним фактом (имени издателя нет), — молчание.
func TestExchangeRoadInjection_FamilyLineRedsAndTwinIsSilent(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       int
	}{
		{"переменная адреса", "        - name: KANAME_HYDRA_TOKEN_URL\n", 1},
		{"переменная якоря", "        - name: KANAME_HYDRA_TOKEN_CA_FILE\n", 1},
		{"переменная издателя", "        - name: KANAME_HYDRA_ISSUER\n", 1},
		{"ключ карты", "      hydra-issuer: \"https://x\"\n", 1},
		{"имя ручки в прозе", "# hydraTokenURL пуст\n", 1},
		{"близнец: своя переменная адресата", "        - name: KANAME_BOOTSTRAP_TOKEN_AUDIENCE\n", 0},
		{"близнец: свой издатель", "      issuer: \"https://kaname.kacho.local\"\n", 0},
	} {
		if got := len(exchangeRoadLines(c.text)); got != c.want {
			t.Errorf("%s: находок %d, ожидалось %d", c.name, got, c.want)
		}
	}
}
