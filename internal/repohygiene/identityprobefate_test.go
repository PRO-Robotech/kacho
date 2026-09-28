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

// identityProbeFateFactsFromTree — состав проб и число строк каждого файла, на
// который ссылается ведомость. Состав берётся из индекса git, а не обходом
// диска: вердикт — свойство коммита, а не рабочего каталога.
func identityProbeFateFactsFromTree(t *testing.T, root string, tt *trackedTree, l identityProbeFateLedger) identityProbeFateFacts {
	t.Helper()
	f := identityProbeFateFacts{Lines: map[string]int{}}
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
	for rel := range need {
		if !tt.files[rel] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304 -- путь из индекса собственного дерева
		if err != nil {
			t.Fatalf("%s в индексе есть, а не читается: %v — судить координату нечем", rel, err)
		}
		f.Lines[rel] = identityProbeLineCount(string(raw))
	}
	return f
}

// identityProbeLineCount — число строк текста, как их нумерует редактор.
func identityProbeLineCount(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// TestIdentityProbeFateLedgerNamesEveryProbe — сам гейт.
func TestIdentityProbeFateLedgerNamesEveryProbe(t *testing.T) {
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
			"%q, заголовок разбивки %q)", IdentityProbeFateLedgerFile, err,
			"| "+strings.Join(identityProbeFateHeader, " | ")+" |",
			"| "+strings.Join(identityProbeFateTotals, " | ")+" |")
	}

	facts := identityProbeFateFactsFromTree(t, root, tt, l)
	found, c := judgeIdentityProbeFate(l, facts, identityProbeDir, IdentityProbeFateLedgerFile)
	t.Logf("перепись: %s", c)
	for _, f := range found {
		t.Error(f)
	}
}
