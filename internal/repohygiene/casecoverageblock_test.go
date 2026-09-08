// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// casecoverageblock_test.go — блок состава в шапке модуля кейсов СХОДИТСЯ с тем,
// что модуль объявляет (#2207).
//
// # Предмет и почему он не закрывается машинным указателем
//
// Перечень в шапке ВЫПИСАН, а не выведен, поэтому не имеет владельца и стареет
// молча. Рядом живёт `docs/CASES-INDEX.md` — он выводится из дерева и держится
// своим сверщиком, — но описаний он не несёт by construction, и читают первым
// НЕ его: читатель модуля читает его шапку. Два места об одном предмете, из
// которых верно то, которое читают вторым.
//
// Замер, из которого гейт заведён: блок несут ДВА модуля из ста, расходятся ОБА.
//
// # Что гейт держит, а что — НЕТ
//
// Держит: перечень блока и множество объявленных совпадают, и совпадают в обе
// стороны (позиция, объявления не имеющая, — тоже находка: она переживёт снятый
// кейс). Печатает объём осмотренного, падает на пустом обходе.
//
// НЕ держит: правдивость описания против идентификатора — машинно не решается.
// Сказано вслух, чтобы «блок зелёный» не читалось шире сделанного.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, которое увидит свежий клон и CI.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// caseModules — модули кейсов newman по ИНДЕКСУ git.
func caseModules(t *testing.T, root string) []string {
	t.Helper()
	// Через помощника, а не голым `exec.Command`: `cmd.Dir` не выбирает
	// репозиторий, когда в окружении есть GIT_DIR.
	cmd := gitenv.Command(root, "ls-files", "--", "*/tests/newman/cases/*.py")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("индекс git не читается (%v) — обход беспредметен, а не пуст", err)
	}
	var mods []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			mods = append(mods, l)
		}
	}
	return mods
}

func TestCaseCoverageBlockMatchesWhatTheModuleDeclares(t *testing.T) {
	root := repoRoot(t)
	mods := map[string]string{}
	for _, rel := range caseModules(t, root) {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("модуль %s не читается: %v", rel, err)
		}
		mods[rel] = string(b)
	}

	findings, c := ScanCaseCoverage(mods)
	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("перепись: модулей кейсов %d, с блоком состава %d, позиций в блоках %d, "+
		"объявлений всего %d", c.Modules, c.Blocks, c.Listed, c.Declared)
}
