// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notifications_flag_test.go — пробы ФЛАГА УСТАНОВКИ и ВЫВЕДЕННОГО ПЕРЕЧНЯ
// источников notify (NTF-1, полоса D2; замысел З28, CX1-83, CX1-106, CX1-111,
// УК20; приёмка NTF1-N01, NTF1-N02, NTF1-N04, NTF1-I01).
//
// ─────────────────────────────────────────────────────────────────────────────
// НА ЧЁМ РЕНДЕРИТСЯ
//
// Каждая проба рендерит ФИКСТУРНУЮ КОПИЮ зонтика (`notifyUmbrellaCopy`): копия
// снимается с отслеживаемого дерева и материализуется владельцем зависимостей,
// поэтому архив чарта notify в ней собран из ТЕКУЩИХ шаблонов, а не взят из
// когда-то собранного `charts/notify-*.tgz` рабочей копии. Цепочки — таблица
// `deploy/stacks.txt` через обёртку D9 (`prod` несёт образец оператора).
//
// ПОРЯДОК ПРОВЕРОК НЕСУЩИЙ: сперва фикстура (helm в PATH, копия собрана,
// каждая цепочка рендерится как есть), и только потом — предмет. Отказ
// фикстуры — «НЕ ВЫПОЛНИЛОСЬ», не красный: поломка фикстуры не вправе выдать
// себя за отсутствие возможности.
//
// Ни одна проба не ссылается на символ, которого в дереве ещё нет: предмет
// судится по рендеру и по тексту файлов, иначе отсутствие помощника дало бы
// ошибку компиляции пакета — «не выполнилось» у всех проб пакета сразу.

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

const (
	// ntfFlagKey — ручка глобального флага установки (NTF1-N01).
	ntfFlagKey = "global.kacho.notifications.enabled"
	// ntfModulesKey — узел переопределений модулей (CX1-106).
	ntfModulesKey = "global.kacho.notifications.modules"
	// ntfProbeEnv — переменная флага процесса `notify-probe` (NTF1-N02).
	ntfProbeEnv = "KACHO_NOTIFYPROBE_NOTIFICATIONS_ENABLED"
	// ntfProbeSource — шаблон рабочей нагрузки `notify-probe` в зонтике.
	ntfProbeSource = "kacho-umbrella/templates/notify-probe.yaml"
	// ntfKanameConfigSource — шаблон ConfigMap kaname в зонтике.
	ntfKanameConfigSource = "kacho-umbrella/charts/kaname/templates/configmap.yaml"
	// ntfNotifySourcePrefix — префикс строки `# Source:` объектов чарта notify.
	ntfNotifySourcePrefix = "kacho-umbrella/charts/notify/"
	// ntfProbeModule — имя источника `notify-probe` в перечне notify.
	ntfProbeModule = "notify-probe"
)

// ntfExpectedRoster — выведенный перечень источников notify по цепочке на
// голове D2: таблица З28 (CX1-83; Д102 — у `a8f60d` перечень пуст).
var ntfExpectedRoster = map[string][]string{
	"dev":         {ntfProbeModule},
	"dev-prod":    {ntfProbeModule},
	"prorobotech": {ntfProbeModule},
	"fe3455":      {ntfProbeModule},
	"own":         {},
	"prod":        {},
	"a8f60d":      {},
}

// requireHelmNTF — helm в PATH; его нет — «НЕ ВЫПОЛНИЛОСЬ» отказом пробы, а не
// пропуском (пропуск читался бы как «проверено»).
func requireHelmNTF(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: helm не в PATH — рендерная проба флага обязана исполняться, условие не создано")
	}
}

// ntfChains — имена цепочек таблицы, отсортированные; пустая таблица — отказ.
func ntfChains(t *testing.T) []string {
	t.Helper()
	names := sortedStackNames(deployStacksForRender(t, "operator.yaml"))
	if len(names) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: в таблице цепочек ноль строк — судить нечего")
	}
	for _, n := range names {
		if _, ok := ntfExpectedRoster[n]; !ok {
			t.Fatalf("КРАСНЫЙ: цепочка %q таблицы не названа таблицей перечня З28 этой пробы — новая цепочка правит таблицу тем же изменением", n)
		}
	}
	if len(names) != len(ntfExpectedRoster) {
		t.Fatalf("КРАСНЫЙ: цепочек в таблице %d, в таблице перечня З28 %d — строка без цепочки", len(names), len(ntfExpectedRoster))
	}
	return names
}

