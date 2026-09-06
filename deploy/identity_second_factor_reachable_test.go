// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_second_factor_reachable_test.go — ПОЛ, КОТОРЫЙ НЕЧЕМ УДОВЛЕТВОРИТЬ,
// ЗАКРЫВАЕТ ГЛАГОЛ НАВСЕГДА (#1213).
//
// # Предмет
//
// Каталог прав объявляет части глаголов пол уровня уверенности «2». Край этот
// пол спрашивает — на всех полосах личности человека, включая браузерную
// (#1201). Значит у арендатора обязан существовать СПОСОБ поднять уровень; иначе
// объявленный пол означает не «подтвердите второй фактор», а «этого действия из
// браузера не существует» — и означает это для ВСЕХ, а не для нарушителей.
//
// Достижимость уровня — свойство ТРЁХ сторон сразу, и ни одна не видит его
// целиком:
//
//  1. настройки службы личности — какие методы второго фактора включены;
//  2. консоль — какие методы она умеет довести до конца в потоке `aal=aal2`;
//  3. каталог прав — сколько глаголов этим полом связано.
//
// Каждая сторона по отдельности валидна: настройки рендерятся, консоль
// собирается и её пробы зелены, каталог проходит свои гейты. Неверна их
// РАЗНИЦА — и увидеть её может только тот, кто читает все три.
//
// # Почему перепись печатает ДВА числа
//
// «Записей с полом „2“ — 32» само по себе выглядит как утверждение о защите. Оно
// скрывает ровно тот случай, ради которого задача заведена: из этих 32
// достижимых может быть НОЛЬ. Поэтому строка переписи несёт обе величины, и
// вторая вычисляется, а не предполагается.
//
// # Что здесь НЕ утверждается
//
// Не утверждается, что второй фактор устойчив к посреднику: это отдельный и
// более широкий предмет (#1188). Пол каталога сегодня нигде не выше «2», и
// проверяется ровно достижимость объявленных полов.
//
// # На что этот гейт опирается — названо, а не подразумевается
//
// Сторону КОНСОЛИ он читает объявлением (`step-up-methods.ts`), а не разбором
// вёрстки. Значит объявление обязано быть кем-то опровергаемо, иначе гейт судил
// бы по обещанию: держит его проба рядом с самим окном
// (`StepUpModal.secondfactor.test.tsx`) — она прогоняет окно ПО КАЖДОМУ
// названному способу. Правя одно, правь второе.
//
// Читается ОБЪЯВЛЕНИЕ, а не рендер: ни helm, ни кластер, ни браузер не нужны,
// поэтому проверка не умеет пропускаться.
package deploy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// stepUpMethodsDeclaration — сторона КОНСОЛИ, объявленная одной строкой.
	stepUpMethodsDeclaration = "../ui-future/shared/src/lib/step-up-methods.ts"
	// permissionCatalogEmbed — сторона КАТАЛОГА ПРАВ (обе встроенные копии
	// побайтово равны и держатся своим гейтом; читаем ту, что энфорсит край).
	permissionCatalogEmbed = "../gateway/internal/middleware/embed/permission_catalog.json"
	// identityRenderedConfigPath — путь, по которому процесс службы личности
	// ЧИТАЕТ наши настройки. Профиль, называющий его, доводит объявление до
	// процесса; не называющий — оставляет процесс на умолчаниях поставщика.
	identityRenderedConfigPath = "/etc/kaname-identity-rendered/kratos.yaml"
)

// identityMethodDecl — объявление одного метода службы личности.
type identityMethodDecl struct {
	Enabled bool
	Config  map[string]string
}

