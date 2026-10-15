// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// prod_header_default_posture_test.go — шапка боевого профиля говорит правду об
// умолчании чарта (kacho#3180).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Шапка values.prod.yaml утверждала, что умолчание зонта стоит на `mode: dev`
// (анонимный полный доступ) и голый `helm install` попадает в небезопасную
// посадку. Умолчание каждого подчарта, у которого ручка посадки есть, —
// `authMode: production`. Оператор решал о посадке по неверному тексту.
//
// Утверждение шапки о посадке сделано машинно-читаемым: одна строка
// «Умолчание чарта: authMode=<значение>». Проба сверяет его с объявлением
// `authMode` в values.yaml КАЖДОГО подчарта зонта, а не одного-двух названных.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧИТАЕТ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Голый рендер зонта отказывает (подчарты требуют удостоверений и ручек, у
// которых умолчания нет) — рендер без профиля об умолчании посадки не
// свидетельствует. Свидетельствуют объявления values подчартов; их и читаем.
// Обход печатает знаменатель и падает на нуле прочитанных.
package umbrella_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const prodProfile = "values.prod.yaml"

// headerDefaultClaim — машинно-читаемое утверждение шапки о посадке умолчания.
var headerDefaultClaim = regexp.MustCompile(`^#\s*Умолчание чарта: authMode=(\S+)\s*$`)

// subchartPosture — объявление посадки в values.yaml одного подчарта.
type subchartPosture struct {
	Chart    string
	AuthMode string // пусто — ручки посадки у подчарта нет
}

// leadingComment — ведущий блок комментариев файла (шапка профиля).
func leadingComment(raw string) []string {
	var out []string
	for _, l := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(l, "#") && strings.TrimSpace(l) != "" {
			break
		}
		out = append(out, l)
	}
	return out
}

// headerClaims — все утверждения шапки об умолчании посадки.
func headerClaims(header []string) []string {
	var out []string
	for _, l := range header {
		if m := headerDefaultClaim.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// judgeHeaderAgainstDefaults — находки: утверждений не ровно одно, либо
// утверждение расходится с объявлением подчарта.
func judgeHeaderAgainstDefaults(claims []string, charts []subchartPosture) []string {
	var findings []string
	if len(claims) != 1 {
		findings = append(findings, "шапка боевого профиля несёт утверждений об умолчании посадки "+
			strconv.Itoa(len(claims))+", а не одно")
		return findings
	}
	for _, c := range charts {
		if c.AuthMode != "" && c.AuthMode != claims[0] {
			findings = append(findings, "шапка говорит authMode="+claims[0]+", а умолчание подчарта "+
				c.Chart+" — authMode="+c.AuthMode)
		}
	}
	return findings
}

// readSubchartPostures — обход подчартов зонта: локальные каталоги charts/ и
// зависимости file:// из Chart.yaml.
func readSubchartPostures(t *testing.T) []subchartPosture {
	t.Helper()
	var dirs []string
	entries, err := os.ReadDir("charts")
	if err != nil {
		t.Fatalf("charts/ не прочитан: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join("charts", e.Name()))
		}
	}
	raw, err := os.ReadFile("Chart.yaml")
	if err != nil {
		t.Fatalf("Chart.yaml не прочитан: %v", err)
	}
	var chart struct {
		Dependencies []struct {
			Repository string `yaml:"repository"`
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(raw, &chart); err != nil {
		t.Fatalf("Chart.yaml не разобран: %v", err)
	}
	seen := map[string]bool{}
	for _, d := range chart.Dependencies {
		if p, ok := strings.CutPrefix(d.Repository, "file://"); ok && !seen[p] {
			seen[p] = true
			dirs = append(dirs, p)
		}
	}
	sort.Strings(dirs)
	var out []subchartPosture
	for _, d := range dirs {
		out = append(out, subchartPosture{
			Chart:    d,
			AuthMode: readNested(t, filepath.Join(d, "values.yaml"), "authMode"),
		})
	}
	return out
}

func TestProdHeaderTellsTheTruthAboutTheDefaultPosture(t *testing.T) {
	raw, err := os.ReadFile(prodProfile)
	if err != nil {
		t.Fatalf("%s не прочитан: %v", prodProfile, err)
	}
	claims := headerClaims(leadingComment(string(raw)))
	charts := readSubchartPostures(t)
	declaring := 0
	for _, c := range charts {
		if c.AuthMode != "" {
			declaring++
		}
	}
	t.Logf("перепись: подчартов %d · объявляют authMode %d · утверждений шапки %d",
		len(charts), declaring, len(claims))
	if declaring == 0 {
		t.Fatal("ни один подчарт не объявляет authMode — обход ничего не прочитал")
	}
	for _, f := range judgeHeaderAgainstDefaults(claims, charts) {
		t.Error(f)
	}
}

func TestInjection_HeaderClaimingDevAgainstProductionDefaultsIsFound(t *testing.T) {
	header := []string{"# Умолчание чарта: authMode=dev"}
	charts := []subchartPosture{{Chart: "a", AuthMode: "production"}, {Chart: "b"}}
	if got := judgeHeaderAgainstDefaults(headerClaims(header), charts); len(got) != 1 {
		t.Fatalf("ложная шапка не найдена: %v", got)
	}
}

func TestInjection_HeaderWithoutAClaimIsFound(t *testing.T) {
	header := []string{"# The chart DEFAULT intentionally runs `mode: dev`"}
	charts := []subchartPosture{{Chart: "a", AuthMode: "production"}}
	if got := judgeHeaderAgainstDefaults(headerClaims(header), charts); len(got) != 1 {
		t.Fatalf("шапка без утверждения не найдена: %v", got)
	}
}

func TestInjection_HeaderAgreeingWithDefaultsIsSilent(t *testing.T) {
	header := []string{"# Умолчание чарта: authMode=production"}
	charts := []subchartPosture{{Chart: "a", AuthMode: "production"}, {Chart: "b"}}
	if got := judgeHeaderAgainstDefaults(headerClaims(header), charts); len(got) != 0 {
		t.Fatalf("верная шапка названа находкой: %v", got)
	}
}
