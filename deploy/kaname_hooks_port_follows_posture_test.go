// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_hooks_port_follows_posture_test.go — ПОДЧАРТ СЛУЖБЫ ДОСТУПА В ЗОНТЕ НЕ
// ОБЪЯВЛЯЕТ ПОРТА ХУКОВ НИ НА ОДНОМ СТЕНДЕ, А ПРОБЫ ВЕДЁТ НА ДИАГНОСТИКУ
// (kacho#2871, служба — PRO-Robotech/kaname#360; kacho#2818).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// `/healthz` и `/readyz` службы живут на диагностическом слушателе — том же,
// что отдаёт `/metrics` (kaname#360). Копия чарта службы в зонте прежде вела обе
// пробы на порт `http-hooks` и объявляла этот порт у контейнера и у внутреннего
// Service; под посадкой, при которой процесс слушателя не строит, под не
// становился готовым никогда, а проба живости перезапускала контейнер по кругу.
// Рендер при этом исправен — ловит это только суд над отрендеренным.
//
// Прежде порт судился по посадке: слушатель вебхуков процесс поднимал на всякой
// посадке, кроме `own`. С kaname#363 слушателя у службы нет ВОВСЕ, посадка одна,
// и ключа посадки у подчарта нет (kacho#2818): порт хуков запрещён безусловно.
// Что подчарт не передаёт и остальной полосы хуков, которую пин не читает, —
// предмет kaname_subchart_retired_identity_wiring_test.go.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ
//
//   - порта `http-hooks` нет ни у контейнера, ни у Service `<релиз>-internal`;
//   - порт диагностики `metrics` у контейнера объявлен, и его номер — тот же,
//     что у объявления сбора (`prometheus.io/port`): одна поверхность, один
//     адрес;
//   - обе пробы (`readinessProbe`, `livenessProbe`) идут на порт `metrics`, а их
//     схема совпадает со схемой объявления сбора (`prometheus.io/scheme`):
//     транспорт этой поверхности задаёт одна ручка.
//
// Способность упасть и смолчать доказана инъекцией настоящим входом —
// копией шаблона из дерева с одной возвращённой строкой:
// kaname_hooks_port_follows_posture_injection_test.go.
//
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ. Что процесс ДЕЙСТВИТЕЛЬНО слушает `/readyz` на
// этом порту: это свойство образа, а не чарта, и держит его проба службы
// (`deploy/hooks_port_follows_posture_test.go` в kaname). Здесь — что чарт
// зонта спрашивает там, где служба отвечает по её же объявлению.
package deploy_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	// kanameHooksPortName — имя порта слушателя вебхуков поставщика у пода и у
	// внутреннего Service подчарта.
	kanameHooksPortName = "http-hooks"
	// kanameDiagPortName — имя порта диагностической поверхности у пода.
	kanameDiagPortName = "metrics"
)

// kanameHooksLaneRaised — поднимает ли процесс слушатель вебхуков. С kaname#363
// — нет, ни при какой посадке: слушателя у службы нет (kacho#2818).
const kanameHooksLaneRaised = false

// kanameProbe — одна проба контейнера в той мере, в какой её судит проверка.
type kanameProbe struct {
	Port   string
	Scheme string
}

// kanameHooksFacts — что рендер одного стенда говорит о портах и пробах службы.
type kanameHooksFacts struct {
	Stack   string
	Posture string
	Raised  bool
	// ContainerPorts — имя порта основного контейнера → номер.
	ContainerPorts map[string]int
	// ServicePorts — имена портов внутреннего Service.
	ServicePorts map[string]bool
	// Probes — имя слота пробы → проба. Отсутствующий слот — отсутствующий ключ.
	Probes map[string]kanameProbe
	// ScrapePort / ScrapeScheme — объявление сбора пода; пусто — сбор не объявлен.
	ScrapePort   string
	ScrapeScheme string
}

// kanameHooksCensus — объём осмотренного по осям.
type kanameHooksCensus struct {
	Stacks          int
	Rendered        int
	Own             int
	HooksAtPod      int
	HooksAtService  int
	ProbesOnDiag    int
	ProbesTotal     int
	SchemesMatching int
}

func (c kanameHooksCensus) String() string {
	return fmt.Sprintf("стендов %d · отрендерено %d · под own %d · порт %s у пода %d · у Service %d · "+
		"проб на %s %d из %d · схема пробы совпала со схемой сбора %d",
		c.Stacks, c.Rendered, c.Own, kanameHooksPortName, c.HooksAtPod, c.HooksAtService,
		kanameDiagPortName, c.ProbesOnDiag, c.ProbesTotal, c.SchemesMatching)
}

// kanameProbeSlots — слоты проб, которые обязаны идти на диагностику.
var kanameProbeSlots = []string{"readinessProbe", "livenessProbe"}

