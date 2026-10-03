// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_chain_fits_its_runner_test.go — КАЖДАЯ ЦЕПОЧКА, КОТОРУЮ КОНВЕЙЕР
// ПОДНИМАЕТ НА kind, ПОМЕЩАЕТСЯ В УЗЕЛ СВОЕГО РАНЕРА ПО ЗАПРОСАМ ПРОЦЕССОРА
// (kacho#2937, сборка 2 волны 4; нога `stack own boots` из kacho#2931).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Нога production-posture.yml «stack own boots» краснела четыре прогона из
// четырёх на голове d4fed1ee одним и тем же текстом отказа:
//
//	Error: resource Deployment/kacho/api-gateway not ready. status: Failed,
//	message: Progress deadline exceeded
//
// и семь подов стенда стояли Pending без узла. Причина — арифметика узла, а не
// продукт: цепочка own просила 3520m процессора (боевой слой задаёт базам
// 500m и 250m), плоскость управления kind несёт 950m, итого 4470m на узле в
// 4000m. Не видит этого никто из прочих стражей: шаблон рендерится, страж
// старта не зовётся (процесса нет), гейт посадки не доходит до суда. Узнавали
// об этом через пятнадцать минут подъёма — по сроку, а не по причине.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ
//
// Для каждой ноги подъёма конвейера (тот же распознаватель, что у
// TestEveryStandChainIsRaisedByTheConveyorOrNamesWhyNot, — conveyorLegs) и для
// КАЖДОЙ цепочки, которую накладывает её рецепт (а не только последней:
// промежуточная фаза тоже стоит на узле целиком):
//
//	запросы цепочки       — рендер умбреллы ручками UMBRELLA_OPTS рецепта;
//	+ релизы до продукта — рендер чарта каждого релиза рецепта cert-manager-up
//	                       (cert-manager и контроллер политики выпуска) его ручками;
//	+ плоскость kind      — базовая линия kindControlPlaneCPU, ИЗМЕРЕННАЯ;
//	≤ ёмкость узла ранера — по метке `runs-on` работы (runnerCapacitySource).
//
// Единица — запрос процессора ПОДА так, как его считает планировщик: сумма
// основных контейнеров и боковых (init с `restartPolicy: Always`) против пика
// обычных init; запрос, не объявленный при объявленном пределе, равен пределу;
// `overhead` пода прибавляется. Реплик — max(spec.replicas, minReplicas его
// HPA): автоскейлер поднимает объект до своего минимума сам. DaemonSet — по
// поду на узел.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
//   - не моделирует накатку: лишний под переката (maxSurge) при обновлении
//     dev → dev-prod сверх суммы не считается;
//   - сужение состава ручкой DEV_UP_VALUES (шарды сквозных проб) не учитывает:
//     судится полный состав цепочки, то есть верхняя оценка;
//   - хуки helm не суммирует (исполняются вне окна ожидания готовности: до
//     применения либо после него), CronJob — тоже (в окне подъёма не идёт);
//     их число печатается переписью;
//   - память не судит: это вторая ось того же класса, и у неё нет объявленной
//     ёмкости узла, с которой сверять.
//
// Способность упасть и смолчать — stand_chain_fits_its_runner_injection_test.go;
// сам гейт по рендеру — stand_chain_fits_its_runner_render_test.go (тег
// helmcharts: рендер умбреллы требует материализованных зависимостей).
package deploy_test

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// kindBaselineKindVersion — версия kind, на узле которой ИЗМЕРЕНА базовая
// линия ниже. Версия kind задаёт образ узла (v0.32.0 → kindest/node v1.36.1), а
// с ним — манифесты плоскости управления и их запросы. Пин конвейера,
// разошедшийся с этой строкой, — отказ предпосылки, а не зелёное.
const kindBaselineKindVersion = "v0.32.0"

