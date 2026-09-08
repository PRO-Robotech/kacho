// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verdictgateabsenceisnotaskip_test.go — ГЕЙТ НА ДЕРЕВЕ.
//
// Норма, обе оси и границы живут рядом с суждением —
// verdictgateabsenceisnotaskip.go. Здесь только добыча входа.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// verdictGateRunnerRoot — каталог рецептов прогона. Одна координата, а не
// перечень имён: перечень разошёлся бы с деревом на первом же новом прогонщике.
const verdictGateRunnerRoot = "deploy/scripts"

// TestVerdictGateAbsenceIsNotASkip — обе оси на живом дереве.
func TestVerdictGateAbsenceIsNotASkip(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, verdictGateRunnerRoot), ".sh")
	if err != nil {
		t.Fatalf("состав %s не прочитан: %v — вердикт беспредметен", verdictGateRunnerRoot, err)
	}

	var findings []VerdictGateFinding
	census := VerdictGateCensus{PerFile: map[string]int{}}
	for _, abs := range files {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			t.Fatalf("путь %s не приводится к корню: %v", abs, err)
		}
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(abs) // #nosec G304 — путь из состава дерева
		if err != nil {
			t.Fatalf("%s не прочитан: %v — это отказ, а не пропуск", rel, err)
		}
		f, checkers, scriptVars, conds, skipped := scanVerdictGateRunner(rel, string(b))
		census.Files++
		census.ScriptVars += scriptVars
		census.Checkers += len(checkers)
		census.Conditions += conds
		census.Skipped += skipped
		if len(checkers) > 0 {
			census.PerFile[rel] = len(checkers)
		}
		findings = append(findings, f...)
	}

	faults := judgeVerdictGateRunners(findings, census)

	t.Logf("осмотрено: находок %d; %s", len(faults), census)

	for _, f := range faults {
		t.Errorf("%s.\n"+
			"  ЧТО ДЕЛАТЬ: развести три исхода по РАЗНЫМ условиям —\n"+
			"    ручка выключена   → сырой счёт законен, выходите его кодом;\n"+
			"    гейта нет по адресу → ОТКАЗ ненулевой КОНСТАНТОЙ — тем кодом, которым этот же\n"+
			"                          прогонщик называет недействительный прогон,\n"+
			"                          и сообщение называет путь, по которому его искали;\n"+
			"    гейт исполнился    → выходите ЕГО кодом.\n"+
			"  Отсутствие вердиктного гейта обязано быть громким: пропуск неотличим\n"+
			"  от прогона с исполненным гейтом, а не исполнились при этом три исхода,\n"+
			"  перепись «исполнено N из M» и отказ на немом отчёте.", f)
	}

	if len(strings.TrimSpace(census.String())) == 0 {
		t.Fatal("перепись пуста — «ноль находок» неотличимо от «ноль прочитанного»")
	}
}
