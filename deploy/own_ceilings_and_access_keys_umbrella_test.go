// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_ceilings_and_access_keys_umbrella_test.go — КОПИЯ ЧАРТА СЛУЖБЫ ДОСТУПА В
// ЗОНТЕ ОБЯЗАНА ОТДАВАТЬ КАЖДЫЙ ПОТОЛОК СЛУЖБЫ И ТРИ ВЕЛИЧИНЫ ПРИВЯЗКИ КЛЮЧЕЙ
// ДОСТУПА (kacho#2715, Ф7 kacho#1273).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Копия рендерила блок `own-ceilings` из ТРЁХ ручек, а таблица потолков службы
// растёт: четвёртая строка — `own-ceilings.access-keys-per-user` — пришла в
// `39628487` (kaname#270). Проверка потолков у службы БЕЗУСЛОВНА (`validate.go`):
// она не смотрит ни на режим, ни на посадку. Значит первый же подъём пина дал бы
// службу, которая под этой копией чарта отказывает стартом НА КАЖДОМ СТЕНДЕ, —
// четвёртого потолка рендер не отдаёт ни при каких значениях профиля. К нему
// добавляются три ручки `authn.access-keys.*` (`required_settings.go`), которых
// в шаблоне тоже не было.
//
// Признак на день заведения (kacho `origin/main` @ `ac33f733`):
//
//	git grep -c -iE 'access-keys|ACCESS_KEYS|accessKeys' origin/main \
//	  -- deploy gateway/deploy   → 0 (код 1)
//
// Задать величину профилем было нельзя ни одним ключом: шаблон отдавал ровно три
// потолка, а карты дополнительных переменных в развёртывании копии нет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЕРЕЧЕНЬ ВЫВОДИТСЯ ИЗ ПИНА, А НЕ ВЫПИСЫВАЕТСЯ ЗДЕСЬ
//
// Выписанный перечень — второе место об одном предмете: он разошёлся бы с
// таблицей службы молча, и разошёлся бы ровно на подъёме пина, то есть там, где
// расхождение и опасно. Поэтому ведущая половина проверки ЧИТАЕТ таблицу у
// пиненного модуля (`go.mod` даёт версию, `go env GOMODCACHE` — каталог: признак,
// который дерево ПРОИЗВОДИТ, а не путь, угаданный проверкой) — тот же приём, что
// у outsourced_module_links_the_same_foundation_test.go.
//
// Следствие, ради которого это и сделано: проверка ЕДЕТ ВМЕСТЕ С ПИНОМ. Подняли
// пин — новая ручка появляется в перечне сама, и чарт, её не выражающий, краснеет
// ДО подъёма стенда, а не отказом пода после.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ЗНАЧИТ НА СЕГОДНЯШНЕМ ПИНЕ, СКАЗАНО ВСЛУХ
//
// Пин — `a3c0158af93e` (kacho#2915; от `06966a081c7a` отличается только пином corelib): таблица потолков там ЧЕТЫРЁХСТРОЧНАЯ,
// четвёртая строка пришла в `39628487` (kaname#270). Выводимая половина требует
// все четыре — перепись печатает «из пина выведено ручек N · сверх пина
// проверено M» на каждом прогоне. На пине заведения (`16b5cade`) таблица была трёхстрочной, и
// четвёртый потолок держала вторая половина — фиксированным предметом этой
// задачи. Перепись печатает обе величины: одно число скрыло бы ровно тот
// случай, ради которого проверка заведена.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Она не утверждает, что ВЕЛИЧИНЫ верны: годность имени доверяющей стороны и
// перечня происхождений — свойство установки, и судит его страж старта службы.
// Она утверждает, что каждой ручке ЕСТЬ ЧЕМ доехать до процесса.
package deploy_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// kanameModulePart — последний сегмент пути модуля службы доступа в go.mod.
const kanameModulePart = "kaname"

