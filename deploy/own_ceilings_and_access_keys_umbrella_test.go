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
// службу, которая под этой копией чарта отказывает стартом НА КАЖДОМ СТЕНДЕ,
// включая посадку `external`, — четвёртого потолка рендер не отдаёт ни при каких
// значениях профиля. Под `own` к нему добавляются три ручки `authn.access-keys.*`
// (`required_settings.go`, `Lanes: own`), которых в шаблоне тоже не было.
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
// Пин — `16b5cade`: там таблица потолков ТРЁХСТРОЧНАЯ и ручек `access-keys` нет
// вовсе. Значит выводимая половина сегодня требует трёх потолков и нуля ручек
// привязки — она не вакуумна (три потолка она требует и проверяет), но четвёртый
// потолок и привязку держит ВТОРАЯ половина, с фиксированным предметом этой
// задачи. Перепись печатает обе величины: «из пина выведено N» и «сверх пина
// проверено M». Одно число скрыло бы ровно тот случай, ради которого проверка
// заведена.
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

// accessKeyConfigKeys — три ключа блока `authn.access-keys` и имена величин в
// значениях чарта. Перечень ФИКСИРОВАН намеренно: это предмет ЭТОЙ задачи, и на
// сегодняшнем пине вывести его неоткуда — в службе он появляется позже.
var accessKeyConfigKeys = []struct{ configKey, valueKey string }{
	{"rp-id", "rpId"},
	{"origins", "origins"},
	{"algorithms", "algorithms"},
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
// потолок таблицы пиненного модуля доезжает до карты настроек, и это верно на
// ОБЕИХ посадках (проверка потолков у службы безусловна).
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

	for _, posture := range []string{"own", "external"} {
		sets := append([]string{"config.authn.identityProvider=" + posture}, ownCeilingsSet(want)...)
		rendered, err := renderIdentitySubchart(t, nil, sets...)
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
		"config.authn.identityProvider=own",
		"config.authn.accessKeys.rpId=access.example.invalid",
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
				"величине (`config.authn.accessKeys.%s`)", k.configKey, k.valueKey)
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: профиль без блока — секции нет, и это НЕ находка. Под
	// `external` привязка не читается, а под `own` незаданное обязано доехать до
	// стража незаданным, а не пустой строкой.
	bare, err := renderIdentitySubchart(t, nil, "config.authn.identityProvider=external")
	if err != nil {
		t.Fatalf("рендер подчарта на посадке external не удался: %v\n%s", err, bare)
	}
	if _, present := configSection(kanameServiceConfig(t, bare), "authn", "access-keys"); present {
		t.Error("посадка external: секция `authn.access-keys` рендерится, хотя профиль её не " +
			"объявлял — незаданная величина доедет до стража ПУСТОЙ, а не незаданной")
	}

	// ПУСТОЙ перечень происхождений — законная величина «никого», и она обязана
	// быть ОТЛИЧИМА от незаданной. Ветвь по `hasKey`, а не `with`, держит ровно
	// это различие.
	none, err := renderIdentitySubchart(t, nil,
		"config.authn.identityProvider=own",
		"config.authn.accessKeys.rpId=access.example.invalid",
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

func containsConfigKey(xs []struct{ configKey, valueKey string }, v string) bool {
	for _, x := range xs {
		if x.configKey == v {
			return true
		}
	}
	return false
}
