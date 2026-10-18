// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// kaname_cpu_supply_render_test.go — мера процессоров службы доступа
// (kaname_cpu_supply_test.go) на РЕНДЕРЕ каждой цепочки таблицы стендов и
// цепочки стенда проб под его слоем (kacho#3291).
//
// Судится то, что получает под, а не объявление профиля: блок ресурсов службы
// проходит через помощник подчарта (`kaname.containerResources`), и правка
// помощника, задевшая процессор, видна только в рендере.
//
// Три утверждения:
//
//	(1) каждая цепочка stacks.txt даёт контейнеру службы П1–П3, кроме цепочки,
//	    которую конвейер поднимает на kind и чей узел ранера двух ядер не
//	    вмещает по арифметике (освобождение печатается обеими суммами);
//	(2) слой стенда проб (scripts/stand-ns.sh, stand_overlay) поверх каждой
//	    цепочки, которую он поднимает, даёт службе П1, П3 и П2′ (порог запроса,
//	    при котором очередь всей ёмкости укладывается в срок операции, —
//	    kaname_cpu_supply_test.go); цепочка, рендер которой под слоем
//	    отказывает (боевая без приёмника писем), называется с первой строкой
//	    отказа, а цепочка стенда наборов обязана рендериться;
//	(3) квота пространства стенда проб (ns_objects) по процессору держит перекат
//	    цепочки стенда наборов под этим слоем: запросы с умолчанием LimitRange
//	    для контейнеров без запроса плюс поды развёртываний сверх реплик.
package deploy_test

