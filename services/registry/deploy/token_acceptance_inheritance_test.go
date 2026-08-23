// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// token_acceptance_inheritance_test.go — профиль, молчащий о приёме токена,
// говорит, ПОЧЕМУ он молчит.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Приём токена объявляется цепочкой, а ищут его в файле. Накладка на
// управляемый кластер описывает образы и размеры, а приём берёт у слоя под
// собой — и поиск по её файлу честно отвечает «ноль попаданий». Ноль читается
// как «забыли», и различить это по дереву было НЕЧЕМ: наследование и пропуск
// выглядят одинаково — пустотой.
//
// Цена измерена: по такому чтению заведена задача уровня P1, утверждавшая, что
// пять профилей поля не объявляют и реестр на них не поднимется. Перепись по
// ЦЕПОЧКАМ (а не по файлам) показала обратное — каждый развёртываемый стенд
// объявление получает. Верной была не находка, а вопрос: почему на него нельзя
// ответить, не собрав цепочку в уме.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ЭТО ЗАКРЫТО
//
// Профиль, который объявляет `registry:` и НЕ объявляет `registry.tokenAcceptance`,
// несёт машинную строку с именем слоя, у которого приём наследуется. Проверка
// требует, чтобы названный слой действительно объявлял приём и стоял в КАЖДОЙ
// цепочке этого профиля ПЕРЕД ним.
//
// Отметка истекает сама: слой, переставший объявлять приём или переехавший в
// цепочке ниже, роняет проверку. Обратная сторона тоже держится — профиль,
// который приём ОБЪЯВЛЯЕТ, отметку нести не вправе: объявление, которое никто
// не читает, переживает свой предмет молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ НЕ ПРОВЕРЯЕТСЯ
//
// Проверка не судит СОДЕРЖАНИЕ приёма — им заняты соседи по каталогу
// (token_acceptance_declared_test.go — разбор объявления,
// token_revocation_transport_declared_test.go — учётные данные ребра отзыва).
// Предмет здесь один: можно ли по файлу профиля отличить наследование от
// пропуска.
//
// Профиль, не объявляющий `registry:` ВОВСЕ, под требование не подпадает: он не
// говорит о реестре ничего, и «ничего» здесь однозначно. Отметки такому файлу
// не полагается — иначе перечень отметок пополз бы на файлы без предмета.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// inheritanceMarker — машинная строка отметки. Имя слоя берётся ЦЕЛИКОМ и
// сверяется на равенство с известным профилем: сравнение по подстроке приняло
// бы `values.dev-prod.yaml.bak` за `values.dev-prod.yaml`.
var inheritanceMarker = regexp.MustCompile(`(?m)^#\s*приём токена наследуется от:\s*(\S+)\s*$`)

// profileFacts — то и только то, что проверка знает о профиле.
type profileFacts struct {
	declaresRegistry   bool
	declaresAcceptance bool
	marker             string // имя слоя из отметки; "" — отметки нет
}

