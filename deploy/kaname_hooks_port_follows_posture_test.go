// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_hooks_port_follows_posture_test.go — ПОДЧАРТ СЛУЖБЫ ДОСТУПА В ЗОНТЕ
// ОБЪЯВЛЯЕТ ПОРТ ХУКОВ ТОЛЬКО ТАМ, ГДЕ ПРОЦЕСС ЕГО ПОДНИМАЕТ, А ПРОБЫ ВЕДЁТ НА
// ДИАГНОСТИКУ (kacho#2871, служба — PRO-Robotech/kaname#360).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба с ревизии, несущей вливание kaname#360, под посадкой `own` слушатель
// вебхуков поставщика не строит (`hooksListenAddress` в её композиционном
// корне отдаёт пусто), а `/healthz` и `/readyz` живут на диагностическом
// слушателе — том же, что отдаёт `/metrics`. Копия чарта службы в зонте вела
// обе пробы на порт `http-hooks` при любой посадке и объявляла этот порт у
// контейнера и у внутреннего Service без условия. Посадку `own` объявляют все
// стенды `stacks.txt`, поэтому после подъёма образа службы до такой ревизии под
// службы доступа ни на одном стенде готовым не стал бы: kubelet стучится в
// порт, который никто не слушает, а проба живости перезапускает контейнер по
// кругу. Рендер при этом исправен — ловит это только суд над отрендеренным.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОЖИДАНИЕ ВЫВОДИТСЯ ИЗ ПРЕДИКАТА ПРОЦЕССА, А НЕ ВЫПИСЫВАЕТСЯ
//
// Процесс снимает слушатель ровно ОДНИМ объявленным значением — `own`
// (`AuthNConfig.HasExternalIdentityProvider` службы: «не own», а не «==
// external»). Сам предикат лежит во внутреннем пакете службы и из этого модуля
// не импортируется; имя значения берётся у его источника — словаря посадок
// фундамента `identityposture` (`identityposture.Own`), тем же, которым
// процесс его разбирает. Второго написания имени здесь не заводится.
//
// Законный близнец — посадка, при которой процесс слушатель ПОДНИМАЕТ. Значение
// `external` снято со словаря фундамента (corelib#30: разбор его отвергает),
// поэтому из объявимых значений «не own» остаётся одно — поле не объявлено.
// Близнец рендерит ту же цепочку стенда с пустой посадкой и требует обратного:
// порт хуков у контейнера и у Service ЕСТЬ, пробы по-прежнему на диагностике.
// Меняется ровно один факт — посадка.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ
//
//   - порт `http-hooks` у контейнера и у Service `<релиз>-internal` — есть ровно
//     тогда, когда процесс поднимает слушатель;
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

	"github.com/PRO-Robotech/corelib/identityposture"
	"gopkg.in/yaml.v3"
)

const (
	// kanameHooksPortName — имя порта слушателя вебхуков поставщика у пода и у
	// внутреннего Service подчарта.
	kanameHooksPortName = "http-hooks"
	// kanameDiagPortName — имя порта диагностической поверхности у пода.
	kanameDiagPortName = "metrics"
)

// kanameHooksLaneRaised — поднимает ли процесс слушатель вебхуков при этой
// объявленной посадке. Зеркало предиката процесса: «не own».
func kanameHooksLaneRaised(posture string) bool {
	return posture != identityposture.Own.String()
}

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
	args := []string{"template", "kacho-umbrella", chartDir, "-n", "kacho", "-f", file,
		"--show-only", "templates/deployment.yaml", "--show-only", "templates/service-internal.yaml"}
	for _, s := range sets {
		args = append(args, "--set-string", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
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
		Stack: stack, Posture: posture, Raised: kanameHooksLaneRaised(posture),
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
// chartDir. postureOverride == nil — посадка стенда как объявлена; иначе —
// рендер с этой посадкой поверх цепочки (близнец).
func auditKanameHooksPort(t *testing.T, chartDir string, postureOverride *string) ([]string, kanameHooksCensus) {
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
		posture := declaredString(lookup(values, "config", "authn", "identityProvider"))
		var sets []string
		if postureOverride != nil {
			posture = *postureOverride
			sets = append(sets, "config.authn.identityProvider="+posture)
		}
		f := kanameHooksFactsOf(t, name, posture, renderKanameHooksSurface(t, chartDir, name, values, sets...))
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

// TestKanameHooksPortFollowsThePosture — на каждом стенде порт хуков объявлен
// ровно там, где процесс его поднимает, а пробы идут на диагностику.
func TestKanameHooksPortFollowsThePosture(t *testing.T) {
	findings, census := auditKanameHooksPort(t, iamSubchartDir, nil)
	t.Logf("перепись: %s", census)
	if census.Own == 0 {
		t.Fatalf("ни один стенд не объявляет посадку %s — предпосылка задачи (все стенды на own) "+
			"изменилась, пересмотрите проверку, а не читайте ноль как исправность",
			identityposture.Own)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestKanameHooksPortTwinKeepsTheHooksWhereTheProcessRaisesThem — законный
// близнец: та же цепочка каждого стенда с НЕОБЪЯВЛЕННОЙ посадкой — процесс
// слушатель поднимает, и порт хуков у контейнера и у Service обязан быть.
func TestKanameHooksPortTwinKeepsTheHooksWhereTheProcessRaisesThem(t *testing.T) {
	undeclared := ""
	findings, census := auditKanameHooksPort(t, iamSubchartDir, &undeclared)
	t.Logf("перепись близнеца (посадка не объявлена): %s", census)
	if census.Own != 0 {
		t.Fatalf("близнец рендерит посадку %s на %d стендах — подмена посадки не доехала до рендера",
			identityposture.Own, census.Own)
	}
	for _, f := range findings {
		t.Error(f)
	}
}
