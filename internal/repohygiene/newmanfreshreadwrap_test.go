// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanfreshreadwrap_test.go — первый доступ к СВОЕМУ свежему ресурсу обёрнут
// ограниченным ретраем ВО ВСЕХ генераторах newman, а не в тех, где вспомнили.
//
// # Предмет
//
// Kachō eventually-consistent: owner-tuple свежесозданного ресурса
// материализуется вне мутации (`api-conventions.md` §Форма ресурса,
// `testing.md` §e2e-инварианты), поэтому ПЕРВОЕ обращение создателя к своему
// ресурсу может кратко получить 403/404. Норма предписывает клиентский
// ограниченный ретрай, и он реализован — `retry_until_authorized` в каждом
// генераторе. Но ставился он ВРУЧНУЮ, а значит пропуск неотличим от решения не
// оборачивать.
//
// Замер по артефактам прогона CI 31002239590 (8 суит, 82 отчёта, 15648
// утверждений, 151 упавшее): из 68 падений полосы видимости (403/404) **42**
// пришлись на шаги, у которых обёртки не было ВОВСЕ — при том что в тех же
// кейсах соседние шаги той же формы обёрнуты. Это пропуск, а не замысел.
//
// # Что здесь считается защитой
//
// Защитой считается ПРЕДИКАТ по свойству шага (`_wrap_own_fresh_reads`),
// провязанный в сериализацию кейса (`case_to_postman`), — а не перечень имён
// шагов и не аккуратность автора. Генератор, у которого есть
// `retry_until_authorized`, но нет предиката в сериализации, — находка: класс в
// нём открыт, и следующий кейс унаследует слепую зону.
//
// Гейт несёт проверку СВОЕЙ предпосылки: если ни одного генератора с
// `retry_until_authorized` в дереве не нашлось, он падает, а не выходит
// «нулём находок» — «ноль находок» обязано быть отличимо от «ноль прочитанного».
// Поведение самого предиката (краснеет на инъекции, молчит на четырёх законных
// близнецах) доказывается отдельно —
// `services/vpc/tests/newman/scripts/selftest_autowrap.go`-эквивалентом на
// python: `services/vpc/tests/newman/scripts/selftest_autowrap.py`, шаг CI
// «предикат обёртки первого доступа (vpc)».
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnFreshReadWrapPredicateWiredInEveryNewmanGenerator(t *testing.T) {
	root := repoRoot(t)

	var generators []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "ui-future":
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() != "gen.py" || !strings.Contains(path, filepath.Join("tests", "newman")) {
			return nil
		}
		generators = append(generators, path)
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	var withHelper, findings []string
	for _, g := range generators {
		b, err := os.ReadFile(g) //nolint:gosec // путь получен обходом дерева репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", g, err)
		}
		src := string(b)
		if !strings.Contains(src, "def retry_until_authorized(") {
			continue // генератор без полосы видимости — предмета нет
		}
		rel, _ := filepath.Rel(root, g)
		withHelper = append(withHelper, rel)

		hasPredicate := strings.Contains(src, "def _wrap_own_fresh_reads(")
		// Открытая скобка без закрывающей — намеренно: у генератора, который сам
		// делает имена уникальными, вызов идёт с `rename=False`, и обе формы
		// одинаково провязывают предикат.
		wired := strings.Contains(src, "_wrap_own_fresh_reads(case.steps")
		if !hasPredicate || !wired {
			findings = append(findings, rel+" (предикат: "+yesNo(hasPredicate)+", провязан в case_to_postman: "+yesNo(wired)+")")
		}
	}

	// Проверка предпосылки: обходчик обязан заявить объём осмотренного, иначе
	// «находок нет» неотличимо от «ничего не прочитано».
	t.Logf("осмотрено генераторов newman: %d, из них с полосой видимости: %d", len(generators), len(withHelper))
	if len(withHelper) == 0 {
		t.Fatalf("предпосылка гейта не выполняется: ни одного генератора с retry_until_authorized "+
			"среди %d найденных gen.py — либо помощник переименован, либо обход смотрит не туда; "+
			"чинить надо гейт, а не молча выходить успехом", len(generators))
	}

	if len(findings) > 0 {
		t.Fatalf("генераторы, где первый доступ к своему свежему ресурсу оборачивается только вручную "+
			"(пропуск неотличим от решения — 42 падения полосы видимости в прогоне 31002239590):\n  %s",
			strings.Join(findings, "\n  "))
	}
}

func yesNo(b bool) string {
	if b {
		return "есть"
	}
	return "нет"
}