// parseIdentityMethods разбирает блок `selfservice.methods` тела настроек службы
// личности по отступам.
//
// Разбор строчный, а не через YAML-библиотеку, и это не лень: тело — Go-шаблон,
// в котором величины стоят подстановками (`{{ $app }}`), поэтому валидным YAML
// оно не является ни на одной ревизии. Тот же приём и у соседней проверки полос
// регистрации.
func parseIdentityMethods(body string) map[string]identityMethodDecl {
	indentOf := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
	lines := strings.Split(body, "\n")

	out := map[string]identityMethodDecl{}
	inMethods := false
	methodsIndent := -1
	method := ""
	inConfig := false

	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "{{") {
			continue
		}
		if !inMethods {
			if trimmed == "methods:" {
				inMethods, methodsIndent = true, indentOf(ln)
			}
			continue
		}
		ind := indentOf(ln)
		if ind <= methodsIndent {
			break // вышли из `methods:`
		}
		switch {
		case ind == methodsIndent+2 && strings.HasSuffix(trimmed, ":"):
			method = strings.TrimSuffix(trimmed, ":")
			inConfig = false
			if _, ok := out[method]; !ok {
				out[method] = identityMethodDecl{Config: map[string]string{}}
			}
		case method == "":
			// величина до первого имени метода — не наша
		case ind == methodsIndent+4:
			inConfig = false
			key, val, ok := splitYAMLPair(trimmed)
			if !ok {
				continue
			}
			if key == "config" && val == "" {
				inConfig = true
				continue
			}
			if key == "enabled" {
				d := out[method]
				d.Enabled = val == "true"
				out[method] = d
			}
		case inConfig && ind == methodsIndent+6:
			if key, val, ok := splitYAMLPair(trimmed); ok {
				out[method].Config[key] = val
			}
		}
	}
	return out
}

