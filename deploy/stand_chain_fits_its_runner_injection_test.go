// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_chain_fits_its_runner_injection_test.go — гейт ёмкости узла ранера
// умеет упасть и умеет смолчать. Каждый отрицательный кейс меняет ровно один
// факт против законного близнеца. Вход синтетический: решение вынесено чистыми
// функциями (stand_chain_fits_its_runner_test.go), helm и дерево не нужны.
package deploy_test

import (
	"strings"
	"testing"
)

func podWith(inits, conts []map[string]any) map[string]any {
	ps := map[string]any{}
	toAny := func(l []map[string]any) []any {
		out := make([]any, 0, len(l))
		for _, c := range l {
			out = append(out, c)
		}
		return out
	}
	if inits != nil {
		ps["initContainers"] = toAny(inits)
	}
	ps["containers"] = toAny(conts)
	return ps
}

func cont(name string, requests, limits map[string]any) map[string]any {
	res := map[string]any{}
	if requests != nil {
		res["requests"] = requests
	}
	if limits != nil {
		res["limits"] = limits
	}
	return map[string]any{"name": name, "resources": res}
}

func TestRunnerFitPodRequest_KnowsEveryLawfulFormOfTheRequest(t *testing.T) {
	sidecar := cont("side", map[string]any{"cpu": "50m"}, nil)
	sidecar["restartPolicy"] = "Always"
	cases := []struct {
		name string
		ps   map[string]any
		want uint64
	}{
		{"запрос объявлен", podWith(nil, []map[string]any{cont("a", map[string]any{"cpu": "100m"}, nil)}), 100},
		{"объявлен только предел — запрос равен пределу",
			podWith(nil, []map[string]any{cont("a", nil, map[string]any{"cpu": "300m"})}), 300},
		{"запрос и предел — берётся запрос",
			podWith(nil, []map[string]any{cont("a", map[string]any{"cpu": "100m"}, map[string]any{"cpu": "2"})}), 100},
		{"целые ядра", podWith(nil, []map[string]any{cont("a", map[string]any{"cpu": 1}, nil)}), 1000},
		{"ничего не объявлено", podWith(nil, []map[string]any{cont("a", nil, nil)}), 0},
		{"два контейнера складываются", podWith(nil, []map[string]any{
			cont("a", map[string]any{"cpu": "100m"}, nil), cont("b", map[string]any{"cpu": "30m"}, nil)}), 130},
		{"init тяжелее основных — пик init", podWith(
			[]map[string]any{cont("i", map[string]any{"cpu": "500m"}, nil)},
			[]map[string]any{cont("a", map[string]any{"cpu": "100m"}, nil)}), 500},
		{"init легче основных — основные", podWith(
			[]map[string]any{cont("i", map[string]any{"cpu": "50m"}, nil)},
			[]map[string]any{cont("a", map[string]any{"cpu": "100m"}, nil)}), 100},
		{"боковой init прибавляется к основным", podWith(
			[]map[string]any{sidecar},
			[]map[string]any{cont("a", map[string]any{"cpu": "100m"}, nil)}), 150},
		{"обычный init после бокового стоит вместе с ним", podWith(
			[]map[string]any{sidecar, cont("i", map[string]any{"cpu": "120m"}, nil)},
			[]map[string]any{cont("a", map[string]any{"cpu": "100m"}, nil)}), 170},
	}
	for _, c := range cases {
		got, err := podCPUMilli(c.ps)
		if err != nil {
			t.Errorf("%s: отказ %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: %dm, ожидалось %dm", c.name, got, c.want)
		}
	}
	withOverhead := podWith(nil, []map[string]any{cont("a", map[string]any{"cpu": "100m"}, nil)})
	withOverhead["overhead"] = map[string]any{"cpu": "10m"}
	if got, err := podCPUMilli(withOverhead); err != nil || got != 110 {
		t.Errorf("overhead пода не прибавлен: %dm, %v", got, err)
	}
	// Форма, которой разбор не знает, — ОТКАЗ вслух, а не ноль.
	if _, err := podCPUMilli(podWith(nil, []map[string]any{cont("a", map[string]any{"cpu": "0.5"}, nil)})); err == nil {
		t.Error("дробные ядра «0.5» прочитаны молча — разбор обязан отказать, а не сложить ноль")
	}
	t.Logf("перепись: форм запроса подано %d + overhead + неизвестная форма", len(cases))
}

