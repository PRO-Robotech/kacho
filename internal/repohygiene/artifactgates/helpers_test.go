// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

// helpers_test.go — общие помощники обхода.
//
// Лежат в ТЕСТОВОМ файле намеренно: они нужны только гейтам, и в не-тестовом
// файле поехали бы в сборку продукта — вместе с запуском подпроцесса, который
// статический анализ справедливо считает поверхностью.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// repoRoot — поднимаемся от каталога теста до каталога с go.mod (корень репо).
//
// Копия соседского помощника — намеренная и дешёвая: связать пакеты ради шести
// строк значило бы завести между ними зависимость, которой нет по существу.
// Разойтись они не могут: предмет у обхода один — каталог с go.mod, и любое
// расхождение немедленно роняет оба пакета.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("не найден корень репозитория (каталог с go.mod)")
		}
		dir = parent
	}
}

// synthTrack делает синтетическое дерево видимым для git-индекса.
//
// Копия по той же причине, что и repoRoot: две строки git-команд, связывать
// пакеты ради которых дороже, чем повторить.
func synthTrack(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := gitenv.Command(root, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v в синтетическом дереве: %v\n%s", args, err, out)
		}
	}
}

// trackedTree — тестовая обёртка над составом дерева (`pkg/treecorpus`):
// отказ превращается в `t.Fatalf`, потому что гейт, не сумевший назвать дерево,
// обязан упасть, а не выдать «ноль находок».
//
// Копия соседской обёртки — намеренная и по той же причине, что repoRoot выше:
// сама раскладка (индекс git → файлы и каталоги-предки) живёт В ОДНОМ месте, в
// `treecorpus`, а двадцать строк перевода отказа в падение дешевле повторить,
// чем связывать два пакета гейтов друг с другом.
type trackedTree struct {
	*treecorpus.Tree
	// files и root — то же, что у Tree, полями: гейты обходят состав напрямую
	// и собирают пути от корня.
	files map[string]bool
	root  string
}

func newTrackedTree(t *testing.T, root string) *trackedTree {
	t.Helper()
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева %s: %v — гейт не может назвать дерево, о котором "+
			"он говорит, и обход диска вместо индекса читал бы игнорируемые "+
			"каталоги (рабочие копии агентов, отчёты прогонов). Это отказ, а не пропуск.",
			root, err)
	}
	return &trackedTree{Tree: tree, files: tree.Files(), root: tree.Root()}
}

func (tt *trackedTree) hasFile(rel string) bool { return tt.Tree.HasFile(rel) }

func (tt *trackedTree) count() int { return tt.Tree.Count() }
