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
			name: "МОЛЧИТ: сентинел — внутреннее значение ошибки, id не несёт",
			src: `package x` + "\n" + `import "errors"` + "\n" +
				`var ErrNotFound = errors.New("operation not found")`,
			found: false,
		},
		{
			name:  "МОЛЧИТ: соседнее сообщение той же полосы",
			src:   `package x` + "\n" + `func f() string { return "operation %s already completed" }`,
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
			name: "МОЛЧИТ: форма стоит в комментарии, объясняющем эту же защиту",
			src: `package x` + "\n" +
				`// Край писал «Operation %s not found» — с прописной, вопреки владельцу.` + "\n" +
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
			found, read := operationNotFoundProducers(dir, []string{p})
			if read != 1 {
				t.Fatalf("прочитано файлов %d, ожидался 1 — перепись беспредметна", read)
			}
			if got := len(found) > 0; got != c.found {
				t.Fatalf("дискриминатор ответил %v, ожидалось %v (находки: %v)", got, c.found, found)
			}
		})
	}
}

// TestOperationNotFoundCensusNamesTheSecondRecord — перепись на возвращённом
// дефекте даёт ДВЕ находки и называет координату каждой; на законном дереве —
// ровно одну.
//
// Это вторая половина инъекции: дискриминатор выше отвечает «узнаю ли я форму»,
// а здесь спрашивается «что напечатает отказ». Находка, не называющая места,
// стоит прогона и посылает искать не там (`testing.md` §«Гейт на класс», п. 8).
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
		`func alreadyDone(id string) string { return "operation %s already completed" }`+"\n"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	found, read := operationNotFoundProducers(dir, []string{producer, twin})
	if read != 2 {
		t.Fatalf("прочитано файлов %d, ожидалось 2", read)
	}
	if len(found) != 1 {
		t.Fatalf("на законном дереве найдено %d записей, ожидалась 1: %v", len(found), found)
	}
	if !strings.HasPrefix(found[0], "notfound.go:") {
		t.Fatalf("координата единственной записи не названа: %q", found[0])
	}

	// Возвращаем дефект: вторая запись того же текста, у края, с прописной буквы.
	edge := filepath.Join(dir, "proxy.go")
	if err := os.WriteFile(edge, []byte(`package opsproxy`+"\n"+
		`func notFound(id string) string { return "Operation %s not found" }`+"\n"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	found, read = operationNotFoundProducers(dir, []string{producer, twin, edge})
	if read != 3 {
		t.Fatalf("прочитано файлов %d, ожидалось 3", read)
	}
	if len(found) != 2 {
		t.Fatalf("возвращённый дефект даёт %d записей, ожидалось 2: %v", len(found), found)
	}
	joined := strings.Join(found, "\n")
	for _, want := range []string{"notfound.go:", "proxy.go:"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("отказ не называет координату %q:\n%s", want, joined)
		}
	}
}
