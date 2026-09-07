// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// client_token_declaration_test.go — профиль, поднимающий токен-эндпоинт
// платформы, обязан ОБЪЯВИТЬ все его величины, а не унаследовать их
// (задача #898, приёмка F2 §9.4).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У эндпоинта четыре величины, и каждая невидима на положительном пути: перечень
// адресатов платформы, адресат по умолчанию, срок выпускаемого токена и потолок
// тела запроса. Пустой перечень означает «выдаём токен, адресованный чему
// угодно»; нулевой потолок — «читаем сколько прислали». Ни одно из этих
// состояний не проявляется отказом: запрос проходит, токен выдаётся.
//
// Страж старта отказывает в пуске на любой из них — но только если величина
// доехала до процесса ПУСТОЙ. Умолчание шаблона довезло бы правдоподобное
// значение, и страж прошёл бы, а решение о посадке принял бы чарт вместо
// оператора.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседних posture_parity_test.go и dbtls_declaration_test.go:
// контракт — то, что профиль ОБЪЯВЛЯЕТ. Проверке не нужны ни helm, ни скачанные
// зависимости чартов, поэтому она не умеет пропуститься. Рендер тут и не помог
// бы: значение, приехавшее из умолчания чарта, в манифесте выглядит точно так
// же, как объявленное.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - проверяются ТОЛЬКО профили, включившие эндпоинт. Профиль, который его не
//     поднимает, ничего объявлять не обязан — требовать величин у того, кто
//     ими не пользуется, значит отказывать в старте без предмета;
//   - проверяется ОБЪЯВЛЕННОСТЬ и непустота, а не содержание: какой именно
//     адресат у стенда — решение профиля, и здесь принимается любой непустой;
//   - перечень профилей берётся КАТАЛОГОМ: новый профиль приходит под проверку
//     без правки этого файла.
package deploy_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// clientTokenKnobs — величины эндпоинта, которые профиль обязан объявить сам.
//
// Перечень выписан здесь намеренно и это единственное выписанное место: он есть
// КОНТРАКТ фазы, а не свойство дерева. Ручка, добавленная в чарт и забытая
// здесь, обязана быть замечена человеком — вывод её из чарта сделал бы проверку
// тождественно истинной.
var clientTokenKnobs = []string{"allowedAudiences", "defaultAudience", "tokenTtl", "bodyCeiling"}

// clientTokenFinding — одна находка с координатой, по которой её чинят.
type clientTokenFinding struct {
	profile string
	what    string
}

func (f clientTokenFinding) String() string { return f.profile + ": " + f.what }

// scanClientTokenDeclarations — ядро проверки, вынесенное отдельной функцией,
// чтобы самопроверка ниже подала ему синтетический вход, а не подделывала дерево.
//
// Возвращает находки И число осмотренных профилей: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
func scanClientTokenDeclarations(profiles map[string]map[string]any) (findings []clientTokenFinding, enabled int) {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		ct := dig(profiles[name], "kaname", "config", "authn", "clientToken")
		if ct == nil {
			continue
		}
		m, ok := ct.(map[string]any)
		if !ok {
			findings = append(findings, clientTokenFinding{name, "clientToken объявлен не отображением"})
			continue
		}
		on, _ := m["enabled"].(bool)
		if !on {
			continue
		}
		enabled++

		// Эндпоинт выпускает НАШИМ подписантом. Профиль, включивший его при
		// выключенной чеканке, не поднимется — и лучше узнать это здесь, чем
		// на стенде.
		if ts, _ := dig(profiles[name], "kaname", "config", "authn", "tokenSigning").(map[string]any); ts == nil {
			findings = append(findings, clientTokenFinding{name, "эндпоинт включён, своя чеканка не объявлена"})
		} else if signing, _ := ts["enabled"].(bool); !signing {
			findings = append(findings, clientTokenFinding{name, "эндпоинт включён при выключенной своей чеканке"})
		}

		for _, knob := range clientTokenKnobs {
			v, present := m[knob]
			switch {
			case !present:
				findings = append(findings, clientTokenFinding{name, "не объявлена величина " + knob})
			case isDegenerate(v):
				findings = append(findings, clientTokenFinding{name,
					fmt.Sprintf("величина %s объявлена вырожденной (%v)", knob, v)})
			}
		}
	}
	return findings, enabled
}

// dig достаёт вложенный узел по пути; отсутствующий — nil.
func dig(tree map[string]any, path ...string) any {
	var cur any = tree
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

// isDegenerate — вырожденное значение: пустое по СУЩЕСТВУ, а не по длине.
//
// Одинокая запятая в перечне непуста по длине и пуста по существу — ровно тот
// вход, на котором страж, меряющий длину строки, объявляет перечень заданным.
func isDegenerate(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		for _, part := range strings.Split(t, ",") {
			if strings.TrimSpace(part) != "" {
				return false
			}
		}
		return true
	case int:
		return t <= 0
	case float64:
		return t <= 0
	default:
		return false
	}
}