// ntfRender — рендер копии; объекты разобраны. err — отказ helm с выводом.
func ntfRender(t *testing.T, c umbrellaCopy, files []string, sets ...string) ([]renderedObj, string, error) {
	t.Helper()
	out, err := c.render(files, sets...)
	if err != nil {
		return nil, out, err
	}
	return parseRendered(t, out), out, nil
}

// ntfMustRender — рендер-фикстура: отказ — «НЕ ВЫПОЛНИЛОСЬ» с хвостом вывода.
func ntfMustRender(t *testing.T, c umbrellaCopy, what string, files []string, sets ...string) []renderedObj {
	t.Helper()
	objs, out, err := ntfRender(t, c, files, sets...)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: опорный рендер %s отказал (%v):\n%s", what, err, lastLines(out, 6))
	}
	if len(objs) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: опорный рендер %s пуст", what)
	}
	return objs
}

// ntfRosterModules — модули выведенного перечня notify рендера, отсортированы.
func ntfRosterModules(t *testing.T, objs []renderedObj) []string {
	t.Helper()
	var notifyObjs []renderedObj
	for _, o := range objs {
		if strings.HasPrefix(o.source, ntfNotifySourcePrefix) {
			notifyObjs = append(notifyObjs, o)
		}
	}
	recs, err := renderedRoster(notifyObjs)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: перечень notify в рендере не разбирается: %v", err)
	}
	out := []string{}
	for _, r := range recs {
		out = append(out, r.Module)
	}
	sort.Strings(out)
	return out
}

// ntfProbeFlag — значение переменной флага `notify-probe`; второй возврат —
// отрендерена ли рабочая нагрузка пробы вовсе.
func ntfProbeFlag(objs []renderedObj) (string, bool) {
	for _, o := range objs {
		if o.source != ntfProbeSource || o.kind != "Deployment" {
			continue
		}
		for _, c := range containersOf(nPodSpec(o)) {
			for _, e := range nlist(c["env"]) {
				if m, ok := e.(map[string]any); ok && nstr(m["name"]) == ntfProbeEnv {
					return nstr(m["value"]), true
				}
			}
		}
		return "<переменной нет>", true
	}
	return "", false
}

// ntfKanameFlag — `notifications.enabled` из config.yaml ConfigMap kaname.
func ntfKanameFlag(t *testing.T, objs []renderedObj) (any, bool) {
	t.Helper()
	for _, o := range objs {
		if o.source != ntfKanameConfigSource || o.kind != "ConfigMap" {
			continue
		}
		raw := nstr(ndig(o.doc, "data", "config.yaml"))
		var cfg map[string]any
		if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: config.yaml ConfigMap kaname не разбирается: %v", err)
		}
		v, ok := lookup(cfg, "notifications", "enabled")
		return v, ok
	}
	return nil, false
}

// ntfEffectiveValues — значения цепочки, наложенные как helm: values.yaml
// зонтика копии, затем файлы цепочки.
func ntfEffectiveValues(t *testing.T, c umbrellaCopy, files []string) map[string]any {
	t.Helper()
	eff := mergeValues(nil, readYAML(t, filepath.Join(c.umbrella, "values.yaml")))
	for _, f := range files {
		eff = mergeValues(eff, readYAML(t, f))
	}
	return eff
}

// ntfRefusalNames — отказ рендера называет ручку name (дословно в выводе).
func ntfRefusalNames(out string, err error, name string) error {
	if err == nil {
		return fmt.Errorf("рендер ПРОШЁЛ — отказа нет")
	}
	if !strings.Contains(out, name) {
		return fmt.Errorf("рендер отказал, но без имени %s: %s", name, lastLines(out, 3))
	}
	if strings.Contains(out, "interface conversion") {
		return fmt.Errorf("отказ — падение разыменования, а не fail с именем ручки: %s", lastLines(out, 3))
	}
	return nil
}

