// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_pool_declaration_parity_test.go — ширина пула службы прав объявлена ДВУМЯ
// чартами, и они обязаны говорить одно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Чарт службы прав живёт в дереве дважды: вендоренной копией внутри умбреллы
// (её и ставят все стеки, deploy/stacks.txt) и отдельным чартом
// services/iam/deploy, который **не устанавливает ни один стек**. У обоих своя
// ширина пула, и раз второй никем не ставится, его число не проверялось ничем —
// оно разошлось с первым и молчало об этом (#709): 50 против 80 в базовых
// значениях и 20 против 40 в dev-накладке.
//
// Это классическая пара «два места об одном предмете, из которых верно одно», и
// опасна она именно тем, что неверным оказывается место БЕЗ читателя: правку
// вносят туда, куда попали, а действует другое.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ, А НЕ КОММЕНТАРИЙ «ПРАВИТЬ ОБЕ»
//
// Такой комментарий в дереве уже есть — в шапке значений одного из боевых
// профилей, — и разойтись копиям он не помешал. Требование, которое держится
// намерением правящего, отличается от невыполняемого только тем, что его
// неисполнение незаметно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ
//
// Она НЕ судит, верна ли сама величина, — это предмет соседнего
// pool_fits_database_test.go, который считает произведение «пул × потолок
// подов» против предела базы. Здесь проверяется только СОГЛАСИЕ двух
// объявлений об одном предмете.
//
// И она НЕ узаконивает второй чарт. Правильный исход у этой пары один —
// оставить в дереве одну копию; пока их две, согласие держится здесь.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// iamPoolDecl — одно объявление ширины пула: где лежит и что говорит.
type iamPoolDecl struct {
	file  string   // путь от каталога deploy
	path  []string // путь ключа внутри файла
	value int
}

func (d iamPoolDecl) where() string { return d.file + " → " + strings.Join(d.path, ".") }

// iamPoolPair — две копии об ОДНОЙ посадке.
type iamPoolPair struct {
	posture string
	a, b    iamPoolDecl
}

// iamPoolPairsFromTree читает обе копии по каждой посадке.
//
// Каждый файл и каждый ключ обязаны существовать: «ключа нет» здесь означает
// «предикат перестал узнавать объявление», а не «расхождения нет». Поэтому
// пропуск — отказ, а не молчание.
func iamPoolPairsFromTree(t *testing.T) []iamPoolPair {
	t.Helper()

	standalone := filepath.Join(repoRoot, "services", "iam", "deploy")
	vendored := filepath.Join(umbrellaDir, "charts", "kaname")

	specs := []struct {
		posture string
		aFile   string
		aPath   []string
		bFile   string
		bPath   []string
	}{
		{
			posture: "базовая",
			aFile:   filepath.Join(standalone, "values.yaml"),
			aPath:   []string{"repository", "postgres", "maxConns"},
			bFile:   filepath.Join(vendored, "values.yaml"),
			bPath:   []string{"config", "repository", "postgres", "maxConns"},
		},
		{
			posture: "dev-накладка",
			aFile:   filepath.Join(standalone, "values.dev.yaml"),
			aPath:   []string{"repository", "postgres", "maxConns"},
			bFile:   filepath.Join(umbrellaDir, "values.dev.yaml"),
			bPath:   []string{"kaname", "config", "repository", "postgres", "maxConns"},
		},
	}

	read := func(file string, path []string) iamPoolDecl {
		vals := readYAML(t, file)
		raw, ok := lookup(vals, path...)
		if !ok {
			t.Fatalf("в %s нет ключа %s — предикат перестал узнавать объявление ширины "+
				"пула, а не копии сошлись. Найдите, куда объявление переехало, и "+
				"поправьте координату здесь: молчащая проверка хуже отсутствующей",
				file, strings.Join(path, "."))
		}
		n, ok := asInt(raw)
		if !ok {
			t.Fatalf("в %s ключ %s не читается числом (%v)", file, strings.Join(path, "."), raw)
		}
		rel := file
		if r, err := filepath.Rel(".", file); err == nil {
			rel = r
		}
		return iamPoolDecl{file: rel, path: path, value: n}
	}

	out := make([]iamPoolPair, 0, len(specs))
	for _, s := range specs {
		out = append(out, iamPoolPair{
			posture: s.posture,
			a:       read(s.aFile, s.aPath),
			b:       read(s.bFile, s.bPath),
		})
	}
	return out
}

