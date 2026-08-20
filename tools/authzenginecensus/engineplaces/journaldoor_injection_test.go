// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journaldoor_injection_test.go — ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ для гейта «в движок
// пишут только через строку журнала».
//
// Гейт судится ПРОГОНОМ, а не чтением своего описания. Здесь проверяется, что он
// умеет и КРАСНЕТЬ (новый обход назван координатой), и МОЛЧАТЬ (законная запись
// той же формы). Без второй половины гейт ловит форму, а не существо, и первый же
// ложный срабат его отключит.
//
// Дерево синтетическое НАМЕРЕННО: инъекция настоящим обходом в продуктовое дерево
// оставила бы после себя правку прод-кода, а «вернул как было» — это обещание, а
// не механизм.
package engineplaces_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/tools/authzenginecensus/engineplaces"
)

// treeWithEngineWrite — законное дерево плюс МЕСТО ЗАПИСИ в движок через порт.
func treeWithEngineWrite() tree {
	files := baseTree()
	files["ports/write.go"] = `package ports

import "context"

// Writer — порт записи: якорный тип удовлетворяет ему структурно.
type Writer interface {
	WriteTuples(ctx context.Context, tuples []string) error
}
`
	files["bypass/bypass.go"] = `package bypass

import (
	"context"

	"example.test/m/ports"
)

// Put — МЕСТО ЗАПИСИ в движок.
func Put(ctx context.Context, w ports.Writer) error {
	return w.WriteTuples(ctx, []string{"user:a viewer obj:b"})
}
`
	return files
}

// writeFilesOf — места рода ЗАПИСИ синтетической переписи, по файлам.
func writeFilesOf(t *testing.T, c *engineplaces.Census) map[string]int {
	t.Helper()
	if c.Void() {
		t.Fatalf("перепись синтетического дерева негодна: %v", c.Errors)
	}
	return writePlacesByFile(c)
}

// TestJournalDoor_InjectionNewBypassIsRedWithItsCoordinate — (а) КРАСНЕЕТ.
//
// Новое место записи в движок, ни одним вердиктом не покрытое, обязано быть
// названо — И названо КООРДИНАТОЙ: находка без координаты неприменима.
func TestJournalDoor_InjectionNewBypassIsRedWithItsCoordinate(t *testing.T) {
	byFile := writeFilesOf(t, buildSynthetic(t, treeWithEngineWrite()))

	if byFile["bypass/bypass.go"] != 1 {
		t.Fatalf("синтетический обход не найден переписью; места записи: %v", byFile)
	}

	// Ведомость ПУСТА — обход не отсужен ничем.
	findings := adjudicateDoors(byFile, map[string]doorRuling{})
	if len(findings) == 0 {
		t.Fatal("гейт промолчал на месте записи в движок, не покрытом ни одним вердиктом — " +
			"ровно та форма, в которой обход и заводят")
	}
	joined := strings.Join(findings, "\n")
	if !strings.Contains(joined, "bypass/bypass.go") {
		t.Errorf("находка не называет координату обхода:\n%s", joined)
	}
	if !strings.Contains(joined, "ОБХОД БЕЗ ВЕРДИКТА") {
		t.Errorf("находка не названа своим родом:\n%s", joined)
	}
	t.Logf("покраснел как должен: %s", joined)
}

// TestJournalDoor_InjectionAdjudicatedWriteIsSilent — (б) МОЛЧИТ.
//
// ЗАКОННЫЙ БЛИЗНЕЦ той же формы: то же самое место записи, но отсуженное. Без
// этой половины «краснеет» означало бы «краснеет на всём», и первый же законный
// путь заставил бы гейт снять.
func TestJournalDoor_InjectionAdjudicatedWriteIsSilent(t *testing.T) {
	byFile := writeFilesOf(t, buildSynthetic(t, treeWithEngineWrite()))

	findings := adjudicateDoors(byFile, map[string]doorRuling{
		"bypass/bypass.go": {
			Verdict: doorJournalBacked, Places: 1,
			JournalAt: "repo/emit.go:EmitWriteTx",
			Why:       "синтетический законный путь: строка журнала со-коммичена вызывающим",
		},
	})
	if len(findings) != 0 {
		t.Errorf("гейт покраснел на ЗАКОННОЙ записи, отсуженной вердиктом — он ловит форму, "+
			"а не существо:\n  %s", strings.Join(findings, "\n  "))
	}
}