// ─── NTF1-N01 ───────────────────────────────────────────────────────────────

// TestNTF1N01_UnsetGlobalFlagRefusesEveryChain — глобальный флаг не задан —
// рендер отвергнут с именем ручки (близнец — задан: рендер проходит).
func TestNTF1N01_UnsetGlobalFlagRefusesEveryChain(t *testing.T) {
	requireHelmNTF(t)
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	names := ntfChains(t)

	// Фикстура: каждая цепочка рендерится как есть (близнец N01).
	for _, n := range names {
		ntfMustRender(t, c, "цепочки "+n, c.chainFiles(t, n))
	}

	// Предмет (а): ручка объявлена в values.yaml зонтика булевым значением.
	v, ok := lookup(readYAML(t, filepath.Join(c.umbrella, "values.yaml")), "global", "kacho", "notifications", "enabled")
	if !ok {
		t.Errorf("КРАСНЫЙ: %s в values.yaml зонтика не объявлен — флага установки нет (NTF1-N01)", ntfFlagKey)
	} else if _, isBool := v.(bool); !isBool {
		t.Errorf("КРАСНЫЙ: %s в values.yaml зонтика = %#v — ожидалось булево значение (УК20)", ntfFlagKey, v)
	}

	// Предмет (б): снятая ручка — отказ рендера каждой цепочки с её именем.
	refused := 0
	for _, n := range names {
		_, out, err := ntfRender(t, c, c.chainFiles(t, n), ntfFlagKey+"=null")
		if rerr := ntfRefusalNames(out, err, ntfFlagKey); rerr != nil {
			t.Errorf("КРАСНЫЙ: цепочка %s без %s: %v (NTF1-N01)", n, ntfFlagKey, rerr)
			continue
		}
		refused++
	}
	t.Logf("перепись N01: цепочек %d; рендер как есть прошёл у %d; отказ с именем ручки без флага у %d",
		len(names), len(names), refused)
}

// TestNTF1N01_FlagValueMustBeBoolean — значение флага не булево (пустая
// строка, строка «false») — отказ рендера с именем ручки, а не молчаливое
// «выключено» (УК20; замысел З12, З28 `kindIs`).
func TestNTF1N01_FlagValueMustBeBoolean(t *testing.T) {
	requireHelmNTF(t)
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	files := c.chainFiles(t, "dev")
	ntfMustRender(t, c, "цепочки dev", files)

	cases := []struct{ what, set, name string }{
		{"пустая строка глобального флага", ntfFlagKey + "=", ntfFlagKey},
		{"строка «false» глобального флага", ntfFlagKey + "=false", ntfFlagKey},
		{"пустая строка переопределения модуля", ntfModulesKey + ".notifyProbe.enabled=", "notifyProbe"},
	}
	for _, tc := range cases {
		// c.render ставит `--set` перед каждым набором; строковое значение
		// (пустая строка, «false» в кавычках) требует `--set-string`.
		out, err := ntfRenderSetString(c, files, tc.set)
		if rerr := ntfRefusalNames(out, err, tc.name); rerr != nil {
			t.Errorf("КРАСНЫЙ: %s (%s): %v (УК20)", tc.what, tc.set, rerr)
			continue
		}
		t.Logf("%s → отказ рендера с именем %s", tc.what, tc.name)
	}
}

// ntfRenderSetString — рендер копии с одним `--set-string`.
func ntfRenderSetString(c umbrellaCopy, files []string, set string) (string, error) {
	args := []string{"template", "kacho-umbrella", c.umbrella, "-n", "kacho"}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	args = append(args, "--set-string", set)
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы пробы
	return string(out), err
}

// ─── NTF1-N02 на дереве ─────────────────────────────────────────────────────

