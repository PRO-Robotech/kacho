// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// posture_parity_test.go — одна защита объявляется ОДНИМ путём чарта, и путь
// у всех развёртываемых профилей один и тот же.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Чарт умеет объявлять переменную окружения ДВУМЯ способами. Первый — своя
// первоклассная ручка: блок шаблона под условием, который рядом с переменной
// несёт и свои проверки связности (у api-gateway блок idempotency прямо падает
// `fail`-ом, если хранилище объявлено общим, а адреса у него нет — выводить
// адрес из соседнего запрещено, он никогда не пуст и потому ведёт в никуда).
// Второй — родовой проброс
// (`extraEnv` / `env`), который печатает пару «имя-значение» и не знает о ней
// ничего.
//
// Когда ту же переменную объявляют пробросом, происходит одно из двух, и оба
// плохи:
//
//   - ручка ВЫКЛЮЧЕНА → переменная всё равно доедет, но собственные проверки
//     ручки не исполнятся ни разу. Защита выглядит провязанной, а условие, при
//     котором она осмысленна, никто не сверял;
//   - ручка ВКЛЮЧЕНА → в контейнере окажутся ДВЕ записи с одним именем.
//     Шаблон при этом зелёный, `helm template` молчит, а apiserver отвечает
//     `hides previous definition of "…", which may be dropped when using apply`.
//     Победит последняя по порядку — то есть та, которую выбрал порядок блоков
//     в шаблоне, а не тот, кто правил профиль.
//
// И третье следствие, из-за которого проверка сравнивает ПРОФИЛИ, а не один:
// профиль, объявивший защиту пробросом, не объявляет соответствующую ручку — и
// тогда ключ посадки есть в одной цепочке и отсутствует в другой, хотя
// поднимается на обеих одно и то же. Отличить это чтением одного профиля
// нельзя.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседнего token_shape_test.go: контракт — то, что
// профиль ОБЪЯВЛЯЕТ. Проверке не нужны ни `helm`, ни скачанные зависимости
// чартов, поэтому она не умеет пропуститься — а рендер-стражи рядом покрывают
// логику шаблонов. Отдельно важно, что рендер тут и не помог бы: цепочка
// values.dev+values.dev-prod даёт НОЛЬ дубликатов именно потому, что ручка в
// ней выключена, — дефект «разными путями» в отрендеренном манифесте не виден
// вовсе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ НЕ ПРОВЕРЯЕТСЯ (названо, чтобы «зелено» не читалось шире, чем есть)
//
// Проверка НЕ сравнивает ЗНАЧЕНИЯ ручек между цепочками. Цепочки расходятся
// значениями законно (локальные образы, адреса внутри кластера, самоподписанный
// Postgres на kind против секрета cert-manager) и незаконно, и разделить это
// механически нельзя — там нужен разбор по пункту. Предмет здесь ровно один:
// ПУТЬ объявления. Сколько осей посадки расходятся значением — измерено и
// вынесено в открытый долг, а не спрятано сюда белым списком.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ─────────────────────────────────────────────────────────────────────────────
// Чтение дерева. Ничего не выписываем руками: состав подчартов берётся из
// Chart.yaml умбреллы и каталога charts/, имена переменных — из шаблонов.

const umbrellaDir = "helm/umbrella"

func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return tree
}

// subchartDirs — ключ значений умбреллы → каталог чарта. Выводится, а не
// перечисляется: alias (или имя) зависимости с `repository: file://…` плюс
// каталоги, физически лежащие в charts/.
func subchartDirs(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}

	chart := readYAML(t, filepath.Join(umbrellaDir, "Chart.yaml"))
	deps, _ := chart["dependencies"].([]any)
	for _, d := range deps {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		repo, _ := m["repository"].(string)
		if !strings.HasPrefix(repo, "file://") {
			continue // внешний чарт: его шаблоны не в нашем дереве
		}
		key, _ := m["alias"].(string)
		if key == "" {
			key, _ = m["name"].(string)
		}
		out[key] = filepath.Clean(filepath.Join(umbrellaDir, strings.TrimPrefix(repo, "file://")))
	}

	entries, err := os.ReadDir(filepath.Join(umbrellaDir, "charts"))
	if err != nil {
		t.Fatalf("read charts/: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out[e.Name()] = filepath.Join(umbrellaDir, "charts", e.Name())
	}
	return out
}

// literalEnvName — `- name: KACHO_FOO` с ЛИТЕРАЛЬНЫМ именем. Родовой проброс
// печатает `- name: {{ $k }}` и под это выражение не попадает by construction,
// поэтому имена из проброса не попадают в «свои» имена чарта.
var literalEnvName = regexp.MustCompile(`(?m)^\s*-\s*name:\s*([A-Z][A-Z0-9_]{3,})\s*$`)

