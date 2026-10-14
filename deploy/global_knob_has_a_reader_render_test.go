// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// global_knob_has_a_reader_render_test.go — КАЖДАЯ РУЧКА `global.kacho.*`,
// ОБЪЯВЛЕННАЯ ПРОФИЛЕМ ЗОНТА, ИМЕЕТ ЧИТАТЕЛЯ В РЕНДЕРЕ (kacho#2997).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Профиль, объявляющий ручку, которой не читает ни один шаблон, говорит
// оператору о настройке, которой нет: правка принимается рендером и ничего не
// меняет. Ровно так прожили `global.kacho.identity.hooks.{scheme,host,port}` —
// с утверждением «пустое значение роняет рендер», — хотя слушателя обратных
// вызовов у службы давно нет.
//
// Проверка статической ссылки (internal/repohygiene, profileknobreader.go)
// поддерево `global` не судит: оно видно каждому сабчарту, а наши шаблоны
// читают его тремя формами — путём, цепочкой скобок и переменной
// (`$id := …identity`, затем `$id.appBaseURL`), — и разбор третьей формы был
// бы угадыванием. Поэтому здесь судится ИСХОД: ручка, у которой есть читатель,
// меняет рендер хотя бы одной цепочки, её объявляющей, при подмене значения.
//
// ─────────────────────────────────────────────────────────────────────────────
// КАК СУДИТСЯ
//
//   - предмет — листья `global.kacho.*` каждого профиля зонта (values.yaml и
//     каждый файл таблицы цепочек deploy/stacks.txt). Поддерево `global` вне
//     `kacho` читают и чужие сабчарты (postgresql, ingress-nginx, cert-manager),
//     их шаблонов этот суд не знает, поэтому оно не судится и считается вслух;
//   - для листа берутся цепочки, в которые входит объявивший его файл
//     (values.yaml — умолчание чарта, входит во все), и значение подменяется
//     двумя пробами разной природы: непустой величиной своего типа и пустой.
//     Шаблон, читающий ручку лишь как «задана ли», реагирует на вторую;
//   - реакция — любое различие рендера с основой ВНЕ шума: два рендера основы
//     сравниваются между собой, и строки, различающиеся уже у них (случайный
//     пароль, отметка времени выпуска), исключаются построчно. Отказ рендера на
//     пробе тоже реакция: страж, отвергающий значение, и есть читатель;
//   - лист, ни одна цепочка которого не отреагировала ни на одну пробу, —
//     находка с координатой файла и ключа.
//
// Способность упасть и смолчать — TestGlobalKnobJudgement_CanFailAndStaysSilent
// ниже: синтетический профиль поверх настоящей цепочки объявляет ручку без
// читателя (находка) рядом с настоящей читаемой ручкой (молчание).
package deploy_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

// globalKnobRoot — поддерево, которое судится: только наше пространство имён.
var globalKnobRoot = []string{"global", "kacho"}

// globalKnobDecl — лист `global.kacho.*`, объявленный хотя бы одним профилем.
type globalKnobDecl struct {
	Key   string   // путь через точку, начиная с `global`
	Files []string // профили, которые его объявляют
	Value any      // значение в первом объявившем профиле (тип пробы)
}

type globalKnobCensus struct {
	Profiles, Chains, Knobs, Renders int
}

func (c globalKnobCensus) String() string {
	return fmt.Sprintf("профилей %d · цепочек %d · ручек global.kacho %d · рендеров %d",
		c.Profiles, c.Chains, c.Knobs, c.Renders)
}

// renderedBaseline — основа цепочки: рендер и маска шума.
type renderedBaseline struct {
	docs  [][]string // документы рендера построчно
	noise []map[int]bool
}

// globalKnobLeaves — пути до листьев под prefix.
func globalKnobLeaves(node any, prefix []string, out map[string]any) {
	m, ok := node.(map[string]any)
	if !ok || len(m) == 0 {
		if len(prefix) > 0 {
			out[strings.Join(prefix, ".")] = node
		}
		return
	}
	for k, v := range m {
		globalKnobLeaves(v, append(append([]string(nil), prefix...), k), out)
	}
}