// auditInheritanceMarkers — сам предикат, отделённый от чтения дерева, чтобы
// самопроверка могла подать ему синтетический вход той же формы.
//
// Возвращает находки (пусто = чисто) и перепись осмотренного.
func auditInheritanceMarkers(
	profiles map[string]profileFacts,
	chains map[string][]string,
) (findings []string, census string) {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	silent, declares, inherits := 0, 0, 0
	for _, name := range names {
		f := profiles[name]
		if !f.declaresRegistry {
			silent++
			if f.marker != "" {
				findings = append(findings, fmt.Sprintf(
					"профиль %q не объявляет registry вовсе, но несёт отметку наследования (%q) — "+
						"отметке нечего объяснять: «ничего о реестре» однозначно и без неё",
					name, f.marker))
			}
			continue
		}
		if f.declaresAcceptance {
			declares++
			if f.marker != "" {
				findings = append(findings, fmt.Sprintf(
					"профиль %q ОБЪЯВЛЯЕТ registry.tokenAcceptance и при этом несёт отметку "+
						"наследования от %q — отметка потеряла предмет и переживёт его молча",
					name, f.marker))
			}
			continue
		}

		inherits++
		if f.marker == "" {
			findings = append(findings, fmt.Sprintf(
				"профиль %q объявляет registry и молчит о registry.tokenAcceptance, не сказав почему — "+
					"поиск по этому файлу даёт ноль попаданий, и ноль неотличим от «забыли». "+
					"Либо объяви приём здесь, либо назови слой строкой "+
					"«# приём токена наследуется от: <файл>»", name))
			continue
		}
		src, known := profiles[f.marker]
		if !known {
			findings = append(findings, fmt.Sprintf(
				"профиль %q наследует приём от %q, а такого профиля в дереве нет — "+
					"отметка называет координату, которой не существует", name, f.marker))
			continue
		}
		if !src.declaresAcceptance {
			findings = append(findings, fmt.Sprintf(
				"профиль %q наследует приём от %q, а тот приём НЕ объявляет — "+
					"отметка пережила свой предмет", name, f.marker))
			continue
		}
		inChain := 0
		for stack, chain := range chains {
			posSelf, posSrc := -1, -1
			for i, p := range chain {
				if p == name {
					posSelf = i
				}
				if p == f.marker {
					posSrc = i
				}
			}
			if posSelf < 0 {
				continue
			}
			inChain++
			if posSrc < 0 || posSrc > posSelf {
				findings = append(findings, fmt.Sprintf(
					"профиль %q наследует приём от %q, но в цепочке стенда %q (%s) тот слой "+
						"НЕ стоит перед ним — наследовать нечего, значение до профиля не доезжает",
					name, f.marker, stack, strings.Join(chain, " → ")))
			}
		}
		if inChain == 0 {
			findings = append(findings, fmt.Sprintf(
				"профиль %q несёт отметку наследования, но ни одна цепочка его не называет — "+
					"«перед ним» не имеет смысла, и отметка ничего не утверждает", name))
		}
	}
	return findings, fmt.Sprintf(
		"осмотрено профилей=%d, из них молчат о реестре вовсе=%d, объявляют приём=%d, наследуют приём=%d",
		len(names), silent, declares, inherits)
}