func workload(kind, name string, replicas any, perPod string, ann map[string]any) map[string]any {
	spec := map[string]any{}
	if replicas != nil {
		spec["replicas"] = replicas
	}
	ps := podWith(nil, []map[string]any{cont("c", map[string]any{"cpu": perPod}, nil)})
	if kind == "Pod" {
		spec = ps
	} else {
		spec["template"] = map[string]any{"spec": ps}
	}
	md := map[string]any{"name": name}
	if ann != nil {
		md["annotations"] = ann
	}
	return map[string]any{"kind": kind, "metadata": md, "spec": spec}
}

func TestRunnerFitRenderCensus_CountsReplicasHPAAndLeavesHooksOut(t *testing.T) {
	docs := []map[string]any{
		workload("Deployment", "d2", 2, "100m", nil),
		workload("StatefulSet", "s", nil, "250m", nil),
		workload("DaemonSet", "ds", nil, "10m", nil),
		workload("Deployment", "scaled", 1, "100m", nil),
		{"kind": "HorizontalPodAutoscaler", "metadata": map[string]any{"name": "scaled"},
			"spec": map[string]any{"minReplicas": 3, "scaleTargetRef": map[string]any{"kind": "Deployment", "name": "scaled"}}},
		workload("Job", "hook", nil, "900m", map[string]any{"helm.sh/hook": "post-install"}),
		workload("Job", "plain", nil, "40m", nil),
		workload("Pod", "p", nil, "5m", nil),
		{"kind": "CronJob", "metadata": map[string]any{"name": "cj"}, "spec": map[string]any{}},
		{"kind": "ConfigMap", "metadata": map[string]any{"name": "cm"}},
	}
	got, err := renderCPURequests(docs, 1)
	if err != nil {
		t.Fatalf("отказ на законном входе: %v", err)
	}
	// 2×100 + 250 + 10 + 3×100 (минимум HPA) + 40 + 5 = 805; хук 900m вне суммы.
	if got.Total() != 805 {
		t.Errorf("сумма %dm, ожидалось 805m: %+v", got.Total(), got.Lines)
	}
	if got.Hooks != 1 || got.Scheduled != 1 || len(got.Lines) != 6 {
		t.Errorf("перепись: строк %d (ожидалось 6) · хуков %d (1) · CronJob %d (1)", len(got.Lines), got.Hooks, got.Scheduled)
	}
	// Тот же вход, но DaemonSet на двух узлах — ровно один факт.
	if two, _ := renderCPURequests(docs, 2); two.Total() != 815 {
		t.Errorf("DaemonSet не умножен на узлы: %dm, ожидалось 815m", two.Total())
	}
	if _, err := renderCPURequests([]map[string]any{workload("Deployment", "bad", "two", "100m", nil)}, 1); err == nil {
		t.Error("replicas строкой прочитано молча — обязан быть отказ с координатой")
	}
	noTpl := map[string]any{"kind": "Deployment", "metadata": map[string]any{"name": "x"}, "spec": map[string]any{}}
	if _, err := renderCPURequests([]map[string]any{noTpl}, 1); err == nil {
		t.Error("объект без шаблона пода прочитан молча — обязан быть отказ")
	}
}

func fitCase(chainMilli uint64, runner string) runnerFitCase {
	return runnerFitCase{Leg: "w.yml / «n»", Runner: runner, Chain: "own",
		Umbrella: renderCPU{Lines: []cpuLine{{Kind: "StatefulSet", Name: "pg-iam", Replicas: 1, PerPod: chainMilli}}}}
}