import (
	"bytes"
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

// kanameServiceResources — блок `resources` контейнера службы доступа из
// документов рендера и строка его объекта в переписи запросов. Нет объекта или
// контейнера — отказ: вердикт был бы о пустом блоке.
func kanameServiceResources(t *testing.T, what string, docs []map[string]any) map[string]any {
	t.Helper()
	deployments := 0
	for _, d := range docs {
		if k, _ := d["kind"].(string); k != "Deployment" {
			continue
		}
		deployments++
		md, _ := d["metadata"].(map[string]any)
		if name, _ := md["name"].(string); name != "kaname" {
			continue
		}
		spec, _ := d["spec"].(map[string]any)
		tpl, _ := spec["template"].(map[string]any)
		ps, _ := tpl["spec"].(map[string]any)
		conts, _ := ps["containers"].([]any)
		for _, x := range conts {
			c, _ := x.(map[string]any)
			if n, _ := c["name"].(string); n == "kaname" {
				res, _ := c["resources"].(map[string]any)
				if res == nil {
					res = map[string]any{}
				}
				return res
			}
		}
		t.Fatalf("%s: в Deployment kaname нет контейнера kaname — вердикт беспредметен", what)
	}
	t.Fatalf("%s: Deployment kaname не найден среди %d Deployment — вердикт беспредметен", what, deployments)
	return nil
}

// kanameLine — строка службы доступа в переписи запросов цепочки.
func kanameLine(t *testing.T, what string, r renderCPU) cpuLine {
	t.Helper()
	for _, l := range r.Lines {
		if l.Kind == "Deployment" && l.Name == "kaname" {
			return l
		}
	}
	t.Fatalf("%s: в переписи запросов нет строки Deployment/kaname (строк %d)", what, len(r.Lines))
	return cpuLine{}
}

// kanameLoginClass — величины полосы входа из карты настроек службы в рендере:
// ёмкость проверяющего и класс проверки. Нет карты либо величины — отказ.
func kanameLoginClass(t *testing.T, what string, docs []map[string]any) (uint64, map[string]string) {
	t.Helper()
	keys := []string{"verifier-capacity", "hasher-format", "hasher-memory", "hasher-iterations", "hasher-parallelism"}
	for _, d := range docs {
		if k, _ := d["kind"].(string); k != "ConfigMap" {
			continue
		}
		md, _ := d["metadata"].(map[string]any)
		if n, _ := md["name"].(string); n != "kaname-config" {
			continue
		}
		data, _ := d["data"].(map[string]any)
		raw, _ := data["config.yaml"].(string)
		var tree map[string]any
		if err := yaml.Unmarshal([]byte(raw), &tree); err != nil {
			t.Fatalf("%s: карта настроек службы не разбирается: %v", what, err)
		}
		found := map[string]string{}
		var walk func(any)
		walk = func(n any) {
			switch x := n.(type) {
			case map[string]any:
				for k, v := range x {
					for _, want := range keys {
						if k == want {
							if prev, seen := found[k]; seen && prev != fmt.Sprint(v) {
								t.Fatalf("%s: величина %s объявлена дважды по-разному (%s и %v)", what, k, prev, v)
							}
							found[k] = fmt.Sprint(v)
						}
					}
					walk(v)
				}
			case []any:
				for _, v := range x {
					walk(v)
				}
			}
		}
		walk(tree)
		for _, k := range keys {
			if _, ok := found[k]; !ok {
				t.Fatalf("%s: в карте настроек службы нет величины %s — порог не выводится", what, k)
			}
		}
		capacity, err := parseUintStrict(found["verifier-capacity"])
		if err != nil {
			t.Fatalf("%s: ёмкость проверяющего не читается: %v", what, err)
		}
		return capacity, found
	}
	t.Fatalf("%s: карты настроек kaname-config в рендере нет", what)
	return 0, nil
}

// kindRaisedChains — цепочка → ноги конвейера, поднимающие её на kind, с
// ранером. Тот же распознаватель ног, что у гейта ёмкости ранера.
func kindRaisedChains(t *testing.T) map[string][]standLeg {
	t.Helper()
	out := map[string][]standLeg{}
	for _, l := range conveyorLegs(t) {
		for _, p := range l.Phases {
			out[p] = append(out[p], l)
		}
	}
	return out
}

func TestEveryChainGivesKanameTwoWholeCoresOrItsRunnerCannot(t *testing.T) {
	raw, err := os.ReadFile(standMakefile)
	if err != nil {
		t.Fatalf("рецепты подъёма %s не читаются: %v", standMakefile, err)
	}
	makefile := string(raw)
	sets, err := umbrellaSets(makefile)
	if err != nil {
		t.Fatalf("ручки применения умбреллы не прочитаны: %v", err)
	}
	chart, cmSets, err := certManagerRelease(makefile)
	if err != nil {
		t.Fatalf("релиз cert-manager рецепта не прочитан: %v", err)
	}
	release, err := renderCPURequests(renderDocBodies(t, "релиза cert-manager",
		renderCertManagerRelease(t, chart, cmSets)), 1)
	if err != nil {
		t.Fatalf("запросы релиза cert-manager не вычислены: %v", err)
	}
	capacity := runnerCapacities(t)
	raised := kindRaisedChains(t)
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("таблица стендов пуста — «все цепочки дают два ядра» было бы сказано о непрочитанном")
	}

	judged, exempt := 0, 0
	for _, name := range names {
		what := "цепочка " + name
		docs := renderDocBodies(t, what, renderChainCached(t, stacks[name], sets...))
		census, verdict := judgeKanameCPUSupply(kanameServiceResources(t, what, docs))
		if verdict == nil {
			judged++
			t.Logf("%s: %s — П1–П3 выполнены", what, census)
			continue
		}
		// Освобождение — только доказанной арифметикой узла ранера каждой ноги,
		// поднимающей цепочку на kind.
		legs := raised[name]
		if len(legs) == 0 {
			t.Errorf("%s: %v (%s) — цепочку конвейер на kind не поднимает, освобождать её нечем", what, verdict, census)
			continue
		}
		cpu, err := renderCPURequests(docs, 1)
		if err != nil {
			t.Fatalf("%s: запросы не вычислены: %v", what, err)
		}
		line := kanameLine(t, what, cpu)
		proven := true
		for _, l := range legs {
			cp, ok := capacity[l.RunsOn]
			if !ok {
				t.Errorf("%s: ёмкость ранера %q ноги %s / «%s» не объявлена — освобождение не доказать", what, l.RunsOn, l.Workflow, l.Name)
				proven = false
				continue
			}
			need, fits := kanameTwoCoresOnRunner(cpu.Total(), line.total(), line.Replicas, release.Total(), kindBaselineMilli(), cp)
			t.Logf("%s: нога %s / «%s», ранер %s: с двумя ядрами службы %dm (цепочка %dm − служба %dm + %d × %dm + cert-manager %dm + kind %dm) из %dm",
				what, l.Workflow, l.Name, l.RunsOn, need, cpu.Total(), line.total(), line.Replicas, kanameCPUWholeCores,
				release.Total(), kindBaselineMilli(), cp)
			if fits {
				proven = false
				t.Errorf("%s: %v (%s) — узел ранера %s ноги %s / «%s» два ядра ВМЕЩАЕТ (%dm из %dm): освобождение снято, "+
					"цепочка обязана дать службе запрос, равный пределу, два ядра", what, verdict, census, l.RunsOn, l.Workflow, l.Name, need, cp)
			}
		}
		if proven {
			exempt++
			t.Logf("%s: освобождена арифметикой узла — %v (%s)", what, verdict, census)
		}
	}
	if judged == 0 {
		t.Error("ни одна цепочка не дала службе двух ядер — освобождение стало правилом, а не исключением")
	}
	t.Logf("перепись: цепочек %d · П1–П3 выполнены %d · освобождены арифметикой ранера %d", len(names), judged, exempt)
}

