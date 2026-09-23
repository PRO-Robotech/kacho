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
	"path/filepath"
	"regexp"
	"sort"
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

	// НАША посадка личности читается у обеих половин по отдельности: профиль
	// вправе назвать любую одну, и стенд от этого под судом не перестаёт быть.
	for _, c := range []struct {
		name, text, iam, edge string
	}{
		{"обе половины", "kaname:\n  config:\n    authn:\n      identityProvider: own\n" +
			"api-gateway:\n  authn:\n    identityProvider: own\n", "own", "own"},
		{"только служба", "kaname:\n  config:\n    authn:\n      identityProvider: external\n", "external", ""},
		{"только край", "api-gateway:\n  authn:\n    identityProvider: own\n", "", "own"},
	} {
		iam, edge := identityLandingOfProfile(c.text)
		if iam != c.iam || edge != c.edge {
			t.Fatalf("%s: посадка прочитана как iam=%q gateway=%q, объявлено iam=%q gateway=%q — "+
				"разбор перестал узнавать НАШУ ручку, и стражи отбирали бы стенды вслепую",
				c.name, iam, edge, c.iam, c.edge)
		}
	}

	// ЧУЖОЙ ФЛАГ ПОСАДКОЙ НЕ СЧИТАЕТСЯ — ни поднятый, ни выключенный. Ровно этим
	// предикат и отличается от прежнего: ручка подчарта поставщика к нашему
	// решению о личности отношения не имеет.
	for _, foreign := range []string{
		"kratos:\n  enabled: true\n  deployment: {}\n",
		"hydra:\n  enabled: true\n",
		"kratos:\n  enabled: false\n",
	} {
		if iam, edge := identityLandingOfProfile(foreign); iam != "" || edge != "" {
			t.Fatalf("чужой флаг %q прочитан как объявление посадки (iam=%q gateway=%q)",
				foreign, iam, edge)
		}
	}

	// Цепочка накладывается слева направо, как её накладывает helm: побеждает
	// последнее непустое объявление, а не первое.
	l := identityLandingOfChain(t, []string{
		"kaname:\n  config:\n    authn:\n      identityProvider: external\n",
		"kaname:\n  config:\n    authn:\n      identityProvider: own\n",
	})
	if !l.lands() || l.IAM != "own" || l.Base {
		t.Fatalf("накладка посадки не победила слой под собой: %+v", l)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: цепочка, посадку не называющая, беспредметной проверку
	// НЕ делает — значение ей даёт база подчарта, и стенд остаётся под судом.
	base := identityLandingOfChain(t, []string{"a: 1\n", "b: 2\n"})
	if !base.lands() || !base.Base {
		t.Fatalf("молчаливая цепочка осталась без посадки (%+v) — стенд выпал бы "+
			"из-под суда ровно так же, как выпадал по чужому флагу", base)
	}
}

// TestIdentitySecondFactorInjection_ForeignFlagOffKeepsEveryStandUnderJudgement —
// ВОЗВРАЩЁННЫЙ ДЕФЕКТ на НАСТОЯЩЕМ входе дерева.
//
// Дефект: предикат отбора стендов судил по `kratos.enabled` — флагу ЧУЖОГО
// подчарта. Измерено инъекцией: выключение чужих флагов в боевом профиле уводило
// из-под суда боевой стенд и стенд посадки `own`, 7 стендов становились 5, и все
// четыре стража личности оставались зелёными — пустыми операциями ровно там, где
// они нужны.
//
// Проба берёт НАСТОЯЩИЕ цепочки из таблицы стеков, выключает в их текстах чужие
// флаги (в памяти — дерево не трогается) и утверждает ДВЕ вещи сразу:
//
//   - инъекция ДЕЙСТВИТЕЛЬНО кусает: прежний признак («поднят чужой подчарт»)
//     после неё находит меньше стендов, чем до. Без этого утверждения проба
//     зеленела бы на инъекции, которая ничего не изменила;
//   - наш признак посадки после той же инъекции судит ТЕ ЖЕ стенды.
func TestIdentitySecondFactorInjection_ForeignFlagOffKeepsEveryStandUnderJudgement(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	// Прежний признак, выписанный здесь ДОСЛОВНО: он больше не судит ничего,
	// но доказывает, что инъекция кусает.
	foreignChartRaised := regexp.MustCompile(`(?m)^kratos:\n(?:[ \t].*\n|\n)*?\s+enabled:\s*true`)
	foreignOff := regexp.MustCompile(`(?m)^(kratos|hydra):\n(\s+)enabled: true$`)

	var judgedBefore, judgedAfter, foreignBefore, foreignAfter int
	var lost []string
	for _, name := range names {
		texts := make([]string, 0, len(stacks[name]))
		for _, prof := range stacks[name] {
			texts = append(texts, readFileForTest(t, filepath.Join(umbrellaDir, prof)))
		}
		injected := make([]string, 0, len(texts))
		for _, text := range texts {
			injected = append(injected, foreignOff.ReplaceAllString(text, "$1:\n${2}enabled: false"))
		}

		raised := func(in []string) bool {
			for _, text := range in {
				if foreignChartRaised.MatchString(text) {
					return true
				}
			}
			return false
		}
		if raised(texts) {
			foreignBefore++
		}
		if raised(injected) {
			foreignAfter++
		}
		if identityChainLandsIdentity(t, texts) {
			judgedBefore++
		}
		if identityChainLandsIdentity(t, injected) {
			judgedAfter++
			continue
		}
		lost = append(lost, name)
	}

	t.Logf("перепись: стендов %d · под судом до инъекции %d · после %d · "+
		"прежний признак (чужой подчарт поднят): до %d · после %d",
		len(names), judgedBefore, judgedAfter, foreignBefore, foreignAfter)

	if judgedBefore != len(names) {
		t.Fatalf("на исправном дереве под судом %d стендов из %d — предикат сузился "+
			"сам по себе, и утверждать об инъекции нечего", judgedBefore, len(names))
	}
	if foreignAfter >= foreignBefore {
		t.Fatalf("инъекция не кусает: по прежнему признаку до неё %d стендов, после %d. "+
			"Либо форма чужого флага в профилях сменилась, либо подстановка перестала "+
			"его находить — и тогда зелёное этой пробы ничего не значит",
			foreignBefore, foreignAfter)
	}
	if len(lost) > 0 {
		t.Fatalf("выключение ЧУЖИХ флагов увело из-под суда стенды %v: %d из %d. "+
			"Это ровно тот дефект, ради которого предикат переутверждён — признак "+
			"посадки снова взят у чужой службы, а не у нашей ручки identityProvider",
			lost, len(lost), len(names))
	}
}