func TestRunnerFitJudge_FiresOnTheOverflowAndStaysSilentOnTheTwin(t *testing.T) {
	capacity := map[string]uint64{"ubuntu-latest": 4000}
	const baseline = 950
	// Законные близнецы: вес стенда dev-prod и ровно край ёмкости.
	for _, c := range []runnerFitCase{fitCase(1970, "ubuntu-latest"), fitCase(4000-baseline, "ubuntu-latest")} {
		if got := judgeRunnerFit([]runnerFitCase{c}, capacity, baseline); len(got) != 0 {
			t.Errorf("законный близнец %dm покраснел: %v", c.Umbrella.Total(), got)
		}
	}
	cases := []struct {
		name string
		in   runnerFitCase
		want []string
	}{
		{"вес цепочки own до правки", fitCase(3520, "ubuntu-latest"),
			[]string{"цепочку own", "3520m (цепочка)", "950m (плоскость управления kind)", "= 4470m", "4000m", "на 470m", "pg-iam 3520m"}},
		{"на миллиядро сверх края", fitCase(4000-baseline+1, "ubuntu-latest"), []string{"на 1m"}},
		{"ёмкость метки не объявлена", fitCase(100, "self-hosted"), []string{"\"self-hosted\" не объявлена"}},
		{"пустой рендер", runnerFitCase{Leg: "l", Runner: "ubuntu-latest", Chain: "own"}, []string{"ни одного объекта с подами"}},
	}
	for _, c := range cases {
		got := judgeRunnerFit([]runnerFitCase{c.in}, capacity, baseline)
		if len(got) != 1 {
			t.Errorf("%s: находок %d, ожидалась одна: %v", c.name, len(got), got)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(got[0], w) {
				t.Errorf("%s: находка не называет %q:\n%s", c.name, w, got[0])
			}
		}
	}
	// Релиз cert-manager входит в сумму: тот же близнец + 1m релиза краснеет.
	edge := fitCase(4000-baseline, "ubuntu-latest")
	edge.Release = renderCPU{Lines: []cpuLine{{Kind: "Deployment", Name: "cm", Replicas: 1, PerPod: 1}}}
	if got := judgeRunnerFit([]runnerFitCase{edge}, capacity, baseline); len(got) != 1 {
		t.Errorf("запросы релиза cert-manager не вошли в сумму: %v", got)
	}
	t.Logf("перепись: близнецов 2 · отрицательных кейсов %d + релиз cert-manager", len(cases))
}

func TestRunnerFitPremise_KindPinAndSingleNode(t *testing.T) {
	ok := "curl -sSLo k https://kind.sigs.k8s.io/dl/" + kindBaselineKindVersion + "/kind-linux-amd64"
	if got := kindPinFindings("w.yml", ok); len(got) != 0 {
		t.Errorf("пин той версии, на которой измерена линия, покраснел: %v", got)
	}
	if got := kindPinFindings("w.yml", strings.Replace(ok, kindBaselineKindVersion, "v0.33.0", 1)); len(got) != 1 ||
		!strings.Contains(got[0], "v0.33.0") {
		t.Errorf("сменившийся пин kind не назван: %v", got)
	}
	if got := kindPinFindings("w.yml", "make own-up"); len(got) != 1 {
		t.Errorf("процесс без пина kind прошёл молча: %v", got)
	}

	one := "nodes:\n  - role: control-plane\n"
	if got := kindNodeFindings(one); len(got) != 0 {
		t.Errorf("один узел без подмены образа покраснел: %v", got)
	}
	if got := kindNodeFindings(one + "  - role: worker\n"); len(got) != 1 {
		t.Errorf("второй узел прошёл молча: %v", got)
	}
	if got := kindNodeFindings("nodes:\n  - role: control-plane\n    image: kindest/node:v1.30.0\n"); len(got) != 1 {
		t.Errorf("подмена образа узла прошла молча: %v", got)
	}
	if kindBaselineMilli() == 0 {
		t.Error("базовая линия плоскости управления — ноль: сумма сверялась бы без неё")
	}
}

func TestRunnerFitRecipe_ReadsTheFlagsAndRefusesAnUnknownOne(t *testing.T) {
	if got, err := setFlags(" --set a.enabled=false --set b=1", nil); err != nil || strings.Join(got, ",") != "a.enabled=false,b=1" {
		t.Errorf("ручки --set не прочитаны: %v, %v", got, err)
	}
	if _, err := setFlags("--set a=1 -f extra.yaml", nil); err == nil {
		t.Error("флаг -f прочитан молча — значения, которых проба не наложила, сделали бы рендер чужим стендом")
	}
	if _, err := setFlags(`--set a=1 --set-json "b=[1]"`, nil); err == nil {
		t.Error("--set-json в ручках умбреллы прочитан молча — рендер цепочки его не накладывает")
	}
	if _, _, err := releaseFlags(`--set-json "b=[1, 2]"`, nil); err == nil {
		t.Error("значение --set-json с пробелом прочитано половиной")
	}
	mk := "UMBRELLA_OPTS := --set cert-manager.enabled=false\n" +
		"CERT_MANAGER_CHART   := ./charts/cm.tgz\n" +
		"cert-manager-up: guard\n" +
		"\t@set -e; \\\n" +
		"\t  helm upgrade --install $(CERT_MANAGER_RELEASE) $(CERT_MANAGER_CHART) -n \"$$ns\" --create-namespace \\\n" +
		"\t    --set crds.enabled=true \\\n" +
		"\t    --wait --timeout 5m\n"
	if got, err := umbrellaSets(mk); err != nil || strings.Join(got, ",") != "cert-manager.enabled=false" {
		t.Errorf("UMBRELLA_OPTS не прочитан: %v, %v", got, err)
	}
	rels, err := releasesBeforeProduct(mk)
	if err != nil || len(rels) != 1 || rels[0].Chart != "./charts/cm.tgz" || strings.Join(rels[0].Sets, ",") != "crds.enabled=true" {
		t.Errorf("релиз cert-manager не прочитан: %+v %v", rels, err)
	}
	withValues := strings.Replace(mk, "--set crds.enabled=true", "-f cm-values.yaml", 1)
	if _, err := releasesBeforeProduct(withValues); err == nil {
		t.Error("файл значений релиза cert-manager прочитан молча — его запросы проба не наложила бы")
	}
}