// kindControlPlaneCPU — запросы процессора, которые узел kind несёт ДО продукта.
//
// Измерено 2026-10-01 на узле kindest/node v1.36.1 (kind v0.32.0), стенд
// kacho-own-2816:
//
//	kubectl -n kube-system get pods -o json \
//	  | jq '[.items[].spec.containers[].resources.requests.cpu // "0m"]'
//
// сумма 950m; то же число получил по прогонам ноги ревью волны. Пространство
// local-path-storage (поставщик томов узла) запросов не объявляет — 0m тем же
// замером. Рендерить это нечем: манифесты лежат в образе узла, а не в дереве.
var kindControlPlaneCPU = []struct {
	Pod   string
	Milli uint64
}{
	{"kube-apiserver", 250},
	{"kube-controller-manager", 200},
	{"kube-scheduler", 100},
	{"etcd", 100},
	{"coredns, две реплики", 200},
	{"kindnet", 100},
	{"kube-proxy", 0},
	{"local-path-provisioner", 0},
}

func kindBaselineMilli() uint64 {
	var s uint64
	for _, p := range kindControlPlaneCPU {
		s += p.Milli
	}
	return s
}

// runnerCapacitySource — метка ранера → документ дерева, называющий allocatable
// его узла. Число не выписывается здесь второй раз: его читает тем же
// выражением и проба приёмника писем (allocatableRe, mail_receiver_core_test.go).
// Метка вне перечня — находка: ёмкость не с чем сверять.
var runnerCapacitySource = map[string]string{
	"ubuntu-latest": "E2E-SHARDS.md",
}

// kindPinRe — пин kind в шаге установки работы конвейера.
var kindPinRe = regexp.MustCompile(`kind\.sigs\.k8s\.io/dl/(v[0-9][0-9.]*)/kind-`)

// cpuLine — один объект рендера, несущий поды.
type cpuLine struct {
	Kind, Name       string
	Replicas, PerPod uint64
}

func (l cpuLine) total() uint64 { return l.Replicas * l.PerPod }

// renderCPU — перепись запросов одного рендера.
type renderCPU struct {
	Lines     []cpuLine
	Hooks     int // хуки helm: вне окна ожидания готовности, не суммируются
	Scheduled int // CronJob: в окне подъёма не исполняется
}

func (r renderCPU) Total() uint64 {
	var s uint64
	for _, l := range r.Lines {
		s += l.total()
	}
	return s
}

// heaviest — до n самых тяжёлых строк, по убыванию.
func (r renderCPU) heaviest(n int) []cpuLine {
	out := append([]cpuLine(nil), r.Lines...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].total() > out[j].total() })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// containerCPUMilli — запрос процессора контейнера так, как его получит
// планировщик: объявленный запрос, а без него — предел (Kubernetes копирует
// предел в запрос, когда объявлен только предел).
func containerCPUMilli(c map[string]any) (uint64, error) {
	res, _ := c["resources"].(map[string]any)
	for _, k := range []string{"requests", "limits"} {
		m, _ := res[k].(map[string]any)
		if v, ok := m["cpu"]; ok && v != nil {
			return parseCPUMillis(fmt.Sprint(v))
		}
	}
	return 0, nil
}

// podCPUMilli — запрос процессора пода: max(основные + боковые, пик обычного
// init вместе с боковыми, объявленными до него) + overhead.
func podCPUMilli(ps map[string]any) (uint64, error) {
	var sidecars, initPeak, app uint64
	inits, _ := ps["initContainers"].([]any)
	for _, x := range inits {
		c, _ := x.(map[string]any)
		v, err := containerCPUMilli(c)
		if err != nil {
			return 0, fmt.Errorf("init-контейнер %v: %w", c["name"], err)
		}
		if rp, _ := c["restartPolicy"].(string); rp == "Always" {
			sidecars += v
			continue
		}
		if p := v + sidecars; p > initPeak {
			initPeak = p
		}
	}
	conts, _ := ps["containers"].([]any)
	for _, x := range conts {
		c, _ := x.(map[string]any)
		v, err := containerCPUMilli(c)
		if err != nil {
			return 0, fmt.Errorf("контейнер %v: %w", c["name"], err)
		}
		app += v
	}
	eff := app + sidecars
	if initPeak > eff {
		eff = initPeak
	}
	if oh, ok := ps["overhead"].(map[string]any); ok {
		if v, ok := oh["cpu"]; ok && v != nil {
			o, err := parseCPUMillis(fmt.Sprint(v))
			if err != nil {
				return 0, fmt.Errorf("overhead пода: %w", err)
			}
			eff += o
		}
	}
	return eff, nil
}

