// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// operationnotfoundproducer_injection_test.go — доказательство, что гейт
// единственного производителя умеет КРАСНЕТЬ и умеет МОЛЧАТЬ.
//
// Проверенный только на зелёном, он неотличим от гейта, который ничего не
// читает; проверенный только на красном — от того, который краснеет на всём.
// Обе стороны нужны здесь особенно: дискриминатор обязан отличать текст ДЛЯ
// ВЫЗЫВАЮЩЕГО от внутрипроцессного сентинела и от соседних сообщений той же
// полосы, а перепись — называть КООРДИНАТУ второй записи, иначе находка
// посылает читателя искать не там.
//
// Инъекция прогоняется по КАЖДОЙ форме перечня, а не по одной наблюдённой, и
// после всякого переустройства распознавателя — заново, включая формы, которые
// он умел прежде: перепись, сошедшаяся с прежней, доказывает, что не сузился
// предмет, и не говорит ничего о сохранности способности падать
// (`testing.md` §«Гейт на класс», п. 8).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOperationNotFoundDiscriminatorKnowsEveryLegalForm — распознаватель против
// каждой формы, в которой предмет записывается законно, и против каждого
// близнеца, которого он трогать не вправе.
//
// У каждой ловимой формы есть законный близнец ТОЙ ЖЕ формы записи: отрицание,
// проверенное только на литералах, зеленело бы на всякой склейке — то есть
// ровно там, где заведена эта половина.
func TestOperationNotFoundDiscriminatorKnowsEveryLegalForm(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		found bool
	}{
		{
			name:  "ЛОВИТСЯ: канон владельца",
			src:   `package x` + "\n" + `func f() string { return "operation %s not found" }`,
			found: true,
		},
		{
			name:  "ЛОВИТСЯ: та же форма с прописной — расхождение, ради которого гейт заведён",
			src:   `package x` + "\n" + `func f() string { return "Operation %s not found" }`,
			found: true,
		},
		{
			name:  "ЛОВИТСЯ: подстановка %q",
			src:   `package x` + "\n" + `func f() string { return "operation %q not found" }`,
			found: true,
		},
		{
			name:  "ЛОВИТСЯ: подстановка %v",
			src:   `package x` + "\n" + `func f() string { return "operation %v not found" }`,
			found: true,
		},
		{
			name:  "ЛОВИТСЯ: склейка — тот же текст, в обзоре изменения неотличима от обычного кода",
			src:   `package x` + "\n" + `func f(id string) string { return "operation " + id + " not found" }`,
			found: true,
		},
		{
			name:  "ЛОВИТСЯ: склейка с прописной",
			src:   `package x` + "\n" + `func f(id string) string { return "Operation " + id + " not found" }`,
			found: true,
		},
		{
			name: "ЛОВИТСЯ: склейка со вложенным fmt.Sprintf",
			src: `package x` + "\n" + `import "fmt"` + "\n" +
				`func f(id string) string { return "operation " + fmt.Sprintf("%s", id) + " not found" }`,
			found: true,
		},
		{
			name: "ЛОВИТСЯ: склейка с константой того же файла",
			src: `package x` + "\n" + `const notFoundSuffix = " not found"` + "\n" +
				`func f(id string) string { return "operation " + id + notFoundSuffix }`,
			found: true,
		},
		{
			name:  "ЛОВИТСЯ: склейка в скобках — скобки не заводят второй записи",
			src:   `package x` + "\n" + `func f(id string) string { return ("operation " + id) + " not found" }`,
			found: true,
		},
		{
			name: "МОЛЧИТ: сентинел — внутреннее значение ошибки, id не несёт",
			src: `package x` + "\n" + `import "errors"` + "\n" +
				`var ErrNotFound = errors.New("operation not found")`,
			found: false,
		},
		{
			name:  "МОЛЧИТ: сентинел, собранный склейкой — подстановки по-прежнему нет",
			src:   `package x` + "\n" + `func f() string { return "operation" + " not found" }`,
			found: false,
		},
		{
			name:  "МОЛЧИТ: соседнее сообщение той же полосы",
			src:   `package x` + "\n" + `func f() string { return "operation %s already completed" }`,
			found: false,
		},
		{
			name:  "МОЛЧИТ: соседнее сообщение той же полосы, собранное склейкой",
			src:   `package x` + "\n" + `func f(id string) string { return "operation " + id + " already completed" }`,
			found: false,
		},
		{
			name:  "МОЛЧИТ: отказ формата id — другая полоса",
			src:   `package x` + "\n" + `func f() string { return "invalid operation id %q" }`,
			found: false,
		},
		{
			name:  "МОЛЧИТ: чужой ресурс той же контракт-формы",
			src:   `package x` + "\n" + `func f() string { return "Network %s not found" }`,
			found: false,
		},
		{
			name:  "МОЛЧИТ: чужой ресурс той же контракт-формы, собранный склейкой",
			src:   `package x` + "\n" + `func f(id string) string { return "Network " + id + " not found" }`,
			found: false,
		},
		{
			name: "МОЛЧИТ: форма стоит в комментарии, объясняющем эту же защиту",
			src: `package x` + "\n" +
				`// Край писал «Operation %s not found» — с прописной, вопреки владельцу.` + "\n" +
				`func f() string { return "" }`,
			found: false,
		},
		{
			name: "МОЛЧИТ: склейка, набранная в комментарии рядом со своим объяснением",
			src: `package x` + "\n" +
				"// Тихая вторая запись выглядела так: \"operation \" + id + \" not found\".\n" +
				`func f() string { return "" }`,
			found: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "synthetic.go")
			if err := os.WriteFile(p, []byte(c.src), 0o600); err != nil {
				t.Fatalf("подготовка: %v", err)
			}
			found, census := operationNotFoundProducers(dir, []string{p})
			if census.filesRead != 1 {
				t.Fatalf("прочитано файлов %d, ожидался 1 — перепись беспредметна", census.filesRead)
			}
			if got := len(found) > 0; got != c.found {
				t.Fatalf("дискриминатор ответил %v, ожидалось %v (находки: %v · %s)",
					got, c.found, found, census)
			}
		})
	}
}

