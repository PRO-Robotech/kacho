// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retiredvendorexceptions_injection_test.go — перечень исключений имени снятого
// поставщика краснеет на каждом своём дефекте и молчит на законном близнеце той
// же формы (задача #1276). Вход — синтетическое дерево в памяти: фикстура,
// привязанная к живой записи, истекла бы вместе с ней.
//
// Имя поставщика в этом файле не пишется ни разу: оно берётся у единственного
// дома отметок во время прогона. Файл лежит в области предиката, и строка с
// именем в нём была бы находкой самого гейта.
package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/identityvendor"
)

// vendorExceptionWorld — синтетическое дерево и его перечень: страж с двумя
// строками имени, его проба с одной, файл без имени, проза вне области с именем.
type vendorExceptionWorld struct {
	files  map[string]string
	ledger []retiredVendorException
}

func newVendorExceptionWorld(t *testing.T) *vendorExceptionWorld {
	t.Helper()
	m := identityvendor.Marks()
	if len(m) == 0 {
		t.Fatal("отметок имени ноль — синтетику строить не из чего")
	}
	name := m[0]
	return &vendorExceptionWorld{
		files: map[string]string{
			"internal/g/guard.go": "package g\n\nfunc JudgeReturn() {}\n\nvar words = []string{\"" + name + "\"}\n" +
				"// возврат " + strings.ToUpper(name[:1]) + name[1:] + " — находка\n",
			"internal/g/guard_injection_test.go": "package g\n\nfunc TestReturnIsFound() { _ = \"" + name + "-public\" }\n",
			"internal/g/other.go":                "package g\n\n// гидра — другой референт, кириллица\nvar x = 1\n",
			"docs/history.md":                    "# история\n\n" + name + " был поставщиком\n",
			"deploy/scripts/live.sh":             "#!/usr/bin/env bash\necho ok\n",
		},
		ledger: []retiredVendorException{
			{Path: "internal/g/guard.go", Lines: 2, Kind: retiredVendorExceptionGuard,
				Anchor: "func JudgeReturn(", Why: "отвергает возврат"},
			{Path: "internal/g/guard_injection_test.go", Lines: 1, Kind: retiredVendorExceptionGuardProbe,
				Anchor: "func TestReturnIsFound(", Guard: "internal/g/guard.go", Why: "инъекция стража"},
		},
	}
}

func (w *vendorExceptionWorld) name() string { return identityvendor.Marks()[0] }

func (w *vendorExceptionWorld) judge(t *testing.T) ([]string, retiredVendorExceptionCensus) {
	t.Helper()
	tree := retiredVendorExceptionTree{Tracked: map[string]bool{}, Text: map[string][]byte{}}
	for p, s := range w.files {
		tree.Tracked[p] = true
		tree.Text[p] = []byte(s)
	}
	found, c, err := judgeRetiredVendorExceptions(w.ledger, identityvendor.Marks(), tree)
	if err != nil {
		t.Fatalf("суд синтетики: %v", err)
	}
	return found, c
}

// mustRedWith — ровно одна находка, и она называет причину, а не симптом.
func mustRedWith(t *testing.T, found []string, wants ...string) {
	t.Helper()
	if len(found) != 1 {
		t.Fatalf("находок %d, ждали ровно одну:\n%s", len(found), strings.Join(found, "\n"))
	}
	for _, w := range wants {
		if !strings.Contains(found[0], w) {
			t.Fatalf("находка не называет %q — она называет симптом, а не причину:\n%s", w, found[0])
		}
	}
}

// Контроль: законное дерево молчит, и перепись называет прочитанное числом.
func TestRetiredVendorExceptions_LawfulWorldIsSilentAndCounted(t *testing.T) {
	t.Parallel()
	w := newVendorExceptionWorld(t)
	found, c := w.judge(t)
	if len(found) != 0 {
		t.Fatalf("законное дерево дало находки:\n%s", strings.Join(found, "\n"))
	}
	if c.ScopeFiles != 4 || c.MarkedFiles != 2 || c.MarkedLines != 3 || c.Covered != 3 || c.Outside != 0 {
		t.Fatalf("перепись законного дерева неверна: %s — ждали файлов в области 4, с отметкой 2, "+
			"строк 3, под записями 3, вне 0 (проза docs/ — вне области)", c)
	}
}

// Строка с именем в файле без записи — находка с координатой и текстом.
func TestRetiredVendorExceptions_LineOutsideTheListIsFound(t *testing.T) {
	t.Parallel()
	w := newVendorExceptionWorld(t)
	w.files["internal/g/other.go"] += "// прежде здесь стоял " + w.name() + "\n"
	found, c := w.judge(t)
	mustRedWith(t, found, "internal/g/other.go:5:", "вне перечня исключений", w.name())
	if c.Outside != 1 {
		t.Fatalf("строк вне перечня %d, ждали 1: %s", c.Outside, c)
	}
}

// Прописными, в составе слова, в развёртывании вне Go — та же находка.
func TestRetiredVendorExceptions_EveryWritingFormInScopeIsFound(t *testing.T) {
	t.Parallel()
	for _, form := range []struct{ path, text string }{
		{"internal/g/other.go", "const k = \"KACHO_" + strings.ToUpper(identityvendor.Marks()[0]) + "_URL\"\n"},
		{"deploy/scripts/live.sh", "kubectl port-forward svc/kacho-umbrella-" + identityvendor.Marks()[0] + "-public 1:2\n"},
		{"deploy/helm/x/values.yaml", "image: " + identityvendor.Marks()[len(identityvendor.Marks())-1] + "x:1\n"},
	} {
		w := newVendorExceptionWorld(t)
		w.files[form.path] += form.text
		found, _ := w.judge(t)
		mustRedWith(t, found, form.path+":", "вне перечня исключений")
	}
}