// disagreeingIamPoolPairs — чистая функция над парами, чтобы самопроверка могла
// подать ей синтетический вход, а не подделывать дерево.
func disagreeingIamPoolPairs(pairs []iamPoolPair) []string {
	var out []string
	for _, p := range pairs {
		if p.a.value == p.b.value {
			continue
		}
		out = append(out, fmt.Sprintf(
			"посадка %q: %s объявляет %d, а %s — %d. Оба числа про ОДНУ величину, и "+
				"действует то, чей чарт ставится; второе тихо лжёт правящему",
			p.posture, p.a.where(), p.a.value, p.b.where(), p.b.value))
	}
	sort.Strings(out)
	return out
}

func TestIamPoolWidthIsDeclaredIdenticallyByBothCharts(t *testing.T) {
	pairs := iamPoolPairsFromTree(t)
	for _, p := range pairs {
		t.Logf("посадка %s: %s = %d · %s = %d",
			p.posture, p.a.where(), p.a.value, p.b.where(), p.b.value)
	}

	for _, why := range disagreeingIamPoolPairs(pairs) {
		t.Error(why)
	}

	// ПЕРЕПИСЬ: «ноль расхождений» обязано быть отличимо от «ноль сравнённого».
	t.Logf("осмотрено: пар объявлений %d", len(pairs))
	if len(pairs) == 0 {
		t.Fatal("ни одной пары не сравнено — проверка ничего не утверждает, " +
			"хотя выглядит зелёной")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в ОБЕ стороны на синтетическом входе той же формы.

func iamPair(a, b int) iamPoolPair {
	return iamPoolPair{
		posture: "injected",
		a:       iamPoolDecl{file: "a/values.yaml", path: []string{"repository", "postgres", "maxConns"}, value: a},
		b:       iamPoolDecl{file: "b/values.yaml", path: []string{"config", "repository", "postgres", "maxConns"}, value: b},
	}
}

func TestIamPoolParityGateSeesDisagreementAndIsSilentOnAgreement(t *testing.T) {
	// ДЕФЕКТ: ровно тот, что нашёлся в дереве, — копии разошлись.
	got := disagreeingIamPoolPairs([]iamPoolPair{iamPair(50, 80)})
	if len(got) != 1 {
		t.Fatalf("расхождение копий не стало находкой: %+v", got)
	}
	// Находка обязана называть ОБЕ координаты и ОБА числа: иначе правящий не
	// узнает, какую копию он смотрит и какая действует.
	for _, want := range []string{"a/values.yaml", "b/values.yaml", "50", "80"} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("находка не называет %q: %q", want, got[0])
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: копии согласны — молчание. Без него отрицание зеленело
	// бы на разборе, переставшем что-либо узнавать.
	if got := disagreeingIamPoolPairs([]iamPoolPair{iamPair(80, 80)}); len(got) != 0 {
		t.Fatalf("проверка сработала на согласных копиях: %+v", got)
	}

	// Обе посадки сравниваются НЕЗАВИСИМО: расхождение в одной не должно
	// маскироваться согласием в другой.
	both := disagreeingIamPoolPairs([]iamPoolPair{iamPair(80, 80), iamPair(20, 40)})
	if len(both) != 1 {
		t.Fatalf("расхождение во второй посадке потерялось рядом с согласием в первой: %+v", both)
	}
}
