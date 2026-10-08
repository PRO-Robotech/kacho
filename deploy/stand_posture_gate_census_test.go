// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// stand_posture_gate_census_test.go — перечень служб гейта посадки сходится с
// тем, что стенд РАЗВОРАЧИВАЕТ.
//
// ПРЕДМЕТ. Перечень SERVICES в scripts/assert-production-posture.sh жёсткий
// намеренно: служба, которой нет в кластере, обязана краснеть, а не молчать. Но
// жёсткость работает только в одну сторону — о развёртывании, которого в
// перечне НЕТ, гейт не знает ничего. Так три процесса notify и стендовая проба
// поднимались на стенде, а их посадку и шифрование их баз не спрашивал никто:
// гейт печатал «production posture ПОДТВЕРЖДЕНА» о стенде, где четыре
// развёртывания продукта им не осматривались.
//
// ЧТО СУДИТСЯ на рендере терминальной цепочки подъёма стенда (dev-prod):
//
//	(а) у каждого развёртывания службы продукта (образ — один из образов служб,
//	    собираемых подъёмом, либо служба, чьи исходники живут вне дерева) есть
//	    строка перечня с его именем;
//	(б) у каждой базы стенда (StatefulSet чарта pg-*) есть строка, называющая её
//	    столбцом базы;
//	(в) строка перечня не называет развёртывания или базы, которых в рендере нет.
//
// ЧЕГО НЕ ДЕЛАЕТ: не судит, что процессы ИСПОЛНЯЮТСЯ в боевой посадке, — это
// делает сам гейт на поднятом стенде; не судит программу вердикта — её держит
// tests/helm/posture-listener-form-test.sh.

import (
	"bufio"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

const (
	postureGateScript     = "scripts/assert-production-posture.sh"
	postureCensusChain    = "dev-prod"
	standServicesMakeLine = `^SERVICES\s*:=\s*(.+)$`
)

// postureRow — строка перечня гейта: развёртывание|служба|база.
type postureRow struct{ dep, svc, pg string }

// postureRowsOf — строки перечня SERVICES из текста гейта. Пустой перечень —
// отказ: «нарушений 0» о непрочитанном перечне было бы ложью.
func postureRowsOf(t *testing.T, script string) []postureRow {
	t.Helper()
	var rows []postureRow
	in := false
	sc := bufio.NewScanner(strings.NewReader(script))
	for sc.Scan() {
		l := sc.Text()
		switch {
		case !in && l == `SERVICES="`:
			in = true
		case in && l == `"`:
			in = false
		case in && strings.TrimSpace(l) != "":
			f := strings.Split(strings.TrimSpace(l), "|")
			if len(f) != 3 {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: строка перечня гейта %q не из трёх полей", l)
			}
			rows = append(rows, postureRow{f[0], f[1], f[2]})
		}
	}
	if len(rows) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: перечень SERVICES в %s не прочитан — гейт изменил форму", postureGateScript)
	}
	return rows
}

// backendImages — имена образов служб продукта: собираемых подъёмом (перечень
// SERVICES рецепта стенда) и служб, чьи исходники живут вне дерева. Имя — у
// единственного источника имён, а не выписано.
func backendImages(t *testing.T) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(standMakefile)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не прочитан: %v", standMakefile, err)
	}
	m := regexp.MustCompile(`(?m)` + standServicesMakeLine).FindStringSubmatch(string(body))
	if m == nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s нет строки SERVICES := … — перечень образов подъёма не прочитан", standMakefile)
	}
	out := map[string]bool{}
	for _, dir := range strings.Fields(m[1]) {
		out[productnaming.ChartName(dir)] = true
	}
	for dir := range productnaming.ExternallySourcedServices() {
		out[productnaming.ChartName(dir)] = true
	}
	return out
}

// postureCensusView — что рендер цепочки разворачивает.
type postureCensusView struct {
	deployments map[string]string // имя развёртывания службы продукта → образ
	databases   map[string]bool   // имя StatefulSet базы
	allDeploys  map[string]bool
}

func postureCensusViewOf(t *testing.T, rendered string, images map[string]bool) postureCensusView {
	t.Helper()
	v := postureCensusView{deployments: map[string]string{}, databases: map[string]bool{}, allDeploys: map[string]bool{}}
	all := parseRendered(t, rendered)
	for _, d := range objsOfKind(all, "Deployment") {
		v.allDeploys[d.name] = true
		for _, c := range nlist(nPodSpec(d)["containers"]) {
			cm, _ := c.(map[string]any)
			repo := nstr(cm["image"])
			if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
				repo = repo[:i]
			}
			if i := strings.Index(repo, "@"); i >= 0 {
				repo = repo[:i]
			}
			if images[repo[strings.LastIndex(repo, "/")+1:]] {
				v.deployments[d.name] = repo
			}
		}
	}
	for _, s := range objsOfKind(all, "StatefulSet") {
		if strings.HasPrefix(nstr(ndig(s.doc, "metadata", "labels", "app.kubernetes.io/name")), "pg-") {
			v.databases[s.name] = true
		}
	}
	return v
}

