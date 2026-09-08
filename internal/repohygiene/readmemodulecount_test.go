// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// readmemodulecount_test.go — ЧИСЛО МОДУЛЕЙ В README ВЫВОДИТСЯ, А НЕ
// ВЫПИСЫВАЕТСЯ, и граница с вынесенной службой имеет адрес.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// README читается ПЕРВЫМ тем, кто клонирует репозиторий. Раздел о модулях Go
// объявлял «`github.com/PRO-Robotech/kacho` — ОДИН `go.mod` на весь
// репозиторий» и выводил из этого следствие: «исчезает вся polyrepo-церемония —
// pseudo-версии, запрет `replace`, `go.work`, пины sibling-веток».
//
// Ложным было и число, и вывод. Дерево несёт второй модуль (вынесенная служба),
// он резолвит платформу псевдоверсией, запрет `replace` действует и является
// нормой, а рабочее пространство — единственное средство локальной
// кросс-модульной правки.
//
// Класс тот же, что уже стоил корпусу отдельной врезки: у выписанного числа нет
// владельца. Оно стареет молча и утаскивает за собой выводы, сделанные из него,
// — а стареет тем незаметнее, чем дальше от него живёт предмет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ НАХОДКОЙ
//
//   - «число-не-выведено» — модулей в дереве больше одного, а README не называет
//     КОМАНДУ, которой перечень выводится. Тогда всякое сказанное там о числе
//     проверяется только чтением дерева, то есть не проверяется;
//   - «граница-без-адреса» — модулей больше одного, а README не адресует
//     ни одного документа дерева. Порядок пина, запрет `replace` и ловушка
//     «локально зелено — в конвейере красно» тогда не имеют места, куда послать
//     читателя, и будут пересказаны в README вторым местом об одном предмете;
//   - «ссылка-в-никуда» — относительная ссылка README, цели которой в составе
//     дерева нет: координата пережила свой предмет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ ГЕЙТ НЕ ДЕЛАЕТ — И ЭТО СКАЗАНО ПРЯМО
//
// Он НЕ судит прозу и не ищет выписанное число лексиконом. Такой детектор над
// естественным языком в этом корпусе уже строился и ПРОВАЛИЛ контроль в обе
// стороны: он не отличает утверждение от разбора этого же утверждения, поэтому
// краснел бы на врезке, объясняющей исправление. Гейт судит ФАКТЫ состава:
// названа ли команда, есть ли адрес, резолвится ли он.
//
// Следствие названо честно: README, назвавший команду и ВДОБАВОК выписавший
// число, гейт пропустит. Держит это обзор, а не он.
package repohygiene

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// readmeModuleFinding — одно расхождение README с составом дерева.
type readmeModuleFinding struct {
	Kind string // "нет-README" | "число-не-выведено" | "граница-без-адреса" | "ссылка-в-никуда"
	What string
	Why  string
}

// readmeLinkRe — относительная ссылка markdown. Внешние адреса и якоря
// отсеиваются после разбора, а не образцом: образец, знающий про «http», не
// знал бы про «mailto», и полоса протекала бы молча.
var readmeLinkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)

// readmeRelativeLinks — относительные цели ссылок README, приведённые к пути от
// корня дерева. Фрагмент срезается: он производится генератором сайта и из
// состава дерева не проверяется.
func readmeRelativeLinks(src string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range readmeLinkRe.FindAllStringSubmatch(src, -1) {
		t := strings.TrimSpace(m[1])
		if i := strings.IndexByte(t, '#'); i >= 0 {
			t = t[:i]
		}
		if t == "" || strings.HasPrefix(t, "/") {
			continue
		}
		if strings.Contains(t, "://") || strings.HasPrefix(t, "mailto:") {
			continue
		}
		t = path.Clean(t)
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// readmeNamesModuleListPredicate — называет ли README команду вывода перечня
// модулей. Судится СТРОКА, несущая обе части сразу (`ls-files` и `go.mod`):
// порознь каждая встречается по другому поводу, и полоса протекала бы.
func readmeNamesModuleListPredicate(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "ls-files") && strings.Contains(line, "go.mod") {
			return true
		}
	}
	return false
}