// standOverlayFile — слой стенда проб файлом для `helm -f`.
func standOverlayFile(t *testing.T) string {
	t.Helper()
	body, err := yaml.Marshal(standOverlay(t))
	if err != nil {
		t.Fatalf("слой стенда не сериализуется: %v", err)
	}
	p := filepath.Join(t.TempDir(), "stand.yaml")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// renderChainUnderStandOverlay — рендер цепочки так, как её ставит подъём стенда
// проб: слои цепочки, затем слой стенда, cert-manager не ставится. Отказ helm
// возвращается: цепочка, которую слой стенда поднять не может, — исход, который
// вызывающий называет, а не роняет пробу.
func renderChainUnderStandOverlay(t *testing.T, chain []string, overlay string) (string, string, error) {
	t.Helper()
	return renderChainUnderStandOverlayWith(t, chain, []string{overlay})
}

// renderChainUnderStandOverlayWith — то же с несколькими слоями поверх цепочки
// (слой стенда, затем инъекция), в порядке наложения.
func renderChainUnderStandOverlayWith(t *testing.T, chain, overlays []string) (string, string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm не в PATH — рендерная проба под тегом helmcharts обязана исполняться, а не пропускаться")
	}
	args := []string{"template", "kacho-umbrella", umbrellaDir, "-n", "t3291-probe"}
	for _, p := range chain {
		args = append(args, "-f", filepath.Join(umbrellaDir, p))
	}
	for _, o := range overlays {
		args = append(args, "-f", o)
	}
	args = append(args, "--set", "cert-manager.enabled=false")
	var so, se bytes.Buffer
	cmd := exec.Command("helm", args...)
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	return so.String(), se.String(), err
}

// firstLine — первая непустая строка текста отказа.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

var suiteStackLine = regexp.MustCompile(`(?m)^SUITE_STACK=([a-z0-9-]+)\s*$`)

// standNsQuotaAndDefault — квота процессора пространства стенда и умолчание
// запроса LimitRange для контейнера, из того же вывода ns_objects, что
// применяет подъём.
func standNsQuotaAndDefault(t *testing.T, stack string) (quota, def uint64) {
	t.Helper()
	call := fmt.Sprintf("ns_objects t3291-probe 3291 2026-10-19T00:00:00Z %s 0000000 cm-ns kube-system %s",
		stack, shellQuote("{k8s-app: kube-dns}"))
	out, err := standNsCall(t, call)
	if err != nil {
		t.Fatalf("объекты пространства стенда не выведены: %v\n%s", err, out)
	}
	quotaSeen, defSeen := false, false
	for _, d := range renderDocBodies(t, "объектов пространства стенда", out) {
		switch d["kind"] {
		case "ResourceQuota":
			v, ok := lookup(d, "spec", "hard", "requests.cpu")
			if !ok {
				t.Fatal("квота пространства стенда не объявляет requests.cpu — перекату нечем быть ограниченным, суд беспредметен")
			}
			if quota, err = parseCPUMillis(fmt.Sprint(v)); err != nil {
				t.Fatalf("квота requests.cpu %v не читается: %v", v, err)
			}
			quotaSeen = true
		case "LimitRange":
			limits, _ := lookup(d, "spec", "limits")
			list, _ := limits.([]any)
			for _, x := range list {
				m, _ := x.(map[string]any)
				if m["type"] != "Container" {
					continue
				}
				if v, ok := lookup(m, "defaultRequest", "cpu"); ok {
					if def, err = parseCPUMillis(fmt.Sprint(v)); err != nil {
						t.Fatalf("умолчание LimitRange cpu %v не читается: %v", v, err)
					}
					defSeen = true
				}
			}
		}
	}
	if !quotaSeen || !defSeen {
		t.Fatalf("в объектах пространства стенда квота (%v) или умолчание LimitRange (%v) не найдены", quotaSeen, defSeen)
	}
	return quota, def
}

