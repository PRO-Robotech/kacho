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

	"gopkg.in/yaml.v3"
)

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
// С #2735 чужие флаги выключены НА ВСЕХ стендах самим деревом (база зонта), и
// дефект, будь он жив, проявился бы на исправном дереве: под судом оказалось бы
// ноль стендов. Поэтому ПЕРВОЕ утверждение пробы — на дереве как есть: наш
// признак посадки судит КАЖДЫЙ стенд, хотя чужой подчарт не поднят ни на одном.
//
// Инъекция идёт теперь в ОБРАТНУЮ сторону — чужие флаги ВКЛЮЧАЮТСЯ (в памяти —
// дерево не трогается), — и утверждает две вещи:
//
//   - инъекция ДЕЙСТВИТЕЛЬНО кусает: прежний признак («поднят чужой подчарт»)
//     после неё находит больше стендов, чем до. Без этого утверждения проба
//     зеленела бы на инъекции, которая ничего не изменила;
//   - наш признак посадки после той же инъекции судит ТЕ ЖЕ стенды: он от
//     чужого флага не зависит ни в одну сторону.
//
// Прежний признак читается РАЗБОРОМ, а не образцом строки: образец по тексту
// брал за флаг включения любой вложенный `enabled: true` под узлом подчарта и
// отвечал «поднят» стенду, где подчарт выключен.
func TestIdentitySecondFactorInjection_ForeignFlagOffKeepsEveryStandUnderJudgement(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	base := readFileForTest(t, filepath.Join(umbrellaDir, "values.yaml"))
	// Инъекция — ПОСЛЕДНИЙ слой цепочки, включающий оба чужих подчарта.
	const foreignOn = "kratos:\n  enabled: true\nhydra:\n  enabled: true\n"

	// Прежний признак: слитые значения (база зонта + цепочка) поднимают подчарт.
	foreignChartRaised := func(texts []string) bool {
		merged := map[string]any{}
		for _, text := range append([]string{base}, texts...) {
			var tree map[string]any
			if err := yaml.Unmarshal([]byte(text), &tree); err != nil {
				t.Fatalf("профиль цепочки не разбирается: %v", err)
			}
			merged = mergeValues(merged, tree)
		}
		for _, chart := range []string{"kratos", "hydra"} {
			if on, ok := lookup(merged, chart, "enabled"); ok && on == true {
				return true
			}
		}
		return false
	}

	var judgedBefore, judgedAfter, foreignBefore, foreignAfter int
	var lost []string
	for _, name := range names {
		texts := make([]string, 0, len(stacks[name])+1)
		for _, prof := range stacks[name] {
			texts = append(texts, readFileForTest(t, filepath.Join(umbrellaDir, prof)))
		}
		injected := append(append([]string{}, texts...), foreignOn)

		if foreignChartRaised(texts) {
			foreignBefore++
		}
		if foreignChartRaised(injected) {
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

	t.Logf("перепись: стендов %d · под судом на дереве %d · после инъекции %d · "+
		"прежний признак (чужой подчарт поднят): на дереве %d · после инъекции %d",
		len(names), judgedBefore, judgedAfter, foreignBefore, foreignAfter)

	if judgedBefore != len(names) {
		t.Fatalf("на исправном дереве под судом %d стендов из %d, хотя чужой подчарт "+
			"поднят на %d — предикат отбора снова зависит от чужого флага, и это ровно "+
			"тот дефект, ради которого он переутверждён", judgedBefore, len(names), foreignBefore)
	}
	if foreignAfter <= foreignBefore {
		t.Fatalf("инъекция не кусает: по прежнему признаку на дереве %d стендов, после "+
			"инъекции %d. Либо форма чужого флага сменилась, либо слой инъекции перестал "+
			"его включать — и тогда зелёное этой пробы ничего не значит",
			foreignBefore, foreignAfter)
	}
	if len(lost) > 0 {
		t.Fatalf("включение ЧУЖИХ флагов увело из-под суда стенды %v: %d из %d. "+
			"Признак посадки обязан браться у нашей ручки identityProvider, а не у "+
			"чужой службы", lost, len(lost), len(names))
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

	// ДЕФЕКТ ПРЕЖНЕЙ РЕДАКЦИИ, ВОЗВРАЩЁННЫЙ ИНЪЕКЦИЕЙ: рядом стоит непустой
	// перечень потока поставщика, а нашей церемонии консоль не ведёт. Прежняя
	// редакция судила own именно по нему и зеленела. Перечень вносит проба:
	// консоль его больше не объявляет (приёмка F8, Р1), и фикстура, привязанная
	// к настоящему объявлению, истекла бы вместе с ним.
	provider := base + "\nexport const STEP_UP_METHODS = [\"totp\", \"lookup_secret\"] as const;\n"
	sides, err := ownSecondFactorSides(pin, root, vocab, rule, provider)
	if err != nil {
		t.Fatalf("стороны own не прочитаны: %v", err)
	}
	if got := floorTwoFindings(sides, byFloor); len(got) != 1 {
		t.Fatalf("консоль без перечня нашей церемонии рядом с перечнем поставщика: находок по полу «2» %d, "+
			"ждали 1 — гейт судил бы own перечнем потока поставщика", len(got))
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ (один факт — объявление нашей церемонии): пол достижим.
	sides, err = ownSecondFactorSides(pin, root, vocab, rule, provider+ownDeclaration("totp", "lookup_secret"))
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

	// ГРАНИЦА СЛОВА: перечень нашей церемонии не читается ни из перечня потока
	// поставщика, ни изнутри более длинного имени; законный близнец — настоящее
	// объявление консоли — читается.
	for _, src := range []string{provider, "export const NOT_OWN_STEP_UP_METHODS = [\"totp\"] as const;"} {
		if got := parseOwnStepUpMethods(src); len(got) != 0 {
			t.Fatalf("перечень нашей церемонии прочитан из чужого объявления: %v", got)
		}
	}
	real := parseOwnStepUpMethods(readFileForTest(t, stepUpMethodsDeclaration))
	if len(real) == 0 {
		t.Fatalf("перечень нашей церемонии не прочитан из %s — граница слова отсекла бы и настоящее "+
			"объявление", stepUpMethodsDeclaration)
	}
	t.Logf("перепись: записей с полом «2» %d · консоль ведёт на own %v · служба у пина %s ведёт вторым фактором %v",
		len(byFloor["2"]), real, pin, sides.Second)
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

// TestIdentitySecondFactorInjection_ExternalLandingIsARefusal — строки `external`
// в таблице посадок нет (#2857), и стенд, вновь объявивший эту посадку, — отказ с
// её именем и причиной, а не молчание и не вердикт.
//
// Утверждается в паре с законным близнецом: та же консоль на посадке `own`
// читается и даёт вердикт. Без близнеца отказ на `external` был бы неотличим от
// отказа, который гейт отдал бы на любой вход.
func TestIdentitySecondFactorInjection_ExternalLandingIsARefusal(t *testing.T) {
	console := readFileForTest(t, stepUpMethodsDeclaration)

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же консоль на посадке own — вердикт есть.
	if _, err := sidesOfLanding(t, landingOwn, console); err != nil {
		t.Fatalf("посадка own с настоящей консолью не прочитана: %v — отказ ниже ничего не различал бы", err)
	}

	// Посадка external — отказ, названный по имени и по причине.
	_, err := sidesOfLanding(t, landingExternal, console)
	if err == nil {
		t.Fatal("посадка external дала вердикт — пол «2» на ней поднимать нечем, и вердикт здесь был бы " +
			"вердиктом без предмета")
	}
	for _, want := range []string{`"` + landingExternal + `"`, "поток поставщика"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("отказ на посадке external не называет %s: %v", want, err)
		}
	}

	// ВОЗВРАЩЁННЫЙ ПРЕДМЕТ СНЯТОЙ СТРОКИ: консоль снова объявляет перечень потока
	// поставщика. Отказ обязан остаться — строка снята, а не выключена отсутствием
	// перечня; вернуть посадку значит вернуть строку вместе с её сторонами.
	withProvider := console + "\nexport const STEP_UP_METHODS = [\"totp\", \"lookup_secret\"] as const;\n"
	if _, err := sidesOfLanding(t, landingExternal, withProvider); err == nil {
		t.Fatal("перечень потока поставщика в консоли вернул посадке external вердикт — строка судилась бы " +
			"сторонами, которых гейт больше не читает")
	}

	// Посадка вне таблицы — отказ с её именем, а не пропуск.
	if _, err := sidesOfLanding(t, "federated", console); err == nil || !strings.Contains(err.Error(), "federated") {
		t.Fatalf("посадка вне таблицы не названа отказом: %v", err)
	}
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

// packageFileWith — единственный компилируемый файл пакета, несущий текст needle,
// и его тело. Тело берётся у пакета, а не с диска: инъекция поверх инъекции
// обязана видеть первую.
func packageFileWith(t *testing.T, src goPackageSource, needle string) (rel, body string) {
	t.Helper()
	var found []string
	for _, f := range src.sortedFiles() {
		b := src.bodies[f]
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

// injectedWithCoordinate — injectedPackage и координата внесённого узла: файл и
// строка, на которую после замены ложится подстрока at текста repl. Замена обязана
// попасть ровно в одно место файла: второе вхождение означало бы, что инъекция
// бьёт не туда, куда названо.
func injectedWithCoordinate(t *testing.T, src goPackageSource, old, repl, at string) (goPackageSource, string) {
	t.Helper()
	rel, body := packageFileWith(t, src, old)
	if n := strings.Count(body, old); n != 1 {
		t.Fatalf("текст %q в %s встречается %d раз, ждали один — инъекция неоднозначна", old, rel, n)
	}
	j := strings.Index(repl, at)
	if j < 0 {
		t.Fatalf("узел %q не входит во внесённый текст %q — координату ждать не на чем", at, repl)
	}
	line := 1 + strings.Count(body[:strings.Index(body, old)]+repl[:j], "\n")
	out, err := src.with(rel, strings.Replace(body, old, repl, 1))
	if err != nil {
		t.Fatalf("инъекция в %s не разобрана: %v", rel, err)
	}
	return out, fmt.Sprintf("%s:%d", rel, line)
}

// Места исполнителя правила у пина, в которые пробы вносят дефект.
const (
	pinnedLevelOfCall  = "return levelOf(rows, presentations)"
	pinnedSetBinding   = "s := presented(presentations)"
	pinnedLadderHead   = "for _, r := range table {"
	pinnedLadderCond   = "if r.holds(s) {"
	pinnedLadderReturn = "return r.level, true"
	pinnedLadderMiss   = `return "", false`
	// pinnedLevelOfDoc — первая строка шапки `LevelOf`: перед ней инъекция ставит
	// объявление уровня пакета (init, постоянную, метод).
	pinnedLevelOfDoc = "// LevelOf — уровень сессии по множеству предъявленного в ней."
	// pinnedTOTPConstructor — конструктор предъявления кода по времени у пина.
	pinnedTOTPConstructor = "func TOTPPresented() Presentation { return Presentation{method: MethodTOTP} }"
)

// beforeLevelOf — объявление уровня пакета, внесённое перед `LevelOf`.
func beforeLevelOf(decl string) string { return decl + "\n\n" + pinnedLevelOfDoc }

// TestIdentitySecondFactorInjection_RuleOutsideTheInterpretationIsARefusal —
// строка, помощник или лестница, которых толкование не узнаёт, — отказ с
// координатой, а не догадка и не пропуск строки.
//
// Судится ТЕКСТ отказа: причина и координата внесённого узла. Один errors.Is
// не отличает отказ по своей причине от отказа соседней проверки — а своя
// проверка, мёртвая за спиной соседней, и была дефектом круга 2: исполнитель
// принимал любое присваивание и любой return, не судил второй аргумент `LevelOf`
// и аргумент условия строки.
func TestIdentitySecondFactorInjection_RuleOutsideTheInterpretationIsARefusal(t *testing.T) {
	pin, _, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)

	// ЗАКОННЫЙ БЛИЗНЕЦ: правило пина толкуется целиком, обе ступени на месте.
	rule := mustOwnRule(t, assurance, vocab)
	if len(rowsOfLevel(rule, "1")) == 0 || len(rowsOfLevel(rule, "2")) == 0 {
		t.Fatalf("у пина %s не толкованы строки «1» или «2» (%v) — утверждения ниже вакуумны", pin, rule.rows)
	}

	// at — внесённый узел, на строку которого обязан указать отказ; пусто — вся замена.
	cases := []struct{ name, old, repl, at, reason string }{
		{"условие строки — не выражение над предъявленным", pinnedSecondFactorRow, "len(s) > 1", "",
			"вне толкования"},
		{"постоянная вне словаря службы", "s.has(MethodWebAuthn) }", "s.has(MethodPasskey) }", "",
			"постоянная MethodPasskey вне словаря службы"},
		{"помощник, который не спрашивает способ", "p.method == MethodWebAuthn && p.userVerified", "p.userVerified", "",
			"не вида «есть предъявление способа с флагами»"},
		{"уровень считается не по этой таблице", pinnedLevelOfCall, "return levelOf(nil, presentations)", "nil",
			"не считает уровень таблицей `rows`"},
		{"исполнителю подана часть предъявленного", pinnedLevelOfCall, "return levelOf(rows, presentations[:1])",
			"presentations[:1]", "подаёт исполнителю не предъявленное целиком"},
		{"`LevelOf` переназначает предъявленное до вызова", pinnedLevelOfCall,
			"presentations = presentations[:1]\n\t" + pinnedLevelOfCall, "presentations = presentations[:1]",
			"несёт шаг помимо вызова исполнителя"},
		{"множество собрано не из предъявленного", pinnedSetBinding, "s := presented(nil)", "",
			"множество предъявленного собрано не из `presentations` целиком"},
		{"таблица переназначена до обхода", pinnedLadderHead, "table = table[3:]\n\t" + pinnedLadderHead,
			"table = table[3:]", "шаг вне лестницы"},
		{"уровень выдан до обхода", pinnedSetBinding, "return Level3, true\n\t" + pinnedSetBinding,
			"return Level3, true", "шаг вне лестницы"},
		{"обходится не таблица исполнителя", pinnedLadderHead, "for _, r := range rows {", "rows",
			"обходит не таблицу `table`"},
		{"лестница — не первая подошедшая строка", pinnedLadderCond, "if !r.holds(s) {", "",
			"не «первая строка с истинным условием даёт уровень»"},
		{"условие строки спрошено не о предъявленном", pinnedLadderCond, "if r.holds(nil) {", "nil",
			"спрашивает строку не о множестве предъявленного"},
		{"подошедшая строка не выдаёт сессию", pinnedLadderReturn, "return r.level, false", "",
			"подошедшая строка возвращает"},
		{"без подошедшей строки сессия выдаётся", pinnedLadderMiss, "return Level1, true", "",
			"без подошедшей строки возвращает"},

		// Круг 3: толкование читает ЛИТЕРАЛ объявления, служба исполняет ЗНАЧЕНИЕ
		// после инициализации пакета. Значение обязано быть литералом: всякая иная
		// запись в состояние, которое толкуется, — отказ на её узле.
		{"таблица переписана в init", pinnedLevelOfDoc, beforeLevelOf("func init() { rows = append(rows[:2:2], rows[3:]...) }"),
			"func init()", "`rows` употреблена вне объявления и вызова исполнителя"},
		{"условие строки переписано в init", pinnedLevelOfDoc, beforeLevelOf("func init() { rows[2].holds = rows[4].holds }"),
			"func init()", "`rows` употреблена вне объявления и вызова исполнителя"},
		{"таблица прочитана вне исполнителя", pinnedLevelOfDoc, beforeLevelOf("func census() int { return len(rows) }"),
			"func census()", "`rows` употреблена вне объявления и вызова исполнителя"},
		{"постоянная словаря переписана в init", pinnedLevelOfDoc, beforeLevelOf("func init() { MethodTOTP = MethodRecoveryCode }"),
			"func init()", "постоянная словаря MethodTOTP переписывается вне объявления"},
		{"адрес постоянной словаря взят", pinnedLevelOfDoc, beforeLevelOf("var totpAddr = &MethodTOTP"),
			"var totpAddr", "постоянная словаря MethodTOTP переписывается вне объявления"},
		{"способ словаря меняется вызовом", pinnedLevelOfDoc, beforeLevelOf("func (m *Method) rename(n string) { m.name = n }"),
			"func (m *Method)", "у способа словаря метод с получателем-указателем `rename`"},
		{"предобъявленное имя затенено пакетом", pinnedLevelOfDoc, beforeLevelOf("const true = false"),
			"const true", "пакет объявляет предобъявленное имя `true`"},

		// Круг 3: предъявление способа собирает конструктор пакета, и гейт считает
		// его предъявлением ЭТОГО способа — конструктор толкуется, а не
		// подразумевается.
		{"конструктор кода по времени собирает другой способ", pinnedTOTPConstructor,
			"func TOTPPresented() Presentation { return Presentation{method: MethodRecoveryCode} }", "",
			"способ recovery_code собирают конструкторов 2"},
		{"конструктор собирает предъявление помощником", pinnedTOTPConstructor,
			"func TOTPPresented() Presentation { return bestPresentation(MethodTOTP) }", "",
			"конструктор предъявления TOTPPresented не вида"},
		{"экспортированный перечень предъявлений", pinnedLevelOfDoc,
			beforeLevelOf("func PasswordAndCode() []Presentation { return []Presentation{PasswordPresented()} }"),
			"func PasswordAndCode()", "PasswordAndCode возвращает не одно предъявление"},
	}
	refused := 0
	for _, c := range cases {
		at := c.at
		if at == "" {
			at = c.repl
		}
		src, where := injectedWithCoordinate(t, assurance, c.old, c.repl, at)
		_, err := readOwnRule(src, vocab)
		if !errors.Is(err, errRuleNotInterpretable) {
			t.Errorf("%s: толкование ответило %v — неузнанная форма правила обязана быть отказом, иначе гейт "+
				"судил бы по правилу, которого служба не исполняет", c.name, err)
			continue
		}
		// Отказ по чужой причине или на чужом узле означал бы, что своя проверка
		// мертва и держится соседней.
		if msg := err.Error(); !strings.Contains(msg, where+": ") || !strings.Contains(msg, c.reason) {
			t.Errorf("%s: отказ %q — ждали координату %s и причину «%s»", c.name, msg, where, c.reason)
			continue
		}
		refused++
	}
	t.Logf("перепись: у пина %s строк правила %d · инъекций %d · отказов толкования с причиной и координатой %d",
		pin, len(rule.rows), len(cases), refused)
}

// TestIdentitySecondFactorInjection_EvaluatorSpelledOtherwiseIsTheSameRule — ось,
// на которой пин обязан МОЛЧАТЬ: исполнитель, записанный иначе, но исполняющий то
// же правило, толкуется без отказа и даёт тот же вердикт. Каждый близнец отличается
// от своей инъекции (соседняя проба) одним фактом — узел тот же, привязан он к
// законному значению. Без них «толкует каждый узел» было бы неотличимо от
// «отказывает на любой правке исполнителя».
func TestIdentitySecondFactorInjection_EvaluatorSpelledOtherwiseIsTheSameRule(t *testing.T) {
	byFloor := readCatalogFloors(t)
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)
	console := consoleWithoutOwnDeclaration(t) + ownDeclaration("totp")
	_, want := ownFloorTwo(t, pin, root, vocab, mustOwnRule(t, assurance, vocab), console, byFloor)
	if !want.Floors["2"] {
		t.Fatalf("у пина %s с консолью [totp] пол «2» недостижим (%v) — близнецам не с чем совпадать", pin, want.Floors)
	}

	twins := []struct{ name, twinOf, old, repl string }{
		{"предъявленное `LevelOf` под другим именем", "исполнителю подана часть предъявленного",
			"func LevelOf(presentations []Presentation) (Level, bool) {\n\t" + pinnedLevelOfCall,
			"func LevelOf(ps []Presentation) (Level, bool) {\n\treturn levelOf(rows, ps)"},
		{"множество под другим именем", "множество собрано не из предъявленного",
			pinnedSetBinding + "\n\t" + pinnedLadderHead + "\n\t\t" + pinnedLadderCond,
			"set := presented(presentations)\n\t" + pinnedLadderHead + "\n\t\tif r.holds(set) {"},
		{"таблица исполнителя под другим именем", "таблица переназначена до обхода",
			"func levelOf(table []row, presentations []Presentation) (Level, bool) {\n\t" + pinnedSetBinding + "\n\t" +
				pinnedLadderHead,
			"func levelOf(ladder []row, presentations []Presentation) (Level, bool) {\n\t" + pinnedSetBinding +
				"\n\tfor _, r := range ladder {"},
		{"множество собрано прямо в условии", "условие строки спрошено не о предъявленном",
			pinnedSetBinding + "\n\t" + pinnedLadderHead + "\n\t\t" + pinnedLadderCond,
			pinnedLadderHead + "\n\t\tif r.holds(presented(presentations)) {"},
		{"строка обхода под другим именем", "подошедшая строка не выдаёт сессию",
			pinnedLadderHead + "\n\t\t" + pinnedLadderCond + "\n\t\t\t" + pinnedLadderReturn,
			"for _, row := range table {\n\t\tif row.holds(s) {\n\t\t\treturn row.level, true"},

		// Круг 3: состояние пакета, которого толкование не читает, либо имя, которое
		// только ПИШЕТСЯ так же, — не отказ. Связывание по области видимости, а не
		// по написанию: иначе «значение = литерал» было бы неотличимо от «отказ на
		// любом слове rows».
		{"init, не трогающий таблицу", "таблица переписана в init", pinnedLevelOfDoc,
			beforeLevelOf("func init() { sort.Strings(nil) }")},
		{"параметр по имени rows в другой функции", "таблица прочитана вне исполнителя", pinnedLevelOfDoc,
			beforeLevelOf("func census(rows []int) int { return len(rows) }")},
		{"постоянная словаря прочитана в init", "постоянная словаря переписана в init", pinnedLevelOfDoc,
			beforeLevelOf("func init() { m := MethodTOTP; _ = m }")},
		{"адрес копии постоянной", "адрес постоянной словаря взят", pinnedLevelOfDoc,
			beforeLevelOf("var totpCopy = MethodTOTP\n\nvar totpAddr = &totpCopy")},
		{"метод способа с получателем-значением", "способ словаря меняется вызовом", pinnedLevelOfDoc,
			beforeLevelOf("func (m Method) renamed(n string) Method { m.name = n; return m }")},
		{"пакетное имя, похожее на предобъявленное", "предобъявленное имя затенено пакетом", pinnedLevelOfDoc,
			beforeLevelOf("const truth = false")},
		{"конструктор с флагом рядом со способом", "конструктор кода по времени собирает другой способ",
			pinnedTOTPConstructor, "func TOTPPresented() Presentation { return Presentation{userVerified: false, " +
				"method: MethodTOTP} }"},
		{"неэкспортированный помощник, собирающий предъявление", "конструктор собирает предъявление помощником",
			pinnedLevelOfDoc, beforeLevelOf("func codeOf(m Method) Presentation { return bestPresentation(m) }")},
	}
	for _, c := range twins {
		src, _ := injectedWithCoordinate(t, assurance, c.old, c.repl, c.repl)
		rule, err := readOwnRule(src, vocab)
		if err != nil {
			t.Fatalf("%s (близнец «%s»): толкование отказало — %v; то же правило записано иначе, и отказ здесь "+
				"значит, что проба краснеет на любой правке исполнителя", c.name, c.twinOf, err)
		}
		_, got := ownFloorTwo(t, pin, root, vocab, rule, console, byFloor)
		if fmt.Sprint(got.Floors, got.Usable) != fmt.Sprint(want.Floors, want.Usable) {
			t.Fatalf("%s: вердикт %v %v, у пина %v %v — близнец исполняет то же правило", c.name,
				got.Floors, got.Usable, want.Floors, want.Usable)
		}
	}
	t.Logf("перепись: у пина %s близнецов исполнителя %d · молчат с вердиктом пина %v %v", pin, len(twins),
		want.Floors, want.Usable)
}

// TestIdentitySecondFactorInjection_SilentRootIsARefusal — корень, не подавший
// самоотчёту перечня вовсе, — отказ, а не «служба не провязала ничего» (круг 1:
// прежняя проба ловила только форму «nil вместо перечня»).
func TestIdentitySecondFactorInjection_SilentRootIsARefusal(t *testing.T) {
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)

	// ЗАКОННЫЙ БЛИЗНЕЦ: настоящий корень даёт перечень.
	want, _, err := ownWiredMethods(root, vocab)
	if err != nil || len(want) == 0 {
		t.Fatalf("перечень корня у пина %s не прочитан (%v, %v) — утверждения ниже вакуумны", pin, want, err)
	}
	list := "return []assurance.Method{" + pinnedSignInList + "}"
	// helper — помощник, отбирающий из перечня первый способ; ставится после
	// производителя, замыкающую скобку даёт сам файл.
	helper := "\n}\n\n// firstFactorOnly — только первый способ перечня.\n" +
		"func firstFactorOnly(ms []assurance.Method) []assurance.Method { return ms[:1] "

	// Судится текст отказа: причина и координата внесённого узла (круг 3: перечень
	// читался по постоянным В ЛЮБОМ месте тела производителя, и помощник,
	// отбиравший из него первый способ, читался перечнем целиком).
	//
	// at — внесённый узел, на строку которого обязан указать отказ; пусто — вся
	// замена. absent — отказ об ОТСУТСТВИИ узла: указывать ему не на что.
	for _, c := range []struct {
		name, old, repl, at, reason string
		absent                      bool
	}{
		{"наблюдатель провязки не зовётся вовсе", laneWiringObserver + "(ctx, cfg,", "unobservedLaneWiring(ctx, cfg,", "",
			"вызовов " + laneWiringObserver, true},
		{"производитель не называет ни одной постоянной словаря", list, "return nil", "",
			"возвращает перечней 0", true},
		{"перечень обёрнут помощником", list,
			"return firstFactorOnly([]assurance.Method{" + pinnedSignInList + "})" + helper, "return firstFactorOnly",
			"не голый литерал", false},
		{"перечень собран до возврата", list, "ms := []assurance.Method{" + pinnedSignInList + "}\n\treturn ms[:1]",
			"return ms[:1]", "не голый литерал", false},
		{"два перечня в двух ветвях", list,
			"if l.freshness > 0 {\n\t\treturn []assurance.Method{assurance.MethodPassword}\n\t}\n\t" + list,
			"[]assurance.Method{assurance.MethodPassword}",
			"возвращает перечней 2", false},
	} {
		at := c.at
		if at == "" {
			at = c.repl
		}
		src, where := injectedWithCoordinate(t, root, c.old, c.repl, at)
		wired, _, err := ownWiredMethods(src, vocab)
		if !errors.Is(err, errNoSignInList) {
			t.Errorf("%s: разбор корня ответил %v (%v) — «перечня нет» обязано быть отказом", c.name, wired, err)
			continue
		}
		if msg := err.Error(); !strings.Contains(msg, c.reason) || (!c.absent && !strings.Contains(msg, where+": ")) {
			t.Errorf("%s: отказ %q — ждали координату %s и причину «%s»", c.name, msg, where, c.reason)
		}
	}

	// ЗАКОННЫЕ БЛИЗНЕЦЫ (один факт против своей инъекции): тот же помощник объявлен,
	// но перечень возвращается голым литералом; ветвь «полосы нет» переписана
	// иначе. Перечень — тот же, что у пина.
	for _, c := range []struct{ name, twinOf, old, repl string }{
		{"помощник объявлен, перечень не обёрнут", "перечень обёрнут помощником", list, list + helper},
		{"перечень возвращается из ветви «полоса есть»", "два перечня в двух ветвях",
			"if !l.wired() {\n\t\treturn nil\n\t}\n\t" + list, "if l.wired() {\n\t\t" + list + "\n\t}\n\treturn nil"},
	} {
		got, _, err := ownWiredMethods(injectedPackage(t, root, c.old, c.repl), vocab)
		if err != nil || strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s (близнец «%s»): перечень %v, отказ %v — у пина %v", c.name, c.twinOf, got, err, want)
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

// pinnedRowsLiteral — объявление таблицы `rows` пина текстом: инъекция кладёт его
// во второй файл пакета.
func pinnedRowsLiteral(t *testing.T, assurance goPackageSource) string {
	t.Helper()
	_, body := packageFileWith(t, assurance, "var rows = []row{")
	from := strings.Index(body, "var rows = []row{")
	to := strings.Index(body[from:], "\n}\n")
	if to < 0 {
		t.Fatal("конец объявления `rows` у пина не найден — форма таблицы сменилась")
	}
	return body[from : from+to+3]
}

// TestIdentitySecondFactorInjection_RuleIsReadAsTheBuildCompilesIt — правило
// читается из тех файлов пакета, которые КОМПИЛИРУЕТ сборка, а не из всех,
// лежащих в каталоге (круг 3: файл с ограничением сборки `ignore` и файл с
// префиксом «_» несли прежнюю таблицу, и гейт брал её вместо той, что исполняет
// служба; поздний файл перетирал словарь).
//
// Строгое правило пина — строка «2» без кода по времени — с консолью [totp] пол
// «2» не поднимает. Рядом кладётся файл с мягкой таблицей, которую сборка
// исключает: вердикт обязан остаться вердиктом строгого правила.
func TestIdentitySecondFactorInjection_RuleIsReadAsTheBuildCompilesIt(t *testing.T) {
	byFloor := readCatalogFloors(t)
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)
	console := consoleWithoutOwnDeclaration(t) + ownDeclaration("totp")
	lenient := pinnedRowsLiteral(t, assurance)
	strict := injectedPackage(t, assurance, pinnedSecondFactorRow, "s.has(MethodPassword) && s.has(MethodLookupSecret)")
	strictRule := mustOwnRule(t, strict, vocab)
	if _, sides := ownFloorTwo(t, pin, root, vocab, strictRule, console, byFloor); sides.Floors["2"] {
		t.Fatalf("строгое правило с консолью [totp] поднимает пол «2» (%v) — утверждать об исключённом файле не "+
			"на чем", sides.Floors)
	}
	dir := kanameAssurancePackage + "/"

	// Файлы, которые сборка исключает: их объявления правилом не являются.
	for _, c := range []struct{ name, file, body string }{
		{"мягкая таблица под ограничением сборки ignore", "a_rows.go", "//go:build ignore\n\npackage assurance\n\n" + lenient},
		{"мягкая таблица в файле с префиксом «_»", "_rows.go", "package assurance\n\n" + lenient},
		{"мягкая таблица в файле другой ОС", "rows_windows.go", "package assurance\n\n" + lenient},
		{"словарь, переназначенный исключённым файлом", "z_vocab.go",
			"//go:build ignore\n\npackage assurance\n\nvar MethodLookupSecret = Method{\"totp\"}\n"},
	} {
		src, err := strict.with(dir+c.file, c.body)
		if err != nil {
			t.Errorf("%s: пакет с исключённым файлом не прочитан: %v", c.name, err)
			continue
		}
		rule, err := readOwnRule(src, assuranceVocabulary(src))
		if err != nil {
			t.Errorf("%s: толкование отказало (%v) — исключённый файл не должен менять правило", c.name, err)
			continue
		}
		if a, b := fmt.Sprint(rowsOfLevel(rule, "2")), fmt.Sprint(rowsOfLevel(strictRule, "2")); a != b {
			t.Errorf("%s: строки «2» прочитаны как %s, служба исполняет %s — гейт читает файл, который сборка "+
				"не компилирует", c.name, a, b)
		}
		if got, _ := ownFloorTwo(t, pin, root, vocab, rule, console, byFloor); len(got) != 1 {
			t.Errorf("%s: находок по полу «2» %d, ждали 1 — вердикт взят у исключённого файла", c.name, len(got))
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: исключённый файл без объявлений правила рядом с правилом пина
	// — вердикт пина.
	_, want := ownFloorTwo(t, pin, root, vocab, mustOwnRule(t, assurance, vocab), console, byFloor)
	gen, err := assurance.with(dir+"a_gen.go", "//go:build ignore\n\npackage assurance\n\nfunc generate() {}\n")
	if err != nil {
		t.Fatalf("пакет с исключённым файлом без объявлений правила не прочитан: %v", err)
	}
	if _, got := ownFloorTwo(t, pin, root, vocab, mustOwnRule(t, gen, vocab), console, byFloor); fmt.Sprint(got.Floors) != fmt.Sprint(want.Floors) {
		t.Errorf("исключённый файл без объявлений сменил вердикт: %v, у пина %v", got.Floors, want.Floors)
	}

	// Пакет, которого сборка не собрала бы либо собрала бы иначе, — отказ с
	// координатой: судить по нему значило бы судить не службу.
	for _, c := range []struct{ name, file, body, reason string }{
		{"вторая таблица в компилируемом файле", "a_rows.go", "package assurance\n\n" + lenient,
			"имя rows объявлено 2 раза"},
		{"второе объявление постоянной словаря", "z_vocab.go", "package assurance\n\nvar MethodTOTP = Method{\"x\"}\n",
			"имя MethodTOTP объявлено 2 раза"},
		{"файл одной архитектуры", "gen_arm64.go", "package assurance\n\nfunc generate() {}\n",
			"набор файлов пакета зависит от архитектуры"},
		{"исходник не на Go", "state.s", "// запись в состояние пакета мимо разбора\n",
			"сборка компилирует не-Go исходник"},
		{"cgo", "c.go", "package assurance\n\nimport \"C\"\n", "файл cgo"},
	} {
		_, err := assurance.with(dir+c.file, c.body)
		if !errors.Is(err, errPackageNotAsBuilt) || !strings.Contains(err.Error(), c.reason) ||
			!strings.Contains(err.Error(), dir+c.file) {
			t.Errorf("%s: разбор пакета ответил %v — ждали отказ «%s» с координатой %s", c.name, err, c.reason, dir+c.file)
		}
	}

	// Ограничение сборки, которого сборка не разбирает, — отказ, и причина сборки
	// лежит в его цепочке, а не только в тексте: вызывающий достаёт её
	// errors.Is/As, как и сторожа. Близнец отличается одной скобкой и читается.
	const buildLine, rest = "//go:build %slinux\n\n", "package assurance\n\nfunc generate() {}\n"
	if _, err := assurance.with(dir+"a_linux.go", fmt.Sprintf(buildLine, "")+rest); err != nil {
		t.Errorf("файл с разбираемым ограничением сборки не прочитан: %v", err)
	}
	_, err = assurance.with(dir+"a_linux.go", fmt.Sprintf(buildLine, "(")+rest)
	if !errors.Is(err, errPackageNotAsBuilt) || !strings.Contains(err.Error(), "правила сборки не прочитаны") ||
		!strings.Contains(err.Error(), dir+"a_linux.go") {
		t.Errorf("неразбираемое ограничение сборки: разбор пакета ответил %v — ждали отказ с координатой %s", err,
			dir+"a_linux.go")
	}
	if !causeInChain(err, "parsing //go:build line") {
		t.Errorf("отказ %v несёт причину сборки только текстом — в цепочке её нет, и вызывающему её не достать", err)
	}
}

// causeInChain — в цепочке err ниже корня есть ошибка с текстом, содержащим want:
// причина обёрнута, а не пересказана.
func causeInChain(err error, want string) bool {
	below := func(e error) []error {
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			return u.Unwrap()
		case interface{ Unwrap() error }:
			if next := u.Unwrap(); next != nil {
				return []error{next}
			}
		}
		return nil
	}
	queue := below(err)
	for len(queue) > 0 {
		e := queue[0]
		queue = append(queue[1:], below(e)...)
		if strings.Contains(e.Error(), want) {
			return true
		}
	}
	return false
}

// TestIdentitySecondFactorInjection_VocabularyWrittenByAnotherPackageIsARefusal —
// словарь правила экспортирован, и переписать его может любой пакет модуля,
// который его импортирует (круг 3: толкование читает литерал объявления, служба
// исполняет значение после инициализации ВСЕЙ программы). Пакет правила
// внутренний, поэтому импортёры — только пакеты своего модуля, и обход модуля
// перечисляет их всех.
func TestIdentitySecondFactorInjection_VocabularyWrittenByAnotherPackageIsARefusal(t *testing.T) {
	pin, root, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)
	importers := make([]string, 0, len(assurance.outside.importers))
	for _, p := range assurance.outside.importers {
		importers = append(importers, p.pkg)
	}
	if assurance.outside.walked == 0 || !contains(importers, kanameMainPackage) {
		t.Fatalf("обход модуля у пина %s: файлов %d, импортёры %v — корень %s, который пакет правила импортирует, "+
			"не найден, и утверждения ниже вакуумны", pin, assurance.outside.walked, importers, kanameMainPackage)
	}
	t.Logf("перепись: у пина %s файлов Go модуля прочитано %d · пакетов-импортёров пакета правила %d (%s)", pin,
		assurance.outside.walked, len(importers), strings.Join(importers, " "))

	imp := "import \"" + assurance.path + "\"\n\n"
	file := kanameMainPackage + "/zz_injected.go"
	injected := func(body string) goPackageSource {
		t.Helper()
		r, err := root.with(file, "package main\n\n"+body)
		if err != nil {
			t.Fatalf("инъекция в корень не разобрана: %v", err)
		}
		return r
	}

	// Отказ с координатой записи: файл-инъекция, строка 5 (после пакета и импорта).
	for _, c := range []struct{ name, body, reason string }{
		{"корень переписывает постоянную словаря", imp + "func init() { assurance.MethodTOTP = assurance.MethodRecoveryCode }\n",
			"постоянная словаря MethodTOTP переписывается вне объявления — пакетом " + kanameMainPackage},
		{"корень берёт адрес постоянной словаря", imp + "var totpAddr = &assurance.MethodTOTP\n",
			"постоянная словаря MethodTOTP переписывается вне объявления"},
		{"пакет правила импортирован точкой", "import . \"" + assurance.path + "\"\n\nvar _ = MethodTOTP\n",
			"пакет правила импортирован точкой"},
	} {
		_, err := readOwnRule(assurance.withImporter(injected(c.body)), vocab)
		want := file + ":"
		if !errors.Is(err, errRuleNotInterpretable) || !strings.Contains(err.Error(), c.reason) ||
			!strings.Contains(err.Error(), want) {
			t.Errorf("%s: толкование ответило %v — ждали отказ «%s» с координатой в %s", c.name, err, c.reason, file)
		}
	}

	// ЗАКОННЫЕ БЛИЗНЕЦЫ: чтение словаря; локальная переменная с именем пакета —
	// связывание по области видимости, а не по написанию.
	for _, c := range []struct{ name, twinOf, body string }{
		{"корень читает постоянную словаря", "корень переписывает постоянную словаря",
			imp + "var totpCopy = assurance.MethodTOTP\n"},
		{"запись в локальную переменную с именем пакета", "корень переписывает постоянную словаря",
			imp + "var _ = assurance.MethodTOTP\n\nfunc shadow() {\n\tassurance := struct{ MethodTOTP int }{}\n" +
				"\tassurance.MethodTOTP = 1\n\t_ = assurance\n}\n"},
	} {
		if _, err := readOwnRule(assurance.withImporter(injected(c.body)), vocab); err != nil {
			t.Errorf("%s (близнец «%s»): толкование отказало — %v", c.name, c.twinOf, err)
		}
	}

	// Обход, не нашедший ни одного импортёра, — не «снаружи никто не пишет», а
	// «снаружи не смотрели».
	blind := assurance
	blind.outside = &outsideState{}
	if _, err := readOwnRule(blind, vocab); !errors.Is(err, errRuleNotInterpretable) ||
		!strings.Contains(err.Error(), "не осмотрены") {
		t.Errorf("пустой обход модуля: толкование ответило %v — ждали отказ «не осмотрены»", err)
	}
}
