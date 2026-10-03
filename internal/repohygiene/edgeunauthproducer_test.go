// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// edgeUnauthTreeSources — непроверочное дерево Go края, спрошенное у индекса.
func edgeUnauthTreeSources(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "gateway/*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	sources := map[string]string{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		sources[rel] = string(src)
	}
	return sources
}

// TestEdgeUnauthenticatedHasOneProducer — у отказа 401 края один производитель
// (`gateway/internal/authnrefusal`; приёмка KA1, Р2, kacho#2958). Исключение —
// указание повысить уровень (Р3), названное поимённо.
func TestEdgeUnauthenticatedHasOneProducer(t *testing.T) {
	t.Parallel()
	findings, census := FindEdgeUnauthProducers(edgeUnauthTreeSources(t))
	t.Logf("перепись: файлов края разобрано %d · не разобрано %d · мест производства 401 %d · из них указаний Р3 %d (объявлено %d)",
		census.Files, census.Unparsed, census.Producers, census.StepUp, len(edgeStepUpProducers))
	if census.Files == 0 {
		t.Fatal("разобрано ноль файлов края — гейт беспредметен: «ноль находок» здесь неотличимо от «ноль прочитанного»")
	}
	if census.Unparsed != 0 {
		t.Errorf("не разобрано файлов края: %d — они не судятся, и молчание по ним читалось бы как чистота", census.Unparsed)
	}
	for _, f := range findings {
		t.Errorf("%s:%d (%s): %s.\nОтказ «удостоверение не принято» пишется только authnrefusal.WriteHTTP / "+
			"authnrefusal.Err — иначе он различим по причине (приёмка KA1, Р2).", f.File, f.Line, f.Func, f.What)
	}
}