// accessKeyConfigKeys — три ключа блока `authn.access-keys` и источники их
// величин в значениях чарта. Перечень ФИКСИРОВАН: это предмет ЭТОЙ задачи. На
// пине её заведения (`16b5cade`) вывести его было неоткуда; на пине
// `4af7fd4fafb4` (kacho#2906) ручки у службы объявлены (`internal/apps/kaname/
// config/required_settings.go`, полоса `own`), а перечень здесь по-прежнему
// выписан.
//
// Имя доверяющей стороны своей ручки в блоке не имеет (kacho#2905): чарт берёт
// его из общего узла личности — того же, из которого его берут настройки службы
// личности.
var accessKeyConfigKeys = []struct{ configKey, source string }{
	{"rp-id", "global.kacho.identity.webauthnRpId"},
	{"origins", "config.authn.accessKeys.origins"},
	{"algorithms", "config.authn.accessKeys.algorithms"},
}

// accessKeysCeilingKey — четвёртый потолок: предмет этой же задачи.
const accessKeysCeilingKey = "access-keys-per-user"

// knobCensus — объём осмотренного, обе величины отдельно.
type knobCensus struct {
	Pin            string // версия модуля службы, закреплённая go.mod этого дерева
	DerivedFromPin int    // ручек, ВЫВЕДЕННЫХ из таблиц пиненного модуля
	BeyondPin      int    // ручек, проверенных СВЕРХ пина (предмет этой задачи)
}

func (c knobCensus) String() string {
	return fmt.Sprintf("пин службы %s · из пина выведено ручек %d · сверх пина проверено %d",
		c.Pin, c.DerivedFromPin, c.BeyondPin)
}

// kanameModuleDir — каталог пиненного модуля службы НА ДИСКЕ.
//
// Резолв — go.mod (версия) плюс `go env GOMODCACHE` (каталог кэша): признак,
// который дерево производит, а не путь, угаданный проверкой.
func kanameModuleDir(t *testing.T, root string) string {
	t.Helper()
	version := productModulePins(t, root)[kanameModulePart]
	if version == "" {
		t.Fatalf("go.mod этого дерева не пинит %s%s — перечень ручек выводить неоткуда, "+
			"и «находок нет» здесь означало бы «сверять было не с чем»",
			productModuleprefix, kanameModulePart)
	}
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)),
		escapeModulePath(productModuleprefix+kanameModulePart)+"@"+version)
	if _, err := os.Stat(dir); err != nil {
		// НЕ ВЫПОЛНИЛОСЬ, а не «согласны»: молчание непрочитанного утверждением
		// о согласии не является.
		t.Fatalf("исходники пиненного модуля %s%s@%s не прочитаны (%v).\n"+
			"Кэш модулей наполняется сборкой: прогони `go mod download %s%s` и повтори — "+
			"иначе вердикт этой проверки беспредметен",
			productModuleprefix, kanameModulePart, version, err, productModuleprefix, kanameModulePart)
	}
	return dir
}

// ceilingKeysOfPin — ключи таблицы потолков, ПРОЧИТАННЫЕ у пиненного модуля.
//
// Читается литерал ключа рядом с префиксом секции, а не имя поля: поле
// переименовывается свободно, а ключ настройки — это контракт с профилем.
func ceilingKeysOfPin(t *testing.T, moduleDir string) []string {
	t.Helper()
	path := filepath.Join(moduleDir, "internal", "apps", "kaname", "config", "own_ceilings.go")
	body, err := os.ReadFile(path) // #nosec G304 -- путь собран из пина go.mod, не из ввода
	if err != nil {
		t.Fatalf("таблица потолков пиненного модуля не прочитана (%s): %v", path, err)
	}
	re := regexp.MustCompile(`ownCeilingKeyPrefix\s*\+\s*"([a-z0-9-]+)"`)
	var keys []string
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			keys = append(keys, m[1])
		}
	}
	if len(keys) == 0 {
		// Пустой обход — ОТКАЗ, а не «ноль находок»: форма таблицы сменилась, и
		// проверка судила бы о непрочитанном.
		t.Fatalf("в таблице потолков пиненного модуля не найдено НИ ОДНОГО ключа (%s) — "+
			"форма объявления сменилась, и перечень выводить больше нечем. Почини разбор, "+
			"а не ослабляй проверку", path)
	}
	sort.Strings(keys)
	return keys
}

