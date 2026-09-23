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
	"errors"
	"fmt"
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

// ─────────────────────────────────────────────────────────────────────────────
// ПОСАДКА ЗА ПОСАДКОЙ (#2691): у каждой — свои стороны, и каждая доказывается в
// обе стороны на НАСТОЯЩЕМ входе — дереве и пине, — с инъекцией в память.

// floorTwoFindings — находки по полу «2» у посадки.
func floorTwoFindings(sides secondFactorSides, byFloor map[string][]string) []string {
	_, findings, _ := judgeSecondFactorReach(sides, byFloor)
	var out []string
	for _, f := range findings {
		if strings.Contains(f, "пол уровня «2»") {
			out = append(out, f)
		}
	}
	return out
}

// ownDeclaration — объявление перечня нашей церемонии в форме файла консоли.
func ownDeclaration(methods ...string) string {
	quoted := make([]string, 0, len(methods))
	for _, m := range methods {
		quoted = append(quoted, `"`+m+`"`)
	}
	return "\nexport const OWN_STEP_UP_METHODS = [" + strings.Join(quoted, ", ") + "] as const;\n"
}

// consoleWithoutOwnDeclaration — настоящий файл консоли, из которого снято
// объявление нашей церемонии, если оно там уже есть: так случай «консоль нашей
// церемонии не ведёт» воспроизводится и после того, как Ф8 его заведёт.
func consoleWithoutOwnDeclaration(t *testing.T) string {
	t.Helper()
	return ownStepUpMethodsLiteral.ReplaceAllString(readFileForTest(t, stepUpMethodsDeclaration), "")
}

func TestIdentitySecondFactorInjection_OwnConsoleDeclarationDecidesTheFloor(t *testing.T) {
	byFloor := readCatalogFloors(t)
	if len(byFloor["2"]) == 0 {
		t.Fatal("в каталоге прав нет записей с полом «2» — утверждать о его достижимости не о чем")
	}
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)
	rule := mustOwnRule(t, assurance, vocab)
	base := consoleWithoutOwnDeclaration(t)

	// Предпосылка ровно того дефекта, ради которого задача заведена: перечень
	// ПОТОКА ПОСТАВЩИКА в файле есть и непуст.
	if len(parseStepUpMethods(base)) == 0 {
		t.Fatal("перечень STEP_UP_METHODS не разобран — случай «own судится перечнем поставщика» " +
			"не воспроизводится, и утверждение ниже ничего не доказывает")
	}

	// ДЕФЕКТ: консоль нашей церемонии не ведёт — пол «2» на own недостижим, хотя
	// перечень потока поставщика рядом стоит непустым. Прежняя редакция гейта
	// судила own именно по нему и зеленела.
	sides, err := ownSecondFactorSides(pin, root, vocab, rule, base)
	if err != nil {
		t.Fatalf("стороны own не прочитаны: %v", err)
	}
	if got := floorTwoFindings(sides, byFloor); len(got) != 1 {
		t.Fatalf("консоль без перечня нашей церемонии: находок по полу «2» %d, ждали 1 — гейт "+
			"судил бы own перечнем потока поставщика (%v)", len(got), parseStepUpMethods(base))
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ (один факт — объявление нашей церемонии): пол достижим.
	sides, err = ownSecondFactorSides(pin, root, vocab, rule, base+ownDeclaration("totp", "lookup_secret"))
	if err != nil {
		t.Fatalf("стороны own не прочитаны: %v", err)
	}
	if got := floorTwoFindings(sides, byFloor); len(got) != 0 {
		t.Fatalf("сошедшиеся стороны own объявлены недостижимыми: %v", got)
	}

	// ДЕФЕКТ: консоль ведёт способ, который уровня «2» не поднимает (код
	// восстановления даёт «1» по правилу службы).
	sides, err = ownSecondFactorSides(pin, root, vocab, rule, base+ownDeclaration("recovery_code"))
	if err != nil {
		t.Fatalf("стороны own не прочитаны: %v", err)
	}
	if got := floorTwoFindings(sides, byFloor); len(got) != 1 {
		t.Fatalf("консоль ведёт только код восстановления, а пол «2» объявлен достижимым (находок %d)", len(got))
	}

	// ГРАНИЦА СЛОВА: объявление нашей церемонии не читается перечнем потока
	// поставщика, и наоборот. Объявление ставится ПЕРЕД перечнем поставщика:
	// разбор берёт первое совпадение, и стоящее после оно не укусило бы вовсе.
	withOwn := ownDeclaration("totp") + base
	if strings.Join(parseStepUpMethods(withOwn), " ") != strings.Join(parseStepUpMethods(base), " ") {
		t.Fatalf("перечень потока поставщика прочитан из объявления нашей церемонии: %v против %v",
			parseStepUpMethods(withOwn), parseStepUpMethods(base))
	}
	if got := parseOwnStepUpMethods(base); len(got) != 0 {
		t.Fatalf("перечень нашей церемонии прочитан из перечня потока поставщика: %v", got)
	}
	t.Logf("перепись: записей с полом «2» %d · перечень поставщика %v · служба у пина %s ведёт вторым фактором %v",
		len(byFloor["2"]), parseStepUpMethods(base), pin, sides.Second)
}

