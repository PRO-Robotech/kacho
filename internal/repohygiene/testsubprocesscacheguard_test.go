// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// testsubprocesscacheguard_test.go — гейт по ЖИВОМУ дереву: ни одна проба,
// чей вердикт строит внешний инструмент подпроцессом, не отвечает на прогоне,
// результат которого `go test` положит в кеш.
//
// Разбор предмета, границы обхода и словарь — testsubprocesscacheguard.go.
package repohygiene_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"

	"github.com/PRO-Robotech/kacho/internal/cachedverdict"
	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// subprocessGuardRoot — корень дерева, выведенный от РАСПОЛОЖЕНИЯ ЭТОГО ФАЙЛА,
// а не от текущего каталога.
//
// Разница не умозрительная. Соседний помощник пакета (`repoRootFor`,
// treeroot_test.go) берёт `filepath.Abs("../..")`, то есть корень от рабочего
// каталога процесса. Пока пробу зовёт `go test` из этого клона, обе формы дают
// одно и то же; но двоичный файл пробы, запущенный из ЧУЖОЙ рабочей копии,
// второй формой судит ЧУЖОЕ дерево и выходит нулём — молча, потому что чужое
// дерево тоже бывает чистым. Форма `runtime.Caller` привязана к файлу
// исходника: где лежит этот файл, то дерево и судится.
//
// Ненайденный корень — ОТКАЗ, а не пустая строка: проба, не открывшая дерева,
// не доказала ничего и не имеет права быть неотличимой от чистой.
func subprocessGuardRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller не назвал файл этой пробы — дерево не было открыто вовсе")
	}
	dir := filepath.Dir(self)
	for range 12 {
		_, gerr := os.Stat(filepath.Join(dir, ".github", "workflows"))
		_, serr := os.Stat(filepath.Join(dir, "services"))
		_, merr := os.Stat(filepath.Join(dir, "go.mod"))
		if gerr == nil && serr == nil && merr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("над %s корня репозитория нет — осмотрено ноль файлов", filepath.Dir(self))
	return ""
}

// TestProbesReachingAnExternalToolRefuseACacheableRun — гейт.
func TestProbesReachingAnExternalToolRefuseACacheableRun(t *testing.T) {
	t.Parallel()
	// Гейт сам берёт состав дерева подпроцессом (`git ls-files` внутри
	// treecorpus) — на кэшируемом прогоне его собственный вердикт недействителен
	// ровно по тому же основанию, которое он судит у других.
	if msg := cachedverdict.SubprocessRefusal("git ls-files"); msg != "" {
		t.Fatal(msg)
	}

	root := subprocessGuardRoot(t)
	paths, err := treecorpus.UnderWithSuffix(root, "_test.go")
	if err != nil {
		t.Fatalf("состав дерева не прочитан: %v — вердикта об объёме, которого не видел, гейт не выносит", err)
	}
	sources := make(map[string]string, len(paths))
	for _, abs := range paths {
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			t.Fatalf("путь %s не приводится к корню %s: %v", abs, root, rerr)
		}
		b, berr := os.ReadFile(abs)
		if berr != nil {
			t.Fatalf("%s: %v — обход неполон, вердикт был бы о меньшем дереве", rel, berr)
		}
		sources[filepath.ToSlash(rel)] = string(b)
	}

	findings, cen := repohygiene.AuditTestSubprocessCacheGuards(sources)

	// ── СТРАЖ ПРЕДПОСЫЛКИ, и он стоит РАНЬШЕ вердикта ──
	if msg := cen.PremiseFailure(); msg != "" {
		t.Fatal(msg)
	}

	for _, f := range findings {
		t.Errorf("%s", f)
	}
	t.Logf("%s", cen.Line())
	if len(cen.Unresolved) > 0 {
		// «Не установлено» печатается ОТДЕЛЬНО и поимённо: гейт о таких местах не
		// судит, и это его собственное незнание, а не чистота дерева.
		t.Logf("программа НЕ УСТАНОВЛЕНА (гейт о них не судит), %d мест:\n\t%s",
			len(cen.Unresolved), strings.Join(cen.Unresolved, "\n\t"))
	}
}
