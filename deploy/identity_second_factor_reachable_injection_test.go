// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_second_factor_reachable_injection_test.go — ДОКАЗАТЕЛЬСТВО, что
// соседний гейт умеет и краснеть, и молчать.
//
// Гейт достижимости второго фактора судит по трём объявлениям сразу, и у каждого
// свой разбор. Разбор, переставший узнавать своё объявление, даёт «ноль находок»,
// неотличимый от «ноль прочитанного», — поэтому проверяются ОБЕ стороны каждой
// оси: возвращённый дефект обязан краснить, законный близнец той же формы обязан
// молчать.
//
// Вход берётся НАСТОЯЩИЙ — из дерева, — а не собирается синтетикой там, где это
// возможно: синтетика доказывала бы, что разбор понимает синтетику.
package deploy_test

import (
	"strings"
	"testing"
)

func TestIdentitySecondFactorInjection_ParserSeesTheRealDeclaration(t *testing.T) {
	// Контроль предпосылки: разбор обязан узнавать объявление ДЕРЕВА. Пустой
	// разбор сделал бы все утверждения ниже вакуумными.
	got := parseIdentityMethods(readFileForTest(t, identityConfigTemplate))
	if len(got) < 4 {
		t.Fatalf("разбор увидел %d методов в %s — этого мало, чтобы утверждать что-либо: "+
			"проверьте, не переехал ли блок `selfservice.methods`", len(got), identityConfigTemplate)
	}
	if _, ok := got["totp"]; !ok {
		t.Fatalf("разбор не увидел метода одноразового кода в %s — предпосылка исчезла",
			identityConfigTemplate)
	}
	t.Logf("перепись: методов разобрано %d", len(got))
}

