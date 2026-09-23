// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_memory_request_test.go — НА ПОСАДКЕ `own` ЗАПРОС ПАМЯТИ СЛУЖБЫ
// ЛИЧНОСТИ ВЫВЕДЕН ИЗ ТОГО ЖЕ ИСТОЧНИКА, ЧТО ПРЕДЕЛ, И КЛАСС ОБСЛУЖИВАНИЯ ПОДА
// НАЗВАН РЕШЕНИЕМ, А НЕ УМОЛЧАНИЕМ ПОДЧАРТА (kacho#2727).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Предел памяти контейнера — третье число бюджета полосы входа (ёмкость × память
// одной проверки на потолке + резерв), его судит own_lane_memory_budget_test.go.
// Запрос памяти жил отдельно — умолчанием подчарта, 128 МиБ (kacho `origin/2795`
// @ `c5a1e7cf`):
//
//	helm template … charts/kaname -f <цепочка стенда own> \
//	  | yq '… .resources'          → limits.memory 1280Mi · requests.memory 128Mi
//
// Страж полосы доказывал право занять 1280 МиБ, а планировщику объявлялось 128:
// узел выбирался по числу вдесятеро меньшему, чем под вправе занять, и правка
// предела запроса не двигала. Под `external` полоса не поднимается, и там это
// размен, названный в профиле; на `own` полоса поднимается, и там это две
// величины одного бюджета, не связанные ничем.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ — ПО РЕНДЕРУ, А НЕ ПО ЗНАЧЕНИЯМ
//
// Запрос на `own` выводит ШАБЛОН (`kaname.containerResources`), поэтому
// значения профиля о нём ничего не говорят: судится то, что уезжает в
// кластер. Подчарт рендерится с поддеревом `kaname` цепочки стенда, наложенной
// слева направо, как её получает helm. На каждом стенде `own`:
//
//  1. предел памяти объявлен — запросу есть из чего выводиться;
//  2. запрос памяти РАВЕН пределу как количество Kubernetes;
//  3. класс обслуживания пода, вычисленный по ВСЕМ контейнерам (включая
//     инициализирующие), равен решению ownLaneQoSDecision.
//
// Стендов на `own` ноль — отказ, а не зелёное: перепись печатает их число.
// Способность упасть и смолчать — own_lane_memory_request_injection_test.go.
package deploy_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ownLaneQoSDecision — класс обслуживания пода службы личности на посадке
// `own`, РЕШЁННЫЙ, а не доставшийся от подчарта (kacho#2727, п. 3 предиката).
//
// Burstable, и вот почему не Guaranteed. Guaranteed требует, чтобы у КАЖДОГО
// контейнера пода, включая инициализирующий `migrate`, запрос равнялся пределу
// и по памяти, и по процессору. Полосу ограничивает память: её бюджет выводится
// арифметикой стража, и запрос памяти равен пределу. Процессор полосе не
// бюджетирован: запрос 100m при пределе 1000m оставляет узлу долю, которой под
// пользуется только под нагрузкой. Защита от вытеснения, ради которой класс и
// выбирают, по памяти от этого не страдает: kubelet при нехватке памяти узла
// вытесняет в первую очередь поды, чьё потребление ПРЕВЫШАЕТ запрос, а под с
// запросом, равным пределу, превысить его не может — выше предела его
// остановит ядро, а не вытеснение.
const ownLaneQoSDecision = "Burstable"

// laneContainerResources — запросы и пределы одного контейнера из рендера.
type laneContainerResources struct {
	Name     string
	Init     bool
	Requests map[string]string
	Limits   map[string]string
}

// laneRequestFacts — что рендер одного стенда на `own` выставил поду.
type laneRequestFacts struct {
	Stack      string
	Main       laneContainerResources   // контейнер службы
	Containers []laneContainerResources // все контейнеры пода, включая main
}

// laneRequestCensus — объём осмотренного.
type laneRequestCensus struct {
	Stacks   int
	Own      int
	Rendered int
}

func (c laneRequestCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · из них на own %d · отрендерено %d", c.Stacks, c.Own, c.Rendered)
}

