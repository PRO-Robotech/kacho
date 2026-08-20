// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package engineplaces_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/tools/authzenginecensus/engineplaces"
)

// repoRoot — корень репозитория относительно каталога этого пакета.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("корень репозитория не резолвится: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("в %s нет go.mod — перепись пошла бы не по тому дереву: %v", root, err)
	}
	return root
}

func buildTree(t *testing.T) *engineplaces.Census {
	t.Helper()
	if testing.Short() {
		t.Skip("перепись типизирует всё дерево (export-данные) — не для -short")
	}
	c, err := engineplaces.Build(repoRoot(t), "./...")
	if err != nil {
		t.Fatalf("перепись не построилась: %v", err)
	}
	return c
}

// TestR7_3_01_CensusIsTakenByDiscriminatorNotByName — R7-3-01.
//
// Утверждается ИСХОД переписи, а не её наличие: две единицы счёта раздельно,
// вычет с причиной из ЗАКРЫТОГО перечня, разложение по роду вопроса, объём
// осмотренного и напечатанные границы.
//
// Числа здесь НЕ закрепляются: перепись — ориентир с ревизией, а не гейт
// (§0.1 приёмки). Закрепляются СВОЙСТВА, которые обязаны пережить любую
// законную правку продукта.
func TestR7_3_01_CensusIsTakenByDiscriminatorNotByName(t *testing.T) {
	c := buildTree(t)

	// Предпосылка: дерево протипизировано. Непротипизированный пакет — это
	// пакет, чьи места НЕВИДИМЫ, и перепись по нему выглядела бы чистой.
	if len(c.Errors) != 0 {
		t.Fatalf("предпосылка не выполнена — пакеты не протипизированы (%d):\n%s",
			len(c.Errors), strings.Join(c.Errors, "\n"))
	}

	// Объём осмотренного: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного».
	if c.Scan.Requested == 0 || c.Scan.Loaded == 0 {
		t.Fatalf("перепись беспредметна: запрошено %d, загружено %d",
			c.Scan.Requested, c.Scan.Loaded)
	}
	if c.Scan.Loaded > c.Scan.Requested {
		t.Fatalf("загружено (%d) больше, чем запрошено (%d) — единица счёта разъехалась",
			c.Scan.Loaded, c.Scan.Requested)
	}

	// Якорь: имя типа — единственное, что перепись знает; дом выводит компилятор.
	if c.Anchor != engineplaces.AnchorType {
		t.Fatalf("якорь переписи %q, ожидался %q", c.Anchor, engineplaces.AnchorType)
	}
	if c.AnchorPkg == "" {
		t.Fatal("дом якорного типа не резолвится — перепись не знает, что считать движком")
	}

	// Две единицы счёта печатаются РАЗДЕЛЬНО: одна без другой не отвечает ни
	// на «сколько чинить», ни на «сколько трогать».
	if len(c.Places) == 0 {
		t.Fatal("мест обращения к движку ноль — на этом дереве это заведомо неверно, " +
			"значит сломан дискриминатор, а не дерево")
	}
	if c.FileCount() == 0 {
		t.Fatal("файлов ноль при непустых местах — единицы счёта разошлись")
	}

	// Род вопроса объявлен у КАЖДОГО места.
	kinds := engineplaces.Kinds()
	known := map[string]bool{}
	for _, k := range kinds {
		known[k] = true
	}
	for _, p := range c.Places {
		if !known[p.Kind] {
			t.Errorf("место %s:%d (%s) несёт род %q вне объявленного перечня",
				p.File, p.Line, p.Method, p.Kind)
		}
	}

	// Каждый метод якорного типа попадает ровно в один род. Метод без рода —
	// находка: выписанный перечень родов разошёлся бы с типом МОЛЧА.
	if len(c.UnclassifiedMethods) != 0 {
		t.Errorf("методы якорного типа без рода (%d): %s\n"+
			"перечень родов разошёлся с типом — это находка, а не стиль",
			len(c.UnclassifiedMethods), strings.Join(c.UnclassifiedMethods, ", "))
	}
	if len(c.Methods) == 0 {
		t.Error("перечень методов якорного типа пуст — таксономия ничего не покрывает")
	}

	// Вычет — только из закрытого перечня и только с причиной. Открытый
	// перечень делает «округлил» выразимым под другим именем.
	closed := map[string]bool{}
	for _, cat := range engineplaces.Categories() {
		closed[cat] = true
	}
	for _, s := range c.Subtractions {
		if !closed[s.Category] {
			t.Errorf("вычет %s:%d по категории %q вне закрытого перечня", s.File, s.Line, s.Category)
		}
		if strings.TrimSpace(s.Reason) == "" {
			t.Errorf("вычет %s:%d (%s) без причины", s.File, s.Line, s.Category)
		}
		if s.Unit != engineplaces.UnitPlace && s.Unit != engineplaces.UnitFile {
			t.Errorf("вычет %s:%d объявляет единицу %q — вычет и перепись обязаны считать одно и то же",
				s.File, s.Line, s.Unit)
		}
	}

	// Границы напечатаны. Перепись, молчащая о том, чего она не видит,
	// зеленеет при живом втором клиенте движка.
	if len(c.Boundaries) == 0 {
		t.Error("перепись не напечатала ни одной границы — она утверждает полноту, которой не имеет")
	}

	t.Log("\n" + c.Report())
}

