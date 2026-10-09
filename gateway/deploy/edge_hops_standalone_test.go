// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edge_hops_standalone_test.go — рендер чарта края БЕЗ ЗОНТИКА и число
// доверенных прыжков (приёмка NTF-2 Р8, строка «рендер чарта края без
// зонтика», §6б (2) и (7); решения Д51, Д52; замысел issue-2917 З28, полоса D6;
// kacho#2917).
//
// # Что судится
//
//	(2) ОТРИЦАНИЕ — `helm template <релиз> gateway/deploy` без слоя
//	    `deploy/testdata/notify-standalone/edge.yaml` отказывает, и текст отказа
//	    называет KACHO_API_GATEWAY_TRUSTED_HOPS; БЛИЗНЕЦ — тот же рендер со
//	    слоем проходит, и переменная края равна значению файла. Ровно один факт
//	    между ними — наличие слоя.
//	(7) ОДНОЛИСТНОСТЬ `edge.yaml` — один документ, одно отображение, множество
//	    листьев равно {ключ ручки}, значение — целое ≥ 0. Ключ ручки проба берёт
//	    из ШАБЛОНА (аргумент `required` строки env KACHO_API_GATEWAY_TRUSTED_HOPS),
//	    а не из своей константы; шаблон без `required` на этой строке —
//	    «не выполнилось» с путём шаблона.
//
// Обе пробы несут инъекцию с законным близнецом: судья отрицания — на копии
// чарта, где строка ручки получила умолчание (рендер без слоя проходит →
// находка), близнец — нетронутая копия; судья однолистности — на синтетических
// файлах `extra: 1`, `extra: null`, `extra: {}`, отсутствующем файле и шаблоне
// без `required`.
package deploy_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// edgeDeploymentTemplate — шаблон Deployment чарта края относительно пакета.
const edgeDeploymentTemplate = "templates/deployment.yaml"

// hopsRequiredLine — строка значения env ручки прыжков: `required "<текст>"
// .Values.<ключ>`. Ключ — подгруппа 1.
var hopsRequiredLine = regexp.MustCompile(`^\s*value:\s*\{\{-?\s*required\s+"[^"]*"\s+\.Values\.([A-Za-z0-9_.]+)`)

// hopsKeyFromTemplate — ключ значений, который читает `required` строки env
// KACHO_API_GATEWAY_TRUSTED_HOPS. found=false — строки env нет либо её значение
// выводится не через `required` (проба тогда не выполнилась, а не прошла).
func hopsKeyFromTemplate(tpl string) (key string, found bool) {
	lines := strings.Split(tpl, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "- name: "+config.TrustedHopsKnob {
			continue
		}
		if i+1 >= len(lines) {
			return "", false
		}
		m := hopsRequiredLine.FindStringSubmatch(lines[i+1])
		if m == nil {
			return "", false
		}
		return m[1], true
	}
	return "", false
}

// layerLeaves — листья разобранного YAML полными путями; пустое отображение и
// null — тоже лист (иначе `extra: {}` прошёл бы незамеченным).
func layerLeaves(prefix string, v any, out map[string]any) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		out[prefix] = v
		return
	}
	for k, sub := range m {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		layerLeaves(p, sub, out)
	}
}

// judgeEdgeLayerLeaves — находки однолистности файла слоя: файл разобрался в
// один документ-отображение, листья — ровно {key}, значение — целое ≥ 0.
func judgeEdgeLayerLeaves(name string, data []byte, readErr error, key string) []string {
	if readErr != nil {
		return []string{fmt.Sprintf("%s: файла нет или он не читается (%v) — после D6 слой — продукт полосы, "+
			"его отсутствие — дефект", name, readErr)}
	}
	var docs []any
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var d any
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return []string{fmt.Sprintf("%s: файл не разобрался: %v", name, err)}
		}
		docs = append(docs, d)
	}
	if len(docs) != 1 {
		return []string{fmt.Sprintf("%s: документов %d, ожидался один", name, len(docs))}
	}
	root, ok := docs[0].(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("%s: документ — не отображение (%T)", name, docs[0])}
	}
	leaves := map[string]any{}
	layerLeaves("", root, leaves)
	var findings []string
	var extra []string
	for l := range leaves {
		if l != key {
			extra = append(extra, l)
		}
	}
	sort.Strings(extra)
	for _, l := range extra {
		findings = append(findings, fmt.Sprintf("%s: лишний лист %s — файл слоя раздаётся каждому чарту скана и "+
			"обязан нести только ключ ручки %s", name, l, key))
	}
	v, has := leaves[key]
	switch {
	case !has:
		findings = append(findings, fmt.Sprintf("%s: ключа ручки %s в корне нет", name, key))
	default:
		n, isInt := v.(int)
		if !isInt || n < 0 {
			findings = append(findings, fmt.Sprintf("%s: %s = %v (%T) — ожидалось целое ≥ 0", name, key, v, v))
		}
	}
	return findings
}