// readGlobalKnobDecls — листья `global.kacho.*` каждого названного профиля.
func readGlobalKnobDecls(t *testing.T, profiles []string) map[string]*globalKnobDecl {
	t.Helper()
	out := map[string]*globalKnobDecl{}
	for _, p := range profiles {
		raw, err := os.ReadFile(p) // #nosec G304 -- профиль зонта из таблицы цепочек этого дерева
		if err != nil {
			t.Fatalf("профиль %s не читается: %v — судить нечего, и это не «ручек нет»", p, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("профиль %s не разобран: %v", p, err)
		}
		node := any(doc)
		for _, k := range globalKnobRoot {
			m, _ := node.(map[string]any)
			node = m[k]
		}
		if node == nil {
			continue
		}
		leaves := map[string]any{}
		globalKnobLeaves(node, globalKnobRoot, leaves)
		for k, v := range leaves {
			d := out[k]
			if d == nil {
				d = &globalKnobDecl{Key: k, Value: v}
				out[k] = d
			}
			d.Files = append(d.Files, filepath.Base(p))
		}
	}
	return out
}

// globalKnobProbes — две подмены разной природы для значения v.
func globalKnobProbes(v any) []any {
	const marker = "kacho-knob-probe"
	var cands []any
	switch x := v.(type) {
	case bool:
		cands = []any{!x}
	case int:
		cands = []any{x + 7, 0}
	case float64:
		cands = []any{x + 7, 0}
	case []any:
		cands = []any{[]any{marker}, []any{}}
	case map[string]any:
		cands = []any{map[string]any{"kachoKnobProbe": marker}}
	case string:
		cands = []any{marker, ""}
	default: // nil
		cands = []any{marker}
	}
	var out []any
	for _, c := range cands {
		a, _ := json.Marshal(c)
		b, _ := json.Marshal(v)
		if string(a) != string(b) {
			out = append(out, c)
		}
	}
	return out
}

func requireHelmForRender(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
}

// helmRenderFiles — рендер умбреллы файлами профилей (абсолютные или
// относительные к каталогу пакета пути) и подменами --set-json.
func helmRenderFiles(files []string, setJSON ...string) (string, error) {
	args := []string{"template", "kacho-umbrella", umbrellaDir, "-n", "kacho"}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	for _, s := range setJSON {
		args = append(args, "--set-json", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- аргументы — профили этого дерева и подмены этой пробы
	return string(out), err
}

// renderClock — отметка времени рендера (`now` шаблона, секундная точность).
// Два рендера основы подряд обычно попадают в одну секунду, а проба — в
// следующую: без нормализации каждая ручка «реагировала» бы часами, и суд не
// нашёл бы ни одной находки (так и было при заведении: 20 из 20 «прочитаны»).
var renderClock = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`)

func splitRenderLines(s string) [][]string {
	s = renderClock.ReplaceAllString(s, "<время рендера>")
	var docs [][]string
	for _, d := range strings.Split(s, "\n---") {
		docs = append(docs, strings.Split(d, "\n"))
	}
	return docs
}

// baselineOf — два рендера основы и маска строк, различающихся уже у них.
func baselineOf(files []string) (*renderedBaseline, error) {
	a, err := helmRenderFiles(files)
	if err != nil {
		return nil, fmt.Errorf("основа не рендерится: %v\n%s", err, a)
	}
	b, err := helmRenderFiles(files)
	if err != nil {
		return nil, fmt.Errorf("второй рендер основы не прошёл: %v\n%s", err, b)
	}
	da, db := splitRenderLines(a), splitRenderLines(b)
	if len(da) != len(db) {
		return nil, fmt.Errorf("два рендера основы дали разное число документов (%d и %d) — "+
			"шум не построчный, и различие пробы ничего бы не значило", len(da), len(db))
	}
	base := &renderedBaseline{docs: da, noise: make([]map[int]bool, len(da))}
	for i := range da {
		if len(da[i]) != len(db[i]) {
			return nil, fmt.Errorf("документ %d основы меняет число строк между рендерами — "+
				"шум не построчный", i)
		}
		base.noise[i] = map[int]bool{}
		for j := range da[i] {
			if da[i][j] != db[i][j] {
				base.noise[i][j] = true
			}
		}
	}
	return base, nil
}

// reacts — отличается ли рендер пробы от основы вне шума.
func (b *renderedBaseline) reacts(rendered string, err error) bool {
	if err != nil {
		return true
	}
	d := splitRenderLines(rendered)
	if len(d) != len(b.docs) {
		return true
	}
	for i := range d {
		if len(d[i]) != len(b.docs[i]) {
			return true
		}
		for j := range d[i] {
			if !b.noise[i][j] && d[i][j] != b.docs[i][j] {
				return true
			}
		}
	}
	return false
}

// judgeGlobalKnobs — находки: листья, ни одна цепочка которых не отреагировала.
//
// chains — имя цепочки → файлы профилей (пути для helm -f); decls — предмет.
func judgeGlobalKnobs(t *testing.T, chains map[string][]string, decls map[string]*globalKnobDecl) ([]string, globalKnobCensus) {
	t.Helper()
	census := globalKnobCensus{Chains: len(chains), Knobs: len(decls)}
	var mu sync.Mutex

	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)

	bases := map[string]*renderedBaseline{}
	{
		var wg sync.WaitGroup
		errs := map[string]error{}
		for _, n := range names {
			wg.Add(1)
			go func(n string) {
				defer wg.Done()
				b, err := baselineOf(chains[n])
				mu.Lock()
				defer mu.Unlock()
				census.Renders += 2
				bases[n], errs[n] = b, err
			}(n)
		}
		wg.Wait()
		for _, n := range names {
			if errs[n] != nil {
				t.Fatalf("цепочка %s: %v — условие суда не создано", n, errs[n])
			}
		}
	}

	keys := make([]string, 0, len(decls))
	for k := range decls {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sem := make(chan struct{}, 4)
	findings := make([]string, len(keys))
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		go func(i int, d *globalKnobDecl) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			for _, n := range names {
				if !chainCarries(chains[n], d.Files) {
					continue
				}
				for _, p := range globalKnobProbes(d.Value) {
					v, _ := json.Marshal(p)
					out, err := helmRenderFiles(chains[n], d.Key+"="+string(v))
					mu.Lock()
					census.Renders++
					mu.Unlock()
					if bases[n].reacts(out, err) {
						return
					}
				}
			}
			files := append([]string(nil), d.Files...)
			sort.Strings(files)
			findings[i] = fmt.Sprintf("%s (объявлена в %s): ни одна цепочка, её объявляющая, не "+
				"меняет рендер при подмене значения — читателя у ручки нет; снять её либо дать ей "+
				"читателя в шаблоне", d.Key, strings.Join(files, ", "))
		}(i, decls[k])
	}
	wg.Wait()

	var out []string
	for _, f := range findings {
		if f != "" {
			out = append(out, f)
		}
	}
	return out, census
}

// chainCarries — входит ли в цепочку хотя бы один из файлов (по имени).
// values.yaml — умолчание чарта: оно входит в каждую цепочку.
func chainCarries(chain, files []string) bool {
	for _, f := range files {
		if f == "values.yaml" {
			return true
		}
		for _, c := range chain {
			if filepath.Base(c) == f {
				return true
			}
		}
	}
	return false
}

// treeGlobalKnobSubject — цепочки таблицы и профили, которые в них входят.
func treeGlobalKnobSubject(t *testing.T) (map[string][]string, []string) {
	t.Helper()
	stacks := deployStacks(t)
	chains := map[string][]string{}
	seen := map[string]bool{"values.yaml": true}
	profiles := []string{filepath.Join(umbrellaDir, "values.yaml")}
	for name, chain := range stacks {
		var files []string
		for _, p := range chain {
			files = append(files, filepath.Join(umbrellaDir, p))
			if !seen[p] {
				seen[p] = true
				profiles = append(profiles, filepath.Join(umbrellaDir, p))
			}
		}
		chains[name] = files
	}
	sort.Strings(profiles)

	// Профиль зонта вне всякой цепочки, объявляющий `global.kacho.*`, судить
	// нечем: рендера, в котором он участвует, нет. Это отказ, а не молчание.
	all, _ := filepath.Glob(filepath.Join(umbrellaDir, "values*.yaml"))
	for _, p := range all {
		if seen[filepath.Base(p)] {
			continue
		}
		if d := readGlobalKnobDecls(t, []string{p}); len(d) > 0 {
			t.Errorf("%s объявляет %d ручек global.kacho и не входит ни в одну цепочку %s — "+
				"их читателя судить нечем", p, len(d), stacksTable)
		}
	}
	return chains, profiles
}

func TestEveryGlobalKachoKnobOfTheUmbrellaHasAReader(t *testing.T) {
	requireHelmForRender(t)
	chains, profiles := treeGlobalKnobSubject(t)
	decls := readGlobalKnobDecls(t, profiles)
	if len(decls) == 0 {
		t.Fatalf("в профилях зонта (%d) не найдено ни одной ручки global.kacho — предмет исчез, "+
			"а не стал чистым", len(profiles))
	}
	findings, census := judgeGlobalKnobs(t, chains, decls)
	census.Profiles = len(profiles)
	t.Logf("перепись: %s · находок %d", census, len(findings))
	for _, f := range findings {
		t.Errorf("ручка без читателя: %s", f)
	}
}

func TestGlobalKnobJudgement_CanFailAndStaysSilent(t *testing.T) {
	requireHelmForRender(t)
	stacks := deployStacks(t)
	chain, ok := stacks["dev"]
	if !ok {
		t.Fatal("цепочки dev в таблице нет — инъекции не на чем стоять")
	}
	var files []string
	for _, p := range chain {
		files = append(files, filepath.Join(umbrellaDir, p))
	}

	// Синтетический профиль поверх настоящей цепочки: одна ручка, которой не
	// читает никто, и одна настоящая читаемая — единственное различие между ними
	// в том, есть ли у ключа читатель.
	dir := t.TempDir()
	synthetic := filepath.Join(dir, "values.knob-probe.yaml")
	body := "global:\n  kacho:\n    identity:\n      knobProbeWithoutReader: \"declared\"\n" +
		"      appBaseURL: \"https://console.example.test\"\n"
	if err := os.WriteFile(synthetic, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	decls := readGlobalKnobDecls(t, []string{synthetic})
	if len(decls) != 2 {
		t.Fatalf("синтетический профиль дал %d ручек вместо 2 — разбор предмета сломан", len(decls))
	}
	findings, census := judgeGlobalKnobs(t, map[string][]string{"dev+probe": append(files, synthetic)},
		map[string]*globalKnobDecl{
			"global.kacho.identity.knobProbeWithoutReader": {Key: "global.kacho.identity.knobProbeWithoutReader",
				Files: []string{"values.knob-probe.yaml"}, Value: "declared"},
			"global.kacho.identity.appBaseURL": {Key: "global.kacho.identity.appBaseURL",
				Files: []string{"values.knob-probe.yaml"}, Value: "https://console.example.test"},
		})
	t.Logf("перепись инъекции: %s", census)
	if len(findings) != 1 || !strings.Contains(findings[0], "knobProbeWithoutReader") {
		t.Fatalf("инъекция: ожидалась ровно одна находка о ручке без читателя, получено %d: %v",
			len(findings), findings)
	}
	if strings.Contains(strings.Join(findings, "\n"), "appBaseURL") {
		t.Fatalf("близнец: читаемая ручка объявлена находкой — суд краснеет на законном: %v", findings)
	}
}