// judgeKanameHooksPort — находки одного стенда. Пустой срез — стенд исправен.
func judgeKanameHooksPort(f kanameHooksFacts) []string {
	var out []string
	_, hooksAtPod := f.ContainerPorts[kanameHooksPortName]
	hooksAtSvc := f.ServicePorts[kanameHooksPortName]
	switch {
	case f.Raised && !hooksAtPod:
		out = append(out, fmt.Sprintf("стенд %s (посадка %q): процесс слушатель вебхуков поднимает, "+
			"а порта %s у контейнера нет", f.Stack, f.Posture, kanameHooksPortName))
	case !f.Raised && hooksAtPod:
		out = append(out, fmt.Sprintf("стенд %s (посадка %q): процесс слушатель вебхуков НЕ поднимает, "+
			"а контейнер объявляет порт %s — дверь, за которой никого нет", f.Stack, f.Posture, kanameHooksPortName))
	}
	switch {
	case f.Raised && !hooksAtSvc:
		out = append(out, fmt.Sprintf("стенд %s (посадка %q): процесс слушатель вебхуков поднимает, "+
			"а у внутреннего Service порта %s нет", f.Stack, f.Posture, kanameHooksPortName))
	case !f.Raised && hooksAtSvc:
		out = append(out, fmt.Sprintf("стенд %s (посадка %q): процесс слушатель вебхуков НЕ поднимает, "+
			"а внутренний Service маршрутизирует порт %s", f.Stack, f.Posture, kanameHooksPortName))
	}

	diag, hasDiag := f.ContainerPorts[kanameDiagPortName]
	switch {
	case !hasDiag:
		out = append(out, fmt.Sprintf("стенд %s: порт диагностики %s у контейнера не объявлен — пробам "+
			"некуда идти по имени", f.Stack, kanameDiagPortName))
	case f.ScrapePort != "" && strconv.Itoa(diag) != f.ScrapePort:
		out = append(out, fmt.Sprintf("стенд %s: порт %s у контейнера %d, а объявление сбора называет %s — "+
			"у одной поверхности два адреса", f.Stack, kanameDiagPortName, diag, f.ScrapePort))
	}

	for _, slot := range kanameProbeSlots {
		p, ok := f.Probes[slot]
		if !ok {
			out = append(out, fmt.Sprintf("стенд %s: пробы %s у контейнера нет", f.Stack, slot))
			continue
		}
		if p.Port != kanameDiagPortName {
			out = append(out, fmt.Sprintf("стенд %s (посадка %q): %s идёт на порт %q, а не на %s — "+
				"служба отвечает о готовности и живости на диагностике", f.Stack, f.Posture, slot, p.Port,
				kanameDiagPortName))
		}
		if f.ScrapeScheme != "" && !strings.EqualFold(kanameProbeScheme(p.Scheme), f.ScrapeScheme) {
			out = append(out, fmt.Sprintf("стенд %s: схема %s %s, а объявление сбора той же поверхности — %s",
				f.Stack, slot, kanameProbeScheme(p.Scheme), f.ScrapeScheme))
		}
	}
	return out
}

// kanameProbeScheme — схема пробы; незаданная у httpGet — HTTP (умолчание kubelet).
func kanameProbeScheme(s string) string {
	if s == "" {
		return "HTTP"
	}
	return s
}

// kanameStackValues — поддерево значений подчарта kaname для цепочки стенда:
// умолчания подчарта ← база зонта ← профили цепочки, так же, как их накладывает
// helm. Глобальные значения сливаются, а не замещаются.
func kanameStackValues(t *testing.T, chain []string) map[string]any {
	t.Helper()
	declared := map[string]any{"kaname": readYAML(t, filepath.Join(iamSubchartDir, "values.yaml"))}
	declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, "values.yaml")))
	for _, p := range chain {
		declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
	}
	sub, _ := declared["kaname"].(map[string]any)
	if sub == nil {
		t.Fatalf("цепочка %v не даёт поддерева kaname — судить нечего", chain)
	}
	if g, ok := declared["global"].(map[string]any); ok {
		own, _ := sub["global"].(map[string]any)
		sub["global"] = mergeValues(own, g)
	}
	return sub
}

// renderKanameHooksSurface — рендер Deployment и внутреннего Service подчарта из
// каталога chartDir с поданными значениями. Отсутствие helm при CI — провал, а
// не пропуск: гейт, молча ставший инертным на задании, гейтом не является.
func renderKanameHooksSurface(t *testing.T, chartDir, stack string, values map[string]any, sets ...string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	body, err := yaml.Marshal(values)
	if err != nil {
		t.Fatalf("стенд %s: значения не сериализуются: %v", stack, err)
	}
	file := filepath.Join(t.TempDir(), stack+".yaml")
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatalf("стенд %s: %v", stack, err)
	}
	// Одиночный рендер подчарта — только обёрткой (renderKanameAlone, CX1-113).
	args := []string{"-n", "kacho", "-f", file,
		"--show-only", "templates/deployment.yaml", "--show-only", "templates/service-internal.yaml"}
	for _, s := range sets {
		args = append(args, "--set-string", s)
	}
	out, err := renderKanameAlone(t, "kacho-umbrella", chartDir, args...)
	if err != nil {
		t.Fatalf("стенд %s: подчарт kaname не рендерится: %v\n%s", stack, err, out)
	}
	return string(out)
}