// TestNTF1N02_ModuleOverrideTurnsOffOnlyThatModule — переопределение модуля
// под `global.kacho.notifications.modules` выключает только его; собственный
// ключ флага модуля вне `global` и ключ модуля не из таблицы — отказ рендера с
// именем ключа (CX1-106, CX1-111 (а), (б), (в)).
func TestNTF1N02_ModuleOverrideTurnsOffOnlyThatModule(t *testing.T) {
	requireHelmNTF(t)
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	files := c.chainFiles(t, "dev")
	ntfMustRender(t, c, "цепочки dev", files)
	globalOn := ntfFlagKey + "=true"

	// Близнец: глобальный true, модуль включён — проба в перечне, её флаг true.
	twin := ntfMustRender(t, c, "dev, глобальный true", files, globalOn, ntfModulesKey+".notifyProbe.enabled=true")
	if got := ntfRosterModules(t, twin); !equalStrings(got, []string{ntfProbeModule}) {
		t.Errorf("КРАСНЫЙ: dev, notifyProbe включён: перечень notify %v — ожидался [%s] (NTF1-N02 близнец)", got, ntfProbeModule)
	}
	if v, rendered := ntfProbeFlag(twin); !rendered || v != "true" {
		t.Errorf("КРАСНЫЙ: dev, notifyProbe включён: %s = %q (нагрузка отрендерена: %v) — ожидалось \"true\"", ntfProbeEnv, v, rendered)
	}

	// Наследование: глобальный true без переопределения — модуль включён.
	inherit := ntfMustRender(t, c, "dev, глобальный true без переопределений", files, globalOn, ntfModulesKey+"=null")
	if v, rendered := ntfProbeFlag(inherit); !rendered || v != "true" {
		t.Errorf("КРАСНЫЙ: dev, глобальный true, переопределений нет: %s = %q (нагрузка отрендерена: %v) — модуль наследует глобальный флаг, ожидалось \"true\"", ntfProbeEnv, v, rendered)
	}

	// Предмет: notifyProbe выключен переопределением — выключен только он.
	off := ntfMustRender(t, c, "dev, notifyProbe выключен", files, globalOn, ntfModulesKey+".notifyProbe.enabled=false")
	if got := ntfRosterModules(t, off); contains(got, ntfProbeModule) {
		t.Errorf("КРАСНЫЙ: dev, notifyProbe выключен: перечень notify %v всё ещё несёт %s", got, ntfProbeModule)
	}
	if v, rendered := ntfProbeFlag(off); !rendered || v != "false" {
		t.Errorf("КРАСНЫЙ: dev, notifyProbe выключен: %s = %q (нагрузка отрендерена: %v) — ожидалось \"false\"", ntfProbeEnv, v, rendered)
	}
	if v, ok := ntfKanameFlag(t, off); !ok || v != true {
		t.Errorf("КРАСНЫЙ: dev, выключен только notifyProbe: ConfigMap kaname notifications.enabled = %#v (есть: %v) — переопределение задело другой модуль", v, ok)
	}

	// CX1-111 (в): kaname выключается только переопределением под global.
	kOff := ntfMustRender(t, c, "dev, kaname выключен под global", files, globalOn, ntfModulesKey+".kaname.enabled=false")
	if v, ok := ntfKanameFlag(t, kOff); !ok || v != false {
		t.Errorf("КРАСНЫЙ: dev, %s.kaname.enabled=false: ConfigMap kaname notifications.enabled = %#v (есть: %v) — ожидалось false (CX1-111 (в))", ntfModulesKey, v, ok)
	}

	// Отрицания по таблице: ключ вне global и ключ не из таблицы.
	refusals := []struct{ what, set, name string }{
		{"собственный ключ флага notifyProbe вне global (N02)", "notifyProbe.notifications.enabled=false", "notifyProbe.notifications.enabled"},
		{"собственный ключ флага kaname вне global (CX1-111 (а))", "kaname.config.notifications.enabled=false", "kaname.config.notifications.enabled"},
		{"ключ модуля не из таблицы (CX1-111 (б))", ntfModulesKey + ".notifyprobe.enabled=false", "notifyprobe"},
	}
	for _, r := range refusals {
		_, out, err := ntfRender(t, c, files, globalOn, r.set)
		if rerr := ntfRefusalNames(out, err, r.name); rerr != nil {
			t.Errorf("КРАСНЫЙ: %s (%s): %v", r.what, r.set, rerr)
			continue
		}
		t.Logf("%s → отказ с именем %s", r.what, r.name)
	}
}

