// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verdictscopeform_test.go — ОДНА СЕМАНТИКА ОБЛАСТИ НА ВСЕ ТОЧКИ ВХОДА.
//
// # Предмет
//
// «Что считать областью объекта» — один вопрос, и отвечать на него обязаны
// одинаково все запросы формы E: точечный вердикт, перечисление, разбор
// оснований, перечисление субъектов. Разойдись они — и перечисление вернёт
// объекты, которые точечная проверка отвергнет; расхождение при этом
// НАБЛЮДАЕМО только на выдачах, сделанных выше непосредственного предка, то есть
// на самых важных (аккаунт, кластер).
//
// Хуже того, пока формы расходятся МЕЖДУ СОБОЙ, сравнение с внешним движком
// судит непонятно что: расхождение может принадлежать любой из двух семантик.
//
// # Почему гейт, а не внимательность
//
// Это уже происходило. Одна из четырёх точек входа была переведена на
// одноразовое чтение таблицы рёбер, три остались на обходе; ответ разошёлся, и
// заметить это можно было только вопросом про выдачу на аккаунт. Дифф выглядел
// локальным — «оптимизация одного запроса».
//
// # Что именно сверяется — ПРЕДИКАТ, А НЕ НОСИТЕЛЬ
//
// Сверяется РЕКУРСИВНЫЙ ШАГ: то, как из области получается следующая область
// вверх. Именно он и есть ответ на вопрос «что считать областью объекта».
//
// Затравка НЕ сверяется, и это не послабление, а разграничение. Точечные
// запросы сеются одним объектом из параметров, перечисление — страницей
// кандидатов и тащит их ключ через всю цепь. Носитель у них законно разный;
// требовать совпадения затравки значило бы краснеть на верной форме, а первый
// же ложный срабат гейт отключает.
//
// Номера параметров стираются ($7 у вердикта, $5 у перечисления, $3 у разбора),
// комментарии снимаются, пробелы нормализуются: расхождение в прозе находкой не
// является.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scopeFormFiles — все точки входа формы E, отвечающие на вопрос об области.
//
// Перечень объявлен здесь, потому что он и есть ОБЪЁМ гейта. Файл пути вердикта,
// заведённый позже и сюда не внесённый, гейт не покроет — поэтому перепись ниже
// печатает, сколько запросов найдено, и падает, если найден один: сверять
// одну форму саму с собой бессмысленно.
var scopeFormFiles = []string{"query.go", "list.go", "subjects.go", "expand.go"}

var (
	reScopeCTE   = regexp.MustCompile(`(?s)scope\([^)]*\) AS \((.*?)\n\),`)
	reSQLComment = regexp.MustCompile(`(?m)^\s*--.*$`)
	reParam      = regexp.MustCompile(`\$\d+`)
	reSpace      = regexp.MustCompile(`\s+`)
)

func TestVerdictScopeFormIsTheSameOnEveryEntryPoint(t *testing.T) {
	dir := filepath.Join(repoRoot(t), verdictGlueRoot)

	forms := map[string]string{}
	var order []string
	for _, name := range scopeFormFiles {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("файл пути вердикта %s не читается: %v", name, err)
		}
		m := reScopeCTE.FindStringSubmatch(string(body))
		if m == nil {
			continue
		}
		step, ok := recursiveStepOf(m[1])
		if !ok {
			t.Errorf("%s: у CTE области нет рекурсивного шага — либо обход снят, либо разбор "+
				"перестал его узнавать; в обоих случаях сверять нечего", name)
			continue
		}
		forms[name] = normalizeScopeBody(step)
		order = append(order, name)
	}

	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: файлов в перечне %d, запросов с областью найдено %d (%s)",
		len(scopeFormFiles), len(order), strings.Join(order, ", "))

	// ПРОВЕРКА СВОЕЙ ПРЕДПОСЫЛКИ: сверять есть что только при двух и более.
	if len(order) < 2 {
		t.Fatalf("запросов с CTE области найдено %d: сверять одну форму саму с собой "+
			"бессмысленно, и «расхождений нет» здесь означает «нечего было сравнивать»", len(order))
	}

	base := order[0]
	for _, name := range order[1:] {
		if forms[name] == forms[base] {
			continue
		}
		t.Errorf("форма области в %s расходится с %s.\n  %s: %s\n  %s: %s\n"+
			"    Одна точка входа отвечает на вопрос «что считать областью объекта» иначе, "+
			"чем другая. Перечисление вернёт объекты, которые точечная проверка отвергнет, "+
			"и наоборот; заметно это только на выдачах выше непосредственного предка.",
			name, base, base, forms[base], name, forms[name])
	}
}