// splitYAMLPair режет `ключ: величина`, отбрасывая хвостовой комментарий.
func splitYAMLPair(trimmed string) (key, val string, ok bool) {
	idx := strings.Index(trimmed, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(trimmed[:idx])
	val = strings.TrimSpace(trimmed[idx+1:])
	if i := strings.Index(val, "#"); i >= 0 {
		val = strings.TrimSpace(val[:i])
	}
	return key, val, key != ""
}

// secondFactorMethods отбирает из объявленных методов те, что дают ВТОРОЙ
// фактор, то есть поднимают сессию до `aal2`.
//
// ПРЕДПОСЫЛКА, НАЗВАННАЯ ЯВНО (служба личности версии, объявленной в том же
// теле — `version: v1.3.1`):
//
//	totp, lookup_secret          — второй фактор всегда;
//	webauthn                     — второй фактор ТОЛЬКО когда `passwordless`
//	                               не включён; в беспарольной посадке это
//	                               ПЕРВЫЙ фактор (`aal1`), и в потоке
//	                               `aal=aal2` служба его не предлагает вовсе;
//	code                         — второй фактор только при `mfa_enabled`;
//	passkey, password, oidc, …   — первый фактор by construction.
//
// Предпосылка проверяема: она привязана к объявленной версии службы, и её
// смена обязана идти вместе с перемером этой функции.
func secondFactorMethods(decls map[string]identityMethodDecl) []string {
	var out []string
	for name, d := range decls {
		if !d.Enabled {
			continue
		}
		switch name {
		case "totp", "lookup_secret":
			out = append(out, name)
		case "webauthn":
			if d.Config["passwordless"] != "true" {
				out = append(out, name)
			}
		case "code":
			if d.Config["mfa_enabled"] == "true" {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// firstFactorMethods — методы, которыми арендатор входит вообще (дают `aal1`).
func firstFactorMethods(decls map[string]identityMethodDecl) []string {
	var out []string
	for name, d := range decls {
		if !d.Enabled {
			continue
		}
		switch name {
		case "password", "passkey", "oidc":
			out = append(out, name)
		case "webauthn":
			if d.Config["passwordless"] == "true" {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

var stepUpMethodsLiteral = regexp.MustCompile(`(?s)STEP_UP_METHODS\s*=\s*\[(.*?)\]`)
var quotedToken = regexp.MustCompile(`"([a-z_]+)"`)

// parseStepUpMethods читает сторону КОНСОЛИ — единственное объявление способов,
// которые окно повторного подтверждения умеет довести до конца.
func parseStepUpMethods(src string) []string {
	m := stepUpMethodsLiteral.FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	var out []string
	for _, g := range quotedToken.FindAllStringSubmatch(m[1], -1) {
		out = append(out, g[1])
	}
	sort.Strings(out)
	return out
}

// catalogEntry — ровно то, что нужно этой проверке.
type catalogEntry struct {
	FQN            string `json:"fqn"`
	RequiredACRMin string `json:"required_acr_min"`
}

func readCatalogFloors(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(permissionCatalogEmbed)) // #nosec G304 -- путь-константа собственного дерева
	if err != nil {
		t.Fatalf("каталог прав не прочитан (%s): %v", permissionCatalogEmbed, err)
	}
	var entries []catalogEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("каталог прав не разобран (%s): %v", permissionCatalogEmbed, err)
	}
	byFloor := map[string][]string{}
	for _, e := range entries {
		byFloor[e.RequiredACRMin] = append(byFloor[e.RequiredACRMin], e.FQN)
	}
	return byFloor
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- путь-константа собственного дерева
	if err != nil {
		t.Fatalf("объявление не прочитано (%s): %v", path, err)
	}
	return string(raw)
}

// attainableFloors — какие полы уровня браузерная сессия способна предъявить.
//
// «Способна» означает пару: служба личности метод ВКЛЮЧАЕТ и консоль его ВЕДЁТ.
// Включённый, но неведомый консоли метод достижимости не даёт — арендатору
// нечем им воспользоваться; ведомый, но выключенный — тем более.
func attainableFloors(secondFactor, firstFactor, drivable []string) (floors map[string]bool, usable []string) {
	set := map[string]bool{}
	for _, m := range drivable {
		set[m] = true
	}
	for _, m := range secondFactor {
		if set[m] {
			usable = append(usable, m)
		}
	}
	sort.Strings(usable)
	return map[string]bool{
		// «0» — анонимный пол: удовлетворяется любой живой сессией.
		"0": true,
		"1": len(firstFactor) > 0,
		"2": len(usable) > 0,
		// «3» — аппаратно-связанный уровень. Служба личности объявленной версии
		// его не выдаёт вовсе, поэтому пол «3», появившись в каталоге, был бы
		// недостижим by construction — и это находка, а не умолчание.
		"3": false,
	}, usable
}

func TestIdentity_SecondFactorReachesTheBrowser(t *testing.T) {
	methods := parseIdentityMethods(readFileForTest(t, identityConfigTemplate))
	if len(methods) == 0 {
		t.Fatalf("методов службы личности не разобрано ни одного (%s) — вердикта нет: "+
			"«ноль находок» здесь неотличимо от «ноль прочитанного». Либо блок "+
			"`selfservice.methods` переехал, либо разбор перестал его видеть",
			identityConfigTemplate)
	}

	drivable := parseStepUpMethods(readFileForTest(t, stepUpMethodsDeclaration))
	if len(drivable) == 0 {
		t.Fatalf("перечень способов подтверждения консоли пуст либо не разобран (%s) — "+
			"вердикта нет. Объявление обязано существовать и быть непустым: "+
			"иначе достижимость уровня считалась бы по одной стороне из двух",
			stepUpMethodsDeclaration)
	}

	second := secondFactorMethods(methods)
	first := firstFactorMethods(methods)
	floors, usable := attainableFloors(second, first, drivable)

	byFloor := readCatalogFloors(t)
	total := 0
	for _, v := range byFloor {
		total += len(v)
	}

	t.Logf("перепись настроек: методов объявлено %d · первым фактором %d (%s) · "+
		"вторым фактором %d (%s) · консоль ведёт %d (%s) · пригодны обеим сторонам %d (%s)",
		len(methods), len(first), strings.Join(first, " "),
		len(second), strings.Join(second, " "),
		len(drivable), strings.Join(drivable, " "),
		len(usable), strings.Join(usable, " "))

	floorNames := make([]string, 0, len(byFloor))
	for f := range byFloor {
		floorNames = append(floorNames, f)
	}
	sort.Strings(floorNames)

	for _, f := range floorNames {
		n := len(byFloor[f])
		if f == "" {
			t.Logf("перепись каталога: записей всего %d · без объявленного пола %d "+
				"(пол не спрашивается)", total, n)
			continue
		}
		reachable := 0
		if floors[f] {
			reachable = n
		}
		t.Logf("перепись каталога: записей всего %d · с полом «%s» %d · из них достижимых из браузера %d",
			total, f, n, reachable)
		if reachable == n {
			continue
		}
		sample := append([]string(nil), byFloor[f]...)
		sort.Strings(sample)
		if len(sample) > 3 {
			sample = sample[:3]
		}
		t.Errorf("пол уровня «%s» объявлен у %d записей каталога и НЕДОСТИЖИМ из браузера: "+
			"достижимых %d.\n"+
			"  служба личности включает вторым фактором: %v\n"+
			"  окно повторного подтверждения ведёт:      %v\n"+
			"  пригодны обеим сторонам:                  %v\n"+
			"Пол, который нечем удовлетворить, означает не «подтвердите второй фактор», "+
			"а «этого действия из браузера не существует» — и означает это для ВСЕХ. "+
			"Чинится ПАРОЙ: метод включается в настройках службы личности (%s) И ведётся "+
			"окном (%s). Одной стороны мало: включённый, но неведомый метод так же "+
			"недостижим, как выключенный. Например: %v",
			f, n, reachable, second, drivable, usable,
			identityConfigTemplate, stepUpMethodsDeclaration, sample)
	}
}

// identityChainMountsOurConfig — цепочка профилей ДОВОДИТ наши настройки до
// процесса службы личности (а не только рендерит их).
//
// Спрашивается у ЦЕПОЧКИ, а не у отдельного профиля: накладка (`values.fe3455-*`)
// поднимает продукт вместе со слоем под собой и своей провязки не несёт — судить
// её в одиночку значило бы называть находкой нормальную раскладку слоёв.
func identityChainMountsOurConfig(texts []string) bool {
	for _, t := range texts {
		if strings.Contains(t, identityRenderedConfigPath) {
			return true
		}
	}
	return false
}

// identityChainRaisesIdentity — цепочка поднимает службу личности.
func identityChainRaisesIdentity(texts []string) bool {
	for _, t := range texts {
		if kratosEnabled.MatchString(t) {
			return true
		}
	}
	return false
}

var kratosEnabled = regexp.MustCompile(`(?m)^kratos:\n(?:[ \t].*\n|\n)*?\s+enabled:\s*true`)

// shadowedSecondFactors — методы второго фактора, О КОТОРЫХ ПРОФИЛЬ ВЫСКАЗАЛСЯ
// САМ, в обход единственного объявления.
func shadowedSecondFactors(text string) []string {
	var out []string
	for _, m := range secondFactorKey.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// secondFactorKey — имена, высказывание о которых в профиле есть второе мнение
// о втором факторе.
//
// `code` сюда НЕ входит намеренно: вторым фактором он становится только при
// `mfa_enabled`, а само слово в профилях встречается в чужих значениях — гейт с
// ним ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
// Появится посадка, где `code` объявлен вторым фактором, — имя добавляется сюда
// вместе с ней.
var secondFactorKey = regexp.MustCompile(`(?m)^\s+(totp|lookup_secret|webauthn|passkey):`)

func TestIdentity_EveryStackDeclaresTheSecondFactor(t *testing.T) {
	stacks := deployStacks(t)

	raising, mounting, clean := 0, 0, 0
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		chain := stacks[name]
		texts := make([]string, 0, len(chain))
		for _, prof := range chain {
			texts = append(texts, readFileForTest(t, filepath.Join(umbrellaDir, prof)))
		}
		if !identityChainRaisesIdentity(texts) {
			continue
		}
		raising++

		if !identityChainMountsOurConfig(texts) {
			t.Errorf("стенд %q (%v) поднимает службу личности и НЕ доводит до неё наши "+
				"настройки (%s): процесс работает на умолчаниях подчарта поставщика, "+
				"где метода второго фактора нет вовсе. Значит объявленный каталогом "+
				"прав пол уровня «2» на этом стенде недостижим ДЛЯ ВСЕХ",
				name, chain, identityRenderedConfigPath)
			continue
		}
		mounting++

		shadowed := map[string][]string{}
		for i, prof := range chain {
			if sh := shadowedSecondFactors(texts[i]); len(sh) > 0 {
				shadowed[prof] = sh
			}
		}
		if len(shadowed) == 0 {
			clean++
			continue
		}
		t.Errorf("стенд %q доводит наши настройки службы личности до процесса и ПРИ ЭТОМ "+
			"его профили сами высказываются о методах второго фактора: %v.\n"+
			"Процесс получает два источника настроек и сливает их по порядку — то есть "+
			"какая из двух величин победит, решает порядок, которого никто не выбирал. "+
			"Два места об одном предмете, из которых верно одно: метод объявляется "+
			"ОДИН раз, в %s.\n"+
			"Наблюдалось: профиль объявлял метод второго фактора выключенным рядом с "+
			"включённым в единственном объявлении, и замер на стенде читал выключенным "+
			"то, что дерево включает",
			name, shadowed, identityConfigTemplate)
	}

	if raising == 0 {
		t.Fatal("ни один стенд не поднимает службу личности — проверка беспредметна, " +
			"и её зелёный ничего не значит")
	}
	t.Logf("перепись стендов: объявлено %d · поднимают службу личности %d · "+
		"доводят настройки до процесса %d · не заводят второго мнения о втором факторе %d",
		len(stacks), raising, mounting, clean)
}