// helmRender — `helm template api-gateway <chart> -n kacho [-f layer]`; ошибка
// рендера возвращается, потому что отрицание утверждает именно её.
func helmRender(t *testing.T, chart string, layers ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	args := []string{"template", "api-gateway", chart, "-n", "kacho"}
	for _, l := range layers {
		args = append(args, "-f", l)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	return string(out), err
}

// judgeStandaloneNegation — отрицание и близнец §6б (2) на чарте chart со
// слоем layer. want — значение ручки в файле слоя.
func judgeStandaloneNegation(t *testing.T, chart, layer string, want int) (findings, log []string) {
	t.Helper()
	out, err := helmRender(t, chart)
	switch {
	case err == nil:
		findings = append(findings, fmt.Sprintf("отрицание: рендер %s без слоя прошёл — число прыжков пришло "+
			"не из слоя (умолчание шаблона либо база чарта; Д51, Д52)", chart))
	case !strings.Contains(out, config.TrustedHopsKnob):
		findings = append(findings, fmt.Sprintf("отрицание: рендер %s без слоя отказал, но текст отказа не "+
			"называет %s:\n%s", chart, config.TrustedHopsKnob, out))
	default:
		log = append(log, fmt.Sprintf("отрицание: рендер без слоя отказал, текст называет %s", config.TrustedHopsKnob))
	}
	out, err = helmRender(t, chart, layer)
	if err != nil {
		findings = append(findings, fmt.Sprintf("близнец: рендер %s со слоем %s отказал (%v):\n%s", chart, layer, err, out))
		return findings, log
	}
	got, found := readEdgeContainer(t, out)
	if !found {
		findings = append(findings, fmt.Sprintf("близнец: в рендере %s нет пода края", chart))
		return findings, log
	}
	v, has := got.env[config.TrustedHopsKnob]
	if !has || v != fmt.Sprint(want) {
		findings = append(findings, fmt.Sprintf("близнец: %s = %q (есть=%v), ожидалось %q — значение файла слоя",
			config.TrustedHopsKnob, v, has, fmt.Sprint(want)))
	} else {
		log = append(log, fmt.Sprintf("близнец: со слоем код 0, %s=%q = значению файла", config.TrustedHopsKnob, v))
	}
	return findings, log
}

// hopsKeyOrNotRun — ключ ручки из шаблона дерева; иначе «не выполнилось».
func hopsKeyOrNotRun(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(edgeDeploymentTemplate)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: шаблон %s не читается: %v", edgeDeploymentTemplate, err)
	}
	key, ok := hopsKeyFromTemplate(string(raw))
	if !ok {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s строка env %s не выводится через `required` — ключа ручки взять неоткуда",
			edgeDeploymentTemplate, config.TrustedHopsKnob)
	}
	return key
}