// recursiveStepOf — часть, определяющая переход к следующей области вверх.
//
// Берётся от `FROM scope s`: всё до него — список выборки, то есть НОСИТЕЛЬ
// (какие колонки цепь тащит с собой), а он у точечного запроса и у страницы
// законно разный.
func recursiveStepOf(body string) (string, bool) {
	i := strings.Index(body, "FROM scope s")
	if i < 0 {
		return "", false
	}
	return body[i:], true
}

// normalizeScopeBody — исполняемая часть без номеров параметров и лишних пробелов.
func normalizeScopeBody(s string) string {
	s = reSQLComment.ReplaceAllString(s, "")
	s = reParam.ReplaceAllString(s, "$N")
	return strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
}

// TestVerdictScopeFormGateRedsOnADivergedStep — доказательство, что гейт СПОСОБЕН
// упасть, и что он молчит на законном различии носителя.
//
// Инъекция идёт настоящими формами: слева — одноразовое чтение таблицы рёбер
// (то, чем одна точка входа уже расходилась с тремя), справа — тот же обход с
// ДРУГИМ носителем (страница вместо объекта), на котором гейт обязан молчать.
func TestVerdictScopeFormGateRedsOnADivergedStep(t *testing.T) {
	const walk = `    SELECT $2::text, $3::text, 0
  UNION
    SELECT e.parent_type, e.parent_id, s.depth + 1
      FROM scope s
      CROSS JOIN LATERAL (
             SELECT pe.parent_type, pe.parent_id
               FROM kacho_iam.resource_parent_edge pe
              WHERE pe.object_type = s.s_type AND pe.object_id = s.s_id
              ORDER BY pe.depth
              LIMIT $7::int
           ) e
     WHERE s.depth < $7::int`

	// ЗАКОННЫЙ БЛИЗНЕЦ: тот же шаг, другой носитель и другие номера параметров.
	const walkPaged = `    SELECT c.object_id, $2::text, c.object_id, 0 FROM candidate c
  UNION
    SELECT s.object_id, e.parent_type, e.parent_id, s.depth + 1
      FROM scope s
      CROSS JOIN LATERAL (
             SELECT pe.parent_type, pe.parent_id
               FROM kacho_iam.resource_parent_edge pe
              WHERE pe.object_type = s.s_type AND pe.object_id = s.s_id
              ORDER BY pe.depth
              LIMIT $5::int
           ) e
     WHERE s.depth < $5::int`

	// НАХОДКА: одноразовое чтение — рекурсивного шага нет вовсе.
	const oneShot = `    SELECT $2::text, $3::text, 0
  UNION ALL
    SELECT e.parent_type, e.parent_id, e.depth
      FROM kacho_iam.resource_parent_edge e
     WHERE e.object_type = $2::text AND e.object_id = $3::text`

	step, ok := recursiveStepOf(walk)
	if !ok {
		t.Fatal("разбор не узнал рекурсивного шага в форме, которая его несёт")
	}
	paged, ok := recursiveStepOf(walkPaged)
	if !ok {
		t.Fatal("разбор не узнал шага у страничной формы — гейт краснел бы на законном носителе")
	}
	if normalizeScopeBody(step) != normalizeScopeBody(paged) {
		t.Errorf("законный близнец объявлен расхождением: носитель у страницы законно другой.\n"+
			"  объект:  %s\n  страница: %s", normalizeScopeBody(step), normalizeScopeBody(paged))
	}
	if _, ok := recursiveStepOf(oneShot); ok {
		t.Error("одноразовое чтение принято за обход: гейт не заметил бы точки входа, " +
			"которая отвечает на вопрос об области иначе, чем остальные")
	}

	// И различие ВНУТРИ шага — тоже находка: предел обхода снят.
	diverged := strings.Replace(walk, "WHERE s.depth < $7::int", "WHERE s.depth < 99", 1)
	dstep, _ := recursiveStepOf(diverged)
	if normalizeScopeBody(dstep) == normalizeScopeBody(step) {
		t.Error("подменённый предел обхода не признан расхождением: гейт сверяет не то, " +
			"что объявляет")
	}
	t.Logf("инъекция: одноразовое чтение и подменённый предел признаны расхождением, " +
		"страничный носитель — нет")
}