// accessKeyKnobsOfPin — ручки привязки ключей доступа у пиненного модуля.
//
// Пусто — законный исход, а не отказ: на пине, где привязки ещё нет, выводить
// нечего. Число печатается переписью, поэтому «ноль выведено» отличимо от
// «ноль проверено».
func accessKeyKnobsOfPin(t *testing.T, moduleDir string) []string {
	t.Helper()
	path := filepath.Join(moduleDir, "internal", "apps", "kaname", "config", "required_settings.go")
	body, err := os.ReadFile(path) // #nosec G304 -- путь собран из пина go.mod, не из ввода
	if err != nil {
		t.Fatalf("перечень обязательных настроек пиненного модуля не прочитан (%s): %v", path, err)
	}
	re := regexp.MustCompile(`accessKeyRequirement\("([a-z0-9-]+)"`)
	var keys []string
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		keys = append(keys, m[1])
	}
	sort.Strings(keys)
	return keys
}

// kanameServiceConfig — тело `config.yaml` службы из рендера, разобранное как YAML.
func kanameServiceConfig(t *testing.T, rendered string) map[string]any {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	docs := 0
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if err != nil {
			break
		}
		docs++
		if doc == nil {
			continue
		}
		if kind, _ := doc["kind"].(string); kind != "ConfigMap" {
			continue
		}
		data, _ := doc["data"].(map[string]any)
		raw, ok := data["config.yaml"].(string)
		if !ok {
			continue
		}
		var cfg map[string]any
		if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatalf("тело настроек службы не разбирается как YAML: %v", err)
		}
		return cfg
	}
	t.Fatalf("в рендере нет карты настроек с ключом config.yaml (документов разобрано %d, байт %d) — "+
		"вердикта НЕТ: «ручки не найдено» здесь неотличимо от «настроек не найдено вовсе»",
		docs, len(rendered))
	return nil
}