func TestIdentitySecondFactorInjection_OwnServiceSideIsReadFromThePinnedRoot(t *testing.T) {
	byFloor := readCatalogFloors(t)
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)
	rule := mustOwnRule(t, assurance, vocab)
	console := consoleWithoutOwnDeclaration(t) + ownDeclaration("totp", "lookup_secret", "webauthn")

	// Контроль предпосылки: разбор корня видит перечень, и второй фактор в нём есть.
	wired, where, err := ownWiredMethods(root, vocab)
	if err != nil || len(wired) == 0 {
		t.Fatalf("перечень способов корня у пина %s не прочитан (%v, %v) — утверждения ниже вакуумны", pin, wired, err)
	}
	real, err := ownSecondFactorSides(pin, root, vocab, rule, console)
	if err != nil || len(real.Second) == 0 {
		t.Fatalf("у пина %s корень не провязывает второго фактора (%v, %v) — инъекции ниже нечего снимать", pin, wired, err)
	}
	rel := where[:strings.LastIndex(where, ":")]
	body := readFileForTest(t, filepath.Join(root.dir, filepath.FromSlash(rel)))

	// ДЕФЕКТ, ВОЗВРАЩЁННЫЙ В НАСТОЯЩИЙ ВХОД: корень собирает только пароль.
	onlyPassword := strings.NewReplacer(
		"assurance.MethodTOTP", "assurance.MethodPassword",
		"assurance.MethodLookupSecret", "assurance.MethodPassword",
		"assurance.MethodWebAuthn", "assurance.MethodPassword").Replace(body)
	if onlyPassword == body {
		t.Fatalf("инъекция не изменила %s — форма перечня сменилась", rel)
	}
	onlyPasswordRoot, err := root.with(rel, onlyPassword)
	if err != nil {
		t.Fatalf("инъекция не разобрана: %v", err)
	}
	sides, err := ownSecondFactorSides(pin, onlyPasswordRoot, vocab, rule, console)
	if err != nil {
		t.Fatalf("стороны own после инъекции не прочитаны: %v", err)
	}
	if len(sides.Second) != 0 || len(floorTwoFindings(sides, byFloor)) != 1 {
		t.Fatalf("корень без второго фактора: вторым фактором %v, находок по полу «2» %d — гейт "+
			"не читает сторону службы", sides.Second, len(floorTwoFindings(sides, byFloor)))
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: настоящий корень с той же консолью — пол достижим.
	sides, err = ownSecondFactorSides(pin, root, vocab, rule, console)
	if err != nil {
		t.Fatalf("стороны own не прочитаны: %v", err)
	}
	if got := floorTwoFindings(sides, byFloor); len(got) != 0 {
		t.Fatalf("настоящий корень с ведущей консолью объявлен недостижимым: %v", got)
	}

	// ПЕРЕЧЕНЬ НЕ ПОДАН САМООТЧЁТУ В УЗНАВАЕМОЙ ФОРМЕ — отказ, а не «ноль способов».
	var serveRel, serveBody string
	for _, f := range root.sortedFiles() {
		b := readFileForTest(t, filepath.Join(root.dir, filepath.FromSlash(f)))
		if strings.Contains(b, laneWiringObserver+"(") && strings.Contains(b, "lane.signInMethods()") {
			serveRel, serveBody = f, b
		}
	}
	if serveRel == "" {
		t.Fatalf("вызов %s с перечнем способов не найден текстом ни в одном файле корня у пина %s", laneWiringObserver, pin)
	}
	unfed, err := root.with(serveRel, strings.Replace(serveBody, "lane.signInMethods()", "nil", 1))
	if err != nil {
		t.Fatalf("инъекция не разобрана: %v", err)
	}
	if _, err := ownSecondFactorSides(pin, unfed, vocab, rule, console); !errors.Is(err, errNoSignInList) {
		t.Fatalf("самоотчёту подан nil вместо перечня, а гейт ответил %v — «перечня нет» обязано быть "+
			"отказом, иначе оно неотличимо от «служба не провязала ничего»", err)
	}
	t.Logf("перепись: пин %s · перечень корня %s: %v · вторым фактором %v", pin, where, wired, real.Second)
}