// judgePostureCensus — решение: находки «предмет — почему».
func judgePostureCensus(rows []postureRow, v postureCensusView) []string {
	var out []string
	byDep := map[string]bool{}
	byPG := map[string]bool{}
	for _, r := range rows {
		byDep[r.dep] = true
		if r.pg != "" {
			byPG[r.pg] = true
		}
		if !v.allDeploys[r.dep] {
			out = append(out, "(в) строка "+r.dep+"|"+r.svc+" называет развёртывание, которого в рендере нет")
		}
		if r.pg != "" && !v.databases[r.pg] {
			out = append(out, "(в) строка "+r.dep+"|"+r.svc+" называет базу "+r.pg+", которой в рендере нет")
		}
	}
	for _, d := range sortedStringKeys(v.deployments) {
		if !byDep[d] {
			out = append(out, "(а) развёртывание службы продукта "+d+" ("+v.deployments[d]+") не в перечне гейта — его посадку не спрашивает никто")
		}
	}
	for _, pg := range sortedBoolKeys(v.databases) {
		if !byPG[pg] {
			out = append(out, "(б) база "+pg+" не названа ни одной строкой — шифрование её соединений не подтверждает никто")
		}
	}
	return out
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// postureCensusInputs — рендер терминальной цепочки подъёма и текст гейта.
func postureCensusInputs(t *testing.T) (string, postureCensusView) {
	t.Helper()
	script, err := os.ReadFile(postureGateScript)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не прочитан: %v", postureGateScript, err)
	}
	chain, ok := deployStacksForRender(t, renderGateOperatorSample)[postureCensusChain]
	if !ok {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочки %s в таблице стеков нет", postureCensusChain)
	}
	rendered, rerr := renderStandProfile(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{}), chain)
	if rerr != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: рендер цепочки %s отказал: %v\n%s", postureCensusChain, rerr, lastLines(rendered, 5))
	}
	v := postureCensusViewOf(t, rendered, backendImages(t))
	if len(v.deployments) == 0 || len(v.databases) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: развёртываний служб продукта %d, баз %d — перепись слепа",
			len(v.deployments), len(v.databases))
	}
	return string(script), v
}

// TestPostureGateNamesEveryDeployedServiceAndDatabase — держатель на дереве как оно есть.
func TestPostureGateNamesEveryDeployedServiceAndDatabase(t *testing.T) {
	script, v := postureCensusInputs(t)
	rows := postureRowsOf(t, script)
	f := judgePostureCensus(rows, v)
	t.Logf("ПЕРЕПИСЬ гейта посадки на цепочке %s: строк перечня %d · развёртываний служб продукта %d · баз %d · находок %d",
		postureCensusChain, len(rows), len(v.deployments), len(v.databases), len(f))
	for _, x := range f {
		t.Errorf("КРАСНЫЙ: %s", x)
	}
}

// TestPostureGateCensusInjections — держатель краснеет на настоящем входе
// дерева, и находка называет ровно внесённый дефект.
func TestPostureGateCensusInjections(t *testing.T) {
	script, v := postureCensusInputs(t)
	if f := judgePostureCensus(postureRowsOf(t, script), v); len(f) != 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец красный — инъекции недействительны: %v", f)
	}
	cases := []struct {
		name, from, to, want string
	}{
		{"снята строка пробы", "kacho-notify-probe|notify-probe|kacho-umbrella-pg-notifyprobe\n", "",
			"(а) развёртывание службы продукта kacho-notify-probe"},
		{"снята строка пробы — и её база без строки", "kacho-notify-probe|notify-probe|kacho-umbrella-pg-notifyprobe\n", "",
			"(б) база kacho-umbrella-pg-notifyprobe"},
		{"строка называет несуществующее развёртывание", "kacho-notify|notify|", "kacho-notifier|notify|",
			"(в) строка kacho-notifier|notify"},
		{"строка называет несуществующую базу", "kacho-notify-api|notify-api|kacho-umbrella-pg-notify\n",
			"kacho-notify-api|notify-api|kacho-umbrella-pg-notify-api\n", "(в) строка kacho-notify-api|notify-api называет базу"},
	}
	for _, c := range cases {
		if strings.Count(script, c.from) != 1 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: инъекция «%s» — образец %q встречается в гейте не ровно раз", c.name, c.from)
		}
		f := judgePostureCensus(postureRowsOf(t, strings.Replace(script, c.from, c.to, 1)), v)
		hit := false
		for _, x := range f {
			if strings.HasPrefix(x, c.want) {
				hit = true
				t.Logf("инъекция «%s» → %s", c.name, x)
			}
		}
		if !hit {
			t.Errorf("инъекция «%s»: держатель промолчал о %q (находки: %v)", c.name, c.want, f)
		}
	}
}
