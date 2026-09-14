// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/contractroot"
	"github.com/PRO-Robotech/corelib/treecorpus"
)

// TestShellAndGoDeclareTheSameContractRoots — два объявления перечня корней
// СХОДЯТСЯ.
//
// Вторая копия неизбежна (оболочка не импортирует Go-пакет), но её расхождение
// с первой обязано краснеть: корень, известный одной стороне и неизвестный
// другой, даёт не отказ, а СУЖЕНИЕ обхода — то самое молчание, ради которого
// заведён скриптовый гейт (#2339, смежный предмет).
func TestShellAndGoDeclareTheSameContractRoots(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	files, err := treecorpus.UnderWithSuffix(root, ".sh", ".bash")
	if err != nil {
		t.Fatalf("состав дерева НЕ ИЗМЕРЕН: %v", err)
	}

	census := ContractRootParityCensus{GoRoots: contractroot.Roots}
	var (
		decls   []string
		lastSet []string
	)
	for _, p := range files {
		src, rerr := readFileString(p)
		if rerr != nil {
			continue
		}
		census.FilesRead++
		if !strings.Contains(src, "KACHO_PROTO_ROOTS") {
			continue
		}
		census.Mentions++
		set, ok := ParseShellContractRoots(src)
		if !ok {
			continue // упоминание без присваивания — обход, комментарий
		}
		census.Assignments++
		rel, _ := filepath.Rel(root, p)
		decls = append(decls, filepath.ToSlash(rel))
		lastSet = set
	}
	census.ShellRoots = lastSet

	if census.FilesRead == 0 {
		t.Fatalf("файлов оболочки прочитано 0 — вердикт беспредметен (%s)", census)
	}
	if census.Mentions == 0 {
		t.Fatalf("имя KACHO_PROTO_ROOTS не встречено НИ РАЗУ при %d файлах: перечень "+
			"переехал либо переименован, и молчание пробы ничего не означает (%s)",
			census.FilesRead, census)
	}
	// Ноль присваиваний при непустых упоминаниях — ОТКАЗ, а не зелёное: значит
	// перечень записан в форме, которой распознаватель не знает, и сверять было
	// нечего. «Ноль находок» здесь неотличимо от «ноль прочитанного».
	if census.Assignments == 0 {
		t.Fatalf("имя упомянуто %d раз, а ПРИСВАИВАНИЙ распознано 0: перечень записан "+
			"в форме, которой проба не знает — сверять было нечего (%s)",
			census.Mentions, census)
	}
	// Два присваивания — уже расхождение по построению: у перечня появилось два
	// дома, и какой из них читает генератор, решает порядок подключения.
	if census.Assignments > 1 {
		t.Errorf("перечень корней ПРИСВАИВАЕТСЯ в %d местах (%s): у него два дома, и "+
			"они разойдутся молча", census.Assignments, strings.Join(decls, ", "))
	}

	if len(census.ShellRoots) == 0 {
		t.Fatalf("перечень оболочки ПУСТ: пустое множество корней означает пустую "+
			"популяцию у каждого читателя (%s)", census)
	}
	if !sameStringSet(census.ShellRoots, contractroot.Roots) {
		t.Errorf("объявления перечня корней РАЗОШЛИСЬ: оболочка (%s) объявляет %v, "+
			"pkg/contractroot.Roots — %v. Корень, известный одной стороне и "+
			"неизвестной другой, даёт не отказ, а СУЖЕНИЕ обхода: дерево этого "+
			"корня перестаёт рассматриваться МОЛЧА",
			strings.Join(decls, ", "), census.ShellRoots, contractroot.Roots)
	}
	t.Logf("перепись: %s", census)
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		seen[x]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// ── ИНЪЕКЦИЯ ────────────────────────────────────────────────────────────────

// TestInjectionParity_DivergentSetIsDetected — расхождение перечней ловится.
func TestInjectionParity_DivergentSetIsDetected(t *testing.T) {
	t.Parallel()
	got, ok := ParseShellContractRoots("KACHO_PROTO_ROOTS=(kacho)\n")
	if !ok {
		t.Fatal("присваивание не распознано — проба вакуумна")
	}
	if sameStringSet(got, []string{"kacho", "kaname"}) {
		t.Fatal("расхождение перечней не замечено: сверка не сработала")
	}
}

// TestInjectionParity_SameSetInAnyOrderIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ: тот же
// набор в другом порядке расхождением НЕ является.
//
// Порядок значим для устойчивости вывода, но не для того, ЧТО обходится;
// краснеть на перестановке значило бы ловить написание, а не существо.
func TestInjectionParity_SameSetInAnyOrderIsSilent(t *testing.T) {
	t.Parallel()
	got, ok := ParseShellContractRoots("KACHO_PROTO_ROOTS=(kaname kacho)\n")
	if !ok {
		t.Fatal("присваивание не распознано")
	}
	if !sameStringSet(got, []string{"kacho", "kaname"}) {
		t.Fatal("перестановка объявлена расхождением — проба ловит написание")
	}
}

// TestInjectionParity_MentionWithoutAssignmentIsNotADeclaration — УПОМИНАНИЕ не
// является объявлением.
//
// Ось несущая: имя встречается в комментариях, объясняющих сам перечень, и в
// обходах `"${KACHO_PROTO_ROOTS[@]}"`. Проверка по подстроке покраснела бы на
// собственном объяснении — тот самый класс, который корпус ловит.
func TestInjectionParity_MentionWithoutAssignmentIsNotADeclaration(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"# KACHO_PROTO_ROOTS — объявленное закрытое множество корней\n",
		"for r in \"${KACHO_PROTO_ROOTS[@]}\"; do echo \"$r\"; done\n",
		"echo \"(${KACHO_PROTO_ROOTS[*]})\"\n",
	} {
		if _, ok := ParseShellContractRoots(src); ok {
			t.Errorf("упоминание принято за объявление: %q", strings.TrimSpace(src))
		}
	}
}

// TestInjectionParity_QuotedFormIsParsed — кавычки вокруг элементов законны.
func TestInjectionParity_QuotedFormIsParsed(t *testing.T) {
	t.Parallel()
	got, ok := ParseShellContractRoots("KACHO_PROTO_ROOTS=(\"kacho\" 'kaname')\n")
	if !ok || !sameStringSet(got, []string{"kacho", "kaname"}) {
		t.Fatalf("форма с кавычками не разобрана: %v ok=%v — форма вне наблюдения", got, ok)
	}
}