// TestClientTokenValuesAreDeclaredByEveryProfileThatServesIt — сама проверка.
func TestClientTokenValuesAreDeclaredByEveryProfileThatServesIt(t *testing.T) {
	files := profileFiles(t)
	profiles := make(map[string]map[string]any, len(files))
	for _, f := range files {
		profiles[f] = readYAML(t, f)
	}

	findings, enabled := scanClientTokenDeclarations(profiles)
	t.Logf("осмотрено профилей: %d, из них поднимают токен-эндпоинт: %d", len(files), enabled)

	if enabled == 0 {
		// Предпосылка проверки: она обоснована тем, что эндпоинт где-то поднят.
		// Ноль поднимающих профилей означает, что она не читала ничего, — и
		// это находка, а не тишина.
		t.Fatal("ни один профиль не поднимает токен-эндпоинт платформы — проверке нечего осматривать")
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}

// TestClientTokenDeclarationScannerSeesTheOmissionAndIsSilentOnTheDeclared —
// инъекция в обе стороны на синтетическом входе.
//
// Без второй половины проверка ловила бы форму: сканер, объявляющий находкой
// всякий профиль, прошёл бы первую половину и был бы бесполезен.
func TestClientTokenDeclarationScannerSeesTheOmissionAndIsSilentOnTheDeclared(t *testing.T) {
	full := func(mutate func(map[string]any)) map[string]any {
		ct := map[string]any{
			"enabled": true, "allowedAudiences": "registry.kacho.local",
			"defaultAudience": "registry.kacho.local", "tokenTtl": "15m", "bodyCeiling": 65536,
		}
		if mutate != nil {
			mutate(ct)
		}
		return map[string]any{"kaname": map[string]any{"config": map[string]any{"authn": map[string]any{
			"tokenSigning": map[string]any{"enabled": true},
			"clientToken":  ct,
		}}}}
	}

	// (а) законный близнец — молчание.
	got, enabled := scanClientTokenDeclarations(map[string]map[string]any{"profile.yaml": full(nil)})
	if len(got) != 0 || enabled != 1 {
		t.Fatalf("на полном объявлении сканер обязан молчать, получено %v (поднимающих %d)", got, enabled)
	}

	// (б) выключенный эндпоинт — тоже молчание и НЕ считается поднимающим.
	got, enabled = scanClientTokenDeclarations(map[string]map[string]any{
		"off.yaml": full(func(m map[string]any) { m["enabled"] = false }),
	})
	if len(got) != 0 || enabled != 0 {
		t.Fatalf("выключенный эндпоинт не обязан ничего объявлять, получено %v (поднимающих %d)", got, enabled)
	}

	// (в) каждая пропущенная величина — находка, называющая себя.
	for _, knob := range clientTokenKnobs {
		got, _ := scanClientTokenDeclarations(map[string]map[string]any{
			"gap.yaml": full(func(m map[string]any) { delete(m, knob) }),
		})
		if len(got) != 1 || !strings.Contains(got[0].what, knob) {
			t.Errorf("пропущенная величина %s обязана быть находкой с именем, получено %v", knob, got)
		}
	}

	// (г) вырожденная величина — находка, даже когда она непуста по длине.
	for _, tc := range []struct {
		knob  string
		value any
	}{
		{"allowedAudiences", ","},
		{"defaultAudience", "  "},
		{"bodyCeiling", 0},
	} {
		got, _ := scanClientTokenDeclarations(map[string]map[string]any{
			"degenerate.yaml": full(func(m map[string]any) { m[tc.knob] = tc.value }),
		})
		if len(got) != 1 || !strings.Contains(got[0].what, tc.knob) {
			t.Errorf("вырожденная величина %s=%v обязана быть находкой, получено %v", tc.knob, tc.value, got)
		}
	}

	// (д) эндпоинт при выключенной чеканке — находка: выпускать нечем.
	got, _ = scanClientTokenDeclarations(map[string]map[string]any{
		"nosigner.yaml": {"kaname": map[string]any{"config": map[string]any{"authn": map[string]any{
			"tokenSigning": map[string]any{"enabled": false},
			"clientToken": map[string]any{
				"enabled": true, "allowedAudiences": "a", "defaultAudience": "a",
				"tokenTtl": "15m", "bodyCeiling": 65536,
			},
		}}}},
	})
	if len(got) != 1 || !strings.Contains(got[0].what, "чеканке") {
		t.Errorf("эндпоинт при выключенной чеканке обязан быть находкой, получено %v", got)
	}
}
