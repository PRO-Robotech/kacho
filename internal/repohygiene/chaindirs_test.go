// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
)

// gateChainDirs — каталоги цепочек миграций, которые обходит гейт.
//
// Настоящее дерево — у migrationchains.List по индексу git (migrationDirs:
// печать числа цепочек по services/notify, красный на нуле). Синтетическое
// дерево во временном каталоге — у migrationchains.FromTree по его составу на
// диске; синтетика, не заведшая ни одной точки наката, — раскладка самой
// фикстуры `services/<svc>/internal/migrations`: её цепочки объявлены путём,
// который фикстура и кладёт, и точка наката предметом такой пробы не является.
func gateChainDirs(t *testing.T, root string) []chainDir {
	t.Helper()
	if sameDir(t, root, repoRoot(t)) {
		return migrationDirs(t, root)
	}
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("синтетическое дерево %s: %v", root, err)
	}
	if hasMigratorPoint(tree) {
		chains, cerr := migrationchains.FromTree(tree)
		if cerr != nil {
			t.Fatalf("перечень цепочек синтетического дерева: %v", cerr)
		}
		out := make([]chainDir, 0, len(chains))
		for _, c := range chains {
			label := c.Service
			if c.Database != "" {
				label += "/" + c.Database
			}
			out = append(out, chainDir{Service: c.Service, Label: label, Dir: filepath.Join(root, filepath.FromSlash(c.Dir))})
		}
		return out
	}
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("синтетическое дерево %s без каталога services: %v", root, err)
	}
	var out []chainDir
	for _, e := range entries {
		dir := filepath.Join(root, "services", e.Name(), "internal", "migrations")
		if st, serr := os.Stat(dir); e.IsDir() && serr == nil && st.IsDir() {
			out = append(out, chainDir{Service: e.Name(), Label: e.Name(), Dir: dir})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// hasMigratorPoint — заведена ли в составе хоть одна точка наката.
func hasMigratorPoint(tree *treecorpus.Tree) bool {
	for f := range tree.Files() {
		if m, _ := filepath.Match("services/*/cmd/migrator/main.go", f); m {
			return true
		}
	}
	return false
}

// sameDir — один ли это каталог.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	aa, err := filepath.Abs(a)
	if err != nil {
		t.Fatalf("абсолютный путь %s: %v", a, err)
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		t.Fatalf("абсолютный путь %s: %v", b, err)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}