// TestR7_3_01_PlaceIsClassifiedNotTheInterface — зернистость.
//
// Один и тот же по форме порт внутри службы прав связан с клиентом движка, а у
// соседа — с клиентом к нашему СОБСТВЕННОМУ методу. Поинтерфейсное правило
// выбрасывает настоящие места и выглядит при этом ЧИЩЕ — то есть ошибается в
// сторону, которую не видно.
//
// Утверждается существование такой пары, а не конкретный порт: привязка к имени
// сгнила бы от законной правки продукта.
func TestR7_3_01_PlaceIsClassifiedNotTheInterface(t *testing.T) {
	c := buildTree(t)

	ambiguous := map[string]bool{}
	for _, p := range c.Ports {
		if p.Ambiguous {
			ambiguous[p.Type] = true
		}
	}
	if len(ambiguous) == 0 {
		t.Skip("на этом дереве нет ни одного структурно-неоднозначного порта — " +
			"свойство не проверяемо, и это состояние дерева, а не зелёный вердикт")
	}

	var counted, subtracted int
	for _, p := range c.Places {
		if ambiguous[p.Via] {
			counted++
		}
	}
	for _, s := range c.Subtractions {
		if s.Category == engineplaces.CategoryNamesake {
			subtracted++
		}
	}
	if subtracted == 0 {
		t.Error("ни одно место через неоднозначный порт не вычтено — третье условие не работает")
	}
	if counted == 0 {
		t.Error("ни одно место через неоднозначный порт не засчитано — правило стало поинтерфейсным, " +
			"а такое правило выбрасывает настоящие места")
	}
	t.Logf("через неоднозначные порты: засчитано %d, вычтено %d (однофамильцы)", counted, subtracted)
}

// TestR7_3_01_NameOnlyPredicateDisagreesWithTheDiscriminator — контраст.
//
// Предикат ПО ИМЕНИ меряет соглашение об именовании, а не предмет. Утверждается
// РАСХОЖДЕНИЕ в обе стороны: имя находит файлы без единого места (проза,
// порождённые стабы, второй прибор) — то есть перепись по имени завышена, — и
// делает это в другой единице счёта.
func TestR7_3_01_NameOnlyPredicateDisagreesWithTheDiscriminator(t *testing.T) {
	c := buildTree(t)

	if c.NameOnly.Files == 0 {
		t.Fatal("предикат по имени не нашёл ни одного файла — контраст беспредметен")
	}
	if c.NameOnly.Files == c.FileCount() {
		t.Error("предикат по имени и дискриминатор дали одно и то же число файлов — " +
			"либо контраст не считается, либо одна из двух переписей не исполнялась")
	}
	t.Logf("по имени: файлов %d · дискриминатором: файлов %d, мест %d",
		c.NameOnly.Files, c.FileCount(), len(c.Places))
}

