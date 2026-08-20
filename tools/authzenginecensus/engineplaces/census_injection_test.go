// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package engineplaces_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/tools/authzenginecensus/engineplaces"
)

// ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ.
//
// Гейт судится ПРОГОНОМ, а не чтением его описания. Здесь на синтетическом
// дереве проверяется, что дискриминатор умеет и КРАСНЕТЬ (находит место и
// называет координату), и МОЛЧАТЬ (законная конструкция той же формы). Без
// второй половины гейт ловит форму, а не существо, и первый же ложный срабат
// его отключит.
//
// Дерево синтетическое НАМЕРЕННО: инъекция настоящим местом в продуктовое
// дерево оставила бы после себя правку прод-кода, а «вернул как было» — это
// обещание, а не механизм.

// tree — один файл синтетического модуля.
type tree map[string]string

// baseTree — законное дерево: движок, узкий порт, потребитель через порт и
// СОСЕД, зовущий форму. Форма — законный близнец: движок ей не удовлетворяет,
// и перепись обязана о ней молчать.
func baseTree() tree {
	return tree{
		"go.mod": "module example.test/m\n\ngo 1.25\n",
		"engine/engine.go": `package engine

import "context"

// OpenFGAHTTPClient — синтетический клиент движка: имя якоря, метод-вердикт и
// метод записи. Дом резолвит компилятор, а не сравнение пути.
type OpenFGAHTTPClient struct{}

func (c *OpenFGAHTTPClient) Check(ctx context.Context, subject, relation, object string) (bool, error) {
	return c.check(ctx, subject, relation, object)
}

func (c *OpenFGAHTTPClient) check(ctx context.Context, subject, relation, object string) (bool, error) {
	_ = ctx
	return subject != "" && relation != "" && object != "", nil
}

func (c *OpenFGAHTTPClient) WriteTuples(ctx context.Context, tuples []string) error {
	_, _ = ctx, tuples
	return nil
}
`,
		"ports/ports.go": `package ports

import "context"

// Store — порт движка: якорный тип удовлетворяет ему структурно.
type Store interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}
`,
		"consumer/consumer.go": `package consumer

import (
	"context"

	"example.test/m/ports"
)

// Ask — МЕСТО обращения к движку через порт.
func Ask(ctx context.Context, s ports.Store) bool {
	ok, err := s.Check(ctx, "user:a", "viewer", "obj:b")
	if err != nil {
		return false
	}
	return ok
}
`,
		"form/form.go": `package form

import "context"

// Verdict — форма: вопрос тот же, ПОДПИСЬ другая, движок ей не удовлетворяет.
type Verdict interface {
	Decide(ctx context.Context, subject, relation, object string) (allowed bool, reason string, err error)
}

// Ask — законный близнец: место, зовущее ФОРМУ, а не движок.
func Ask(ctx context.Context, v Verdict) bool {
	ok, _, err := v.Decide(ctx, "user:a", "viewer", "obj:b")
	return err == nil && ok
}
`,
	}
}

func writeTree(t *testing.T, files tree) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("подготовка дерева: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", name, err)
		}
	}
	return dir
}

func buildSynthetic(t *testing.T, files tree) *engineplaces.Census {
	t.Helper()
	if testing.Short() {
		t.Skip("инъекция типизирует синтетический модуль — не для -short")
	}
	c, err := engineplaces.Build(writeTree(t, files), "./...")
	if err != nil {
		t.Fatalf("перепись синтетического дерева не построилась: %v", err)
	}
	return c
}

func placeAt(c *engineplaces.Census, file string) *engineplaces.Place {
	for i := range c.Places {
		if c.Places[i].File == file {
			return &c.Places[i]
		}
	}
	return nil
}

func subtractionAt(c *engineplaces.Census, file, category string) *engineplaces.Subtraction {
	for i := range c.Subtractions {
		if c.Subtractions[i].File == file && c.Subtractions[i].Category == category {
			return &c.Subtractions[i]
		}
	}
	return nil
}

// TestR7_3_01_InjectionSyntheticPlaceIsFoundWithItsCoordinate — (а) КРАСНЕЕТ.
//
// Место обращения к движку через порт обязано попасть в перепись И быть
// названо координатой: гейт, называющий число без координаты, невозможно
// применить.
func TestR7_3_01_InjectionSyntheticPlaceIsFoundWithItsCoordinate(t *testing.T) {
	c := buildSynthetic(t, baseTree())
	if c.Void() {
		t.Fatalf("перепись объявила себя негодной на исправном дереве: %v", c.Errors)
	}

	p := placeAt(c, "consumer/consumer.go")
	if p == nil {
		t.Fatalf("синтетическое место не найдено; перепись: %d мест в %d файлах\n%s",
			len(c.Places), c.FileCount(), c.Findings())
	}
	if p.Line == 0 {
		t.Error("место найдено без строки — координата неполна")
	}
	if p.Method != "Check" || p.Kind != engineplaces.KindVerdict {
		t.Errorf("место названо как %s [%s]; ожидались Check [%s]", p.Method, p.Kind, engineplaces.KindVerdict)
	}
	if p.Via != "example.test/m/ports.Store" {
		t.Errorf("порт места %q — ожидался example.test/m/ports.Store", p.Via)
	}

	// Законный близнец в том же дереве: место, зовущее ФОРМУ. Без него
	// «нашли» означало бы «находит всё, что похоже на вопрос».
	if fp := placeAt(c, "form/form.go"); fp != nil {
		t.Errorf("вызов ФОРМЫ засчитан местом обращения к движку: %s:%d %s",
			fp.File, fp.Line, fp.Method)
	}
	t.Logf("найдено: %s:%d %s через %s", p.File, p.Line, p.Method, p.Via)
}