// declaredReplicas — spec.replicas объекта; не объявлено — одна.
func declaredReplicas(spec map[string]any) (uint64, error) {
	v, ok := spec["replicas"]
	if !ok || v == nil {
		return 1, nil
	}
	switch n := v.(type) {
	case int:
		if n < 0 {
			return 0, fmt.Errorf("replicas %d отрицательно", n)
		}
		return uint64(n), nil
	default:
		return 0, fmt.Errorf("replicas %v (%T) — не целое", v, v)
	}
}

// renderCPURequests — перепись запросов процессора по документам рендера.
// Неразборная величина — отказ с координатой: пропустить объект значило бы
// сузить сумму молча.
func renderCPURequests(docs []map[string]any, nodes uint64) (renderCPU, error) {
	var out renderCPU
	hpaMin := map[string]uint64{}
	for _, d := range docs {
		if k, _ := d["kind"].(string); k != "HorizontalPodAutoscaler" {
			continue
		}
		spec, _ := d["spec"].(map[string]any)
		ref, _ := spec["scaleTargetRef"].(map[string]any)
		minR := uint64(1)
		if v, ok := spec["minReplicas"].(int); ok && v > 0 {
			minR = uint64(v)
		}
		hpaMin[fmt.Sprint(ref["kind"])+"/"+fmt.Sprint(ref["name"])] = minR
	}
	for _, d := range docs {
		kind, _ := d["kind"].(string)
		md, _ := d["metadata"].(map[string]any)
		name, _ := md["name"].(string)
		ann, _ := md["annotations"].(map[string]any)
		hook := ann["helm.sh/hook"] != nil && fmt.Sprint(ann["helm.sh/hook"]) != ""
		spec, _ := d["spec"].(map[string]any)
		var (
			replicas uint64
			ps       map[string]any
			err      error
		)
		switch kind {
		case "Deployment", "StatefulSet", "ReplicaSet":
			if replicas, err = declaredReplicas(spec); err != nil {
				return out, fmt.Errorf("%s/%s: %w", kind, name, err)
			}
			if m, ok := hpaMin[kind+"/"+name]; ok && m > replicas {
				replicas = m
			}
		case "DaemonSet":
			replicas = nodes
		case "Job":
			if hook {
				out.Hooks++
				continue
			}
			replicas = 1
			if p, ok := spec["parallelism"].(int); ok && p >= 0 {
				replicas = uint64(p)
			}
		case "Pod":
			if hook {
				out.Hooks++
				continue
			}
			replicas, ps = 1, spec
		case "CronJob":
			out.Scheduled++
			continue
		default:
			continue
		}
		if ps == nil {
			tpl, _ := spec["template"].(map[string]any)
			ps, _ = tpl["spec"].(map[string]any)
		}
		if ps == nil {
			return out, fmt.Errorf("%s/%s: шаблона пода нет — запрос не вычислить", kind, name)
		}
		per, err := podCPUMilli(ps)
		if err != nil {
			return out, fmt.Errorf("%s/%s: %w", kind, name, err)
		}
		out.Lines = append(out.Lines, cpuLine{Kind: kind, Name: name, Replicas: replicas, PerPod: per})
	}
	return out, nil
}

// runnerFitCase — одна цепочка одной ноги на одном ранере.
type runnerFitCase struct {
	Leg, Runner, Chain string
	Umbrella, Release  renderCPU
}