// ─── NTF1-N02 на фикстурной копии с `probe-b` ───────────────────────────────

// ntfModuleTableDefine — имя `define` в _sources.tpl, тело которого несёт
// строку модуля `notifyProbe` (таблица модулей З28). Ровно одно — иначе ошибка.
func ntfModuleTableDefine(src string) (string, error) {
	re := regexp.MustCompile(`\{\{-?\s*define\s+"([^"]+)"\s*-?\}\}`)
	locs := re.FindAllStringSubmatchIndex(src, -1)
	var hits []string
	for i, l := range locs {
		end := len(src)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if strings.Contains(src[l[1]:end], `"notifyProbe"`) {
			hits = append(hits, src[l[2]:l[3]])
		}
	}
	if len(hits) != 1 {
		return "", fmt.Errorf("define с телом, несущим строку модуля \"notifyProbe\": %d (%v) из %d определений", len(hits), hits, len(locs))
	}
	return hits[0], nil
}

// ntfProbeBEdit — правка копии _sources.tpl: таблица модулей дополняется
// строкой `probe-b`, выведенной из строки `notifyProbe` переименованием
// (форма строки не выписывается пробой — она берётся у дерева).
func ntfProbeBEdit(name string) func(string) string {
	return func(s string) string {
		decl := fmt.Sprintf(`define "%s"`, name)
		if strings.Count(s, decl) != 1 {
			return s
		}
		s = strings.Replace(s, decl, fmt.Sprintf(`define "%s.ntf1n02tree"`, name), 1)
		return s + fmt.Sprintf(`
{{- define %q -}}
{{- $out := list -}}
{{- range (include "%s.ntf1n02tree" . | fromJsonArray) -}}
{{- $out = append $out . -}}
{{- if contains "\"notifyProbe\"" (toJson .) -}}
{{- $out = append $out (toJson . | replace "notifyProbe" "probeB" | replace "notify-probe" "probe-b" | replace "notifyprobe" "probeb" | fromJson) -}}
{{- end -}}
{{- end -}}
{{- $out | toJson -}}
{{- end -}}
`, name, name)
	}
}