func TestIdentitySecondFactorInjection_ExternalLandingKeepsItsOwnSides(t *testing.T) {
	byFloor := readCatalogFloors(t)
	settings := readFileForTest(t, identityConfigTemplate)
	console := consoleWithoutOwnDeclaration(t)

	// ЗАКОННЫЙ БЛИЗНЕЦ: настоящие стороны external — пол «2» достижим.
	sides, err := externalSecondFactorSides(settings, console)
	if err != nil {
		t.Fatalf("стороны external не прочитаны: %v", err)
	}
	if got := floorTwoFindings(sides, byFloor); len(got) != 0 {
		t.Fatalf("настоящие стороны external объявлены недостижимыми: %v", got)
	}

	// ДЕФЕКТ: оба кода второго фактора выключены в настройке поставщика.
	off := strings.NewReplacer(
		"    totp:\n      enabled: true", "    totp:\n      enabled: false",
		"    lookup_secret:\n      enabled: true", "    lookup_secret:\n      enabled: false").Replace(settings)
	if off == settings {
		t.Fatal("инъекция не изменила настройку поставщика — форма объявления сменилась")
	}
	sides, err = externalSecondFactorSides(off, console)
	if err != nil {
		t.Fatalf("стороны external после инъекции не прочитаны: %v", err)
	}
	if got := floorTwoFindings(sides, byFloor); len(got) != 1 {
		t.Fatalf("настройка без второго фактора: находок по полу «2» %d, ждали 1", len(got))
	}

	// Объявление нашей церемонии сторону external не трогает: её консоль — поток
	// поставщика, и перечень own её пол не поднимает.
	sides, err = externalSecondFactorSides(off, console+ownDeclaration("totp", "lookup_secret"))
	if err != nil {
		t.Fatalf("стороны external не прочитаны: %v", err)
	}
	if got := floorTwoFindings(sides, byFloor); len(got) != 1 {
		t.Fatalf("перечень нашей церемонии поднял пол на посадке external (находок %d)", len(got))
	}

	// Посадка вне таблицы — отказ с её именем, а не пропуск.
	if _, err := sidesOfLanding(t, "federated", console); err == nil || !strings.Contains(err.Error(), "federated") {
		t.Fatalf("посадка вне таблицы не названа отказом: %v", err)
	}
}