func layerHopsValue(t *testing.T, key string) int {
	t.Helper()
	raw, err := os.ReadFile(edgeStandaloneLayer)
	if err != nil {
		t.Fatalf("слоя %s нет: %v", edgeStandaloneLayer, err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("слой %s не разобрался: %v", edgeStandaloneLayer, err)
	}
	n, ok := m[key].(int)
	if !ok {
		t.Fatalf("в слое %s нет целого %s в корне (%v)", edgeStandaloneLayer, key, m[key])
	}
	return n
}

// §6б (2): отрицание без слоя и близнец со слоем на чарте края дерева.
func TestEdgeHopsStandalone_NoLayerRefusesWithTheKnobName(t *testing.T) {
	key := hopsKeyOrNotRun(t)
	want := layerHopsValue(t, key)
	findings, log := judgeStandaloneNegation(t, ".", edgeStandaloneLayer, want)
	for _, l := range log {
		t.Log("  " + l)
	}
	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("ключ ручки из %s: %s; значение слоя %d", edgeDeploymentTemplate, key, want)
}

// §6б (7): однолистность `edge.yaml` дерева.
func TestEdgeHopsStandalone_LayerHasExactlyTheKnobKey(t *testing.T) {
	key := hopsKeyOrNotRun(t)
	data, err := os.ReadFile(edgeStandaloneLayer)
	findings := judgeEdgeLayerLeaves(edgeStandaloneLayer, data, err, key)
	for _, f := range findings {
		t.Error(f)
	}
	leaves := map[string]any{}
	var root any
	if err == nil && yaml.Unmarshal(data, &root) == nil {
		layerLeaves("", root, leaves)
	}
	names := make([]string, 0, len(leaves))
	for l := range leaves {
		names = append(names, l)
	}
	sort.Strings(names)
	t.Logf("листья %s: %v; ключ ручки из шаблона %s — %s", edgeStandaloneLayer, names, edgeDeploymentTemplate, key)
}

// Инъекции с законным близнецом для судьи однолистности и распознавателя ключа.
func TestEdgeHopsStandalone_LeafJudgeFiresAndStaysSilent(t *testing.T) {
	const key = "trustedHops"
	twin := []byte("trustedHops: 1\n")
	if f := judgeEdgeLayerLeaves("близнец", twin, nil, key); len(f) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %q", f)
	}
	for _, inj := range []string{"extra: 1\n", "extra: null\n", "extra: {}\n"} {
		f := judgeEdgeLayerLeaves("инъекция", append(append([]byte{}, twin...), inj...), nil, key)
		if len(f) != 1 || !strings.Contains(f[0], "extra") {
			t.Errorf("инъекция %q: ожидалась одна находка с именем extra, получено %q", strings.TrimSpace(inj), f)
		}
	}
	if f := judgeEdgeLayerLeaves("нет файла", nil, os.ErrNotExist, key); len(f) != 1 {
		t.Errorf("отсутствующий файл не найден судьёй: %q", f)
	}
	if f := judgeEdgeLayerLeaves("не в корне", []byte("api-gateway:\n  trustedHops: 1\n"), nil, key); len(f) == 0 {
		t.Errorf("ключ ручки под api-gateway принят за ключ в корне")
	}
	if f := judgeEdgeLayerLeaves("отрицательное", []byte("trustedHops: -1\n"), nil, key); len(f) != 1 {
		t.Errorf("значение -1 не найдено судьёй: %q", f)
	}

	tpl := "            - name: " + config.TrustedHopsKnob + "\n" +
		"              value: {{ required \"x\" .Values.trustedHops | quote }}\n" +
		"            - name: OTHER\n" +
		"              value: {{ required \"y\" .Values.other | quote }}\n"
	if k, ok := hopsKeyFromTemplate(tpl); !ok || k != "trustedHops" {
		t.Errorf("ключ ручки из шаблона не взят (второй required рядом): %q %v", k, ok)
	}
	bare := strings.Replace(tpl, "required \"x\" .Values.trustedHops", ".Values.trustedHops", 1)
	if _, ok := hopsKeyFromTemplate(bare); ok {
		t.Error("шаблон без required на строке ручки дал ключ — проба выполнилась бы там, где обязана не выполниться")
	}
}

// Инъекция для судьи отрицания: копия чарта, где строка ручки получила
// умолчание, — рендер без слоя проходит, находка; близнец — нетронутая копия.
func TestEdgeHopsStandalone_NegationJudgeFiresAndStaysSilent(t *testing.T) {
	key := hopsKeyOrNotRun(t)
	want := layerHopsValue(t, key)
	twin := copyEdgeChart(t, nil)
	if f, _ := judgeStandaloneNegation(t, twin, edgeStandaloneLayer, want); len(f) != 0 {
		t.Fatalf("законный близнец (нетронутая копия чарта) объявлен находкой: %q", f)
	}
	defaulted := copyEdgeChart(t, func(tpl string) string {
		re := regexp.MustCompile(`\{\{\s*required\s+"[^"]*"\s+\.Values\.` + regexp.QuoteMeta(key) + `\s*\|\s*quote\s*\}\}`)
		if !re.MatchString(tpl) {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: инъекции некуда идти — строки required ключа %s в шаблоне нет", key)
		}
		return re.ReplaceAllString(tpl, `{{ .Values.`+key+` | default 1 | quote }}`)
	})
	f, _ := judgeStandaloneNegation(t, defaulted, edgeStandaloneLayer, want)
	if len(f) != 1 || !strings.Contains(f[0], "без слоя прошёл") {
		t.Errorf("инъекция «умолчание в шаблоне»: ожидалась одна находка отрицания, получено %q", f)
	}
}

// copyEdgeChart — копия чарта края (Chart.yaml, values.yaml, templates/) во
// временный каталог; mutate правит текст шаблона Deployment.
func copyEdgeChart(t *testing.T, mutate func(string) string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "edge")
	for _, f := range []string{"Chart.yaml", "values.yaml"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("копия чарта: %v", err)
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, f), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir("templates")
	if err != nil {
		t.Fatalf("копия чарта: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join("templates", e.Name())
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if p == edgeDeploymentTemplate && mutate != nil {
			text = mutate(text)
		}
		if err := os.WriteFile(filepath.Join(dst, p), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}