// judgeRunnerFit — НАХОДКИ. Чистая функция: инъекция подаёт ей синтетический
// вход и не трогает ни дерева, ни helm.
func judgeRunnerFit(cases []runnerFitCase, capacity map[string]uint64, baseline uint64) []string {
	var out []string
	for _, c := range cases {
		if len(c.Umbrella.Lines) == 0 {
			out = append(out, fmt.Sprintf("нога %s, цепочка %s: в рендере ни одного объекта с подами — "+
				"«помещается» было бы сказано о пустом", c.Leg, c.Chain))
			continue
		}
		capMilli, ok := capacity[c.Runner]
		if !ok || capMilli == 0 {
			out = append(out, fmt.Sprintf("нога %s, цепочка %s: ёмкость узла ранера %q не объявлена "+
				"(runnerCapacitySource) — помещается ли цепочка, сверять не с чем", c.Leg, c.Chain, c.Runner))
			continue
		}
		total := c.Umbrella.Total() + c.Release.Total() + baseline
		if total <= capMilli {
			continue
		}
		var top []string
		for _, l := range c.Umbrella.heaviest(5) {
			top = append(top, fmt.Sprintf("%s %dm", l.Name, l.total()))
		}
		out = append(out, fmt.Sprintf("нога %s поднимает цепочку %s на ранере %s: запросы процессора "+
			"%dm (цепочка) + %dm (релиз cert-manager) + %dm (плоскость управления kind) = %dm при ёмкости "+
			"узла %dm — не помещается на %dm. Планировщик оставит поды Pending, и подъём упадёт по сроку "+
			"(«Progress deadline exceeded»), не назвав причины. Тяжелейшие объекты цепочки: %s. Снимите "+
			"аппетит слоем площадки стенда либо смените ранер",
			c.Leg, c.Chain, c.Runner, c.Umbrella.Total(), c.Release.Total(), baseline, total, capMilli,
			total-capMilli, strings.Join(top, ", ")))
	}
	return out
}

// kindPinsOf — версии kind, которые ставит процесс конвейера.
func kindPinsOf(workflow string) []string {
	var out []string
	for _, m := range kindPinRe.FindAllStringSubmatch(workflow, -1) {
		out = append(out, m[1])
	}
	return out
}

// kindPinFindings — предпосылка базовой линии: процесс ноги ставит kind ровно
// той версии, на узле которой линия измерена.
func kindPinFindings(workflow, text string) []string {
	pins := kindPinsOf(text)
	if len(pins) == 0 {
		return []string{fmt.Sprintf("процесс %s поднимает стенд, а пин kind в нём не прочитан — "+
			"базовая линия плоскости управления относилась бы к неизвестному узлу", workflow)}
	}
	var out []string
	for _, p := range pins {
		if p != kindBaselineKindVersion {
			out = append(out, fmt.Sprintf("процесс %s ставит kind %s, а базовая линия плоскости управления "+
				"(kindControlPlaneCPU) измерена на kind %s — перемерьте её на новом узле и смените обе "+
				"строки одним изменением", workflow, p, kindBaselineKindVersion))
		}
	}
	return out
}

// kindNodeFindings — предпосылка арифметики ОДНОГО узла: конфиг кластера
// объявляет ровно один узел и не подменяет его образ.
func kindNodeFindings(config string) []string {
	var doc struct {
		Nodes []map[string]any `yaml:"nodes"`
	}
	if err := yaml.Unmarshal([]byte(config), &doc); err != nil {
		return []string{fmt.Sprintf("конфиг кластера kind не разбирается: %v", err)}
	}
	var out []string
	if len(doc.Nodes) != 1 {
		out = append(out, fmt.Sprintf("конфиг кластера kind объявляет узлов %d, а арифметика гейта — "+
			"одного узла: сумма запросов против ёмкости одного узла о многоузловом кластере не говорит", len(doc.Nodes)))
	}
	for i, n := range doc.Nodes {
		if img, ok := n["image"]; ok {
			out = append(out, fmt.Sprintf("узел %d конфига kind подменяет образ (%v) — базовая линия измерена "+
				"на образе пина kind %s и к нему не относится", i, img, kindBaselineKindVersion))
		}
	}
	return out
}

