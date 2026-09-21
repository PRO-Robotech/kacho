// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"
)

// TestBakedDescriptorLedgerEntryHasASubjectInTheTree — сам гейт.
//
// Судится БЕЗУСЛОВНО: ось, которую ведомость обслуживает, на сегодняшнем дереве
// беспредметна и уходит в `Skip`, а `Skip` срабатывает раньше счёта вхождений.
// Самоистечение, спрятанное за `Skip`, самоистечением не является.
func TestBakedDescriptorLedgerEntryHasASubjectInTheTree(t *testing.T) {
	t.Parallel()
	// ПРЕДПОСЫЛКА ОБХОДА: три исхода вместо одного зелёного.
	requireTreeWalkOverCorpus(t, repoRoot(t), treeWalkKeepAll, "git ls-tree -r HEAD, все пути", "отслеживаемых путей")
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	if tt.count() == 0 {
		t.Fatal("состав дерева пуст — судить ведомость не по чему, и «предмета нет» " +
			"было бы вердиктом о непрочитанном")
	}

	stale, census := JudgeBakedLedgerSubjects(knownBakedDescriptors, tt.hasFile, tt.count())
	t.Logf("%s", census)

	if len(stale) > 0 {
		t.Errorf("%d записи ведомости запечённых дескрипторов без предмета:%s",
			len(stale), FormatBakedLedgerStale(stale))
	}
}