// TestProfilesSilentAboutTokenAcceptanceSayWhyTheyAreSilent — сама проверка.
func TestProfilesSilentAboutTokenAcceptanceSayWhyTheyAreSilent(t *testing.T) {
	names := umbrellaProfiles(t)
	if len(names) == 0 {
		t.Fatalf("профилей umbrella не найдено — предикат перестал узнавать дерево, "+
			"а не дерево стало чистым (каталог %s)",
			filepath.Join("..", "..", "..", "deploy", "helm", "umbrella"))
	}

	profiles := make(map[string]profileFacts, len(names))
	for _, name := range names {
		tree := umbrellaValues(t, name)
		reg, hasReg := tree["registry"].(map[string]any)
		facts := profileFacts{declaresRegistry: hasReg}
		if hasReg {
			_, facts.declaresAcceptance = digOpt(reg, "tokenAcceptance")
		}
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "helm", "umbrella", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if m := inheritanceMarker.FindStringSubmatch(string(raw)); m != nil {
			facts.marker = m[1]
		}
		profiles[name] = facts
	}

	findings, census := auditInheritanceMarkers(profiles, deployStackChains(t))
	for _, f := range findings {
		t.Errorf("%s", f)
	}
	t.Logf("%s", census)
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе той же формы.

func TestInheritanceMarkerAudit_SelfTest(t *testing.T) {
	base := func() map[string]profileFacts {
		return map[string]profileFacts{
			"values.dev.yaml":      {declaresRegistry: true, declaresAcceptance: true},
			"values.dev-prod.yaml": {declaresRegistry: true, declaresAcceptance: true},
			"values.overlay.yaml":  {declaresRegistry: true, marker: "values.dev-prod.yaml"},
			"values.nothing.yaml":  {},
		}
	}
	chains := map[string][]string{
		"stand": {"values.dev.yaml", "values.dev-prod.yaml", "values.overlay.yaml"},
	}

	cases := []struct {
		name     string
		mutate   func(map[string]profileFacts) map[string][]string
		wantFind string // подстрока находки; "" = обязан молчать
	}{
		// (б) законный вход — обязан молчать.
		{"наследование объявлено и доезжает", func(map[string]profileFacts) map[string][]string { return chains }, ""},

		// (а) внесённые дефекты — ровно те, что делают ноль неотличимым от «забыли».
		{"отметки нет вовсе", func(p map[string]profileFacts) map[string][]string {
			f := p["values.overlay.yaml"]
			f.marker = ""
			p["values.overlay.yaml"] = f
			return chains
		}, "не сказав почему"},
		{"отметка называет несуществующий профиль", func(p map[string]profileFacts) map[string][]string {
			f := p["values.overlay.yaml"]
			f.marker = "values.dev-prod.yaml.bak"
			p["values.overlay.yaml"] = f
			return chains
		}, "такого профиля в дереве нет"},
		{"названный слой перестал объявлять приём", func(p map[string]profileFacts) map[string][]string {
			f := p["values.dev-prod.yaml"]
			f.declaresAcceptance = false
			p["values.dev-prod.yaml"] = f
			return chains
		}, "отметка пережила свой предмет"},
		{"названный слой стоит в цепочке ПОСЛЕ", func(map[string]profileFacts) map[string][]string {
			return map[string][]string{
				"stand": {"values.dev.yaml", "values.overlay.yaml", "values.dev-prod.yaml"},
			}
		}, "НЕ стоит перед ним"},
		{"профиль не назван ни одной цепочкой", func(map[string]profileFacts) map[string][]string {
			return map[string][]string{"stand": {"values.dev.yaml", "values.dev-prod.yaml"}}
		}, "ни одна цепочка его не называет"},
		{"отметка у профиля, который приём объявляет", func(p map[string]profileFacts) map[string][]string {
			f := p["values.dev.yaml"]
			f.marker = "values.dev-prod.yaml"
			p["values.dev.yaml"] = f
			return chains
		}, "отметка потеряла предмет"},
		{"отметка у профиля без registry", func(p map[string]profileFacts) map[string][]string {
			f := p["values.nothing.yaml"]
			f.marker = "values.dev-prod.yaml"
			p["values.nothing.yaml"] = f
			return chains
		}, "нечего объяснять"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base()
			ch := tc.mutate(p)
			findings, census := auditInheritanceMarkers(p, ch)
			if tc.wantFind == "" {
				if len(findings) != 0 {
					t.Fatalf("законный вход отвергнут: %v", findings)
				}
				if !strings.Contains(census, "осмотрено профилей=4") {
					t.Fatalf("перепись не считает вход: %q", census)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("внесённый дефект не пойман (ждали упоминания %q); перепись: %s", tc.wantFind, census)
			}
			if !strings.Contains(strings.Join(findings, "\n"), tc.wantFind) {
				t.Fatalf("находка без координаты: %v не называет %q", findings, tc.wantFind)
			}
		})
	}
}

// TestInheritanceMarkerBoundary_SelfTest — ГРАНИЦА ИМЕНИ, названная отдельно.
//
// Разбор берёт имя слоя целиком и сверяет его на равенство с известным
// профилем. Проверка по подстроке приняла бы соседнее имя за названное — этот
// класс в дереве уже ловили, поэтому здесь показаны ОБЕ стороны на одном входе.
func TestInheritanceMarkerBoundary_SelfTest(t *testing.T) {
	const text = "# приём токена наследуется от: values.dev-prod.yaml.bak\n"
	m := inheritanceMarker.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("фикстура не создала условие: отметка не разобрана вовсе")
	}
	if m[1] != "values.dev-prod.yaml.bak" {
		t.Fatalf("имя слоя взято не целиком: %q", m[1])
	}
	if !strings.Contains(m[1], "values.dev-prod.yaml") {
		t.Fatalf("фикстура не создала условие: соседнее имя не содержит названного как подстроку, " +
			"и контроль ничего не показывает")
	}
	profiles := map[string]profileFacts{
		"values.dev-prod.yaml": {declaresRegistry: true, declaresAcceptance: true},
		"values.overlay.yaml":  {declaresRegistry: true, marker: m[1]},
	}
	chains := map[string][]string{"stand": {"values.dev-prod.yaml", "values.overlay.yaml"}}
	findings, _ := auditInheritanceMarkers(profiles, chains)
	if len(findings) == 0 {
		t.Fatalf("сверка по имени целиком не работает: предикат принял соседнее имя за названный слой")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "такого профиля в дереве нет") {
		t.Fatalf("находка не о том: %v", findings)
	}
}