// TestOperationNotFoundConcatIsJudgedOnceWholeAndCounted — склейка судится ЦЕЛИКОМ
// и один раз, а перепись её видит.
//
// Без этого «ровно один» ломалось бы дважды и по-разному: составная склейка
// `a + b + c` — это два узла, и суд над обоими дал бы ДВЕ координаты одному
// месту (ложное «производителей 2» на исправном дереве); а склейка, которую
// перепись не считает, была бы вне наблюдения молча.
func TestOperationNotFoundConcatIsJudgedOnceWholeAndCounted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "synthetic.go")
	src := `package x` + "\n" +
		`func f(id string) string { return "operation " + id + " not found" }` + "\n"
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	found, census := operationNotFoundProducers(dir, []string{p})
	if len(found) != 1 {
		t.Fatalf("склейка дала %d находок, ожидалась 1: %v", len(found), found)
	}
	if !strings.Contains(found[0], `"operation %v not found"`) {
		t.Fatalf("находка не называет ФОРМУ выражения: %q", found[0])
	}
	if census.concats != 1 {
		t.Fatalf("склеек на суде %d, ожидалась 1 — вложенные судятся вместо внешней либо не судятся вовсе (%s)",
			census.concats, census)
	}
	if census.literals != 2 {
		t.Fatalf("литералов на суде %d, ожидалось 2 — перепись литералов сбилась вместе со склейками (%s)",
			census.literals, census)
	}
}

