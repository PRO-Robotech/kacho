// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_subchart_knob_parity_test.go — каждая ручка собственного чарта службы
// доступа (по пину модуля в go.mod) есть у его копии в зонте либо названа в
// закрытой ведомости намеренных отличий с причиной (kacho#3314).
//
// Класс измерен четырежды: kacho#2714, kacho#2715, kacho#3020 и выкатка
// kaname@746-4477a979 на стенд — ручка, добавленная в чарт службы, до стенда не
// доезжала, пока расхождение не находили руками на стенде.
//
// РУЧКА — то, что доходит до ПРОЦЕССА: ключ файла настроек (`config.yaml` в
// карте настроек) и имя переменной окружения `KANAME_*` контейнера. Ключи
// `values` у двух чартов названы по-разному по построению (копия кладёт
// настройки службы под `config.*`, соседей выводит из `global.*`), и сравнение
// их сравнивало бы раскладку, а не ручки.
//
// ЧИТАЕТСЯ ТЕКСТ ШАБЛОНОВ, А НЕ РЕНДЕР. Ключ, отданный ветвью (`with`,
// `hasKey`), в рендере профиля без этой величины отсутствует — ровно так
// `apiServer.registryToken.anonymousClientId` чарта службы не был виден ни в
// одном рендере: незаданная величина ключа не рендерит. Обход шаблона берёт
// каждый ключ, который шаблон СПОСОБЕН выдать: путь — по отступу внутри блока
// `config.yaml: |`, строки директив и комментарии пропускаются, ключ с
// подстановкой в имени считается отдельно (печатается, не сравнивается).
//
// Чарт службы — из кэша модулей по пину go.mod (тот же пин держит образ:
// deploy/outsourced_image_pin_agrees_with_module_pin_test.go). Переопределение —
// KACHO_KANAME_OWN_CHART (каталог чарта): перепись на другом пине без правки
// go.mod. Перепись печатается всегда; ноль сравнённых ручек — отказ.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// kanameChartDifferencesLedger — закрытая ведомость намеренных отличий копии.
const kanameChartDifferencesLedger = "kaname-chart-differences.yaml"

// chartKnobs — ручки чарта, выведенные из текста его шаблонов.
type chartKnobs struct {
	config  map[string]bool // листовые пути файла настроек
	env     map[string]bool // имена переменных KANAME_*
	dynamic int             // ключей с подстановкой в имени — не сравниваются
	files   int             // прочитано файлов шаблонов
}

var (
	knobKeyLine  = regexp.MustCompile(`^(\s*)([A-Za-z0-9_.-]+):(\s|$)`)
	knobBlock    = regexp.MustCompile(`^\s*config\.yaml:\s*\|`)
	knobEnvName  = regexp.MustCompile(`-\s+name:\s*"?(KANAME_[A-Z0-9_]+)"?`)
	knobComments = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)
	knobDynamic  = regexp.MustCompile(`^\{\{[^}]*\}\}\s*:`)
)