// TestNTF1N02_FixtureProbeBOverrideTurnsOffOnlyNotifyProbe — гейт рендера на
// фикстурной копии, где таблица несёт два источника `notify-probe` и
// `probe-b`: переопределение выключает только `notify-probe`; тот же ключ вне
// `global` — отказ с его именем; собственный ключ `probe-b` задан слоем —
// отказ с этим путём (отказ построен по таблице, а не по образцу, CX1-111 (3)).
func TestNTF1N02_FixtureProbeBOverrideTurnsOffOnlyNotifyProbe(t *testing.T) {
	requireHelmNTF(t)
	body, err := os.ReadFile(filepath.Join(notifyChartDir, "templates", "_sources.tpl"))
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s/templates/_sources.tpl не прочитан: %v", notifyChartDir, err)
	}
	name, err := ntfModuleTableDefine(string(body))
	if err != nil {
		t.Fatalf("КРАСНЫЙ: таблица модулей notify не несёт строки notifyProbe — %v (З28, NTF1-N02)", err)
	}
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{notifyEdits: map[string]func(string) string{
		"templates/_sources.tpl": ntfProbeBEdit(name),
	}})
	files := c.chainFiles(t, "dev")
	// sourceLimits источника probe-b — копия записи notify-probe (N03).
	eff := ntfEffectiveValues(t, c, files)
	if lim, ok := lookup(eff, c.effectiveDepName(t), "sourceLimits", ntfProbeModule); ok {
		layer := map[string]any{c.effectiveDepName(t): map[string]any{"sourceLimits": map[string]any{"probe-b": lim}}}
		raw, _ := yaml.Marshal(layer)
		p := filepath.Join(t.TempDir(), "probe-b-limits.yaml")
		if werr := os.WriteFile(p, raw, 0o600); werr != nil {
			t.Fatal(werr)
		}
		files = append(files, p)
	}
	globalOn := ntfFlagKey + "=true"

	twin := ntfMustRender(t, c, "копии, оба включены", files, globalOn, ntfModulesKey+".notifyProbe.enabled=true")
	if got := ntfRosterModules(t, twin); !equalStrings(got, []string{ntfProbeModule, "probe-b"}) {
		t.Errorf("КРАСНЫЙ: близнец — перечень %v, ожидался [notify-probe probe-b]", got)
	}
	off := ntfMustRender(t, c, "копии, notifyProbe выключен", files, globalOn, ntfModulesKey+".notifyProbe.enabled=false")
	if got := ntfRosterModules(t, off); !equalStrings(got, []string{"probe-b"}) {
		t.Errorf("КРАСНЫЙ: notifyProbe выключен — перечень %v, ожидался [probe-b]", got)
	}
	if v, _ := ntfProbeFlag(off); v != "false" {
		t.Errorf("КРАСНЫЙ: notifyProbe выключен — %s = %q, ожидалось \"false\"", ntfProbeEnv, v)
	}
	for _, r := range []struct{ what, set, name string }{
		{"ключ notifyProbe вне global", "notifyProbe.notifications.enabled=false", "notifyProbe.notifications.enabled"},
		{"собственный ключ probe-b задан слоем", "probeB.notifications.enabled=false", "probeB.notifications.enabled"},
	} {
		_, out, err := ntfRender(t, c, files, globalOn, r.set)
		if rerr := ntfRefusalNames(out, err, r.name); rerr != nil {
			t.Errorf("КРАСНЫЙ: %s (%s): %v", r.what, r.set, rerr)
		}
	}
}

// ─── NTF1-N04, УК20 ─────────────────────────────────────────────────────────

// ntfSecretRefs — объекты рендера, ссылающиеся на секрет name, по префиксу
// источника чарта (`charts/<имя>` либо `templates`).
func ntfSecretRefs(objs []renderedObj, name string) map[string][]string {
	out := map[string][]string{}
	for _, o := range objs {
		if name == "" || !ntfRefersSecret(o.doc, name) {
			continue
		}
		owner := "umbrella"
		if rest, ok := strings.CutPrefix(o.source, "kacho-umbrella/charts/"); ok {
			owner = strings.SplitN(rest, "/", 2)[0]
		}
		out[owner] = append(out[owner], o.kind+"/"+o.name)
	}
	return out
}

func ntfRefersSecret(v any, name string) bool {
	switch n := v.(type) {
	case map[string]any:
		for k, sub := range n {
			if m, ok := sub.(map[string]any); ok {
				switch k {
				case "secretKeyRef", "secretRef":
					if nstr(m["name"]) == name {
						return true
					}
				case "secret":
					if nstr(m["secretName"]) == name || nstr(m["name"]) == name {
						return true
					}
				}
			}
			if ntfRefersSecret(sub, name) {
				return true
			}
		}
	case []any:
		for _, e := range n {
			if ntfRefersSecret(e, name) {
				return true
			}
		}
	}
	return false
}