// TestJournalDoor_InjectionExtraWriteInAdjudicatedFileIsFound — (а) КРАСНЕЕТ.
//
// Число мест пиннится именно ради этого случая: обход, добавленный в файл,
// который УЖЕ отсужен, иначе прошёл бы молча — а это самый дешёвый способ
// завести его незаметно.
func TestJournalDoor_InjectionExtraWriteInAdjudicatedFileIsFound(t *testing.T) {
	files := treeWithEngineWrite()
	files["bypass/bypass.go"] += `
// PutAgain — ВТОРОЕ место записи в том же, уже отсуженном файле.
func PutAgain(ctx context.Context, w ports.Writer) error {
	return w.WriteTuples(ctx, []string{"user:c viewer obj:d"})
}
`
	byFile := writeFilesOf(t, buildSynthetic(t, files))

	findings := adjudicateDoors(byFile, map[string]doorRuling{
		"bypass/bypass.go": {
			Verdict: doorJournalBacked, Places: 1, // ведомость всё ещё знает ОДНО
			JournalAt: "repo/emit.go:EmitWriteTx",
			Why:       "синтетический законный путь",
		},
	})
	if len(findings) == 0 {
		t.Fatal("второе место записи в уже отсуженном файле прошло молча — " +
			"пиннинг числа не работает, и обход заводится одной строкой в «благословлённом» файле")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "ЧИСЛО МЕСТ РАЗОШЛОСЬ") {
		t.Errorf("находка не названа своим родом:\n%s", strings.Join(findings, "\n"))
	}
}

// TestJournalDoor_InjectionRulingWithoutAPlaceIsAFinding — САМОИСТЕЧЕНИЕ.
//
// Послабление, которому больше нечего исключать, — находка, а не норма: иначе
// его унаследует следующая слепая зона, и снимать его будет некому.
func TestJournalDoor_InjectionRulingWithoutAPlaceIsAFinding(t *testing.T) {
	findings := adjudicateDoors(map[string]int{}, map[string]doorRuling{
		"снятый/путь.go": {
			Verdict: doorException, Places: 1,
			Why:       "исключение, предмет которого снят",
			Predicate: "должно было истечь вместе с предметом",
		},
	})
	if len(findings) == 0 {
		t.Fatal("вердикт, которому не соответствует ни одно место, не признан находкой — " +
			"послабление переживёт свой предмет и никого не разбудит")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "ВЕРДИКТ БЕЗ МЕСТА") {
		t.Errorf("находка не названа своим родом:\n%s", strings.Join(findings, "\n"))
	}
}

// TestJournalDoor_InjectionExceptionWithoutPredicateIsAFinding — (а) КРАСНЕЕТ.
//
// Исключение без предиката снятия бессрочно, а бессрочное послабление — это
// «оставили как есть» под другим именем.
func TestJournalDoor_InjectionExceptionWithoutPredicateIsAFinding(t *testing.T) {
	findings := adjudicateDoors(map[string]int{"a/b.go": 1}, map[string]doorRuling{
		"a/b.go": {Verdict: doorException, Places: 1, Why: "потому что"},
	})
	if len(findings) == 0 {
		t.Fatal("исключение без предиката снятия принято молча")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "ИСКЛЮЧЕНИЕ БЕЗ ПРЕДИКАТА") {
		t.Errorf("находка не названа своим родом:\n%s", strings.Join(findings, "\n"))
	}
}

// TestJournalDoor_InjectionShellCommentStripperKeepsTheTuple — регрессия на
// СОБСТВЕННЫЙ дефект пробы, а не на дерево.
//
// Первая редакция срезала комментарий по первому `#` — и уничтожала ровно тот
// текст, ради которого читала файл: решётка стоит ВНУТРИ кортежа
// (`cluster:<id>#viewer@user:*`). Проба объявляла «вызовов записи не найдено» на
// файле, где их два. Спасло только то, что у неё была проверка предпосылки;
// без неё дефект зеленел бы как успех.
func TestJournalDoor_InjectionShellCommentStripperKeepsTheTuple(t *testing.T) {
	const body = "fga_write \"cluster:${ID}#viewer@user:*\" \"{}\"   # объяснение\n" +
		"# целиком комментарий с /stores/x/write внутри\n"

	got := shellExecutablePart(body)

	if !strings.Contains(got, "cluster:${ID}#viewer@user:*") {
		t.Errorf("решётка ВНУТРИ кортежа принята за комментарий — проба уничтожает предмет, "+
			"который читает:\n%q", got)
	}
	if strings.Contains(got, "объяснение") {
		t.Errorf("настоящий комментарий не срезан — гейт будет краснеть на объяснении рядом "+
			"с самой защитой:\n%q", got)
	}
	if strings.Contains(got, "целиком комментарий") {
		t.Errorf("строка-комментарий целиком не срезана:\n%q", got)
	}
}