// withLimitRangeDefault — контейнерам без запроса и предела процессора ставит
// умолчание LimitRange, как это сделает допуск пространства.
func withLimitRangeDefault(docs []map[string]any, def uint64) int {
	filled := 0
	fill := func(list []any) {
		for _, x := range list {
			c, _ := x.(map[string]any)
			if c == nil {
				continue
			}
			res, _ := c["resources"].(map[string]any)
			has := false
			for _, k := range []string{"requests", "limits"} {
				m, _ := res[k].(map[string]any)
				if v, ok := m["cpu"]; ok && v != nil {
					has = true
				}
			}
			if has {
				continue
			}
			if res == nil {
				res = map[string]any{}
				c["resources"] = res
			}
			req, _ := res["requests"].(map[string]any)
			if req == nil {
				req = map[string]any{}
				res["requests"] = req
			}
			req["cpu"] = fmt.Sprintf("%dm", def)
			filled++
		}
	}
	for _, d := range docs {
		spec, _ := d["spec"].(map[string]any)
		ps := spec
		if tpl, ok := spec["template"].(map[string]any); ok {
			ps, _ = tpl["spec"].(map[string]any)
		}
		if ps == nil {
			continue
		}
		if l, ok := ps["containers"].([]any); ok {
			fill(l)
		}
		if l, ok := ps["initContainers"].([]any); ok {
			fill(l)
		}
	}
	return filled
}

func TestStandOverlayGivesKanameItsCPUFloorAndTheQuotaHoldsTheRollout(t *testing.T) {
	overlay := standOverlayFile(t)
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("таблица стендов пуста — слой стенда судить не над чем")
	}

	script, err := os.ReadFile(standNsScript)
	if err != nil {
		t.Fatalf("скрипт стенда %s не читается: %v", standNsScript, err)
	}
	m := suiteStackLine.FindStringSubmatch(string(script))
	if m == nil {
		t.Fatalf("в %s нет строки SUITE_STACK= — цепочку стенда наборов не назвать", standNsScript)
	}
	suite := m[1]
	if _, ok := stacks[suite]; !ok {
		t.Fatalf("цепочки стенда наборов %q в таблице стендов нет", suite)
	}

	prodChain, ok := stacks["prod"]
	if !ok {
		t.Fatal("боевой цепочки prod в таблице стендов нет — ёмкость продукта для порога не прочитать")
	}
	prodCapacity, _ := kanameLoginClass(t, "цепочка prod", renderDocBodies(t, "цепочка prod", renderChainCached(t, prodChain)))

	var (
		suiteDocs []map[string]any
		refused   []string
		judged    int
	)
	for _, name := range names {
		what := "цепочка " + name + " под слоем стенда проб"
		out, stderr, err := renderChainUnderStandOverlay(t, stacks[name], overlay)
		if err != nil {
			if name == suite {
				t.Fatalf("%s: рендер не выполнен (%v) — стенд наборов не поднимаем, условие не создано:\n%s", what, err, stderr)
			}
			refused = append(refused, name+" — "+firstLine(stderr))
			continue
		}
		docs := renderDocBodies(t, what, out)
		own, class := kanameLoginClass(t, what, docs)
		capacity := max(own, prodCapacity)
		census, floor, verdict := judgeStandKanameCPU(kanameServiceResources(t, what, docs), capacity, class)
		if verdict != nil {
			t.Errorf("%s: %v (%s)", what, verdict, census)
			continue
		}
		judged++
		t.Logf("%s: %s · ёмкость max(цепочка %d, prod %d) = %d · %s — П1, П3, П2′ выполнены",
			what, census, own, prodCapacity, capacity, floor)
		if name == suite {
			suiteDocs = docs
		}
	}
	if suiteDocs == nil {
		t.Fatalf("рендер цепочки стенда наборов %s под слоем не получен — квоту сверять не с чем", suite)
	}

	t.Logf("перепись слоя стенда: цепочек %d · судимы под слоем %d · слой их не поднимает %d: %v",
		len(names), judged, len(refused), refused)

	quota, def := standNsQuotaAndDefault(t, suite)
	filled := withLimitRangeDefault(suiteDocs, def)
	cpu, err := renderCPURequests(suiteDocs, 1)
	if err != nil {
		t.Fatalf("запросы цепочки %s под слоем не вычислены: %v", suite, err)
	}
	line := kanameLine(t, "цепочка "+suite+" под слоем стенда проб", cpu)
	surge, err := rolloutSurgeMilli(suiteDocs)
	if err != nil {
		t.Fatalf("прибавка переката цепочки %s не вычислена: %v", suite, err)
	}
	t.Logf("стенд проб на цепочке %s: запросы %dm (служба доступа %dm, контейнеров с умолчанием LimitRange %d × %dm), "+
		"поды развёртываний сверх реплик %dm, с перекатом %dm, квота %dm", suite, cpu.Total(), line.total(), filled, def,
		surge, cpu.Total()+surge, quota)
	if err := judgeStandQuotaHoldsRollout(cpu.Total(), surge, quota); err != nil {
		t.Errorf("цепочка %s: %v", suite, err)
	}
}

