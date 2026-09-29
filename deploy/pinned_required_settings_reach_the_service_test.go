// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// pinned_required_settings_reach_the_service_test.go — КАЖДАЯ ВЕЛИЧИНА, БЕЗ
// КОТОРОЙ ПИНЕННАЯ СЛУЖБА ДОСТУПА НЕ ПУСКАЕТСЯ, ДОЕЗЖАЕТ ДО НЕЁ НА КАЖДОМ
// ОБЪЯВЛЕННОМ СТЕКЕ (kacho#2896).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ — КЛАСС, А НЕ ЭКЗЕМПЛЯР
//
// Подъём пина службы на `2e1d01af171d` уронил старт на каждом стенде три раза
// подряд, каждый раз новой ручкой: шесть величин темпа поверхности выдачи
// (kaname#315), затем два срока собственной церемонии (kaname#318). Копия
// чарта их не выражала, профили их не объявляли, и узнать это можно было только
// отказом пода — по одной причине за прогон конвейера.
//
// Перечень величин, без которых служба не пускается, у службы ЕСТЬ, и он
// машинный: блок «Обязательные величины» её INSTALL.md порождается из таблицы
// стража старта и сверяется с ней гейтом службы (`tools/operatordocs`). Здесь
// этот блок читается у ПИНЕННОГО модуля (`go.mod` даёт версию, `go env
// GOMODCACHE` — каталог), и по каждому стеку `deploy/stacks.txt` рендер подчарта
// службы обязан доставить каждую строку, обязательную на посадке стека: ключом
// в файле настроек либо переменной окружения контейнера.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗНАЧИТ «ОБЯЗАТЕЛЬНА НА ПОСАДКЕ СТЕКА»
//
// Колонка «Когда обязателен» блока знает три формы, и каждая судится так:
//
//	на любой посадке                    — на каждом стеке;
//	посадка `own` …                     — на стеке, чья посадка `own`;
//	посадка `own`, при выполненном условии
//	                                    — там же, если включена секция ключа
//	                                      (`<секция>.enabled`, у токен-эндпоинта
//	                                      это единственное условие).
//
// Форма, которой разбор не знает, — отказ разбора с цитатой строки, а не
// пропуск: строка ушла бы из-под наблюдения молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Она не судит ВЕЛИЧИНЫ — годность судит страж старта службы. Она не судит две
// другие стадии отказа старта (посадку и сборку): блок службы о них не говорит,
// и эта проверка тоже. Половина «И ВСЮДУ, где поднят собственный публичный
// REST-фронт» у строк чеканки и приёма предъявленного вне `own` не судится —
// перепись называет число таких строк на каждом стеке, а не молчит о них.
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

const (
	requiredBlockBegin = "<!-- ПОРОЖДЕНО: обязательные величины — начало -->"
	requiredBlockEnd   = "<!-- ПОРОЖДЕНО: обязательные величины — конец -->"
)

// requiredRow — одна строка блока: ключ настройки, переменная и условие.
type requiredRow struct {
	key, env string
	anyLane  bool // «на любой посадке»
	onEnable bool // «при выполненном условии»: секция ключа включена
}

var (
	requiredRowLine = regexp.MustCompile("^\\| `([a-z0-9.-]+)` \\| переменная `([A-Z0-9_]+)` \\| ([^|]+)\\|")
	requiredRowHead = regexp.MustCompile("^\\| `")
)

// parseRequiredBlock — строки блока обязательных величин из текста INSTALL.md.
//
// Отказы называются: маркеров нет, строка таблицы не разобрана, форма условия
// неизвестна, строк ноль.
func parseRequiredBlock(md string) ([]requiredRow, error) {
	_, rest, ok := strings.Cut(md, requiredBlockBegin)
	if !ok {
		return nil, fmt.Errorf("маркер начала блока обязательных величин не найден")
	}
	body, _, ok := strings.Cut(rest, requiredBlockEnd)
	if !ok {
		return nil, fmt.Errorf("маркер конца блока обязательных величин не найден")
	}
	var rows []requiredRow
	for _, line := range strings.Split(body, "\n") {
		if !requiredRowHead.MatchString(line) {
			continue
		}
		m := requiredRowLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("строка блока не разобрана: %q", line)
		}
		when := strings.TrimSpace(m[3])
		row := requiredRow{key: m[1], env: m[2]}
		switch {
		case when == "на любой посадке":
			row.anyLane = true
		case strings.HasPrefix(when, "посадка `own`, при выполненном условии"):
			row.onEnable = true
		case strings.HasPrefix(when, "посадка `own`"):
		default:
			return nil, fmt.Errorf("условие %q у `%s` неизвестно разбору — допиши форму", when, m[1])
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("в блоке обязательных величин ни одной строки — форма сменилась")
	}
	return rows, nil
}