// section — вложенная карта по пути ключей; отсутствие отличимо от пустоты.
func configSection(cfg map[string]any, keys ...string) (map[string]any, bool) {
	cur := cfg
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// ownCeilingsSet — значения профиля, объявляющие ВСЕ потолки пина плюс четвёртый.
// Подаются точечными установками, чтобы рендер судил шаблон, а не базовый профиль.
func ownCeilingsSet(keys []string) []string {
	valueKey := map[string]string{
		"accounts-per-identity":           "accountsPerIdentity",
		"credentials-per-user":            "credentialsPerUser",
		"credentials-per-service-account": "credentialsPerServiceAccount",
		"access-keys-per-user":            "accessKeysPerUser",
	}
	var sets []string
	for _, k := range keys {
		if v, ok := valueKey[k]; ok {
			sets = append(sets, "config.ownCeilings."+v+"=1")
		}
	}
	return sets
}

// TestOwnCeilings_RenderEmitsEveryCeilingOfThePin — ВЫВОДИМАЯ половина: каждый
// потолок таблицы пиненного модуля доезжает до карты настроек (проверка
// потолков у службы безусловна). Посадка у службы одна (kaname#363), и
// рендер один — второй посадки, на которой его стоило бы повторить, нет.
func TestOwnCeilings_RenderEmitsEveryCeilingOfThePin(t *testing.T) {
	moduleDir := kanameModuleDir(t, "..")
	pinned := ceilingKeysOfPin(t, moduleDir)
	census := knobCensus{
		Pin:            productModulePins(t, "..")[kanameModulePart],
		DerivedFromPin: len(pinned),
	}

	// Четвёртый потолок — предмет ЭТОЙ задачи. На сегодняшнем пине его в таблице
	// нет, поэтому он добавляется сверх выведенного и считается отдельно.
	want := append([]string{}, pinned...)
	if !contains(pinned, accessKeysCeilingKey) {
		want = append(want, accessKeysCeilingKey)
		census.BeyondPin++
	}
	sort.Strings(want)

	for _, posture := range []string{kanameLanding} {
		rendered, err := renderIdentitySubchart(t, nil, ownCeilingsSet(want)...)
		if err != nil {
			t.Fatalf("рендер подчарта на посадке %s не удался: %v\n%s", posture, err, rendered)
		}
		ceilings, ok := configSection(kanameServiceConfig(t, rendered), "own-ceilings")
		if !ok {
			t.Fatalf("посадка %s: в настройках нет секции `own-ceilings` вовсе — "+
				"проверка потолков у службы безусловна, и стенд не поднимется", posture)
		}
		for _, k := range want {
			if _, present := ceilings[k]; !present {
				t.Errorf("посадка %s: потолок `own-ceilings.%s` рендер НЕ отдаёт при объявленной "+
					"величине — служба откажет в старте с именем ручки, и отказ придёт на КАЖДОМ "+
					"стенде: проверка потолков не зависит ни от режима, ни от посадки", posture, k)
			}
		}
	}
	t.Logf("перепись: %s · потолков в ожидании %d (%s)", census, len(want), strings.Join(want, ", "))
}

// TestAccessKeys_RenderEmitsTheBindingUnderOwn — три величины привязки доезжают
// до карты настроек, когда профиль их объявил, и НЕ рендерятся, когда не
// объявил: незаданное обязано доехать до стража незаданным.
func TestAccessKeys_RenderEmitsTheBindingUnderOwn(t *testing.T) {
	moduleDir := kanameModuleDir(t, "..")
	pinned := accessKeyKnobsOfPin(t, moduleDir)
	census := knobCensus{
		Pin:            productModulePins(t, "..")[kanameModulePart],
		DerivedFromPin: len(pinned),
		BeyondPin:      len(accessKeyConfigKeys),
	}

	// Перечень пина не должен УЙТИ ВПЕРЁД фиксированного: ручка, появившаяся у
	// службы и не покрытая здесь, — находка, а не молчание.
	for _, k := range pinned {
		if !containsConfigKey(accessKeyConfigKeys, k) {
			t.Errorf("у пиненного модуля появилась ручка привязки `authn.access-keys.%s`, "+
				"которой эта проверка не знает — допиши её в перечень вместе со шаблоном, "+
				"иначе чарт выразить её не сможет", k)
		}
	}

	sets := []string{
		"global.kacho.identity.webauthnRpId=access.example.invalid",
		"config.authn.accessKeys.origins[0]=https://console.access.example.invalid",
		"config.authn.accessKeys.algorithms[0]=-7",
	}
	rendered, err := renderIdentitySubchart(t, nil, sets...)
	if err != nil {
		t.Fatalf("рендер подчарта на посадке own не удался: %v\n%s", err, rendered)
	}
	binding, ok := configSection(kanameServiceConfig(t, rendered), "authn", "access-keys")
	if !ok {
		t.Fatalf("посадка own: секции `authn.access-keys` в настройках нет вовсе при объявленных " +
			"величинах — профиль на `own` не может выразить привязку ни одним ключом, и служба " +
			"откажет в старте (Ф7, kacho#1273)")
	}
	for _, k := range accessKeyConfigKeys {
		if _, present := binding[k.configKey]; !present {
			t.Errorf("посадка own: ключ `authn.access-keys.%s` рендер НЕ отдаёт при объявленной "+
				"величине (`%s`)", k.configKey, k.source)
		}
	}
	if got := binding["rp-id"]; got != "access.example.invalid" {
		t.Errorf("имя доверяющей стороны взято не из общего узла личности: `rp-id` = %v, "+
			"а `global.kacho.identity.webauthnRpId` = access.example.invalid (kacho#2905)", got)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: профиль без блока (умолчания подчарта его не несут) —
	// секции нет, и это НЕ находка: незаданное обязано доехать до стража
	// незаданным, а не пустой строкой.
	bare, err := renderIdentitySubchart(t, nil)
	if err != nil {
		t.Fatalf("рендер подчарта на умолчаниях не удался: %v\n%s", err, bare)
	}
	if _, present := configSection(kanameServiceConfig(t, bare), "authn", "access-keys"); present {
		t.Error("профиль без блока: секция `authn.access-keys` рендерится, хотя профиль её не " +
			"объявлял — незаданная величина доедет до стража ПУСТОЙ, а не незаданной")
	}

	// ПУСТОЙ перечень происхождений — законная величина «никого», и она обязана
	// быть ОТЛИЧИМА от незаданной. Ветвь по `hasKey`, а не `with`, держит ровно
	// это различие.
	none, err := renderIdentitySubchart(t, nil,
		"config.authn.accessKeys.algorithms[0]=-7",
		"config.authn.accessKeys.origins=null")
	if err != nil {
		t.Fatalf("рендер подчарта с пустым перечнем происхождений не удался: %v\n%s", err, none)
	}
	if binding, ok := configSection(kanameServiceConfig(t, none), "authn", "access-keys"); ok {
		if _, present := binding["origins"]; present {
			t.Error("СНЯТЫЙ ключ происхождений (`origins=null`) отрендерился как объявленный — " +
				"«никого» и «не задано» перестали различаться, а это разные решения")
		}
	}

	t.Logf("перепись: %s · ключей привязки в ожидании %d", census, len(accessKeyConfigKeys))
}

// TestAccessKeys_RelyingPartyAndConsoleOriginComeFromTheSharedIdentityNode —
// имя доверяющей стороны и происхождение консоли у привязки ключей доступа
// берутся из общего узла личности, а не из литерала профиля (kacho#2905).
//
// Предмет — имя доверяющей стороны и происхождение консоли выводятся из общего
// узла. Ключ браузер привязывает к имени доверяющей стороны; пока имя стояло
// литералом профиля рядом с общим ключом, расхождение ничем не держалось: у
// площадки `fe3455` оно и было — литерал назвал одно имя, общий узел этой
// цепочки другое. Прежде читателей было два — ещё настройки внешнего поставщика
// личности; их подчарт больше не производит (kacho#2818), и судится один.
//
// Судится ИСХОД рендера, а не текст шаблона: вывод из общего узла на одних
// значениях, отказ рендера на снятой ручке и на двойном объявлении перечня, и
// запасной путь `domain` при пустом `webauthnRpId`.
func TestAccessKeys_RelyingPartyAndConsoleOriginComeFromTheSharedIdentityNode(t *testing.T) {
	var converged, refused, twins int

	// (а) ВЫВОД: на одних значениях общего узла — ожидаемое имя и ожидаемое
	// происхождение. Цепочка стенда разработки — настоящий вход; величины
	// подаются поверх, нерезолвимыми (RFC 2606).
	for _, tc := range []struct {
		name  string
		sets  []string
		rp    string
		orign string
	}{
		{
			name: "объявлен webauthnRpId",
			sets: []string{
				"global.kacho.identity.webauthnRpId=access.example.invalid",
				"global.kacho.identity.appBaseURL=https://console.access.example.invalid",
			},
			rp: "access.example.invalid", orign: "https://console.access.example.invalid",
		},
		{
			name: "webauthnRpId пуст — имя из domain",
			sets: []string{
				"global.kacho.identity.webauthnRpId=",
				"global.kacho.identity.domain=access.example.invalid",
				"global.kacho.identity.appBaseURL=",
				"global.kacho.identity.appSubdomain=console",
			},
			rp: "access.example.invalid", orign: "https://console.access.example.invalid",
		},
	} {
		sets := append([]string{
			"config.authn.accessKeys.origins=null",
			"config.authn.accessKeys.originFromConsole=true",
		}, tc.sets...)
		out, err := renderIdentitySubchart(t, chainOf(t, "dev"), sets...)
		if err != nil {
			t.Fatalf("%s: рендер отказал: %v\n%s", tc.name, err, out)
		}
		binding, ok := configSection(kanameServiceConfig(t, out), "authn", "access-keys")
		if !ok {
			t.Fatalf("%s: секции `authn.access-keys` нет — сходимость судить не с чем", tc.name)
		}
		if binding["rp-id"] != tc.rp {
			t.Errorf("%s: имя доверяющей стороны не выведено из общего узла — "+
				"`access-keys.rp-id` = %v, ждали %q", tc.name, binding["rp-id"], tc.rp)
		}
		got := fmt.Sprint(binding["origins"])
		if got != "["+tc.orign+"]" {
			t.Errorf("%s: происхождение консоли не выведено из общего узла — "+
				"`access-keys.origins` = %s, ждали [%s]", tc.name, got, tc.orign)
		}
		converged++
	}

	// (б) ОТКАЗЫ РЕНДЕРА: снятая ручка и двойное объявление перечня называются,
	// а не выбрасываются молча.
	for _, tc := range []struct {
		name, want string
		sets       []string
	}{
		{
			name: "в профиле объявлен снятый rpId",
			want: "config.authn.accessKeys.rpId снят",
			sets: []string{"config.authn.accessKeys.rpId=access.example.invalid"},
		},
		{
			name: "originFromConsole вместе с origins",
			want: "перечень происхождений дважды",
			sets: []string{
				"config.authn.accessKeys.originFromConsole=true",
				"config.authn.accessKeys.origins[0]=https://console.access.example.invalid",
			},
		},
	} {
		out, err := renderIdentitySubchart(t, nil, tc.sets...)
		if err == nil || !strings.Contains(out, tc.want) {
			t.Errorf("%s: рендер обязан отказать с %q, получено err=%v\n%s", tc.name, tc.want, err, out)
			continue
		}
		refused++
	}

	// (в) ЗАКОННЫЕ БЛИЗНЕЦЫ: `originFromConsole: false` с «никого» и с
	// перечнем профиля — рендер проходит. Второй отличается от отказа (б) ровно
	// одним фактом — значением ручки.
	out, err := renderIdentitySubchart(t, nil,
		"config.authn.accessKeys.originFromConsole=false",
		"config.authn.accessKeys.origins=null")
	if err != nil {
		t.Fatalf("близнец «originFromConsole=false, никого»: рендер отказал: %v\n%s", err, out)
	}
	twins++
	out, err = renderIdentitySubchart(t, nil,
		"config.authn.accessKeys.originFromConsole=false",
		"config.authn.accessKeys.origins[0]=https://console.access.example.invalid")
	if err != nil {
		t.Fatalf("близнец «originFromConsole=false с перечнем»: рендер отказал: %v\n%s", err, out)
	}
	if binding, ok := configSection(kanameServiceConfig(t, out), "authn", "access-keys"); !ok ||
		fmt.Sprint(binding["origins"]) != "[https://console.access.example.invalid]" {
		t.Errorf("близнец «originFromConsole=false с перечнем»: перечень профиля не доехал: %v", binding)
	}
	twins++

	t.Logf("перепись: вывод из общего узла %d · отказов рендера %d · законных близнецов %d",
		converged, refused, twins)
	if converged != 2 || refused != 2 || twins != 2 {
		t.Fatalf("перепись неполна: вывод %d из 2, отказов %d из 2, близнецов %d из 2",
			converged, refused, twins)
	}
}

func containsConfigKey(xs []struct{ configKey, source string }, v string) bool {
	for _, x := range xs {
		if x.configKey == v {
			return true
		}
	}
	return false
}