// runnerCapacities — ёмкость узла по меткам, прочитанная из документов-источников.
func runnerCapacities(t *testing.T) map[string]uint64 {
	t.Helper()
	out := map[string]uint64{}
	for label, doc := range runnerCapacitySource {
		raw, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("источник ёмкости ранера %s (%s) не читается: %v — сверять не с чем", label, doc, err)
		}
		m := allocatableRe.FindStringSubmatch(string(raw))
		if m == nil {
			t.Fatalf("в %s не назван allocatable ранера %s — источник ёмкости потерял предмет", doc, label)
		}
		v, err := parseCPUMillis(m[1] + "m")
		if err != nil || v == 0 {
			t.Fatalf("allocatable ранера %s в %s прочитан как %q — сверять не с чем", label, doc, m[1])
		}
		out[label] = v
	}
	return out
}

// runsOnLabel — метка ранера работы конвейера одной строкой.
func runsOnLabel(v any) string {
	switch r := v.(type) {
	case string:
		return r
	case []any:
		parts := make([]string, 0, len(r))
		for _, p := range r {
			parts = append(parts, fmt.Sprint(p))
		}
		return strings.Join(parts, ",")
	case nil:
		return ""
	default:
		return fmt.Sprint(r)
	}
}

// umbrellaOptsRe — ручки, которые рецепты подъёма дописывают к каждому
// применению умбреллы.
var umbrellaOptsRe = regexp.MustCompile(`(?m)^UMBRELLA_OPTS\s*:?=(.*)$`)

// setFlags — значения `--set` из строки флагов helm. Иной флаг — отказ:
// значения, которые проба не наложила, сделали бы её рендер чужим стендом.
func setFlags(flags string, allowed map[string]int) ([]string, error) {
	sets, setJSON, err := releaseFlags(flags, allowed)
	if err == nil && len(setJSON) > 0 {
		err = fmt.Errorf("флаг %q проба не моделирует", "--set-json")
	}
	return sets, err
}

// releaseFlags — значения `--set` и `--set-json` из строки флагов helm. Иной
// флаг — отказ, по той же причине, что у setFlags.
//
// Значение `--set-json` снимается с кавычек оболочки и с удвоения `$$` рецепта
// make; переменная оболочки внутри (`$approve`, `$issuer`) остаётся ЛИТЕРАЛОМ.
// Это не послабление: ручки, которые так задаёт рецепт, называют выпускающих
// для права одобрения (строка RBAC), и на запросы процессора пода не влияют, а
// ключ ручки наложен — чарт, отвергающий его форму, откажет рендеру и здесь.
func releaseFlags(flags string, allowed map[string]int) (sets, setJSON []string, err error) {
	f := strings.Fields(flags)
	for i := 0; i < len(f); i++ {
		switch {
		case f[i] == "--set" && i+1 < len(f):
			sets = append(sets, f[i+1])
			i++
		case f[i] == "--set-json" && i+1 < len(f):
			v, err := shellWord(f[i+1])
			if err != nil {
				return nil, nil, fmt.Errorf("значение --set-json %s: %w", f[i+1], err)
			}
			setJSON = append(setJSON, v)
			i++
		case allowed[f[i]] > 0:
			i += allowed[f[i]] - 1
		default:
			return nil, nil, fmt.Errorf("флаг %q проба не моделирует", f[i])
		}
	}
	return sets, setJSON, nil
}

// shellWord — одно слово рецепта так, как его получит helm: внешние кавычки
// оболочки сняты, `\"` внутри двойных — кавычка, `$$` make — `$`. Слово, чьи
// кавычки не закрыты в нём же (значение с пробелом), — отказ: разбиение по
// пробелам прочитало бы его половину.
func shellWord(w string) (string, error) {
	switch {
	case len(w) >= 2 && w[0] == '"' && w[len(w)-1] == '"':
		w = strings.ReplaceAll(w[1:len(w)-1], `\"`, `"`)
	case len(w) >= 2 && w[0] == '\'' && w[len(w)-1] == '\'':
		w = w[1 : len(w)-1]
	case strings.ContainsAny(w, `"'`):
		return "", fmt.Errorf("кавычки слова не закрыты в нём же")
	}
	return strings.ReplaceAll(w, "$$", "$"), nil
}

