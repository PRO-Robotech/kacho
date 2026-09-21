// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subchart_ignore_rule_has_a_subject_test.go — ПРАВИЛО ИГНОРИРОВАНИЯ ПОДЧАРТА
// ОБЯЗАНО ИМЕТЬ ПРЕДМЕТ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// `deploy/.gitignore` выводит из-под учёта каталоги, которые helm материализует
// в `helm/umbrella/charts/<имя>/`. Каждая такая строка есть ПОСЛАБЛЕНИЕ: она
// говорит «этот путь появится сам, коммитить его не нужно». Послабление обязано
// истекать вместе со своим предметом — чарт снят, правило осталось, и следующий
// читатель наследует его как действующее, а заодно перестаёт различать «правило
// работает» и «правилу нечего покрывать».
//
// Так и вышло: `zitadel` заменён на Ory (KAC-127), `resource-manager` упразднён
// (KAC-124), чарт консоли переименован `ui` → `uif`. Три строки пережили свои
// чарты и молчали — ни один гейт дерева их не судил: соседний
// `deploy/scripts/assert-vendored-external-charts.py` смотрит на записи
// `!charts/<имя>-<версия>.tgz` (вендоренные АРХИВЫ), а правила КАТАЛОГОВ не
// читает вовсе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ ПРЕДМЕТОМ — ТРИ ЗАКОННЫХ ИСТОЧНИКА ИМЕНИ, А НЕ ОДИН
//
// Имя правила обязано найтись хотя бы в одном из трёх мест, и все три нужны:
//
//	имя зависимости `Chart.yaml`   — `- name: ingress-nginx`;
//	псевдоним зависимости          — `alias: pg-iam` (девять баз);
//	каталог чарта в `charts/`      — `kaname`, `kacho-geo`,
//	                                 `kratos-selfservice-ui`: три чарта лежат
//	                                 исходниками и НАМЕРЕННО не объявлены
//	                                 зависимостями (довод — в шапке Chart.yaml).
//
// Предикат по одному источнику дал бы находки на верной работе: перечень
// зависимостей не знает о трёх каталогах-исходниках, а перечень каталогов не
// знает о тех, что материализуются архивом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЕРЕПИСЬ ОТДЕЛЬНО ОТ НАХОДОК
//
// «Находок ноль» и «прочитано ноль» — разные исходы. Гейт печатает объём
// осмотренного (строк файла · правил · объявленных имён) и ОТКАЗЫВАЕТ на пустом
// обходе: `.gitignore` без единого правила подчарта означает, что предикат
// перестал видеть свой предмет, а не что дерево чисто.
//
// Обе стороны доказаны инъекцией на СИНТЕТИКЕ во временном каталоге
// (`subchart_ignore_rule_has_a_subject_injection_test.go`): фикстура, привязанная
// к живой строке, истекла бы вместе с ней.
package deploy_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// subchartIgnoreRule — одна строка правила игнорирования каталога подчарта.
type subchartIgnoreRule struct {
	Line int
	Name string
	Text string
}

// subchartIgnoreCensus — объём осмотренного.
type subchartIgnoreCensus struct {
	LinesRead int
	Rules     int
	Declared  int
	Dirs      int
}

func (c subchartIgnoreCensus) String() string {
	return fmt.Sprintf("строк прочитано %d · правил игнорирования подчарта %d · "+
		"имён объявлено зонтом %d (из них каталогов-исходников %d)",
		c.LinesRead, c.Rules, c.Declared, c.Dirs)
}

// subchartIgnoreFinding — правило, у которого предмета нет.
type subchartIgnoreFinding struct {
	Line int
	Name string
	Text string
}

func (f subchartIgnoreFinding) String() string {
	return fmt.Sprintf("deploy/.gitignore:%d: `%s` — чарта `%s` зонт не объявляет "+
		"ни зависимостью, ни псевдонимом, ни каталогом в charts/: правило "+
		"пережило свой предмет. Снимите строку тем же изменением, которым снят "+
		"чарт; послабление без предмета читается следующим как действующее",
		f.Line, f.Text, f.Name)
}

// reSubchartIgnore — правило вида `helm/umbrella/charts/<имя>/`.
//
// Разбирается ИМЕННО правило каталога: записи `!helm/umbrella/charts/<имя>-*.tgz`
// судит другой владелец (assert-vendored-external-charts.py), и читать их здесь
// значило бы завести второе место об одном предмете.
var reSubchartIgnore = regexp.MustCompile(`^(helm/umbrella/charts/([A-Za-z0-9._-]+)/)\s*$`)