// passthroughRange — `{{- range $k, $v := .Values.extraEnv }}` со СЛЕДУЮЩЕЙ
// строкой `- name: {{ $k }}`. Именно пара, а не один только range: чарт
// перебирает `.Values.<что-то>` и в других местах (тома, порты), и одна лишь
// первая строка объявила бы пробросом что попало.
var passthroughRange = regexp.MustCompile(
	`\{\{-?\s*range\s+\$(\w+),\s*\$\w+\s*:=\s*\.Values\.([A-Za-z0-9_]+)\s*-?\}\}\s*\n\s*-\s*name:\s*\{\{\s*\$(\w+)\s*\}\}`)

// chartFacts — что чарт объявляет САМ и через какие родовые ключи пробрасывает
// чужое.
type chartFacts struct {
	ownEnv       map[string]string // имя переменной → файл шаблона, где она объявлена
	passthroughs map[string]bool   // ключ значений (extraEnv / env / …)
}

func readChart(t *testing.T, dir string) chartFacts {
	t.Helper()
	f := chartFacts{ownEnv: map[string]string{}, passthroughs: map[string]bool{}}
	tpls, err := filepath.Glob(filepath.Join(dir, "templates", "*.yaml"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	for _, p := range tpls {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		txt := string(raw)
		for _, m := range literalEnvName.FindAllStringSubmatch(txt, -1) {
			if _, seen := f.ownEnv[m[1]]; !seen {
				f.ownEnv[m[1]] = p
			}
		}
		for _, m := range passthroughRange.FindAllStringSubmatch(txt, -1) {
			if m[1] == m[3] { // печатается именно ключ карты, а не значение
				f.passthroughs[m[2]] = true
			}
		}
	}
	return f
}

// declaredPassthroughEnv — какие имена переменных профиль объявляет через
// родовой проброс подчарта.
func declaredPassthroughEnv(tree map[string]any, subchart, key string) []string {
	sub, ok := tree[subchart].(map[string]any)
	if !ok {
		return nil
	}
	m, ok := sub[key].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// profileFiles — все файлы значений умбреллы. Берём каталог, а не список:
// новый профиль обязан попасть под проверку без правки этого файла.
func profileFiles(t *testing.T) []string {
	t.Helper()
	all, err := filepath.Glob(filepath.Join(umbrellaDir, "values*.yaml"))
	if err != nil {
		t.Fatalf("glob values: %v", err)
	}
	sort.Strings(all)
	return all
}

// finding — одна находка с координатой, по которой её можно открыть и починить.
type finding struct {
	profile  string
	subchart string
	key      string
	env      string
	ownedBy  string
}

// scanCrossWiring — ядро проверки, вынесенное отдельной функцией, чтобы
// самопроверка ниже могла подать ему синтетический вход, а не подделывать
// дерево.
func scanCrossWiring(profiles map[string]map[string]any, charts map[string]chartFacts) []finding {
	var out []finding
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, prof := range names {
		tree := profiles[prof]
		subs := make([]string, 0, len(charts))
		for s := range charts {
			subs = append(subs, s)
		}
		sort.Strings(subs)
		for _, sub := range subs {
			facts := charts[sub]
			keys := make([]string, 0, len(facts.passthroughs))
			for k := range facts.passthroughs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, key := range keys {
				for _, env := range declaredPassthroughEnv(tree, sub, key) {
					if src, owned := facts.ownEnv[env]; owned {
						out = append(out, finding{prof, sub, key, env, src})
					}
				}
			}
		}
	}
	return out
}

// TestChartOwnedEnvIsNeverSuppliedByAGenericPassthrough — сама проверка.
//
// Она же покрывает и «ключ посадки объявлен в одной цепочке и отсутствует в
// другой»: профиль, провязавший защиту пробросом, свою ручку не объявляет, и
// цепочки расходятся именно здесь.
func TestChartOwnedEnvIsNeverSuppliedByAGenericPassthrough(t *testing.T) {
	dirs := subchartDirs(t)
	charts := map[string]chartFacts{}
	ownTotal, passTotal := 0, 0
	for key, dir := range dirs {
		f := readChart(t, dir)
		charts[key] = f
		ownTotal += len(f.ownEnv)
		passTotal += len(f.passthroughs)
	}

	profiles := map[string]map[string]any{}
	for _, p := range profileFiles(t) {
		profiles[filepath.Base(p)] = readYAML(t, p)
	}

	// Проверка СВОЕЙ предпосылки. «Ноль находок» обязано быть отличимо от
	// «ноль прочитанного»: если выражение перестало узнавать блоки шаблона
	// (переписали отступы, перешли на named template), обход вернёт пусто и
	// молча объявит дерево чистым.
	if len(dirs) == 0 || len(profiles) == 0 || ownTotal == 0 || passTotal == 0 {
		t.Fatalf("обход ничего не прочитал: подчартов=%d, профилей=%d, своих переменных=%d, "+
			"родовых пробросов=%d — предикат перестал узнавать блоки шаблона, "+
			"а не дерево стало чистым", len(dirs), len(profiles), ownTotal, passTotal)
	}
	t.Logf("осмотрено: подчартов=%d, профилей=%d, своих имён переменных=%d, родовых пробросов=%d",
		len(dirs), len(profiles), ownTotal, passTotal)

	for _, f := range scanCrossWiring(profiles, charts) {
		t.Errorf("%s: %s.%s.%s — эту переменную объявляет САМ чарт (%s). "+
			"Родовой проброс либо задвоит её в контейнере (побеждает последняя, "+
			"порядок выбирает шаблон), либо обойдёт проверки связности, которые "+
			"стоят рядом с первоклассной ручкой. Объяви ручкой чарта, а не пробросом",
			f.profile, f.subchart, f.key, f.env, f.ownedBy)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе той же формы.
//
// Без положительного контроля отрицание зеленеет на всём сломанном: обход,
// который перестал что-либо узнавать, «не находит» ровно так же, как чистое
// дерево.

func TestScanCrossWiring_SelfTest(t *testing.T) {
	// Имя переменной ЖИВОЕ, а не любое: синтетика тут описывает форму, но
	// фикстура, названная снятой переменной, — утверждение, пережившее свой
	// предмет (прежде здесь стояла KACHO_API_GATEWAY_INTERNAL_GRPC_MTLS_ENABLE,
	// снятая вместе с внутренним листенером края, #1024). KACHO_APP_ENV чарт
	// объявляет сам, и это утверждает соседняя проба против НАСТОЯЩЕГО шаблона.
	charts := map[string]chartFacts{
		"api-gateway": {
			ownEnv:       map[string]string{"KACHO_APP_ENV": "templates/deployment.yaml"},
			passthroughs: map[string]bool{"extraEnv": true},
		},
	}

	// (а) внесённое расхождение — проброс объявляет переменную, которую чарт
	//     объявляет сам. Обязан покраснеть И назвать координату.
	broken := map[string]map[string]any{
		"values.injected.yaml": {
			"api-gateway": map[string]any{
				"extraEnv": map[string]any{"KACHO_APP_ENV": "production"},
			},
		},
	}
	got := scanCrossWiring(broken, charts)
	if len(got) != 1 {
		t.Fatalf("внесённое расхождение не поймано: находок %d, ждали 1", len(got))
	}
	if got[0].profile != "values.injected.yaml" || got[0].subchart != "api-gateway" ||
		got[0].key != "extraEnv" || got[0].env != "KACHO_APP_ENV" {
		t.Fatalf("находка без координаты: %+v", got[0])
	}

	// (б) законная конструкция ТОЙ ЖЕ формы — проброс объявляет переменную,
	//     которой чарт сам не объявляет (профиль правомерно передаёт своё), и
	//     первоклассная ручка, объявленная как ручка. Обязан молчать.
	legit := map[string]map[string]any{
		"values.legit.yaml": {
			"api-gateway": map[string]any{
				"extraEnv": map[string]any{"KACHO_API_GATEWAY_SOMETHING_THE_CHART_DOES_NOT_EMIT": "1"},
				"appEnv":   "production",
				"image":    map[string]any{"repository": "kacho-api-gateway", "tag": "dev"},
			},
		},
	}
	if got := scanCrossWiring(legit, charts); len(got) != 0 {
		t.Fatalf("законная конструкция покрашена: %+v", got)
	}
}

// Предикат обязан узнавать блоки в НАСТОЯЩЕМ шаблоне, а не только в синтетике:
// иначе самопроверка выше зелёная, а обход дерева читает ноль.
func TestChartTemplateParsing_RecognisesRealChart(t *testing.T) {
	dirs := subchartDirs(t)
	dir, ok := dirs["api-gateway"]
	if !ok {
		t.Fatalf("подчарт api-gateway не выведен из Chart.yaml — состав подчартов сменился, "+
			"проверь вывод: %v", dirs)
	}
	f := readChart(t, dir)
	if !f.passthroughs["extraEnv"] {
		t.Errorf("родовой проброс extraEnv не опознан в %s — выражение перестало узнавать "+
			"блок шаблона, и обход дерева стал бы пустым", dir)
	}
	if _, ok := f.ownEnv["KACHO_APP_ENV"]; !ok {
		t.Errorf("собственная переменная KACHO_APP_ENV не опознана в %s — то же самое", dir)
	}
}