// podQoSClass — класс обслуживания пода по правилам Kubernetes: BestEffort — ни
// у одного контейнера нет ни запроса, ни предела процессора и памяти;
// Guaranteed — у каждого контейнера есть предел процессора и памяти, и запрос
// (незаданный запрос Kubernetes берёт равным пределу) равен пределу; иначе
// Burstable.
func podQoSClass(containers []laneContainerResources) (string, error) {
	declared := false
	guaranteed := true
	for _, c := range containers {
		for _, res := range []string{"cpu", "memory"} {
			req, hasReq := c.Requests[res]
			lim, hasLim := c.Limits[res]
			if hasReq || hasLim {
				declared = true
			}
			if !hasLim {
				guaranteed = false
				continue
			}
			if !hasReq {
				continue // запрос по умолчанию равен пределу
			}
			eq, err := sameQuantity(res, req, lim)
			if err != nil {
				return "", fmt.Errorf("контейнер %s: %w", c.Name, err)
			}
			if !eq {
				guaranteed = false
			}
		}
	}
	switch {
	case !declared:
		return "BestEffort", nil
	case guaranteed:
		return "Guaranteed", nil
	default:
		return "Burstable", nil
	}
}

// sameQuantity — равны ли два количества одного ресурса. Память сравнивается
// в байтах, процессор — в милли-ядрах.
func sameQuantity(res, a, b string) (bool, error) {
	parse := parseMemoryQuantity
	if res == "cpu" {
		parse = parseCPUMillis
	}
	x, err := parse(a)
	if err != nil {
		return false, fmt.Errorf("%s %q: %w", res, a, err)
	}
	y, err := parse(b)
	if err != nil {
		return false, fmt.Errorf("%s %q: %w", res, b, err)
	}
	return x == y, nil
}

// parseCPUMillis — количество процессора Kubernetes в милли-ядрах: «100m» или
// целое число ядер. Иной формы разбор не знает и отказывает вслух.
func parseCPUMillis(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if m, ok := strings.CutSuffix(s, "m"); ok {
		return parseUintStrict(m)
	}
	n, err := parseUintStrict(s)
	return n * 1000, err
}

func parseUintStrict(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q не целое: %w", s, err)
	}
	return n, nil
}

// judgeLaneMemoryRequest — НАХОДКИ по стендам на `own`. Чистая функция:
// инъекция подаёт ей синтетический вход.
func judgeLaneMemoryRequest(facts []laneRequestFacts) []string {
	var findings []string
	for _, f := range facts {
		lim, hasLim := f.Main.Limits["memory"]
		if !hasLim {
			findings = append(findings, fmt.Sprintf(
				"стенд %s на посадке own: у контейнера службы нет предела памяти — запросу не из чего "+
					"выводиться, а страж полосы откажет в старте «предел памяти средой не наложен»", f.Stack))
			continue
		}
		req, hasReq := f.Main.Requests["memory"]
		if hasReq {
			eq, err := sameQuantity("memory", req, lim)
			if err != nil {
				findings = append(findings, fmt.Sprintf("стенд %s: ресурсы контейнера службы не читаются: %v", f.Stack, err))
				continue
			}
			if !eq {
				findings = append(findings, fmt.Sprintf(
					"стенд %s на посадке own: запрос памяти %s, а предел %s — две величины одного бюджета "+
						"разошлись: страж полосы доказывает право занять предел, а узел выбран по запросу. "+
						"На own запрос выводится из предела (`kaname.containerResources`, kacho#2727)",
					f.Stack, req, lim))
			}
		}
		// Незаданный запрос Kubernetes берёт равным пределу — это тоже «из
		// одного источника», и находкой не является.
		class, err := podQoSClass(f.Containers)
		if err != nil {
			findings = append(findings, fmt.Sprintf("стенд %s: класс обслуживания не вычисляется: %v", f.Stack, err))
			continue
		}
		if class != ownLaneQoSDecision {
			findings = append(findings, fmt.Sprintf(
				"стенд %s на посадке own: класс обслуживания пода %s, а решение — %s (ownLaneQoSDecision). "+
					"Класс сменился не решением, а правкой ресурсов: пересмотри решение вместе с доводом",
				f.Stack, class, ownLaneQoSDecision))
		}
	}
	return findings
}

// TestOwnLaneMemoryRequest_DerivedFromTheLimitOnOwn — сверка по рендеру дерева.
func TestOwnLaneMemoryRequest_DerivedFromTheLimitOnOwn(t *testing.T) {
	facts, census := renderOwnLaneRequestFacts(t, nil)
	t.Logf("перепись: %s", census)
	if census.Own == 0 {
		t.Fatalf("ни один из %d стендов не стоит на посадке own — судить связь запроса с пределом не "+
			"на чем, и «находок нет» здесь означало бы «рендерить было нечего»", census.Stacks)
	}
	for _, f := range facts {
		class, _ := podQoSClass(f.Containers)
		t.Logf("  %s: запрос памяти %s · предел памяти %s · класс %s · контейнеров %d",
			f.Stack, f.Main.Requests["memory"], f.Main.Limits["memory"], class, len(f.Containers))
	}
	for _, f := range judgeLaneMemoryRequest(facts) {
		t.Error(f)
	}
}