func TestIdentitySecondFactorInjection_OwnStackIsNotJudgedByProviderSettings(t *testing.T) {
	for _, c := range []struct {
		name    string
		landing identityLanding
		judged  bool
	}{
		{"external у службы", identityLanding{IAM: landingExternal, Edge: landingExternal}, true},
		{"own у службы", identityLanding{IAM: landingOwn, Edge: landingOwn}, false},
		{"own только у края", identityLanding{Edge: landingOwn}, false},
		{"служба решает при расхождении", identityLanding{IAM: landingExternal, Edge: landingOwn}, true},
	} {
		if got := c.landing.judgedByProviderSettings(); got != c.judged {
			t.Fatalf("%s (%+v): судится настройкой поставщика = %v, ждали %v", c.name, c.landing, got, c.judged)
		}
	}

	// На НАСТОЯЩЕЙ таблице стенд own из-под суда настройкой поставщика выведен, а
	// остальные — нет.
	landings := stacksByLanding(t)
	if len(landings[landingOwn]) == 0 || len(landings[landingExternal]) == 0 {
		t.Fatalf("в таблице стендов нет обеих посадок (%v) — различение нечем доказать", landings)
	}
	t.Logf("перепись: стендов на external %d (%v) · на own %d (%v)",
		len(landings[landingExternal]), landings[landingExternal], len(landings[landingOwn]), landings[landingOwn])
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРАВИЛО УРОВНЯ НА own ТОЛКУЕТСЯ У ПИНА, А НЕ ПЕРЕПИСЫВАЕТСЯ ГЕЙТОМ (#2691, круг 2)
//
// Круг 1 показал два места, где гейт судил не правилом службы, а своим пересказом
// его: классификатор способов был таблицей гейта (мутация, снявшая в ней
// требование пароля, не роняла ни одной пробы), а сверка с пином сравнивала
// НАБОРЫ ИМЁН строки, а не её логику. Пробы ниже подают правилу и корню пина
// дефект одним фактом и утверждают, что вердикт меняется ровно вместе с ним.

// Места пина, в которые пробы вносят дефект. Сменилась форма — инъекция не
// меняет вход, и проба отказывает, а не зеленеет на неукушенном входе.
const (
	pinnedSignInList      = "assurance.MethodPassword, assurance.MethodTOTP, assurance.MethodLookupSecret"
	pinnedSecondFactorRow = "s.has(MethodPassword) && (s.has(MethodTOTP) || s.has(MethodLookupSecret))"
)

// packageFileWith — единственный файл пакета, несущий текст needle, и его тело.
func packageFileWith(t *testing.T, src goPackageSource, needle string) (rel, body string) {
	t.Helper()
	var found []string
	for _, f := range src.sortedFiles() {
		b := readFileForTest(t, filepath.Join(src.dir, filepath.FromSlash(f)))
		if strings.Contains(b, needle) {
			found = append(found, f)
			rel, body = f, b
		}
	}
	if len(found) != 1 {
		t.Fatalf("текст %q найден в %d файлах пакета (%v), ждали ровно один — форма пина сменилась, "+
			"и инъекция перестала что-либо доказывать", needle, len(found), found)
	}
	return rel, body
}

// injectedPackage — копия пакета, в которой одно место заменено; настоящий разбор
// не меняется.
func injectedPackage(t *testing.T, src goPackageSource, old, repl string) goPackageSource {
	t.Helper()
	rel, body := packageFileWith(t, src, old)
	out, err := src.with(rel, strings.Replace(body, old, repl, 1))
	if err != nil {
		t.Fatalf("инъекция в %s не разобрана: %v", rel, err)
	}
	return out
}

// mustOwnRule — правило уровня пакета, толкованное гейтом.
func mustOwnRule(t *testing.T, assurance goPackageSource, vocab map[string]string) ownRule {
	t.Helper()
	rule, err := readOwnRule(assurance, vocab)
	if err != nil {
		t.Fatalf("правило уровня не толкуется: %v", err)
	}
	return rule
}

// ownFloorTwo — находки по полу «2» посадки own при названных корне, правиле и консоли.
func ownFloorTwo(t *testing.T, pin string, root goPackageSource, vocab map[string]string, rule ownRule,
	console string, byFloor map[string][]string,
) ([]string, secondFactorSides) {
	t.Helper()
	sides, err := ownSecondFactorSides(pin, root, vocab, rule, console)
	if err != nil {
		t.Fatalf("стороны own не прочитаны: %v", err)
	}
	return floorTwoFindings(sides, byFloor), sides
}

// TestIdentitySecondFactorInjection_PasswordRequirementComesFromTheRule — ось
// мутации M4 круга 1: код по времени поднимает уровень ТОЛЬКО вместе с паролем, и
// знает это правило пина, а не таблица гейта.
func TestIdentitySecondFactorInjection_PasswordRequirementComesFromTheRule(t *testing.T) {
	byFloor := readCatalogFloors(t)
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)
	rule := mustOwnRule(t, assurance, vocab)
	console := consoleWithoutOwnDeclaration(t) + ownDeclaration("totp")

	// ДЕФЕКТ (вход круга 1): корень провязывает ключ доступа и код по времени, пароля
	// нет; консоль ведёт только код по времени. Поднять уровень нечем: код без
	// пароля «2» не даёт, а ключ доступа консоль на церемонии не ведёт.
	noPassword := injectedPackage(t, root, pinnedSignInList, "assurance.MethodWebAuthn, assurance.MethodTOTP")
	if got, sides := ownFloorTwo(t, pin, noPassword, vocab, rule, console, byFloor); len(got) != 1 {
		t.Fatalf("корень [webauthn totp] без пароля, консоль ведёт [totp]: находок по полу «2» %d, ждали 1 — "+
			"гейт засчитал код по времени без пароля (пригодны %v)", len(got), sides.Usable)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ (один факт — пароль вместо ключа): код по времени поднимает.
	withPassword := injectedPackage(t, root, pinnedSignInList, "assurance.MethodPassword, assurance.MethodTOTP")
	if got, _ := ownFloorTwo(t, pin, withPassword, vocab, rule, console, byFloor); len(got) != 0 {
		t.Fatalf("корень [password totp], консоль ведёт [totp]: пол «2» объявлен недостижимым: %v", got)
	}

	// Пароль, который консоль ведёт на церемонии рядом с кодом, уровня не поднимает:
	// без него подъём состоялся бы и так. Пригодным называется только минимальное.
	if _, sides := ownFloorTwo(t, pin, withPassword, vocab, rule, consoleWithoutOwnDeclaration(t)+
		ownDeclaration("password", "totp"), byFloor); strings.Join(sides.Usable, " ") != "totp" {
		t.Fatalf("консоль ведёт [password totp]: пригодными названы %v, ждали [totp] — перепись приписала "+
			"подъём способу, без которого он состоялся бы и так", sides.Usable)
	}

	// Требование пароля — у ПРАВИЛА: строка «2», в которой код по времени поднимает
	// уровень сам, делает тот же корень без пароля достижимым. Гейт, судящий своей
	// таблицей, этого перехода не увидел бы ни в одну сторону.
	totpAlone := mustOwnRule(t, injectedPackage(t, assurance, pinnedSecondFactorRow, "s.has(MethodTOTP)"), vocab)
	if got, _ := ownFloorTwo(t, pin, noPassword, vocab, totpAlone, console, byFloor); len(got) != 0 {
		t.Fatalf("правило, где код по времени даёт «2» сам, а пол по-прежнему недостижим: %v — гейт "+
			"классифицирует своей таблицей, а не правилом пина", got)
	}
}

// ruleRowNames — имена способов в каждой строке правила (то, что сверяла прежняя
// посылка): уровень → наборы имён.
func ruleRowNames(rule ownRule) map[string][]string {
	name := regexp.MustCompile(`\(([a-z_]+)\)`)
	out := map[string][]string{}
	for _, r := range rule.rows {
		set := map[string]bool{}
		for _, m := range name.FindAllStringSubmatch(r.cond.String(), -1) {
			set[m[1]] = true
		}
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		out[r.level] = append(out[r.level], strings.Join(names, "+"))
	}
	for l := range out {
		sort.Strings(out[l])
	}
	return out
}

// TestIdentitySecondFactorInjection_RuleLogicNotItsNamesDecidesTheFloor — ось
// круга 1: правило читается ЛОГИКОЙ строки, а не набором имён в ней.
func TestIdentitySecondFactorInjection_RuleLogicNotItsNamesDecidesTheFloor(t *testing.T) {
	byFloor := readCatalogFloors(t)
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)
	rule := mustOwnRule(t, assurance, vocab)
	base := consoleWithoutOwnDeclaration(t)

	// ДЕФЕКТ (вход круга 1): «пароль и (код по времени или запасной код)» →
	// «пароль и код по времени и запасной код». Набор имён в строке тот же — именно
	// его сверяла прежняя посылка, и её проба оставалась зелёной.
	allThree := mustOwnRule(t, injectedPackage(t, assurance, pinnedSecondFactorRow,
		"s.has(MethodPassword) && s.has(MethodTOTP) && s.has(MethodLookupSecret)"), vocab)
	if a, b := fmt.Sprint(ruleRowNames(rule)), fmt.Sprint(ruleRowNames(allThree)); a != b {
		t.Fatalf("инъекция сменила НАБОР имён (%s → %s), а должна была сменить только логику — "+
			"проба перестала воспроизводить вход круга 1", a, b)
	}

	// Консоль ведёт один код по времени: по правилу пина этого хватает, по
	// переписанному — нет.
	if got, _ := ownFloorTwo(t, pin, root, vocab, rule, base+ownDeclaration("totp"), byFloor); len(got) != 0 {
		t.Fatalf("правило пина, консоль ведёт [totp]: пол «2» объявлен недостижимым: %v", got)
	}
	if got, sides := ownFloorTwo(t, pin, root, vocab, allThree, base+ownDeclaration("totp"), byFloor); len(got) != 1 {
		t.Fatalf("правило «пароль и оба кода», консоль ведёт только [totp]: находок по полу «2» %d, ждали 1 "+
			"(пригодны %v) — гейт читает имена строки, а не её логику", len(got), sides.Usable)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ переписанного правила: консоль ведёт оба кода — уровень
	// поднимается двумя предъявлениями, и гейт обязан молчать. Иначе «читает логику»
	// было бы неотличимо от «краснеет на любой смене строки».
	if got, _ := ownFloorTwo(t, pin, root, vocab, allThree, base+ownDeclaration("totp", "lookup_secret"), byFloor); len(got) != 0 {
		t.Fatalf("правило «пароль и оба кода», консоль ведёт оба: пол «2» объявлен недостижимым: %v", got)
	}
	t.Logf("перепись: у пина %s строк правила %d · строки «2» у пина %v · после инъекции %v",
		pin, len(rule.rows), rowsOfLevel(rule, "2"), rowsOfLevel(allThree, "2"))
}

// rowsOfLevel — толкованные условия строк одного уровня, как их печатает гейт.
func rowsOfLevel(rule ownRule, level string) []string {
	var out []string
	for _, r := range rule.rows {
		if r.level == level {
			out = append(out, r.cond.String())
		}
	}
	return out
}

// TestIdentitySecondFactorInjection_RuleOutsideTheInterpretationIsARefusal —
// строка, помощник или лестница, которых толкование не узнаёт, — отказ с
// координатой, а не догадка и не пропуск строки.
func TestIdentitySecondFactorInjection_RuleOutsideTheInterpretationIsARefusal(t *testing.T) {
	pin, _, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)

	// ЗАКОННЫЙ БЛИЗНЕЦ: правило пина толкуется целиком, обе ступени на месте.
	rule := mustOwnRule(t, assurance, vocab)
	if len(rowsOfLevel(rule, "1")) == 0 || len(rowsOfLevel(rule, "2")) == 0 {
		t.Fatalf("у пина %s не толкованы строки «1» или «2» (%v) — утверждения ниже вакуумны", pin, rule.rows)
	}

	cases := []struct{ name, old, repl string }{
		{"условие строки — не выражение над предъявленным", pinnedSecondFactorRow, "len(s) > 1"},
		{"постоянная вне словаря службы", "s.has(MethodWebAuthn) }", "s.has(MethodPasskey) }"},
		{"помощник, который не спрашивает способ", "p.method == MethodWebAuthn && p.userVerified", "p.userVerified"},
		{"уровень считается не по этой таблице", "return levelOf(rows, presentations)", "return levelOf(nil, presentations)"},
		{"лестница — не первая подошедшая строка", "if r.holds(s) {", "if !r.holds(s) {"},
	}
	refused := 0
	for _, c := range cases {
		_, err := readOwnRule(injectedPackage(t, assurance, c.old, c.repl), vocab)
		if !errors.Is(err, errRuleNotInterpretable) {
			t.Fatalf("%s: толкование ответило %v — неузнанная форма правила обязана быть отказом, иначе гейт "+
				"судил бы по правилу, которого служба не исполняет", c.name, err)
		}
		refused++
	}
	t.Logf("перепись: у пина %s строк правила %d · инъекций %d · отказов толкования %d",
		pin, len(rule.rows), len(cases), refused)
}

// TestIdentitySecondFactorInjection_SilentRootIsARefusal — корень, не подавший
// самоотчёту перечня вовсе, — отказ, а не «служба не провязала ничего» (круг 1:
// прежняя проба ловила только форму «nil вместо перечня»).
func TestIdentitySecondFactorInjection_SilentRootIsARefusal(t *testing.T) {
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)

	// ЗАКОННЫЙ БЛИЗНЕЦ: настоящий корень даёт перечень.
	if wired, _, err := ownWiredMethods(root, vocab); err != nil || len(wired) == 0 {
		t.Fatalf("перечень корня у пина %s не прочитан (%v, %v) — утверждения ниже вакуумны", pin, wired, err)
	}
	for _, c := range []struct{ name, old, repl string }{
		{"наблюдатель провязки не зовётся вовсе", laneWiringObserver + "(ctx, cfg,", "unobservedLaneWiring(ctx, cfg,"},
		{"производитель не называет ни одной постоянной словаря", "[]assurance.Method{" + pinnedSignInList + "}", "nil"},
	} {
		if wired, _, err := ownWiredMethods(injectedPackage(t, root, c.old, c.repl), vocab); !errors.Is(err, errNoSignInList) {
			t.Fatalf("%s: разбор корня ответил %v (%v) — «перечня нет» обязано быть отказом", c.name, wired, err)
		}
	}
}