// TestOperationNotFoundCensusCountsWhatItJudges — перепись растёт ровно от того,
// что распознаватель начал осматривать.
//
// Расширение, не изменившее осмотренного, холостое; изменившее только находки —
// не отличимо от регрессии дерева (`testing.md` §«Гейт на класс», п. 7).
func TestOperationNotFoundCensusCountsWhatItJudges(t *testing.T) {
	dir := t.TempDir()

	plain := filepath.Join(dir, "plain.go")
	if err := os.WriteFile(plain, []byte(`package x`+"\n"+
		`func f() string { return "ничего интересного" }`+"\n"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	_, base := operationNotFoundProducers(dir, []string{plain})
	if base.concats != 0 || base.literals != 1 {
		t.Fatalf("на файле без склеек перепись сбилась: %s", base)
	}

	withConcat := filepath.Join(dir, "concat.go")
	if err := os.WriteFile(withConcat, []byte(`package x`+"\n"+
		`func g(a string) string { return "часть " + a + " хвост" }`+"\n"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	_, grown := operationNotFoundProducers(dir, []string{plain, withConcat})
	if grown.concats <= base.concats {
		t.Fatalf("склейка добавлена, а осмотренное не выросло: было %s, стало %s", base, grown)
	}
}

// TestOperationNotFoundCensusNamesTheSecondRecord — перепись на возвращённом
// дефекте даёт ДВЕ находки и называет координату каждой; на законном дереве —
// ровно одну.
//
// Это вторая половина инъекции: дискриминатор выше отвечает «узнаю ли я форму»,
// а здесь спрашивается «что напечатает отказ». Находка, не называющая места,
// стоит прогона и посылает искать не там (`testing.md` §«Гейт на класс», п. 8).
//
// Возвращаемый дефект — ТИХИЙ: вторая запись собрана склейкой и даёт побайтово
// тот же текст, что владелец. Именно такую вторую запись прежний распознаватель
// не видел, отвечая «производителей найдено: 1» над деревом, где их два.
func TestOperationNotFoundCensusNamesTheSecondRecord(t *testing.T) {
	dir := t.TempDir()

	producer := filepath.Join(dir, "notfound.go")
	if err := os.WriteFile(producer, []byte(`package operations`+"\n"+
		`func NotFoundStatus(id string) string { return "operation %s not found" }`+"\n"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	// Законный сосед: та же полоса, другой предмет. Он обязан молчать, иначе
	// «ровно один» зеленело бы только на дереве без соседей.
	twin := filepath.Join(dir, "twin.go")
	if err := os.WriteFile(twin, []byte(`package operations`+"\n"+
		`func alreadyDone(id string) string { return "operation " + id + " already completed" }`+"\n"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	found, census := operationNotFoundProducers(dir, []string{producer, twin})
	if census.filesRead != 2 {
		t.Fatalf("прочитано файлов %d, ожидалось 2", census.filesRead)
	}
	if census.concats != 1 {
		t.Fatalf("склейка соседа не попала на суд: %s", census)
	}
	if len(found) != 1 {
		t.Fatalf("на законном дереве найдено %d записей, ожидалась 1: %v", len(found), found)
	}
	if !strings.HasPrefix(found[0], "notfound.go:") {
		t.Fatalf("координата единственной записи не названа: %q", found[0])
	}

	// Возвращаем дефект: вторая запись того же текста, у края, собранная
	// склейкой — та самая форма, что прежде уходила из-под наблюдения.
	edge := filepath.Join(dir, "proxy.go")
	if err := os.WriteFile(edge, []byte(`package opsproxy`+"\n"+
		`func notFoundLocal(id string) error { return status.Error(codes.NotFound, "operation "+id+" not found") }`+"\n"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	found, census = operationNotFoundProducers(dir, []string{producer, twin, edge})
	if census.filesRead != 3 {
		t.Fatalf("прочитано файлов %d, ожидалось 3", census.filesRead)
	}
	if len(found) != 2 {
		t.Fatalf("возвращённый дефект даёт %d записей, ожидалось 2: %v (%s)", len(found), found, census)
	}
	joined := strings.Join(found, "\n")
	for _, want := range []string{"notfound.go:", "proxy.go:"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("отказ не называет координату %q:\n%s", want, joined)
		}
	}
}
