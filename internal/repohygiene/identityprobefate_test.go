// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identityprobefate_test.go — ведомость судьбы проб полосы личности судится по
// индексу git (задача #2731). Устройство и требования — шапка
// identityprobefate.go; способность упасть и смолчать — соседний
// identityprobefate_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// identityProbeFateFactsFromTree — состав проб и строки каждого файла, на
// который ссылается ведомость. Состав берётся из индекса git, а не обходом
// диска: вердикт — свойство коммита, а не рабочего каталога.
func identityProbeFateFactsFromTree(t *testing.T, root string, tt *trackedTree, l identityProbeFateLedger) identityProbeFateFacts {
	t.Helper()
	f := identityProbeFateFacts{Text: map[string][]string{}}
	prefix := identityProbeDir + "/"
	for rel := range tt.files {
		name := strings.TrimPrefix(rel, prefix)
		if name == rel || strings.Contains(name, "/") || !identityProbeName.MatchString(name) {
			continue
		}
		f.Probes = append(f.Probes, name)
	}
	sort.Strings(f.Probes)

	need := map[string]bool{}
	for _, p := range f.Probes {
		need[prefix+p] = true
	}
	for _, r := range l.Rows {
		for _, c := range r.Coords {
			need[c.Path] = true
		}
	}
	for _, c := range l.Elsewhere {
		need[c.Path] = true
	}
	for rel := range need {
		if !tt.files[rel] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s в индексе есть, а не читается: %v — судить координату нечем", rel, err)
		}
		f.Text[rel] = identityProbeLinesOf(string(raw))
	}
	return f
}

// TestIdentityProbeFateLedgerNamesEveryProbe — сам гейт.
func TestIdentityProbeFateLedgerNamesEveryProbe(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(IdentityProbeFateLedgerFile)))
	if err != nil {
		t.Fatalf("ведомость %s не читается (%v) — судьба проб полосы личности не записана нигде, "+
			"и «находок ноль» здесь означало бы «ноль прочитанного»", IdentityProbeFateLedgerFile, err)
	}
	l, err := parseIdentityProbeFateLedger(string(raw), identityProbeDir)
	if err != nil {
		t.Fatalf("%s: %v — форма ведомости не распознана, и судить её нечем (заголовок ведомости "+
			"%q, заголовок разбивки %q, заголовок таблицы якорей %q)", IdentityProbeFateLedgerFile, err,
			"| "+strings.Join(identityProbeFateHeader, " | ")+" |",
			"| "+strings.Join(identityProbeFateTotals, " | ")+" |",
			"| "+strings.Join(identityProbeFateAnchorsHeader, " | ")+" |")
	}

	facts := identityProbeFateFactsFromTree(t, root, tt, l)
	found, c := judgeIdentityProbeFate(l, facts, identityProbeDir, IdentityProbeFateLedgerFile)
	t.Logf("перепись: %s", c)
	for _, f := range found {
		t.Error(f)
	}
}

// identityProbeFateLedgerOfTree — ведомость дерева, разобранная. Отказ разбора —
// «не выполнилось», а не зелёное.
func identityProbeFateLedgerOfTree(t *testing.T, root string) identityProbeFateLedger {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(IdentityProbeFateLedgerFile)))
	if err != nil {
		t.Fatalf("ведомость %s не читается (%v) — судить нечем", IdentityProbeFateLedgerFile, err)
	}
	l, err := parseIdentityProbeFateLedger(string(raw), identityProbeDir)
	if err != nil {
		t.Fatalf("%s: %v — форма ведомости не распознана, и судить её нечем", IdentityProbeFateLedgerFile, err)
	}
	return l
}

// identitySignFactsFromTree — индекс и файлы пиненных модулей, на которые
// ссылаются доводы решений. Модуль читается по ПИНУ из go.mod; пин не
// разрешён — проверка не исполнялась.
func identitySignFactsFromTree(t *testing.T, root string, tt *trackedTree, l identityProbeFateLedger) identitySignFacts {
	t.Helper()
	f := identitySignFacts{Tracked: tt.files, Pinned: map[string][]string{}}
	dirs := map[string]string{}
	for _, r := range l.Beyond.Sign {
		for _, p := range r.Pinned {
			module, ok := identityPinnedModules[p.Alias]
			if !ok {
				continue // псевдоним вне перечня называет суд
			}
			dir, seen := dirs[p.Alias]
			if !seen {
				dir = vendorModuleDir(t, root, module)
				dirs[p.Alias] = dir
			}
			raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p.Path)))
			if err != nil {
				continue // файла в пиненном дереве нет — это называет суд
			}
			f.Pinned[p.Key()] = identityProbeLinesOf(string(raw))
		}
	}
	return f
}

// TestIdentityProbeFateLedgerDecidesEverySignGate — запись А: решение по
// каждому гейту признака #1276 с тем, чем держится его класс отказа.
func TestIdentityProbeFateLedgerDecidesEverySignGate(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	l := identityProbeFateLedgerOfTree(t, root)
	found, c := judgeIdentitySignGates(l, identitySignFactsFromTree(t, root, tt, l), identityProbeDir,
		IdentityProbeFateLedgerFile, identitySignGateCount)
	t.Logf("перепись: %s", c)
	for _, f := range found {
		t.Error(f)
	}
}

// identityBeyondFactsFromTree — тексты всех отслеживаемых файлов формы пробы.
// Двоичный файл пробой не читается и в перепись не входит.
func identityBeyondFactsFromTree(t *testing.T, root string, tt *trackedTree) identityBeyondFacts {
	t.Helper()
	texts := map[string]string{}
	for rel := range tt.files {
		if identityBeyondForm(rel) == "" || identityLedgerProbe(rel, identityProbeDir) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s в индексе есть, а не читается: %v — популяция неизвестна", rel, err)
		}
		if !vendorTextBytes(raw) {
			continue
		}
		texts[rel] = string(raw)
	}
	return identityBeyondFactsOf(tt.files, texts, identityProbeDir)
}

// TestIdentityProbeFateLedgerNamesEveryProbeBeyondIt — запись Б: судьба каждой
// пробы вне `deploy/identity_*_test.go`, называющей поставщика.
func TestIdentityProbeFateLedgerNamesEveryProbeBeyondIt(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	l := identityProbeFateLedgerOfTree(t, root)
	facts := identityBeyondFactsFromTree(t, root, tt)
	if facts.Read == 0 {
		t.Fatal("файлов формы пробы в индексе ноль — обход прочитал ничто, и «находок ноль» значило бы " +
			"«ноль осмотренного»")
	}
	found, c := judgeIdentityBeyond(l, facts, identityProbeDir, IdentityProbeFateLedgerFile)
	t.Logf("перепись: %s", c)
	for _, f := range found {
		t.Error(f)
	}
}
