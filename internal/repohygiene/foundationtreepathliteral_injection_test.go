// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// foundationtreepathliteral_injection_test.go — доказательство способности гейта
// падать И молчать.
//
// Законный близнец подан по каждой оси, и он не декоративный: словарь корней —
// это ОБЫЧНЫЕ СЛОВА (`services`, `internal`, `deploy`), и запрет по слову краснел
// бы на прозе, поехавшей в текст отказа. Поэтому близнец каждой инъекции —
// литерал с тем же словом, координатой НЕ являющийся.

func lawfulCensus() treePathLiteralCensus {
	return treePathLiteralCensus{Packages: 68, ProdFiles: 169, Literals: 4292, Roots: 8}
}

func TestTreePathLiteralJudgeIsSilentWhenNoLiteralNamesAForeignRoot(t *testing.T) {
	t.Parallel()

	faults := judgeFoundationTreePathLiterals(nil, lawfulCensus())

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на чистом входе (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
}

// Инъекция ОДНОГО факта: прод-файл фундамента называет координату дерева края.
func TestTreePathLiteralJudgeCatchesAForeignTreeCoordinate(t *testing.T) {
	t.Parallel()

	faults := judgeFoundationTreePathLiterals([]treePathLiteral{{
		File:  "pkg/authz/catalogderive/catalog.go",
		Value: "gateway/internal/middleware/embed/permission_catalog.json",
		Root:  "gateway",
	}}, lawfulCensus())

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	for _, want := range []string{"catalog.go", "gateway/internal/middleware", "Исходов три"} {
		if !strings.Contains(faults[0], want) {
			t.Fatalf("находка не называет %q: %s", want, faults[0])
		}
	}
}

// Инъекция ОДНОГО факта: координата СВОЕГО пакета в форме дерева. Ломается она
// так же, и гейт обязан называть её тем же образом.
func TestTreePathLiteralJudgeCatchesTheFoundationsOwnTreeCoordinate(t *testing.T) {
	t.Parallel()

	faults := judgeFoundationTreePathLiterals([]treePathLiteral{{
		File:  "pkg/nameformdb/nameformdb.go",
		Value: "pkg/validate/nameform",
		Root:  "pkg",
	}}, lawfulCensus())

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d", len(faults))
	}
	if !strings.Contains(faults[0], "pkg/validate/nameform") {
		t.Fatalf("находка не называет координату: %s", faults[0])
	}
}

// Пустой обход — находка, а не зелёное.
func TestTreePathLiteralJudgeRefusesAnEmptyWalk(t *testing.T) {
	t.Parallel()

	faults := judgeFoundationTreePathLiterals(nil, treePathLiteralCensus{Roots: 8})
	if len(faults) == 0 {
		t.Fatal("пустой обход прошёл зелёным — «ноль координат» стало неотличимо от «ноль прочитанного»")
	}
	if !strings.Contains(faults[0], "обход пуст") {
		t.Fatalf("находка не назвала предмет: %s", faults[0])
	}
}

// Файлы прочитаны, литералов не осмотрено ни одного — разбор перестал их видеть.
func TestTreePathLiteralJudgeRefusesWhenNoLiteralWasEverSeen(t *testing.T) {
	t.Parallel()

	c := lawfulCensus()
	c.Literals = 0
	faults := judgeFoundationTreePathLiterals(nil, c)
	if len(faults) == 0 {
		t.Fatal("вход из 169 файлов без единого литерала прошёл зелёным")
	}
	if !strings.Contains(faults[0], "вакуумным") {
		t.Fatalf("находка не назвала предмет: %s", faults[0])
	}
}

// Пустой словарь корней — гейт не запрещает ничего.
func TestTreePathLiteralJudgeRefusesAnEmptyRootVocabulary(t *testing.T) {
	t.Parallel()

	c := lawfulCensus()
	c.Roots = 0
	faults := judgeFoundationTreePathLiterals(nil, c)
	if len(faults) == 0 {
		t.Fatal("пустой словарь корней прошёл зелёным")
	}
}

// Словарь корней ВЫВОДИТСЯ из карты классов, а не выписан.
func TestForbiddenRootsAreDerivedFromTheClassMap(t *testing.T) {
	t.Parallel()

	roots := forbiddenTreeRootsForFoundation()
	if len(roots) == 0 {
		t.Fatal("словарь корней пуст — гейт не запрещал бы ничего")
	}
	index := map[string]struct{}{}
	for _, r := range roots {
		index[r] = struct{}{}
	}
	// `pkg` добавляется сверх карты: он исчезает не как корень дерева, а как
	// приставка пакетов фундамента.
	if _, ok := index["pkg"]; !ok {
		t.Error("в словаре нет `pkg` — координата СВОЕГО пакета в форме дерева осталась бы невидимой")
	}
	// Каждый верхний корень карты обязан быть представлен своим ПЕРВЫМ сегментом:
	// `services/iam` и `services` — один корень дерева.
	for _, r := range foundationRoots {
		first := r.Prefix
		if i := strings.IndexByte(first, '/'); i >= 0 {
			first = first[:i]
		}
		if _, ok := index[first]; !ok {
			t.Errorf("корень %q объявлен картой классов и не попал в словарь запрета", first)
		}
	}
}

// Законный близнец распознавателя: слово корня БЕЗ слэша координатой не является.
func TestLiteralRecognizerIgnoresARootWordThatIsNotAPath(t *testing.T) {
	t.Parallel()

	roots := forbiddenTreeRootsForFoundation()
	for _, lawful := range []string{
		"services",
		"internal error",
		"deploy the stand first",
		"pkg",
		"a gateway timed out",
	} {
		if r, bad := literalNamesForbiddenRoot(lawful, roots); bad {
			t.Errorf("распознаватель принял за координату обычный текст %q (корень %q) — "+
				"первое же ложное срабатывание снимает гейт", lawful, r)
		}
	}
	// И обратная сторона: та же приставка СО слэшем — координата.
	if _, bad := literalNamesForbiddenRoot("services/iam/internal/x.go", roots); !bad {
		t.Error("настоящая координата не опознана — отрицание выше зеленело бы на всём")
	}
}