// TestNTF1N04_AllSourcesOffRendersNoNotify — все источники выключены — notify и
// ссылка на секрет почты не рендерятся; близнец — глобальный false и модуль
// `notifyProbe` true (вариант УК20): notify и ссылка есть.
func TestNTF1N04_AllSourcesOffRendersNoNotify(t *testing.T) {
	requireHelmNTF(t)
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	const chain = "a8f60d" // узел с удостоверением (credentialSecret)
	files := c.chainFiles(t, chain)
	ntfMustRender(t, c, "цепочки "+chain, files)
	secret := nstr(ndig(ntfEffectiveValues(t, c, files), "global", "kacho", "identity", "smtp", "credentialSecret", "name"))
	if secret == "" {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: у цепочки %s узел почты без credentialSecret — ссылку судить нечем", chain)
	}

	off := ntfMustRender(t, c, chain+", всё выключено", files, ntfFlagKey+"=false", ntfModulesKey+"=null")
	offNotify := notifySourceCount(off)
	offRefs := ntfSecretRefs(off, secret)["notify"]

	// Близнец не снимает узел переопределений: `--set <узел>=null` вместе с
	// набором внутри того же узла helm не разбирает («interface conversion» в
	// разборе --set) — это отказ фикстуры, а не предмета. Переопределения
	// цепочки, кроме notifyProbe, остаются; судится вхождение notify-probe.
	on := ntfMustRender(t, c, chain+", глобальный false, notifyProbe true", files,
		ntfFlagKey+"=false", ntfModulesKey+".notifyProbe.enabled=true")
	onNotify := notifySourceCount(on)
	onRefs := ntfSecretRefs(on, secret)["notify"]
	onRoster := ntfRosterModules(t, on)
	t.Logf("%s: выключено — объектов notify %d, ссылок notify на %s %d; глобальный false + notifyProbe true — объектов %d, ссылок %d, перечень %v",
		chain, offNotify, secret, len(offRefs), onNotify, len(onRefs), onRoster)

	if onNotify == 0 || len(onRefs) == 0 || !contains(onRoster, ntfProbeModule) {
		t.Errorf("КРАСНЫЙ: близнец N04 / УК20 (глобальный false, модуль notifyProbe true): объектов notify %d, ссылок на секрет %d, перечень %v — ожидались >0, >0, перечень с %s",
			onNotify, len(onRefs), onRoster, ntfProbeModule)
	}
	if offNotify != 0 || len(offRefs) != 0 {
		t.Errorf("КРАСНЫЙ: всё выключено: объектов notify %d, ссылок notify на секрет почты %d — ожидалось 0 и 0 (NTF1-N04)", offNotify, len(offRefs))
	}
}

// ─── NTF1-I01, перепись «цепочка → выведенный перечень» (CX1-83) ────────────

// TestNTF1I01_MailSecretOnlyAtNotifyAndRosterPerChain — по каждой цепочке:
// выведенный перечень равен таблице З28; секрет почты среди служб kacho — только
// у notify (там, где он рендерится); ссылки kaname печатаются числом отдельно;
// ConfigMap kaname несёт флаг, равный выводу `enabledFor` для kaname.
func TestNTF1I01_MailSecretOnlyAtNotifyAndRosterPerChain(t *testing.T) {
	requireHelmNTF(t)
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	names := ntfChains(t)
	type row struct {
		objs  []renderedObj
		files []string
	}
	rows := map[string]row{}
	for _, n := range names {
		files := c.chainFiles(t, n)
		rows[n] = row{objs: ntfMustRender(t, c, "цепочки "+n, files), files: files}
	}

	total := 0
	for _, n := range names {
		r := rows[n]
		total += len(r.objs)
		eff := ntfEffectiveValues(t, c, r.files)
		roster := ntfRosterModules(t, r.objs)
		want := ntfExpectedRoster[n]
		if !equalStrings(roster, want) {
			t.Errorf("КРАСНЫЙ: цепочка %s — выведенный перечень %v, таблица З28 — %v (CX1-83)", n, roster, want)
		}
		secret := nstr(ndig(eff, "global", "kacho", "identity", "smtp", "credentialSecret", "name"))
		refs := ntfSecretRefs(r.objs, secret)
		var foreign []string
		for owner, objs := range refs {
			switch owner {
			case "notify", "kaname":
			default:
				foreign = append(foreign, owner+": "+strings.Join(objs, ", "))
			}
		}
		sort.Strings(foreign)
		if len(foreign) > 0 {
			t.Errorf("КРАСНЫЙ: цепочка %s — секрет почты %q у служб kacho кроме notify: %s (NTF1-I01)", n, secret, strings.Join(foreign, "; "))
		}
		notifyObjs := notifySourceCount(r.objs)
		if len(want) > 0 && secret != "" && len(refs["notify"]) == 0 {
			t.Errorf("КРАСНЫЙ: цепочка %s — notify рендерится (перечень %v), узел несёт удостоверение %q, а ссылки notify на него нет", n, want, secret)
		}
		// Флаг kaname в ConfigMap = вывод enabledFor для kaname.
		wantK, wok := lookup(eff, "global", "kacho", "notifications", "modules", "kaname", "enabled")
		if _, isBool := wantK.(bool); !wok || !isBool {
			wantK, wok = lookup(eff, "global", "kacho", "notifications", "enabled")
		}
		gotK, gok := ntfKanameFlag(t, r.objs)
		if !wok {
			t.Errorf("КРАСНЫЙ: цепочка %s — %s не выводится из значений цепочки: флага нет (NTF1-N01)", n, ntfFlagKey)
		} else if !gok || gotK != wantK {
			t.Errorf("КРАСНЫЙ: цепочка %s — ConfigMap kaname notifications.enabled = %#v, вывод enabledFor(kaname) = %#v (CX1-111 (в))", n, gotK, wantK)
		}
		t.Logf("цепочка %s: объектов %d, notify %d, перечень %v; секрет %q — notify %d, kaname %d (печать NTF-2)",
			n, len(r.objs), notifyObjs, roster, secret, len(refs["notify"]), len(refs["kaname"]))
	}
	t.Logf("перепись I01: цепочек %d, отрендерено объектов %d", len(names), total)
}