// pinnedRequiredRows — строки блока у пиненного модуля службы.
func pinnedRequiredRows(t *testing.T) []requiredRow {
	t.Helper()
	path := filepath.Join(kanameModuleDir(t, ".."), "INSTALL.md")
	body, err := os.ReadFile(path) // #nosec G304 -- путь собран из пина go.mod, не из ввода
	if err != nil {
		t.Fatalf("INSTALL.md пиненного модуля не прочитан (%s): %v", path, err)
	}
	rows, err := parseRequiredBlock(string(body))
	if err != nil {
		t.Fatalf("блок обязательных величин пиненного модуля не разобран (%s): %v", path, err)
	}
	return rows
}

// requiredStackPosture — что о стеке нужно суду: посадка и включённые секции.
type requiredStackPosture struct {
	own     bool
	enabled func(section string) bool
}

// requiredFinding — строка, которую рендер стека не доставляет.
type requiredFinding struct{ stack, key, env string }

func (f requiredFinding) String() string {
	return fmt.Sprintf("стек %s: `%s` (%s) рендер не доставляет ни ключом настроек, ни переменной "+
		"окружения — служба откажет в старте", f.stack, f.key, f.env)
}

// judgeRequiredDelivery — ядро суда: какие строки, обязательные на посадке
// стека, не доставлены. Возвращает находки и число судимых строк.
func judgeRequiredDelivery(stack string, rows []requiredRow, p requiredStackPosture,
	cfg map[string]any, env map[string]bool) (findings []requiredFinding, judged int) {
	for _, r := range rows {
		switch {
		case r.anyLane:
		case !p.own:
			continue
		case r.onEnable && !p.enabled(r.key[:strings.LastIndex(r.key, ".")]):
			continue
		}
		judged++
		v, present := lookup(cfg, strings.Split(r.key, ".")...)
		if (present && v != nil) || env[r.env] {
			continue
		}
		findings = append(findings, requiredFinding{stack, r.key, r.env})
	}
	return findings, judged
}

// renderedDelivery — файл настроек службы и имена переменных её контейнеров.
func renderedDelivery(t *testing.T, rendered string) (map[string]any, map[string]bool) {
	t.Helper()
	env := map[string]bool{}
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break
		}
		if kind, _ := doc["kind"].(string); kind != "Deployment" {
			continue
		}
		containers, _ := lookup(doc, "spec", "template", "spec", "containers")
		list, _ := containers.([]any)
		for _, c := range list {
			cm, _ := c.(map[string]any)
			vars, _ := cm["env"].([]any)
			for _, e := range vars {
				if em, ok := e.(map[string]any); ok {
					if name, ok := em["name"].(string); ok {
						env[name] = true
					}
				}
			}
		}
	}
	return kanameServiceConfig(t, rendered), env
}

// stackIdentityValues — значения подчарта службы по цепочке стека: секция
// `kaname` и `global` умбреллы, слитые так же, как их сливает helm.
func stackIdentityValues(t *testing.T, chain []string) map[string]any {
	t.Helper()
	merged := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	for _, f := range chain {
		merged = mergeValues(merged, readYAML(t, filepath.Join(umbrellaDir, f)))
	}
	sub, _ := merged["kaname"].(map[string]any)
	if sub == nil {
		t.Fatalf("цепочка %v не несёт секции `kaname` — подчарт службы рендерить не с чем", chain)
	}
	sub = mergeValues(map[string]any{}, sub)
	if g, ok := merged["global"].(map[string]any); ok {
		sub["global"] = mergeValues(map[string]any{}, g)
	}
	return sub
}

// TestPinnedRequiredSettingsReachTheServiceOnEveryStack — суд по всем стекам.
func TestPinnedRequiredSettingsReachTheServiceOnEveryStack(t *testing.T) {
	rows := pinnedRequiredRows(t)
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var census []string
	for _, name := range names {
		values := stackIdentityValues(t, stacks[name])
		idp, _ := lookup(values, "config", "authn", "identityProvider")
		posture := requiredStackPosture{
			own: idp == "own",
			enabled: func(section string) bool {
				path := []string{"config"}
				for _, seg := range strings.Split(section, ".") {
					path = append(path, clientTokenKnob{key: seg}.valueKey())
				}
				on, _ := lookup(values, append(path, "enabled")...)
				b, _ := on.(bool)
				return b
			},
		}
		body, err := yaml.Marshal(values)
		if err != nil {
			t.Fatalf("стек %s: значения подчарта не сериализуются: %v", name, err)
		}
		file := filepath.Join(t.TempDir(), name+".yaml")
		if err := os.WriteFile(file, body, 0o600); err != nil {
			t.Fatal(err)
		}
		base, err := filepath.Abs(umbrellaDir)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(base, file)
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := renderIdentitySubchart(t, []string{rel})
		if err != nil {
			t.Fatalf("стек %s: рендер подчарта службы не удался: %v\n%s", name, err, rendered)
		}
		cfg, env := renderedDelivery(t, rendered)
		findings, judged := judgeRequiredDelivery(name, rows, posture, cfg, env)
		for _, f := range findings {
			t.Errorf("%s", f)
		}
		census = append(census, fmt.Sprintf("%s(own=%v): судимо %d, доставлено %d",
			name, posture.own, judged, judged-len(findings)))
	}
	if len(census) == 0 {
		t.Fatal("ни одного стека не осмотрено — вердикта нет")
	}
	t.Logf("перепись: пин %s · строк блока %d · стеков %d\n  %s",
		productModulePins(t, "..")[kanameModulePart], len(rows), len(census), strings.Join(census, "\n  "))
}