// configKnobPaths — листовые пути ключей блока `config.yaml: |` в тексте шаблона.
func configKnobPaths(text string) (paths []string, dynamic int) {
	text = knobComments.ReplaceAllString(text, "")
	type frame struct {
		indent int
		key    string
	}
	var stack []frame
	in, base := false, -1
	all := map[string]bool{}
	for _, ln := range strings.Split(text, "\n") {
		if !in {
			if knobBlock.MatchString(ln) {
				in, base, stack = true, -1, nil
			}
			continue
		}
		s := strings.TrimSpace(ln)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if strings.HasPrefix(s, "{{") {
			if knobDynamic.MatchString(s) {
				dynamic++
			}
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		if base < 0 {
			base = indent
		}
		if indent < base {
			in = false
			continue
		}
		m := knobKeyLine.FindStringSubmatch(ln)
		if m == nil {
			if head, _, ok := strings.Cut(s, ":"); ok && strings.Contains(head, "{{") {
				dynamic++
			}
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, frame{indent, m[2]})
		keys := make([]string, len(stack))
		for i, f := range stack {
			keys[i] = f.key
		}
		all[strings.Join(keys, ".")] = true
	}
	for p := range all {
		leaf := true
		for q := range all {
			if strings.HasPrefix(q, p+".") {
				leaf = false
				break
			}
		}
		if leaf {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, dynamic
}

// templateKnobs — ручки чарта по всем файлам его каталога templates.
func templateKnobs(chartDir string) (chartKnobs, error) {
	k := chartKnobs{config: map[string]bool{}, env: map[string]bool{}}
	dir := filepath.Join(chartDir, "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return k, err
	}
	for _, e := range entries {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".tpl")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 -- каталог чарта из дерева либо кэша модулей
		if err != nil {
			return k, err
		}
		k.files++
		paths, dyn := configKnobPaths(string(b))
		k.dynamic += dyn
		for _, p := range paths {
			k.config[p] = true
		}
		for _, m := range knobEnvName.FindAllStringSubmatch(string(b), -1) {
			k.env[m[1]] = true
		}
	}
	return k, nil
}

// kanameDifference — строка ведомости: ручка и причина отличия.
type kanameDifference struct {
	Knob string `yaml:"knob"` // config:<путь> | env:<ИМЯ>
	Why  string `yaml:"why"`
}

func readKanameDifferences(path string) ([]kanameDifference, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- путь ведомости из дерева
	if err != nil {
		return nil, err
	}
	var doc struct {
		Differences []kanameDifference `yaml:"differences"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return doc.Differences, nil
}

// judgeKnobParity — находки: ручка чарта службы, которой нет у копии и нет в
// ведомости; запись ведомости без предмета; запись без причины. compared —
// число сравнённых ручек чарта службы.
func judgeKnobParity(own, cp chartKnobs, ledger []kanameDifference) (findings []string, compared int) {
	gaps := map[string]bool{}
	for p := range own.config {
		compared++
		if !cp.config[p] {
			gaps["config:"+p] = true
		}
	}
	for n := range own.env {
		compared++
		if !cp.env[n] {
			gaps["env:"+n] = true
		}
	}
	excused := map[string]bool{}
	for _, d := range ledger {
		switch {
		case strings.TrimSpace(d.Why) == "":
			findings = append(findings, fmt.Sprintf("ведомость: «%s» без причины — отличие без довода не решение", d.Knob))
		case !gaps[d.Knob]:
			findings = append(findings, fmt.Sprintf("ведомость: «%s» не отличие (у чарта службы ручки нет либо копия её несёт) — запись без предмета снять", d.Knob))
		}
		excused[d.Knob] = true
	}
	var missing []string
	for g := range gaps {
		if !excused[g] {
			missing = append(missing, g)
		}
	}
	sort.Strings(missing)
	for _, g := range missing {
		findings = append(findings, fmt.Sprintf("ручка %s есть у чарта службы, а у копии в зонте её нет — до стенда она не доедет; "+
			"перенести в копию либо назвать в %s с причиной", g, kanameChartDifferencesLedger))
	}
	return findings, compared
}

// kanameOwnChartDir — каталог собственного чарта службы: по пину go.mod либо
// KACHO_KANAME_OWN_CHART.
func kanameOwnChartDir(t *testing.T) (dir, origin string) {
	t.Helper()
	if d := os.Getenv("KACHO_KANAME_OWN_CHART"); d != "" {
		return d, "KACHO_KANAME_OWN_CHART"
	}
	m := kanameModuleDir(t, "..")
	return filepath.Join(m, "deploy"), "пин go.mod " + filepath.Base(m)
}

func TestKanameSubchartCarriesEveryKnobOfTheServiceChart(t *testing.T) {
	ownDir, origin := kanameOwnChartDir(t)
	own, err := templateKnobs(ownDir)
	if err != nil {
		t.Fatalf("шаблоны чарта службы (%s) не прочитаны: %v — вердикт беспредметен", origin, err)
	}
	cp, err := templateKnobs(iamSubchartDir)
	if err != nil {
		t.Fatalf("шаблоны копии %s не прочитаны: %v", iamSubchartDir, err)
	}
	ledger, err := readKanameDifferences(kanameChartDifferencesLedger)
	if err != nil {
		t.Fatalf("ведомость %s не прочитана: %v", kanameChartDifferencesLedger, err)
	}
	findings, compared := judgeKnobParity(own, cp, ledger)
	t.Logf("перепись: чарт службы — %s (%d файлов шаблонов): ключей настроек %d, переменных %d, ключей с подстановкой %d; "+
		"копия %s (%d файлов): ключей %d, переменных %d; сравнено ручек %d; записей ведомости %d; находок %d",
		origin, own.files, len(own.config), len(own.env), own.dynamic, iamSubchartDir, cp.files, len(cp.config), len(cp.env),
		compared, len(ledger), len(findings))
	if compared == 0 || own.files == 0 || cp.files == 0 {
		t.Fatalf("сравнено ручек %d (файлов: чарт службы %d, копия %d) — вердикт беспредметен", compared, own.files, cp.files)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// Инъекция: настоящий текст шаблона копии и синтетический чарт службы, отличающийся
// РОВНО одной ручкой; законный близнец — без неё.
func knobParityFixture(t *testing.T, extraOwn string) (own, cp chartKnobs) {
	t.Helper()
	cp, err := templateKnobs(iamSubchartDir)
	if err != nil {
		t.Fatalf("копия не прочитана: %v", err)
	}
	own = chartKnobs{config: map[string]bool{}, env: map[string]bool{}, files: 1}
	for p := range cp.config {
		own.config[p] = true
	}
	if extraOwn == "" {
		return own, cp
	}
	tpl := "data:\n  config.yaml: |\n    api-server:\n      {{- with .Values.x }}\n" + extraOwn + "      {{- end }}\n"
	paths, _ := configKnobPaths(tpl)
	for _, p := range paths {
		own.config[p] = true
	}
	return own, cp
}

func TestKanameKnobParityInjection_KnobMissingFromTheCopyIsFound(t *testing.T) {
	own, cp := knobParityFixture(t, "      probe-only-knob: {{ . | quote }}\n")
	got, n := judgeKnobParity(own, cp, nil)
	if n == 0 || len(got) != 1 || !strings.Contains(got[0], "config:api-server.probe-only-knob") {
		t.Fatalf("ручка чарта службы, которой нет у копии, не найдена (сравнено %d): %v", n, got)
	}
}

func TestKanameKnobParityInjection_TwinWithoutTheKnobIsSilent(t *testing.T) {
	own, cp := knobParityFixture(t, "")
	if got, n := judgeKnobParity(own, cp, nil); n == 0 || len(got) != 0 {
		t.Fatalf("законный близнец назван находкой (сравнено %d): %v", n, got)
	}
}

func TestKanameKnobParityInjection_LedgerExcusesOnlyWithAReasonAndAGap(t *testing.T) {
	own, cp := knobParityFixture(t, "      probe-only-knob: {{ . | quote }}\n")
	if got, _ := judgeKnobParity(own, cp, []kanameDifference{{Knob: "config:api-server.probe-only-knob", Why: "проба"}}); len(got) != 0 {
		t.Fatalf("отличие с причиной названо находкой: %v", got)
	}
	if got, _ := judgeKnobParity(own, cp, []kanameDifference{{Knob: "config:api-server.probe-only-knob"}}); len(got) != 1 {
		t.Fatalf("запись без причины не найдена: %v", got)
	}
	twin, _ := knobParityFixture(t, "")
	if got, _ := judgeKnobParity(twin, cp, []kanameDifference{{Knob: "config:api-server.probe-only-knob", Why: "проба"}}); len(got) != 1 {
		t.Fatalf("запись без предмета не найдена: %v", got)
	}
}

func TestKanameKnobParityReader_SeesGuardedKeysAndSkipsDirectivesAndComments(t *testing.T) {
	tpl := "data:\n  config.yaml: |\n    api-server:\n      {{- /* ключ: не ручка */}}\n" +
		"      # comment-key: нет\n      {{- if hasKey .Values \"a\" }}\n      metrics-endpoint: {{ .Values.a | quote }}\n      {{- end }}\n" +
		"      registry-token:\n        {{- with .Values.b }}\n        anonymous-client-id: {{ . }}\n        {{- end }}\n" +
		"      {{ $k }}: dynamic\n    repository:\n      type: POSTGRES\nkind: ConfigMap\n"
	paths, dyn := configKnobPaths(tpl)
	want := []string{"api-server.metrics-endpoint", "api-server.registry-token.anonymous-client-id", "repository.type"}
	if strings.Join(paths, ",") != strings.Join(want, ",") || dyn != 1 {
		t.Fatalf("обход шаблона: %v (подстановок %d), ждали %v и 1", paths, dyn, want)
	}
	if got, _ := configKnobPaths("kind: ConfigMap\n"); len(got) != 0 {
		t.Fatalf("без блока config.yaml ключей быть не должно: %v", got)
	}
}