// checkReadmeModuleCount — ТЕЛО гейта. Вынесено, чтобы инъекция звала то же, что
// исполняется на дереве.
//
// `modules` — каталоги с `go.mod` в составе; `tracked` отвечает, лежит ли путь в
// составе дерева.
func checkReadmeModuleCount(readmePresent bool, readme string, modules []string, tracked func(rel string) bool) []readmeModuleFinding {
	var out []readmeModuleFinding

	if !readmePresent {
		return append(out, readmeModuleFinding{
			Kind: "нет-README",
			What: "README.md",
			Why: "первого документа, который читает клонирующий, в составе дерева нет: " +
				"вердикт о его утверждениях беспредметен",
		})
	}

	// Граница с вынесенной службой возникает вместе со ВТОРЫМ модулем. При одном
	// модуле предмета у двух осей ниже нет, и это ЗАКОННОЕ состояние дерева, а не
	// послабление: перепись называет его числом.
	if len(modules) > 1 {
		if !readmeNamesModuleListPredicate(readme) {
			out = append(out, readmeModuleFinding{
				Kind: "число-не-выведено",
				What: "перечень модулей",
				Why: "модулей в дереве больше одного, а README не называет команды, которой " +
					"перечень выводится: всякое сказанное там о числе не имеет владельца и " +
					"устареет молча — вместе с выводами, сделанными из него",
			})
		}
		if len(readmeRelativeLinks(readme)) == 0 {
			out = append(out, readmeModuleFinding{
				Kind: "граница-без-адреса",
				What: "README.md",
				Why: "модулей больше одного, а README не адресует ни одного документа дерева: " +
					"порядку пина, запрету `replace` и рабочему пространству негде жить, кроме " +
					"как вторым местом об одном предмете",
			})
		}
	}

	for _, target := range readmeRelativeLinks(readme) {
		if !tracked(target) {
			out = append(out, readmeModuleFinding{
				Kind: "ссылка-в-никуда",
				What: target,
				Why:  "относительная ссылка README ведёт по координате, которой в составе дерева нет",
			})
		}
	}
	return out
}

// TestReadmeDerivesTheModuleListInsteadOfWritingIt — гейт на дереве.
func TestReadmeDerivesTheModuleListInsteadOfWritingIt(t *testing.T) {
	root := repoRoot(t)
	tree := newTrackedTree(t, root)

	if tree.count() == 0 {
		t.Fatal("обход пуст: состав дерева не прочитан — вердикт беспредметен")
	}

	var modules []string
	for _, rel := range tree.Tree.SortedFiles() {
		if path.Base(rel) == "go.mod" {
			modules = append(modules, path.Dir(rel))
		}
	}
	sort.Strings(modules)

	// Предпосылка. «Модулей ноль» означает, что состав прочитан неверно: корневой
	// `go.mod` в этом дереве есть всегда.
	if len(modules) == 0 {
		t.Fatal("обход пуст: объявлений модуля в составе дерева не найдено ни одного — вердикт беспредметен")
	}

	readmePresent := tree.hasFile("README.md")
	src := ""
	if readmePresent {
		b, err := os.ReadFile(filepath.Join(root, "README.md"))
		if err != nil {
			t.Fatalf("README.md есть в составе, но не прочитан: %v — это отказ, а не пропуск", err)
		}
		src = string(b)
	}

	found := checkReadmeModuleCount(readmePresent, src, modules, tree.hasFile)

	links := readmeRelativeLinks(src)
	t.Logf("осмотрено: состав %d файлов -> модулей %d (%s); README: %s; "+
		"перечень выведен командой: %v; относительных ссылок %d; находок %d",
		tree.count(), len(modules), strings.Join(modules, ", "),
		map[bool]string{true: "есть", false: "НЕТ"}[readmePresent],
		readmeNamesModuleListPredicate(src), len(links), len(found))

	for _, f := range found {
		t.Errorf("%s %q — %s.\n"+
			"  ЧТО ДЕЛАТЬ: в README число модулей не выписывать. Назвать команду "+
			"(`git ls-files '*go.mod'`) и адресовать следствия документу дерева "+
			"(порядок пина, запрет `replace`, рабочее пространство).",
			f.Kind, f.What, f.Why)
	}
}
