// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_step_declaration_parses_test.go — объявление шага подстановки обязано
// РАЗБИРАТЬСЯ как YAML, и каждое имя в его окружении обязано быть объявлено ОДИН раз.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Слияние двух работ склеило две строки объявления в одну:
//
//	optional: true    - name: KANAME_HOOK_TOKEN
//
// Со стороны это выглядит правкой отступа. На деле разбор обрывается на ней
// («mapping values are not allowed in this context»), то есть чарт НЕ
// РЕНДЕРИТСЯ ВОВСЕ — ни `helm template`, ни `helm upgrade`, — и стенд не
// поднимается. Обе слитые работы по отдельности верны: это столкновение
// раскладок (`multi-agent-flow.md` §14), которое по построению не находится ни
// на одной из веток порознь.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТОГО НЕ ПОЙМАЛ НИ ОДИН ИЗ СОСЕДНИХ ГЕЙТОВ
//
// Их несколько десятков, и ВСЕ они читают объявление ПОСТРОЧНО — образцом,
// отступом, перечнем имён. Склеенная строка для такого чтения неотличима от
// законной: имя на ней есть, ссылка на секрет есть, ключ есть. Ни один не
// спрашивает единственного, что здесь решает: СКЛАДЫВАЕТСЯ ЛИ ЭТО В YAML.
//
// Это ровно `testing.md` §«Гейт на класс», п.7: форма, о которой распознаватель
// не знает, не даёт ни красного, ни зелёного — она МОЛЧИТ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА — НАЗВАНА ЧЕСТНО
//
//   - Судится ОБЪЯВЛЕНИЕ, а не рендер. Рендер требует `helm` и скачанных
//     зависимостей, поэтому проверка над ним умеет пропуститься, а пропущенная
//     проверка не краснеет никогда (тот же довод, что у соседа
//     identity_substitution_judges_the_form_test.go).
//   - Популяция находится ПО ПРИЗНАКУ — тело объявления, несущее и образ, и
//     запускаемую команду, то есть описывающее КОНТЕЙНЕР. Перечня имён здесь
//     нет: он рос бы вместе с чартом и не рос бы вместе с деревом. Замер на
//     обоих деревьях: таких объявлений РОВНО ОДНО.
//   - Сплошной разбор ВСЕХ объявлений чарта рассмотрен и ОТВЕРГНУТ замером: на
//     стволе он даёт 5 находок из 17 объявлений, и все пять законны — тела
//     jsonnet и conf языком YAML не являются. Гейт, красный на исправном
//     дереве, отключают первым.
//   - О ВЕЛИЧИНАХ (какой секрет, какой ключ) не судится — это предмет соседей.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// helmAction — действие шаблонизатора. Оно обезвреживается, а не удаляется:
// удаление строки `key: {{ … }}` унесло бы вместе с ней и ключ, то есть
// проверка судила бы уже не то объявление, которое лежит в дереве.
var helmAction = regexp.MustCompile(`\{\{.*?\}\}`)

// neutraliseHelmActions заменяет действия скаляром-заглушкой. Строка, состоящая
// ИЗ ОДНОГО действия, снимается целиком: это управляющая конструкция, и в
// разобранном виде её не существует.
func neutraliseHelmActions(body string) string {
	var out []string
	for _, ln := range strings.Split(body, "\n") {
		if s := strings.TrimSpace(ln); strings.HasPrefix(s, "{{") && strings.HasSuffix(s, "}}") {
			continue
		}
		out = append(out, helmAction.ReplaceAllString(ln, "X"))
	}
	return strings.Join(out, "\n")
}

type stepEnvEntry struct {
	Name string `yaml:"name"`
}

type stepContainer struct {
	Name string         `yaml:"name"`
	Env  []stepEnvEntry `yaml:"env"`
}

// containerDefines — объявления чарта, описывающие КОНТЕЙНЕР: тело несёт и
// образ, и запускаемую команду. Признак, а не перечень имён.
func containerDefines(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	var paths []string
	for _, pat := range []string{
		filepath.Join(root, "charts", "*", "templates", "*"),
		filepath.Join(root, "templates", "*"),
	} {
		g, err := filepath.Glob(pat)
		if err != nil {
			t.Fatalf("обход шаблонов (%s): %v", pat, err)
		}
		paths = append(paths, g...)
	}
	sort.Strings(paths)
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		b, err := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if err != nil {
			continue
		}
		for name, blk := range defineBodies(string(b)) {
			if !strings.Contains(blk, "image:") || !strings.Contains(blk, "args:") {
				continue // это не объявление контейнера
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				rel = p
			}
			out[rel+":"+name] = blk
		}
	}
	return out
}

// stepDeclarationFindings — находки по объявлениям-контейнерам под корнем root.
// Возвращает также объём осмотренного: «ноль находок» обязано быть отличимо от
// «ноль прочитанного».
func stepDeclarationFindings(t *testing.T, root string) (findings []string, examined, lines int) {
	t.Helper()
	defs := containerDefines(t, root)
	coords := make([]string, 0, len(defs))
	for c := range defs {
		coords = append(coords, c)
	}
	sort.Strings(coords)

	for _, coord := range coords {
		body := defs[coord]
		examined++
		neutral := neutraliseHelmActions(body)
		lines += strings.Count(neutral, "\n") + 1

		var steps []stepContainer
		if err := yaml.Unmarshal([]byte(neutral), &steps); err != nil {
			findings = append(findings, fmt.Sprintf(
				"%s: ОБЪЯВЛЕНИЕ НЕ РАЗБИРАЕТСЯ как YAML: %v — чарт при этом НЕ РЕНДЕРИТСЯ "+
					"ВОВСЕ (ни `helm template`, ни `helm upgrade`), и стенд не поднимается", coord, err))
			continue
		}
		if len(steps) == 0 {
			findings = append(findings, fmt.Sprintf(
				"%s: объявление разобралось в ПУСТУЮ последовательность — проверять нечего, "+
					"и это отказ, а не успех", coord))
			continue
		}
		seen := map[string]int{}
		for _, s := range steps {
			for _, e := range s.Env {
				if e.Name != "" {
					seen[e.Name]++
				}
			}
		}
		names := make([]string, 0, len(seen))
		for n := range seen {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if seen[n] > 1 {
				findings = append(findings, fmt.Sprintf(
					"%s: переменная %s объявлена %d раза — побеждает последняя, и какая именно, "+
						"из объявления не видно; два места об одном предмете разойдутся молча",
					coord, n, seen[n]))
			}
		}
	}
	return findings, examined, lines
}

// TestIdentityStepDeclarationParsesAsYAML — объявление шага подстановки
// складывается в YAML, и его окружение объявляет каждое имя один раз.
func TestIdentityStepDeclarationParsesAsYAML(t *testing.T) {
	findings, examined, lines := stepDeclarationFindings(t, umbrellaDir)
	if examined == 0 {
		t.Fatalf("обход ПУСТ: объявлений-контейнеров не найдено ни одного — вердикт " +
			"беспредметен, а не зелёный")
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
	t.Logf("перепись: объявлений-контейнеров осмотрено %d · строк разобрано %d · находок %d",
		examined, lines, len(findings))
}
