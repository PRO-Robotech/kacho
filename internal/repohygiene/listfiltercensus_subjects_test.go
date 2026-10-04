// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Перепись «каждый транспортный список виден анализатору» читает ту же
// предпосылку, что три соседних теста покрытия (Д88): служба, записанная в
// noListingSurface, анализатора и цели `audit-list-filter` не имеет, и звать её
// анализатор значит получить отказ make, а не измерение. Одна предпосылка — один
// источник: запись читается из той же ведомости, а не выписывается здесь.
//
// Запись не глушит службу молча: у записанной службы обход дерева обязан
// находить 0 транспортных списков; первый List* — находка «запись пережила
// предмет», и служба судится анализатором, как все.

// TestCensusSubjects_RecordedServiceIsNotRunAndUnrecordedIs — инъекция в обе
// стороны на синтетике: записанная служба без списков исключена без находки;
// записанная служба со списком — находка и анализатор; незаписанная — анализатор.
func TestCensusSubjects_RecordedServiceIsNotRunAndUnrecordedIs(t *testing.T) {
	t.Parallel()
	records := []surfaceDecision{{Service: "notify", Issue: 2915, Record: "r", Because: "b"}}
	count := func(m map[string]int) func(string) int { return func(s string) int { return m[s] } }

	judge, findings := censusSubjects([]string{"notify", "vpc"}, records, count(map[string]int{"vpc": 23}))
	if strings.Join(judge, ",") != "vpc" || len(findings) != 0 {
		t.Errorf("близнец: записанная notify без списков исключена, vpc судится; получено judge=%v findings=%v",
			judge, findings)
	}

	judge, findings = censusSubjects([]string{"notify", "vpc"}, records, count(map[string]int{"vpc": 23, "notify": 1}))
	if strings.Join(judge, ",") != "notify,vpc" || len(findings) != 1 || !strings.Contains(findings[0], "notify") {
		t.Errorf("инъекция: у записанной notify появился List* — находка и анализатор; получено judge=%v findings=%v",
			judge, findings)
	}

	judge, findings = censusSubjects([]string{"notify", "vpc"}, nil, count(map[string]int{"vpc": 23}))
	if strings.Join(judge, ",") != "notify,vpc" || len(findings) != 0 {
		t.Errorf("инъекция: запись снята — notify судится анализатором; получено judge=%v findings=%v",
			judge, findings)
	}
}

// TestCensusSubjects_ReadsTheSharedLedger — перепись читает ту же ведомость,
// что соседние тесты: на дереве notify записана и исключается из прогона
// анализаторов, а служб для анализатора остаётся не ноль.
func TestCensusSubjects_ReadsTheSharedLedger(t *testing.T) {
	t.Parallel()
	root := repoRootForCoverage(t)
	svcs := servicesFromGit(t, root)
	judge, findings := censusSubjects(svcs, noListingSurface,
		func(svc string) int { return len(treeListings(t, root, svc)) })
	for _, f := range findings {
		t.Error(f)
	}
	for _, s := range judge {
		if s == "notify" {
			t.Errorf("notify записана в noListingSurface, а перепись зовёт её анализатор")
		}
	}
	if len(judge) == 0 {
		t.Fatal("служб для анализатора 0 — перепись беспредметна")
	}
	t.Logf("служб %d · судится анализатором %d · записано без списочной поверхности %d",
		len(svcs), len(judge), len(svcs)-len(judge))
}