// ─── ключи флага вне `global` в файлах значений (CX1-111, CX1-112 (б)) ───────

// ntfValuesFiles — все файлы значений, которые читают гейты N01, N02: values*.yaml
// зонтика, charts/*/values.yaml (каталоги), values.yaml чарта notify.
func ntfValuesFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	top, _ := filepath.Glob(filepath.Join(umbrellaDir, "values*.yaml"))
	sub, _ := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "values.yaml"))
	out = append(append(append(out, top...), sub...), filepath.Join(notifyChartDir, "values.yaml"))
	sort.Strings(out)
	return out
}

// ntfFlagLeaves — пути листьев `…notifications.enabled` вне узла
// `global.kacho.notifications`.
func ntfFlagLeaves(v any, path []string, out *[]string) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k, sub := range m {
		p := append(append([]string(nil), path...), k)
		if k == "enabled" && len(path) > 0 && path[len(path)-1] == "notifications" {
			full := strings.Join(p, ".")
			if !strings.HasPrefix(full, "global.kacho.notifications.") {
				*out = append(*out, full)
			}
		}
		ntfFlagLeaves(sub, p, out)
	}
}

// TestNTF1N02_NoModuleFlagKeyOutsideGlobalInAnyValuesFile — ни один файл
// значений не несёт собственного ключа флага модуля (`…notifications.enabled`
// вне `global.kacho.notifications`): читатель и значения уходят одним
// изменением (CX1-111; умолчание `config.notifications.enabled` подчарта kaname
// и ключи `kaname.config.notifications.enabled` профилей сняты).
func TestNTF1N02_NoModuleFlagKeyOutsideGlobalInAnyValuesFile(t *testing.T) {
	files := ntfValuesFiles(t)
	if len(files) < 3 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файлов значений %d — обход пуст либо сломан", len(files))
	}
	var findings []string
	for _, f := range files {
		var leaves []string
		ntfFlagLeaves(readYAML(t, f), nil, &leaves)
		sort.Strings(leaves)
		for _, l := range leaves {
			findings = append(findings, f+": "+l)
		}
	}
	t.Logf("осмотрено файлов значений %d; ключей флага модуля вне global %d", len(files), len(findings))
	if len(findings) > 0 {
		t.Errorf("КРАСНЫЙ: собственный ключ флага модуля вне %s — принят и не прочитан никем после перевода читателя (CX1-111):\n  %s",
			ntfModulesKey, strings.Join(findings, "\n  "))
	}
}

// ─── мелкие помощники ───────────────────────────────────────────────────────

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