// TestRequiredSettingsJudgeSeesTheGapAndIsSilentOnDelivery — самопроверка на
// синтетике: разбор и суд.
func TestRequiredSettingsJudgeSeesTheGapAndIsSilentOnDelivery(t *testing.T) {
	md := "intro\n" + requiredBlockBegin + "\n\n| Ключ | Как | Когда | Почему |\n|---|---|---|---|\n" +
		"| `a.any` | переменная `K_A_ANY` | на любой посадке | x |\n" +
		"| `a.own` | переменная `K_A_OWN` | посадка `own` | x |\n" +
		"| `s.pace` | переменная `K_S_PACE` | посадка `own`, при выполненном условии | x |\n" +
		requiredBlockEnd + "\n"
	rows, err := parseRequiredBlock(md)
	if err != nil || len(rows) != 3 {
		t.Fatalf("законный блок обязан разбираться в 3 строки: %v, %v", rows, err)
	}

	on := func(string) bool { return true }
	off := func(string) bool { return false }
	full := map[string]any{"a": map[string]any{"any": 1, "own": 1}, "s": map[string]any{"pace": 1}}

	// (а) всё доставлено ключами — молчание, судимо 3.
	if got, judged := judgeRequiredDelivery("x", rows, requiredStackPosture{own: true, enabled: on}, full, nil); len(got) != 0 || judged != 3 {
		t.Errorf("полная доставка: находок %v, судимо %d", got, judged)
	}
	// (б) недоставленная строка — находка с ключом.
	gap := map[string]any{"a": map[string]any{"any": 1}, "s": map[string]any{"pace": 1}}
	if got, _ := judgeRequiredDelivery("x", rows, requiredStackPosture{own: true, enabled: on}, gap, nil); len(got) != 1 || got[0].key != "a.own" {
		t.Errorf("недоставленная `a.own` обязана быть находкой, получено %v", got)
	}
	// (в) та же строка переменной окружения — молчание.
	if got, _ := judgeRequiredDelivery("x", rows, requiredStackPosture{own: true, enabled: on}, gap, map[string]bool{"K_A_OWN": true}); len(got) != 0 {
		t.Errorf("доставка переменной обязана молчать, получено %v", got)
	}
	// (г) пустое значение ключа — не доставка.
	null := map[string]any{"a": map[string]any{"any": 1, "own": nil}, "s": map[string]any{"pace": 1}}
	if got, _ := judgeRequiredDelivery("x", rows, requiredStackPosture{own: true, enabled: on}, null, nil); len(got) != 1 {
		t.Errorf("пустое значение обязано быть находкой, получено %v", got)
	}
	// (д) вне own судится только «на любой посадке»; условная строка — только при включённой секции.
	if got, judged := judgeRequiredDelivery("x", rows, requiredStackPosture{own: false, enabled: on}, map[string]any{}, nil); len(got) != 1 || judged != 1 || got[0].key != "a.any" {
		t.Errorf("вне own: находок %v, судимо %d", got, judged)
	}
	if got, judged := judgeRequiredDelivery("x", rows, requiredStackPosture{own: true, enabled: off}, gap, nil); judged != 2 || len(got) != 1 {
		t.Errorf("выключенная секция: находок %v, судимо %d", got, judged)
	}

	// Отказы разбора называются.
	for name, tc := range map[string]struct{ md, reason string }{
		"маркеров нет":        {"| `a` | переменная `A` | на любой посадке | x |", "маркер начала"},
		"строк ноль":          {requiredBlockBegin + "\n" + requiredBlockEnd, "ни одной строки"},
		"форма условия":       {requiredBlockBegin + "\n| `a.b` | переменная `A_B` | по праздникам | x |\n" + requiredBlockEnd, "неизвестно разбору"},
		"строка не разобрана": {requiredBlockBegin + "\n| `a.b` | файлом | на любой посадке | x |\n" + requiredBlockEnd, "не разобрана"},
	} {
		if _, err := parseRequiredBlock(tc.md); err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: разбор обязан отказать с %q, получено %v", name, tc.reason, err)
		}
	}
}