// TestR7_3_01_SubtractionCategoriesAreClosedAndEachExpires — послабление
// истекает само.
//
// Категория вычета, которой больше нечего вычитать, — находка, а не «на всякий
// случай»: её унаследует следующая слепая зона. Утверждение мягкое (t.Log для
// пустых) ровно там, где пустота есть ЦЕЛЬ линии: движок снимается, и вычеты
// обязаны обнулиться вместе с ним. Жёсткое здесь — перечень объявленных
// сегментов оснастки проб: сегмент, не совпавший ни с чем, — это уже мёртвая
// запись, а не достигнутая цель.
func TestR7_3_01_SubtractionCategoriesAreClosedAndEachExpires(t *testing.T) {
	c := buildTree(t)

	used := map[string]int{}
	for _, s := range c.Subtractions {
		used[s.Category]++
	}
	var empty []string
	for _, cat := range engineplaces.Categories() {
		if used[cat] == 0 {
			empty = append(empty, cat)
		}
	}
	sort.Strings(empty)
	if len(empty) != 0 {
		t.Logf("категории вычета без предмета на этом дереве: %s "+
			"(норма после снятия движка; до снятия — сигнал, что дискриминатор их не строит)",
			strings.Join(empty, ", "))
	}

	for _, seg := range c.TestRigSegments {
		if seg.Matched == 0 {
			t.Errorf("объявленный сегмент оснастки проб %q не совпал ни с одним пакетом — "+
				"мёртвая запись перечня, которую унаследует следующая слепая зона", seg.Segment)
		}
	}
}

// TestR7_3_01_AnchorPremiseIsCheckedByTheCensusItself — гейт несёт проверку
// своей предпосылки.
//
// Перепись знает ровно одно имя. Если якорный тип переименован, снят или
// объявлен дважды — она обязана заявить об этом САМА, а не молча вернуть ноль
// мест: ноль мест и отсутствие якоря снаружи неотличимы.
func TestR7_3_01_AnchorPremiseIsCheckedByTheCensusItself(t *testing.T) {
	c := buildTree(t)

	if c.AnchorDeclarations != 1 {
		t.Fatalf("якорный тип %q объявлен %d раз(а) — перепись не знает, что считать движком",
			engineplaces.AnchorType, c.AnchorDeclarations)
	}
	if !strings.HasPrefix(c.AnchorPkg, "github.com/PRO-Robotech/kacho/") {
		t.Fatalf("дом якорного типа %q вне модуля продукта — перепись пошла не по тому дереву", c.AnchorPkg)
	}
	t.Logf("якорь %s объявлен в %s (путь резолвит компилятор, не сравнение строк)",
		c.Anchor, c.AnchorPkg)
}

// TestR7_3_01_CIRunsThisCensus — вторая половина шва: гейт, который конвейер не
// зовёт, стоит ровно столько же, сколько гейт, ничего не проверяющий.
//
// Перепись пропускает себя под кратким режимом (типизирует всё дерево), поэтому
// быстрая джоба до неё не доходит, а отбор интеграционной смотрит только внутрь
// `services/<svc>/internal/(repo|clients|reconciler)` — `tools/` в него не
// входит. Без своего шага она не исполнялась бы НИГДЕ, и её зелёное не значило
// бы ничего.
//
// Первая половина шва — запись в `internal/repohygiene`
// (`shortGatedRunByOwnCIStep`): она требует, чтобы объявленная там команда
// нашлась в ci.yaml. Обе половины нужны вместе: без записи пакет числился бы
// незаявленным долгом, без этой пробы запись освобождала бы его от переписи,
// ничего при этом не запуская.
func TestR7_3_01_CIRunsThisCensus(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci.yaml"))
	if err != nil {
		t.Fatalf("ci.yaml не прочитан — провязку нечем проверить: %v", err)
	}
	const invocation = "go test ./tools/authzenginecensus/engineplaces/ -run TestR7_3_01"
	text := string(body)
	if !strings.Contains(text, invocation) {
		t.Fatalf("ci.yaml не зовёт %q — перепись пропускает себя под кратким, "+
			"и ни одна джоба до её пакета не доходит: без этого шага она не исполняется НИГДЕ",
			invocation)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, invocation) && strings.Contains(line, "-short") {
			t.Fatalf("шаг зовёт перепись с -short, то есть ровно с тем пропуском, "+
				"ради обхода которого он и заведён: %s", strings.TrimSpace(line))
		}
	}
}