// Инъекции настоящим входом: боевая цепочка prod, один факт `--set` поверх неё.
// Контроль молчит, каждое нарушение называет своё условие, близнец 4/4 молчит.
func TestKanameCPUSupplyRenderInjection(t *testing.T) {
	stacks := deployStacks(t)
	chain, ok := stacks["prod"]
	if !ok {
		t.Fatal("боевой цепочки prod в таблице стендов нет — инъекции не над чем")
	}
	judge := func(sets ...string) (kanameCPUCensus, error) {
		what := fmt.Sprintf("prod %v", sets)
		return judgeKanameCPUSupply(kanameServiceResources(t, what, renderDocBodies(t, what, renderChainCached(t, chain, sets...))))
	}
	if census, err := judge(); err != nil {
		t.Fatalf("контроль красен — вердикты инъекций недействительны: %v (%s)", err, census)
	}
	cases := []struct {
		name string
		sets []string
		want string
	}{
		{"снятый предел", []string{"kaname.resources.limits.cpu=null"}, "П1:"},
		{"запрос ниже предела", []string{"kaname.resources.requests.cpu=100m"}, "П2:"},
		{"дробный предел", []string{"kaname.resources.requests.cpu=1500m", "kaname.resources.limits.cpu=1500m"}, "П3:"},
		{"предел один процессор", []string{"kaname.resources.requests.cpu=1", "kaname.resources.limits.cpu=1"}, "П3:"},
	}
	for _, c := range cases {
		census, err := judge(c.sets...)
		if err == nil {
			t.Errorf("%s: мера смолчала на %v (%s)", c.name, c.sets, census)
			continue
		}
		if !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%s: отказ называет не то условие: %v", c.name, err)
		}
	}
	if census, err := judge("kaname.resources.requests.cpu=4", "kaname.resources.limits.cpu=4"); err != nil || census.GOMAXPROCS != 4 {
		t.Errorf("законный близнец 4/4 отвергнут или прочитан не так: %v (%s)", err, census)
	}
}

// Инъекция П2′ настоящим входом: цепочка стенда наборов под слоем стенда проб
// с ОДНИМ фактом поверх — запрос 100m краснеет, поставляемые 500m молчат.
func TestStandOverlayFloorRenderInjection(t *testing.T) {
	stacks := deployStacks(t)
	script, err := os.ReadFile(standNsScript)
	if err != nil {
		t.Fatalf("скрипт стенда %s не читается: %v", standNsScript, err)
	}
	m := suiteStackLine.FindStringSubmatch(string(script))
	if m == nil {
		t.Fatalf("в %s нет строки SUITE_STACK=", standNsScript)
	}
	chain, ok := stacks[m[1]]
	if !ok {
		t.Fatalf("цепочки стенда наборов %q в таблице нет", m[1])
	}
	prodCapacity, _ := kanameLoginClass(t, "цепочка prod", renderDocBodies(t, "цепочка prod", renderChainCached(t, stacks["prod"])))
	overlay := standOverlayFile(t)
	judge := func(extra string) (standCPUFloor, error) {
		args := []string{}
		if extra != "" {
			p := filepath.Join(t.TempDir(), "inject.yaml")
			if err := os.WriteFile(p, []byte(extra), 0o600); err != nil {
				t.Fatal(err)
			}
			args = append(args, p)
		}
		out, stderr, err := renderChainUnderStandOverlayWith(t, chain, append([]string{overlay}, args...))
		if err != nil {
			t.Fatalf("рендер инъекции не выполнен (%v):\n%s", err, stderr)
		}
		docs := renderDocBodies(t, "инъекция", out)
		own, class := kanameLoginClass(t, "инъекция", docs)
		_, f, verr := judgeStandKanameCPU(kanameServiceResources(t, "инъекция", docs), max(own, prodCapacity), class)
		return f, verr
	}
	if f, err := judge(""); err != nil {
		t.Fatalf("контроль (поставляемый слой) красен — вердикт инъекции недействителен: %v", err)
	} else {
		t.Logf("контроль: %s", f)
	}
	f, err := judge("kaname:\n  resources:\n    requests:\n      cpu: 100m\n")
	if err == nil || !strings.HasPrefix(err.Error(), "П2′:") {
		t.Errorf("запрос 100m под слоем стенда: мера смолчала либо назвала не то условие: %v (%s)", err, f)
	}
	t.Logf("инъекция 100m: %v", err)
}