// umbrellaSets — `--set` ручки UMBRELLA_OPTS.
func umbrellaSets(makefile string) ([]string, error) {
	m := umbrellaOptsRe.FindStringSubmatch(makefile)
	if m == nil {
		return nil, fmt.Errorf("UMBRELLA_OPTS в Makefile не объявлен")
	}
	return setFlags(m[1], nil)
}

// helmRelease — отдельный релиз, который рецепт cert-manager-up ставит ДО
// продукта: его поды стоят на том же узле, что цепочка.
type helmRelease struct {
	Name, Chart   string
	Sets, SetJSON []string
}

// releaseVars — релизы, которые проба умеет моделировать: имя релиза и его
// чарт — переменными Makefile. Перечень ЗАКРЫТ: релиз рецепта, которого здесь
// нет, — отказ, а не пропуск. Так покраснел бы второй релиз (контроллер
// политики выпуска сертификатов служб, kacho#2916), заведённый в рецепт после
// пробы: молча он бы в сумму не попал.
var releaseVars = []struct{ ReleaseVar, ChartVar string }{
	{"CERT_MANAGER_RELEASE", "CERT_MANAGER_CHART"},
	{"APPROVER_POLICY_RELEASE", "APPROVER_POLICY_CHART"},
}

var (
	helmInstallRe      = regexp.MustCompile(`helm upgrade --install (\S+) (\S+)(.*?)--wait\b`)
	recipeContinuation = regexp.MustCompile(`\\\n\s*`)
)

// makeVar — значение простой переменной Makefile (`NAME := value`).
func makeVar(makefile, name string) (string, bool) {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*:?=\s*(\S+)\s*$`).FindStringSubmatch(makefile)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// releasesBeforeProduct — релизы рецепта cert-manager-up, каждый со своим
// чартом и ручками, в порядке рецепта.
func releasesBeforeProduct(makefile string) ([]helmRelease, error) {
	t, ok := parseRecipeTargets(makefile)["cert-manager-up"]
	if !ok {
		return nil, fmt.Errorf("цели cert-manager-up в Makefile нет")
	}
	recipe := recipeContinuation.ReplaceAllString(strings.Join(t.recipe, "\n"), " ")
	installs := helmInstallRe.FindAllStringSubmatch(recipe, -1)
	if len(installs) == 0 {
		return nil, fmt.Errorf("применение релиза в рецепте cert-manager-up не распознано")
	}
	out := make([]helmRelease, 0, len(installs))
	for _, in := range installs {
		var rel helmRelease
		known := false
		for _, rv := range releaseVars {
			if in[1] != "$("+rv.ReleaseVar+")" {
				continue
			}
			known = true
			if in[2] != "$("+rv.ChartVar+")" {
				return nil, fmt.Errorf("релиз %s ставится чартом %s, а не объявленным $(%s)", in[1], in[2], rv.ChartVar)
			}
			chart, ok := makeVar(makefile, rv.ChartVar)
			if !ok {
				return nil, fmt.Errorf("%s в Makefile не объявлен", rv.ChartVar)
			}
			name, ok := makeVar(makefile, rv.ReleaseVar)
			if !ok {
				name = strings.ToLower(strings.ReplaceAll(rv.ReleaseVar, "_", "-"))
			}
			rel = helmRelease{Name: name, Chart: chart}
		}
		if !known {
			return nil, fmt.Errorf("рецепт cert-manager-up ставит релиз %s (чарт %s), которого проба не "+
				"моделирует — его запросы не попали бы в сумму узла", in[1], in[2])
		}
		var err error
		rel.Sets, rel.SetJSON, err = releaseFlags(in[3], map[string]int{"-n": 2, "--create-namespace": 1})
		if err != nil {
			return nil, fmt.Errorf("релиз %s: %w", rel.Name, err)
		}
		out = append(out, rel)
	}
	return out, nil
}
