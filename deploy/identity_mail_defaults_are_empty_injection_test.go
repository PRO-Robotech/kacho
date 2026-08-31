// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_mail_defaults_are_empty_injection_test.go — доказательство того, что
// гейт MAIL-12 СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Без второй половины «зелено» неотличимо от «ничего не проверяет»: гейт,
// объявляющий отсутствие встроенных умолчаний, на дереве без них зелен и тогда,
// когда обход ослеп.
//
// ИНЪЕКЦИЯ РОНЯЕТ ТОЛЬКО ПРОВЕРЯЕМОЕ (`testing.md` §«Гейт на класс», п. 2в):
// каждая двигает ОДНУ величину синтетического слоя. Прогонов три — контроль
// (всё пусто, молчание), инъекция нового свойства (источник удостоверения),
// инъекция существующего (адрес узла): молчание существующей проверки иначе
// неотличимо от молчания мёртвой.
package deploy_test

import (
	"strings"
	"testing"
)

// syntheticLayer — слой умолчаний в разобранном виде. Дерево не трогается: ядро
// гейта — чистая функция над разобранными слоями, и обе стороны утверждения
// проверяются на ней.
func syntheticLayer(smtp map[string]any, hooksScheme string) map[string]any {
	return map[string]any{
		"global": map[string]any{
			"kacho": map[string]any{
				"identity": map[string]any{
					// Величина, которая ОБЯЗАНА иметь умолчание, лежит рядом
					// намеренно: она и есть положительный контроль узости
					// предиката.
					"hooks": map[string]any{"scheme": hooksScheme},
					"smtp":  smtp,
				},
			},
		},
	}
}

func emptyMailSmtp() map[string]any {
	return map[string]any{
		"connectionURI":    "",
		"fromAddress":      "",
		"fromName":         "",
		"credentialSecret": map[string]any{"name": "", "key": ""},
	}
}

func TestMailDefaultsGateCanStaySilent(t *testing.T) {
	// КОНТРОЛЬ: всё пусто, рядом — непустая ручка, обязанная иметь умолчание.
	layers := map[string]map[string]any{
		"values.yaml": syntheticLayer(emptyMailSmtp(), "http"),
	}
	findings, c := scanMailDefaults(layers)
	if len(findings) != 0 {
		t.Fatalf("контроль: гейт краснеет на исправном слое — %v", findings)
	}
	if c.nodesFound != 1 || c.knobsSeen != len(mailDefaultKnobs) {
		t.Fatalf("контроль: перепись не сошлась — узлов %d, величин осмотрено %d, ожидалось 1 и %d; "+
			"молчание при неполном обходе ничего не доказывает", c.nodesFound, c.knobsSeen, len(mailDefaultKnobs))
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ УЗОСТИ: величина, которая ОБЯЗАНА иметь умолчание,
	// гейт не роняет — иначе он требовал бы пустоты от всего подряд и был бы
	// снят первым же, кто заведёт законную ручку.
	if strings.Contains(strings.Join(knobNames(), " "), "hooks") {
		t.Fatal("предикат гейта захватил ручку обратных вызовов — он судит не свой предмет")
	}
}

func knobNames() []string {
	out := make([]string, 0, len(mailDefaultKnobs))
	for _, k := range mailDefaultKnobs {
		out = append(out, strings.Join(k, "."))
	}
	return out
}

func TestMailDefaultsGateCanFail(t *testing.T) {
	cases := []struct {
		name string
		mut  func(map[string]any)
		want string
	}{
		{
			// ИНЪЕКЦИЯ СУЩЕСТВУЮЩЕГО СВОЙСТВА: ровно та строка, что здесь стояла
			// и молча посылала письма в никуда.
			name: "адрес узла вернулся встроенным умолчанием",
			mut:  func(s map[string]any) { s["connectionURI"] = "smtp://mailhog.kacho.svc:1025/?disable_starttls=true" },
			want: "connectionURI",
		},
		{
			name: "адрес отправителя вернулся встроенным умолчанием",
			mut:  func(s map[string]any) { s["fromAddress"] = "noreply@example.invalid" },
			want: "fromAddress",
		},
		{
			// ИНЪЕКЦИЯ НОВОГО СВОЙСТВА: источник удостоверения заведён вместе с
			// решением Р6, и умолчания у него нет по той же причине.
			name: "имя секрета удостоверения вернулось встроенным умолчанием",
			mut: func(s map[string]any) {
				s["credentialSecret"] = map[string]any{"name": "kacho-identity-smtp", "key": ""}
			},
			want: "credentialSecret.name",
		},
		{
			name: "ключ секрета удостоверения вернулся встроенным умолчанием",
			mut: func(s map[string]any) {
				s["credentialSecret"] = map[string]any{"name": "", "key": "password"}
			},
			want: "credentialSecret.key",
		},
		{
			// Вырожденное СЧИТАЕТСЯ НЕЗАДАННЫМ тем же предикатом, что у обоих
			// стражей: одинокая запятая находкой быть не должна.
			name: "одинокая запятая — не находка",
			mut:  func(s map[string]any) { s["fromName"] = " , " },
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			smtp := emptyMailSmtp()
			tc.mut(smtp)
			findings, c := scanMailDefaults(map[string]map[string]any{
				"values.yaml": syntheticLayer(smtp, "http"),
			})
			if c.knobsSeen == 0 {
				t.Fatal("осмотрено ноль величин — инъекция проверяла пустоту, а не гейт")
			}
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("законный близнец объявлен находкой: %v", findings)
				}
				return
			}
			var got []string
			for _, f := range findings {
				got = append(got, f.knob)
			}
			if len(findings) != 1 || got[0] != tc.want {
				t.Fatalf("возвращённый дефект не назван координатой: находки %v, ожидалась ровно одна — %q.\n"+
					"Инъекция обязана ронять ТОЛЬКО проверяемое: красное от соседа оставило бы\n"+
					"вакуумность этой оси незамеченной", got, tc.want)
			}
		})
	}
}