// kanameRenderedDoc — документ рендера в той мере, в какой его читает проверка.
// Типизированный разбор, а не обход карт: имя поля, которого в документе нет,
// даёт пустое значение ЗДЕСЬ, а не молчаливый промах приведения типа.
type kanameRenderedDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		// Service
		Ports []struct {
			Name string `yaml:"name"`
		} `yaml:"ports"`
		// Deployment
		Template struct {
			Metadata struct {
				Annotations map[string]string `yaml:"annotations"`
			} `yaml:"metadata"`
			Spec struct {
				Containers []struct {
					Ports []struct {
						Name          string `yaml:"name"`
						ContainerPort int    `yaml:"containerPort"`
					} `yaml:"ports"`
					ReadinessProbe *kanameRenderedProbe `yaml:"readinessProbe"`
					LivenessProbe  *kanameRenderedProbe `yaml:"livenessProbe"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type kanameRenderedProbe struct {
	HTTPGet *struct {
		Port   any    `yaml:"port"`
		Scheme string `yaml:"scheme"`
	} `yaml:"httpGet"`
}

// kanameHooksFactsOf — факты из рендера. Deployment без контейнеров и рендер
// без внутреннего Service — ОТКАЗ: «портов хуков нет» было бы неотличимо от
// «ничего не прочитано».
func kanameHooksFactsOf(t *testing.T, stack, posture, rendered string) kanameHooksFacts {
	t.Helper()
	f := kanameHooksFacts{
		Stack: stack, Posture: posture, Raised: kanameHooksLaneRaised,
		ContainerPorts: map[string]int{}, ServicePorts: map[string]bool{}, Probes: map[string]kanameProbe{},
	}
	var sawDeployment, sawService bool
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var d kanameRenderedDoc
		if err := dec.Decode(&d); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("стенд %s: рендер не разбирается: %v", stack, err)
		}
		switch d.Kind {
		case "Deployment":
			sawDeployment = true
			ann := d.Spec.Template.Metadata.Annotations
			f.ScrapePort = ann["prometheus.io/port"]
			f.ScrapeScheme = ann["prometheus.io/scheme"]
			containers := d.Spec.Template.Spec.Containers
			if len(containers) == 0 {
				t.Fatalf("стенд %s: Deployment без контейнеров — вердикта нет", stack)
			}
			main := containers[0]
			for _, p := range main.Ports {
				f.ContainerPorts[p.Name] = p.ContainerPort
			}
			for slot, probe := range map[string]*kanameRenderedProbe{
				"readinessProbe": main.ReadinessProbe, "livenessProbe": main.LivenessProbe,
			} {
				if probe == nil || probe.HTTPGet == nil {
					continue
				}
				f.Probes[slot] = kanameProbe{Port: fmt.Sprint(probe.HTTPGet.Port), Scheme: probe.HTTPGet.Scheme}
			}
		case "Service":
			if !strings.HasSuffix(d.Metadata.Name, "-internal") {
				continue
			}
			sawService = true
			for _, p := range d.Spec.Ports {
				f.ServicePorts[p.Name] = true
			}
		}
	}
	if !sawDeployment || !sawService {
		t.Fatalf("стенд %s: в рендере Deployment %t, внутренний Service %t — вердикта нет", stack,
			sawDeployment, sawService)
	}
	return f
}

// auditKanameHooksPort — суд над каждым стендом `stacks.txt`, отрендеренным из
// chartDir.
func auditKanameHooksPort(t *testing.T, chartDir string) ([]string, kanameHooksCensus) {
	t.Helper()
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var census kanameHooksCensus
	var findings []string
	for _, name := range names {
		census.Stacks++
		values := kanameStackValues(t, stacks[name])
		f := kanameHooksFactsOf(t, name, kanameLanding, renderKanameHooksSurface(t, chartDir, name, values))
		census.Rendered++
		if !f.Raised {
			census.Own++
		}
		if _, ok := f.ContainerPorts[kanameHooksPortName]; ok {
			census.HooksAtPod++
		}
		if f.ServicePorts[kanameHooksPortName] {
			census.HooksAtService++
		}
		for _, slot := range kanameProbeSlots {
			census.ProbesTotal++
			p, ok := f.Probes[slot]
			if ok && p.Port == kanameDiagPortName {
				census.ProbesOnDiag++
			}
			if ok && f.ScrapeScheme != "" && strings.EqualFold(kanameProbeScheme(p.Scheme), f.ScrapeScheme) {
				census.SchemesMatching++
			}
		}
		findings = append(findings, judgeKanameHooksPort(f)...)
	}
	if census.Rendered == 0 || census.Rendered != census.Stacks {
		t.Fatalf("отрендерено %d стендов из %d — «находок нет» означало бы «не прочитано»",
			census.Rendered, census.Stacks)
	}
	return findings, census
}

// TestKanameHooksPortFollowsThePosture — ни на одном стенде порта хуков нет, а
// пробы идут на диагностику.
func TestKanameHooksPortFollowsThePosture(t *testing.T) {
	findings, census := auditKanameHooksPort(t, iamSubchartDir)
	t.Logf("перепись: %s", census)
	for _, f := range findings {
		t.Error(f)
	}
}