// Двоичный файл с именем в байтах — одна единица, как у git.
func TestRetiredVendorExceptions_BinaryFileWithTheNameIsFound(t *testing.T) {
	t.Parallel()
	w := newVendorExceptionWorld(t)
	w.files["deploy/helm/charts/x.tgz"] = "\x00\x01" + w.name() + "\x00"
	found, c := w.judge(t)
	mustRedWith(t, found, "deploy/helm/charts/x.tgz:0:", "двоичный")
	if c.BinaryMarked != 1 || c.Outside != 1 {
		t.Fatalf("двоичный файл не посчитан одной единицей: %s", c)
	}
}

// Число записи точное: рост и убыль — находка обе.
func TestRetiredVendorExceptions_CountDriftIsFoundBothWays(t *testing.T) {
	t.Parallel()
	grown := newVendorExceptionWorld(t)
	grown.files["internal/g/guard.go"] += "var again = \"" + grown.name() + "\"\n"
	found, _ := grown.judge(t)
	mustRedWith(t, found, "internal/g/guard.go: в перечне 2 строк, в файле 3", "ТОЧНО")

	shrunk := newVendorExceptionWorld(t)
	shrunk.ledger[0].Lines = 3
	found, _ = shrunk.judge(t)
	mustRedWith(t, found, "internal/g/guard.go: в перечне 3 строк, в файле 2")
}

// Исключение без предмета истекает: файла нет либо имени в нём нет.
func TestRetiredVendorExceptions_EntryWithNothingToExcludeIsFound(t *testing.T) {
	t.Parallel()
	gone := newVendorExceptionWorld(t)
	delete(gone.files, "internal/g/guard_injection_test.go")
	found, _ := gone.judge(t)
	mustRedWith(t, found, "internal/g/guard_injection_test.go: исключению нечего исключать — файла в индексе нет")

	clean := newVendorExceptionWorld(t)
	clean.files["internal/g/guard_injection_test.go"] = "package g\n\nfunc TestReturnIsFound() {}\n"
	found, _ = clean.judge(t)
	mustRedWith(t, found, "internal/g/guard_injection_test.go: исключению нечего исключать — отметки имени в файле нет")
}

// Вид вне словаря, якорь не на месте, проба без стража, повтор, путь вне области.
func TestRetiredVendorExceptions_MalformedEntryIsFound(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		edit  func(w *vendorExceptionWorld)
		wants []string
	}{
		{"вид вне словаря", func(w *vendorExceptionWorld) { w.ledger[0].Kind = "синтетика" }, []string{"вне закрытого словаря"}},
		{"якорь не стоит", func(w *vendorExceptionWorld) { w.ledger[0].Anchor = "func Gone(" }, []string{"якорь", "другое содержимое"}},
		{"проба без стража", func(w *vendorExceptionWorld) { w.ledger[1].Guard = "" }, []string{"не называет стража"}},
		{"страж пробы снят", func(w *vendorExceptionWorld) { w.ledger[1].Guard = "internal/g/gone.go" }, []string{"которого в индексе нет"}},
		{"у стража поле пробы", func(w *vendorExceptionWorld) { w.ledger[0].Guard = "internal/g/other.go" }, []string{"поля Guard нет"}},
		{"довода нет", func(w *vendorExceptionWorld) { w.ledger[0].Why = " " }, []string{"довода нет"}},
		{"повтор", func(w *vendorExceptionWorld) { w.ledger = append(w.ledger, w.ledger[0]) }, []string{"повторена"}},
		{"путь вне области", func(w *vendorExceptionWorld) {
			w.ledger = append(w.ledger, retiredVendorException{Path: "docs/history.md", Lines: 1,
				Kind: retiredVendorExceptionDictionary, Anchor: "# история", Why: "проза"})
		}, []string{"docs/history.md: путь вне области"}},
	} {
		w := newVendorExceptionWorld(t)
		c.edit(w)
		found, _ := w.judge(t)
		if len(found) != 1 {
			t.Fatalf("%s: находок %d, ждали одну:\n%s", c.name, len(found), strings.Join(found, "\n"))
		}
		for _, want := range c.wants {
			if !strings.Contains(found[0], want) {
				t.Fatalf("%s: находка не называет %q:\n%s", c.name, want, found[0])
			}
		}
	}
}

// Законные близнецы той же формы молчат: кириллица другого референта, имя в
// прозе вне области, имя, разорванное разделителем.
func TestRetiredVendorExceptions_LawfulTwinsAreSilent(t *testing.T) {
	t.Parallel()
	n := identityvendor.Marks()[0]
	broken := n[:2] + "-" + n[2:]
	w := newVendorExceptionWorld(t)
	w.files["internal/g/other.go"] += "// " + broken + " — не имя\n// гидратация состояния\n"
	w.files["docs/history.md"] += n + " снова в прозе\n"
	found, c := w.judge(t)
	if len(found) != 0 {
		t.Fatalf("законные близнецы дали находки:\n%s", strings.Join(found, "\n"))
	}
	if c.MarkedLines != 3 {
		t.Fatalf("близнецы сдвинули перепись: %s", c)
	}
}

// Без отметок судить нечем: отказ, а не «вне перечня 0».
func TestRetiredVendorExceptions_NoMarksIsARefusal(t *testing.T) {
	t.Parallel()
	_, _, err := judgeRetiredVendorExceptions(nil, nil, retiredVendorExceptionTree{})
	if err == nil {
		t.Fatal("суд без отметок вернул вердикт — пустой распознаватель прочитан как «имени нет»")
	}
}