func TestIdentitySecondFactorInjection_DisabledSecondFactorIsFound(t *testing.T) {
	body := readFileForTest(t, identityConfigTemplate)

	// ДЕФЕКТ, ВОЗВРАЩЁННЫЙ В НАСТОЯЩИЙ ВХОД: единственный включённый способ,
	// который ведёт консоль, выключается.
	broken := strings.Replace(body, "    totp:\n      enabled: true", "    totp:\n      enabled: false", 1)
	if broken == body {
		t.Fatal("инъекция не изменила вход — форма объявления сменилась, и это утверждение " +
			"перестало что-либо доказывать")
	}
	brokenSecond := secondFactorMethods(parseIdentityMethods(broken))
	for _, m := range brokenSecond {
		if m == "totp" {
			t.Fatal("выключенный одноразовый код всё ещё считается вторым фактором — " +
				"разбор не читает `enabled`")
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: тот же вход без инъекции обязан молчать.
	if !contains(secondFactorMethods(parseIdentityMethods(body)), "totp") {
		t.Fatal("включённый одноразовый код не признан вторым фактором — гейт ловил бы " +
			"форму, а не существо")
	}
	t.Logf("перепись: вторых факторов на дереве %d · после инъекции %d",
		len(secondFactorMethods(parseIdentityMethods(body))), len(brokenSecond))
}

func TestIdentitySecondFactorInjection_PasswordlessKeyIsNotASecondFactor(t *testing.T) {
	// Ось, ради которой гейт и заведён: ключ доступа в БЕСПАРОЛЬНОЙ посадке —
	// первый фактор, и вторым он быть не вправе.
	passwordless := map[string]identityMethodDecl{
		"webauthn": {Enabled: true, Config: map[string]string{"passwordless": "true"}},
	}
	if contains(secondFactorMethods(passwordless), "webauthn") {
		t.Fatal("беспарольный ключ доступа засчитан вторым фактором — гейт объявил бы " +
			"достижимым уровень, которого этим способом не достичь")
	}
	if !contains(firstFactorMethods(passwordless), "webauthn") {
		t.Fatal("беспарольный ключ доступа не засчитан ПЕРВЫМ фактором — тогда гейт " +
			"объявил бы недостижимым и обычный вход")
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ той же формы: тот же метод в НЕбеспарольной посадке —
	// второй фактор, и молчать на нём обязательно.
	twofactor := map[string]identityMethodDecl{
		"webauthn": {Enabled: true, Config: map[string]string{"passwordless": "false"}},
	}
	if !contains(secondFactorMethods(twofactor), "webauthn") {
		t.Fatal("ключ доступа вторым фактором не признан — гейт ловил бы имя метода, " +
			"а не его посадку")
	}
}

func TestIdentitySecondFactorInjection_EmptyConsoleDeclarationIsNotSilence(t *testing.T) {
	real := parseStepUpMethods(readFileForTest(t, stepUpMethodsDeclaration))
	if len(real) == 0 {
		t.Fatalf("объявление способов консоли не разобрано (%s) — предпосылка исчезла",
			stepUpMethodsDeclaration)
	}
	for _, src := range []string{
		"",
		"export const STEP_UP_METHODS = [] as const;",
		"// STEP_UP_METHODS переименован",
	} {
		if got := parseStepUpMethods(src); len(got) != 0 {
			t.Fatalf("на входе %q разбор вернул %v — пустая сторона консоли обязана быть "+
				"отличима от непустой, иначе достижимость считалась бы по одной стороне", src, got)
		}
	}
	t.Logf("перепись: способов у консоли %d (%s)", len(real), strings.Join(real, " "))
}

func TestIdentitySecondFactorInjection_FloorFollowsTheIntersection(t *testing.T) {
	// Пусто с обеих сторон и вразнобой — пол «2» недостижим.
	for _, c := range []struct {
		name           string
		second, drivab []string
	}{
		{"настройки молчат", nil, []string{"totp"}},
		{"консоль молчит", []string{"totp"}, nil},
		{"стороны говорят о разном", []string{"totp"}, []string{"webauthn"}},
	} {
		floors, usable := attainableFloors(c.second, []string{"password"}, c.drivab)
		if floors["2"] || len(usable) != 0 {
			t.Fatalf("%s: пол «2» объявлен достижимым (пригодны %v) — гейт не покраснел бы "+
				"на том самом состоянии, ради которого заведён", c.name, usable)
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: стороны сошлись — пол достижим, и гейт обязан молчать.
	floors, usable := attainableFloors([]string{"lookup_secret", "totp"}, []string{"password"},
		[]string{"lookup_secret", "totp", "webauthn"})
	if !floors["2"] || len(usable) != 2 {
		t.Fatalf("сошедшиеся стороны объявлены недостижимыми (пригодны %v) — гейт краснел бы "+
			"на исправном дереве и был бы снят первым же читателем", usable)
	}
	// Пол первого уровня не должен зависеть от второго фактора.
	if !floors["1"] {
		t.Fatal("пол «1» объявлен недостижимым при включённом входе паролем")
	}
}

func TestIdentitySecondFactorInjection_ShadowDeclarationIsFound(t *testing.T) {
	shadow := "kratos:\n  kratos:\n    config:\n      selfservice:\n        methods:\n" +
		"          password: { enabled: true }\n          totp: { enabled: false }\n"
	if got := shadowedSecondFactors(shadow); len(got) != 1 || got[0] != "totp" {
		t.Fatalf("второе мнение о втором факторе не найдено: %v", got)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: профиль, высказавшийся только о первом факторе, — не
	// находка. Иначе гейт краснел бы на каждой накладке посадки.
	clean := "kratos:\n  kratos:\n    config:\n      selfservice:\n        methods:\n" +
		"          password: { enabled: true }\n"
	if got := shadowedSecondFactors(clean); len(got) != 0 {
		t.Fatalf("объявление только первого фактора принято за второе мнение: %v", got)
	}
}

func TestIdentitySecondFactorInjection_ChainPredicatesReadBothSides(t *testing.T) {
	mounted := []string{"a: 1\n", "extraArgs:\n  - --config\n  - " + identityRenderedConfigPath + "\n"}
	if !identityChainMountsOurConfig(mounted) {
		t.Fatal("провязка настроек в цепочке не найдена — гейт объявил бы стенд " +
			"работающим на умолчаниях поставщика при живой провязке")
	}
	if identityChainMountsOurConfig([]string{"a: 1\n", "b: 2\n"}) {
		t.Fatal("провязка найдена там, где её нет — гейт молчал бы на стенде без настроек")
	}

	up := []string{"kratos:\n  enabled: true\n  deployment: {}\n"}
	if !identityChainRaisesIdentity(up) {
		t.Fatal("поднятая служба личности не распознана — вся проверка стала бы " +
			"беспредметной и вышла бы зелёной")
	}
	if identityChainRaisesIdentity([]string{"hydra:\n  enabled: true\n"}) {
		t.Fatal("служба личности распознана по чужому объявлению")
	}
}