// TestRunnerFitRecipe_EveryReleaseBeforeTheProductIsModelled — рецепт
// cert-manager-up ставит ДВА релиза до продукта (cert-manager и контроллер
// политики выпуска, kacho#2916), и оба стоят на узле. Релиз, который рецепт
// ставит, а проба не узнала, — отказ: его запросы в сумму не попали бы молча.
// Ручка `--set-json` читается (значение снимается с кавычек оболочки), а не
// отвергается и не пропускается.
func TestRunnerFitRecipe_EveryReleaseBeforeTheProductIsModelled(t *testing.T) {
	mk := "CERT_MANAGER_CHART   := ./charts/cm.tgz\n" +
		"APPROVER_POLICY_CHART   := ./charts/ap.tgz\n" +
		"cert-manager-up: guard\n" +
		"\t@set -e; \\\n" +
		"\t  helm upgrade --install $(CERT_MANAGER_RELEASE) $(CERT_MANAGER_CHART) -n \"$$ns\" --create-namespace \\\n" +
		"\t    --set crds.enabled=true \\\n" +
		"\t    --set-json \"approveSignerNames=[\\\"$$approve\\\"]\" \\\n" +
		"\t    --wait --timeout 5m; \\\n" +
		"\thelm upgrade --install $(APPROVER_POLICY_RELEASE) $(APPROVER_POLICY_CHART) -n \"$$ns\" \\\n" +
		"\t  --set crds.enabled=true \\\n" +
		"\t  --set-json \"app.approveSignerNames=[\\\"clusterissuers.cert-manager.io/$$issuer\\\"]\" \\\n" +
		"\t  --wait --timeout 5m\n"
	rels, err := releasesBeforeProduct(mk)
	if err != nil || len(rels) != 2 {
		t.Fatalf("релизы до продукта не прочитаны: %v, %v", rels, err)
	}
	if r := rels[0]; r.Chart != "./charts/cm.tgz" || strings.Join(r.Sets, ",") != "crds.enabled=true" ||
		strings.Join(r.SetJSON, ",") != `approveSignerNames=["$approve"]` {
		t.Errorf("релиз cert-manager прочитан не тем: %+v", r)
	}
	if r := rels[1]; r.Chart != "./charts/ap.tgz" || strings.Join(r.Sets, ",") != "crds.enabled=true" ||
		strings.Join(r.SetJSON, ",") != `app.approveSignerNames=["clusterissuers.cert-manager.io/$issuer"]` {
		t.Errorf("релиз контроллера политики прочитан не тем: %+v", r)
	}
	unknown := strings.Replace(mk, "--wait --timeout 5m\n",
		"--wait --timeout 5m; \\\n\thelm upgrade --install other ./charts/other.tgz --wait\n", 1)
	if _, err := releasesBeforeProduct(unknown); err == nil || !strings.Contains(err.Error(), "other") {
		t.Errorf("третий релиз рецепта прошёл молча — его запросы не попали бы в сумму: %v", err)
	}
	noPolicy := strings.Replace(mk, "$(APPROVER_POLICY_CHART)", "./x.tgz", 1)
	if _, err := releasesBeforeProduct(noPolicy); err == nil {
		t.Error("релиз, поставленный не объявленным чартом, прошёл молча")
	}
}