// renderOwnLaneRequestFacts — рендер подчарта kaname для каждого стенда на
// `own`; set — точечные установки поверх цепочки (для инъекции настоящим
// входом).
func renderOwnLaneRequestFacts(t *testing.T, set []string) ([]laneRequestFacts, laneRequestCensus) {
	t.Helper()
	stacksTbl := deployStacks(t)
	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	var census laneRequestCensus
	var out []laneRequestFacts
	for _, name := range names {
		census.Stacks++
		// Умолчания подчарта читаются ЗАНОВО на каждый стенд: `mergeValues`
		// правит карту на месте.
		declared := map[string]any{"kaname": readYAML(t, filepath.Join(kanameSubchart(t), "values.yaml"))}
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		if declaredString(lookup(declared, "kaname", "config", "authn", "identityProvider")) != "own" {
			continue
		}
		census.Own++

		sub, _ := declared["kaname"].(map[string]any)
		if g, ok := declared["global"]; ok {
			sub["global"] = g
		}
		body, err := yaml.Marshal(sub)
		if err != nil {
			t.Fatalf("стенд %s: поддерево kaname не сериализуется: %v", name, err)
		}
		valuesFile := filepath.Join(t.TempDir(), name+".yaml")
		if err := os.WriteFile(valuesFile, body, 0o600); err != nil {
			t.Fatalf("стенд %s: %v", name, err)
		}
		rendered := renderKanameDeployment(t, name, valuesFile, set)
		out = append(out, laneRequestFactsOf(t, name, rendered))
		census.Rendered++
	}
	return out, census
}

// renderKanameDeployment — рендер Deployment подчарта kaname. Отсутствие helm
// в CI — жёсткий провал, а не пропуск (та же дисциплина, что у
// renderIdentitySubchart).
func renderKanameDeployment(t *testing.T, stack, valuesFile string, set []string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	args := []string{"template", "kacho-umbrella", iamSubchartDir, "-n", "kacho",
		"-f", valuesFile, "--show-only", "templates/deployment.yaml"}
	for _, s := range set {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	if err != nil {
		t.Fatalf("стенд %s: подчарт kaname не рендерится с цепочкой стенда: %v\n%s", stack, err, out)
	}
	return string(out)
}

// laneRequestFactsOf — ресурсы контейнеров пода из отрендеренного Deployment.
func laneRequestFactsOf(t *testing.T, stack, rendered string) laneRequestFacts {
	t.Helper()
	var doc struct {
		Kind string `yaml:"kind"`
		Spec struct {
			Template struct {
				Spec struct {
					InitContainers []renderedContainer `yaml:"initContainers"`
					Containers     []renderedContainer `yaml:"containers"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
		t.Fatalf("стенд %s: рендер Deployment не разбирается: %v", stack, err)
	}
	if doc.Kind != "Deployment" || len(doc.Spec.Template.Spec.Containers) == 0 {
		t.Fatalf("стенд %s: в рендере нет Deployment с контейнерами (kind %q, контейнеров %d) — вердикта "+
			"нет: «запрос равен пределу» неотличимо от «контейнера не найдено»",
			stack, doc.Kind, len(doc.Spec.Template.Spec.Containers))
	}
	f := laneRequestFacts{Stack: stack}
	for _, c := range doc.Spec.Template.Spec.InitContainers {
		f.Containers = append(f.Containers, c.resources(true))
	}
	for i, c := range doc.Spec.Template.Spec.Containers {
		r := c.resources(false)
		if i == 0 {
			f.Main = r
		}
		f.Containers = append(f.Containers, r)
	}
	return f
}

type renderedContainer struct {
	Name      string `yaml:"name"`
	Resources struct {
		Requests map[string]string `yaml:"requests"`
		Limits   map[string]string `yaml:"limits"`
	} `yaml:"resources"`
}

func (c renderedContainer) resources(init bool) laneContainerResources {
	return laneContainerResources{Name: c.Name, Init: init, Requests: c.Resources.Requests, Limits: c.Resources.Limits}
}