// TestR7_3_01_InjectionTypeMovedToNeighbourPackageIsStillFound — (а) КРАСНЕЕТ.
//
// ТРЕТЬЕ УСЛОВИЕ — место объявления конкретного типа — берётся из загруженного
// пакета, а не сравнением пути в исходнике. Тот же тип, переехавший в соседний
// пакет, обязан быть найден: путь пакета мутабелен ровно как имя, и предикат,
// сравнивающий строку пути, пережил бы переезд каталога молча.
func TestR7_3_01_InjectionTypeMovedToNeighbourPackageIsStillFound(t *testing.T) {
	files := baseTree()
	body := files["engine/engine.go"]
	delete(files, "engine/engine.go")
	files["relations/adapter/engine.go"] = body

	c := buildSynthetic(t, files)
	if c.Void() {
		t.Fatalf("перепись негодна: %v", c.Errors)
	}
	if c.AnchorPkg != "example.test/m/relations/adapter" {
		t.Fatalf("дом якоря %q — компилятор обязан был назвать example.test/m/relations/adapter", c.AnchorPkg)
	}
	if p := placeAt(c, "consumer/consumer.go"); p == nil {
		t.Fatalf("после переезда типа место потеряно — дискриминатор держится на пути, а не на типе\n%s",
			c.Report())
	}
	t.Logf("тип переехал в %s, место найдено", c.AnchorPkg)
}

// TestR7_3_01_InjectionStructuralNamesakeIsSubtractedNotCounted — (б) МОЛЧИТ.
//
// Удовлетворение интерфейсу в Go СТРУКТУРНО. Тот же по форме порт, у которого
// появилась реализация ВНЕ дома движка, перестаёт однозначно указывать на
// движок: место у соседа обязано быть вычтено С ПРИЧИНОЙ, а не засчитано.
//
// Пара к первой пробе: деревья отличаются РОВНО одним файлом-однофамильцем.
func TestR7_3_01_InjectionStructuralNamesakeIsSubtractedNotCounted(t *testing.T) {
	files := baseTree()
	files["other/grpc.go"] = `package other

import "context"

// GRPCClient — однофамилец: тот же набор методов, но это клиент к НАШЕМУ
// собственному методу, а не к движку. Объявлен ВНЕ дома движка.
type GRPCClient struct{}

func (g *GRPCClient) Check(ctx context.Context, subject, relation, object string) (bool, error) {
	_ = ctx
	return false, nil
}
`
	c := buildSynthetic(t, files)
	if c.Void() {
		t.Fatalf("перепись негодна: %v", c.Errors)
	}
	if p := placeAt(c, "consumer/consumer.go"); p != nil {
		t.Errorf("место через структурно неоднозначный порт засчитано, хотя реализация "+
			"объявлена вне дома движка: %s:%d", p.File, p.Line)
	}
	sub := subtractionAt(c, "consumer/consumer.go", engineplaces.CategoryNamesake)
	if sub == nil {
		t.Fatalf("однофамилец не вычтен с причиной; вычеты:\n%s", c.Report())
	}
	if !strings.Contains(sub.Reason, "other.GRPCClient") {
		t.Errorf("причина вычета не называет чужую реализацию: %s", sub.Reason)
	}
	if sub.Unit != engineplaces.UnitPlace {
		t.Errorf("вычет объявил единицу %q — перепись считает места", sub.Unit)
	}
	t.Logf("вычтено с причиной: %s:%d — %s", sub.File, sub.Line, sub.Reason)
}