// TestIdentitySecondFactorInjection_EmptyCatalogIsARefusal — каталог, в котором
// судить нечего, — отказ, а не «записей 0 · находок 0» (круг 1: пустой каталог
// давал код 0 без единой строки переписи).
func TestIdentitySecondFactorInjection_EmptyCatalogIsARefusal(t *testing.T) {
	// ЗАКОННЫЙ БЛИЗНЕЦ: настоящий каталог разбирается, и пол «2» в нём есть.
	real, err := catalogFloorsFrom([]byte(readFileForTest(t, permissionCatalogEmbed)))
	if err != nil || len(real["2"]) == 0 {
		t.Fatalf("настоящий каталог не разобран либо без пола «2» (%v, %d)", err, len(real["2"]))
	}
	// Каждый отказ называет СВОЮ причину: отказ по чужой причине означал бы, что
	// своя проверка мертва и держится соседней.
	for _, c := range []struct{ name, body, reason string }{
		{"пустой каталог", "[]", "записей 0"},
		{"ни одна запись не объявляет пола", `[{"fqn":"kacho.cloud.vpc.v1.NetworkService/Get"}]`, "не объявляет required_acr_min"},
		{"запись без имени метода", `[{"fqn":"kacho.cloud.vpc.v1.NetworkService/Get","required_acr_min":"2"},{"required_acr_min":"2"}]`, "без поля fqn"},
		{"не JSON", "{", "не разобран"},
	} {
		got, err := catalogFloorsFrom([]byte(c.body))
		if err == nil {
			t.Fatalf("%s: каталог принят (%v) — «достижимых 0 из 0» неотличимо от непрочитанного", c.name, got)
		}
		if !strings.Contains(err.Error(), c.reason) {
			t.Fatalf("%s: отказ по чужой причине (%v), ждали «%s»", c.name, err, c.reason)
		}
	}
}