// readSubchartIgnoreRules — правила каталогов подчартов из файла игнорирования.
func readSubchartIgnoreRules(path string) ([]subchartIgnoreRule, int, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer fh.Close()

	var rules []subchartIgnoreRule
	lines := 0
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		lines++
		text := strings.TrimRight(sc.Text(), " \t")
		if strings.HasPrefix(strings.TrimLeft(text, " \t"), "#") {
			continue
		}
		m := reSubchartIgnore.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		rules = append(rules, subchartIgnoreRule{Line: lines, Name: m[2], Text: m[1]})
	}
	if err := sc.Err(); err != nil {
		return nil, lines, err
	}
	return rules, lines, nil
}

// reChartDep — `- name: <имя>` в перечне зависимостей; reChartAlias — `alias: <имя>`.
var (
	reChartDep   = regexp.MustCompile(`^\s+-\s+name:\s+"?([A-Za-z0-9._-]+)"?\s*$`)
	reChartAlias = regexp.MustCompile(`^\s+alias:\s+"?([A-Za-z0-9._-]+)"?\s*$`)
)

// umbrellaDeclaredNames — имена, которые зонт объявляет: зависимости, псевдонимы
// и каталоги-исходники в `charts/`. Второе возвращаемое — сколько из них дали
// каталоги.
func umbrellaDeclaredNames(umbrella string) (map[string]bool, int, error) {
	out := map[string]bool{}

	fh, err := os.Open(filepath.Join(umbrella, "Chart.yaml"))
	if err != nil {
		return nil, 0, err
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		text := sc.Text()
		if strings.HasPrefix(strings.TrimLeft(text, " \t"), "#") {
			continue
		}
		if m := reChartDep.FindStringSubmatch(text); m != nil {
			out[m[1]] = true
		}
		if m := reChartAlias.FindStringSubmatch(text); m != nil {
			out[m[1]] = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}

	dirs := 0
	entries, err := os.ReadDir(filepath.Join(umbrella, "charts"))
	if err != nil && !os.IsNotExist(err) {
		return nil, 0, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !out[e.Name()] {
			dirs++
		}
		out[e.Name()] = true
	}
	return out, dirs, nil
}

// judgeSubchartIgnoreRules — правило без предмета есть находка.
func judgeSubchartIgnoreRules(rules []subchartIgnoreRule, declared map[string]bool,
	lines, dirs int) ([]subchartIgnoreFinding, subchartIgnoreCensus) {
	census := subchartIgnoreCensus{
		LinesRead: lines,
		Rules:     len(rules),
		Declared:  len(declared),
		Dirs:      dirs,
	}
	var findings []subchartIgnoreFinding
	for _, r := range rules {
		if declared[r.Name] {
			continue
		}
		findings = append(findings, subchartIgnoreFinding{Line: r.Line, Name: r.Name, Text: r.Text})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Line < findings[j].Line })
	return findings, census
}

// TestSubchartIgnoreRuleHasASubject — каждое правило игнорирования каталога
// подчарта названо зонтом.
//
// Прогон одной командой:
//
//	go test ./deploy/ -run TestSubchartIgnoreRuleHasASubject -count=1 -v
func TestSubchartIgnoreRuleHasASubject(t *testing.T) {
	t.Parallel()

	rules, lines, err := readSubchartIgnoreRules(".gitignore")
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: файл игнорирования не прочитан: %v", err)
	}
	declared, dirs, err := umbrellaDeclaredNames(filepath.Join("helm", "umbrella"))
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: объявления зонта не прочитаны: %v", err)
	}

	findings, census := judgeSubchartIgnoreRules(rules, declared, lines, dirs)
	t.Logf("перепись: %s", census)

	if census.Rules == 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: в deploy/.gitignore нет НИ ОДНОГО правила "+
			"каталога подчарта (строк прочитано %d) — «находок ноль» здесь означало "+
			"бы «ноль прочитанного»", census.LinesRead)
	}
	if census.Declared == 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: зонт не объявил ни одного имени — "+
			"сравнивать не с чем (правил %d)", census.Rules)
	}

	for _, f := range findings {
		t.Error(f)
	}
	if len(findings) == 0 {
		t.Logf("правил без предмета нет: все %d названы зонтом", census.Rules)
	}
}