// TestR7_3_01_InjectionCallToTheFormIsSilent — (б) МОЛЧИТ.
//
// Отдельная проба на законного близнеца: дерево, где движка НЕТ ВОВСЕ, а
// вопрос задаётся форме. Перепись обязана объявить себя негодной по отсутствию
// ЯКОРЯ — а не вернуть «мест 0», потому что снаружи эти два ответа неотличимы,
// и второй читался бы как «движок уже снят».
func TestR7_3_01_InjectionCallToTheFormIsSilent(t *testing.T) {
	files := baseTree()
	delete(files, "engine/engine.go")
	delete(files, "ports/ports.go")
	delete(files, "consumer/consumer.go")

	c := buildSynthetic(t, files)
	if len(c.Places) != 0 {
		t.Errorf("на дереве без движка найдено %d мест:\n%s", len(c.Places), c.Findings())
	}
	if !c.Void() {
		t.Fatal("перепись без якорного типа объявила себя годной: " +
			"«мест 0» и «прибора нет» стали неотличимы")
	}
	joined := strings.Join(c.Errors, "\n")
	if !strings.Contains(joined, engineplaces.AnchorType) {
		t.Errorf("отказ не называет якорный тип: %s", joined)
	}
	t.Logf("отказ по предпосылке: %s", joined)
}

// TestR7_3_01_InjectionUncompilablePackageRefusesInsteadOfReportingZero —
// предпосылка проверяется САМА.
//
// «Мест 0» и «пакет не загрузился» — РАЗНЫЕ исходы. Пакет, который не
// типизируется, обязан быть НАЗВАН, а перепись — объявить себя негодной
// целиком: необойдённое дерево иначе выглядит чистой переписью.
func TestR7_3_01_InjectionUncompilablePackageRefusesInsteadOfReportingZero(t *testing.T) {
	files := baseTree()
	files["broken/broken.go"] = `package broken

func Broken() int {
	return "это не число"
}
`
	c := buildSynthetic(t, files)
	if !c.Void() {
		t.Fatal("дерево с несобираемым пакетом дало ГОДНУЮ перепись — " +
			"необойдённое дерево неотличимо от чистого")
	}
	joined := strings.Join(c.Errors, "\n")
	if !strings.Contains(joined, "broken") {
		t.Errorf("отказ не называет пакет: %s", joined)
	}
	t.Logf("отказ с именем пакета: %s", strings.SplitN(joined, "\n", 2)[0])
}

// TestR7_3_01_InjectionMethodWithoutAKindIsAFinding — таксономия выводится ИЗ
// ТИПА.
//
// Метод, добавленный клиенту движка и не отнесённый ни к одному роду, обязан
// стать НАХОДКОЙ. Выписанный перечень родов разошёлся бы с типом молча — ровно
// так разошёлся действующий в дереве страж, держащий список имён.
func TestR7_3_01_InjectionMethodWithoutAKindIsAFinding(t *testing.T) {
	files := baseTree()
	files["engine/engine.go"] += `
// ListRelationsSomehow — метод, которого нет ни в одном роде.
func (c *OpenFGAHTTPClient) ListRelationsSomehow() []string { return nil }
`
	c := buildSynthetic(t, files)
	if len(c.UnclassifiedMethods) == 0 {
		t.Fatalf("метод без рода не стал находкой; методы: %d классифицировано", len(c.Methods))
	}
	found := false
	for _, m := range c.UnclassifiedMethods {
		if m == "ListRelationsSomehow" {
			found = true
		}
	}
	if !found {
		t.Errorf("находка не называет метод: %v", c.UnclassifiedMethods)
	}

	// Законный близнец: на дереве БЕЗ добавленного метода находок нет.
	clean := buildSynthetic(t, baseTree())
	if len(clean.UnclassifiedMethods) != 0 {
		t.Errorf("на дереве без лишнего метода находки есть: %v — проверка краснеет вхолостую",
			clean.UnclassifiedMethods)
	}
	t.Logf("метод без рода назван находкой: %v", c.UnclassifiedMethods)
}

// TestR7_3_01_InjectionMethodValueIsAPlaceToo — метод, взятый ЗНАЧЕНИЕМ.
//
// Обращение, отданное дальше функцией, — тоже место: вызов состоится в другом
// кадре. Не считать его значило бы занизить перепись ровно там, где движок
// передают как функцию, — и занижение выглядело бы как более чистое дерево.
func TestR7_3_01_InjectionMethodValueIsAPlaceToo(t *testing.T) {
	files := baseTree()
	files["handoff/handoff.go"] = `package handoff

import (
	"context"

	"example.test/m/ports"
)

// Ask — тип обращения, переданного значением.
type Ask func(ctx context.Context, subject, relation, object string) (bool, error)

// Hand отдаёт обращение к движку дальше, НЕ вызывая его здесь.
func Hand(s ports.Store) Ask {
	return s.Check
}
`
	c := buildSynthetic(t, files)
	if c.Void() {
		t.Fatalf("перепись негодна: %v", c.Errors)
	}
	p := placeAt(c, "handoff/handoff.go")
	if p == nil {
		t.Fatalf("метод, взятый значением, местом не стал:\n%s", c.Findings())
	}
	if !p.MethodValue {
		t.Error("место не помечено как значение метода — читатель не отличит его от вызова")
	}
	t.Logf("значение метода засчитано местом: %s:%d %s", p.File, p.Line, p.Method)
}
