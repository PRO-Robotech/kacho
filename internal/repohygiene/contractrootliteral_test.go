// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"
)

// contractRootLiteralDirs — где отбор популяции живёт в этом дереве.
//
// Перечень выписан, а не выведен, и это его единственное оправдание: обход
// ВСЕГО дерева судил бы и рабочие копии, и произведённое чужими инструментами.
// Каталог, которого нет, — ОТКАЗ обхода, а не тихий пропуск: переезд иначе
// завёл бы слепую зону молча.
var contractRootLiteralDirs = []string{"internal/repohygiene", "tools"}

// TestPopulationIsSelectedByTheDeclaredRootsNotALiteral — отбор популяции берётся
// у объявленного словаря корней.
func TestPopulationIsSelectedByTheDeclaredRootsNotALiteral(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	findings, census, err := AuditContractRootLiterals(root, contractRootLiteralDirs)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if census.FilesRead == 0 {
		t.Fatalf("обход пуст: файлов Go прочитано 0 — вердикт беспредметен (%s)", census)
	}
	if census.PrefixCalls == 0 {
		t.Fatalf("вызовов проверки приставки не встречено НИ ОДНОГО при %d файлах: "+
			"распознаватель перестал их видеть, и молчание гейта ничего не означает (%s)",
			census.FilesRead, census)
	}
	for _, u := range census.Unparsed {
		t.Logf("НЕ РАЗОБРАН (вердикт по нему не выносится): %s", u)
	}
	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("перепись: %s", census)
}