// TestIdentitySecondFactorInjection_FlagQualifiedRungIsNotClaimed — ступень,
// которую держат флаги предъявления (ключ с проверкой пользователя, не
// допускающий резервного копирования), гейт достижимой НЕ называет: флаги ему
// неизвестны, и «неизвестно» не превращается в «да».
func TestIdentitySecondFactorInjection_FlagQualifiedRungIsNotClaimed(t *testing.T) {
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)
	rule := mustOwnRule(t, assurance, vocab)
	keyRoot := injectedPackage(t, root, pinnedSignInList, "assurance.MethodPassword, assurance.MethodWebAuthn")
	sides, err := ownSecondFactorSides(pin, keyRoot, vocab, rule, consoleWithoutOwnDeclaration(t)+ownDeclaration("webauthn"))
	if err != nil {
		t.Fatalf("стороны own не прочитаны: %v", err)
	}
	// ЗАКОННЫЙ БЛИЗНЕЦ: до «2» ключ доступа поднимает строкой без флагов.
	if !sides.Floors["2"] {
		t.Fatalf("ключ доступа провязан и ведётся церемонией, а пол «2» недостижим (%v)", sides.Floors)
	}
	// «3» держится флагами — достоверно его не достичь.
	if sides.Floors["3"] {
		t.Fatalf("ступень «3» объявлена достижимой (%v), хотя её держат флаги предъявления, которых гейт не знает",
			sides.Floors)
	}
}
