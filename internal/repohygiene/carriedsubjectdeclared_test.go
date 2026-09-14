// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"
)

// carriedsubjectdeclared_test.go — держатель условия «надгробие не объявляет
// предмета исчезнувшим, пока он стережётся в другом репозитории». Предмет,
// устройство разбора и границы — в шапке carriedsubjectdeclared.go; здесь только
// добыча входа, перепись и вердикт.

// TestCarriedSubjectIsDeclaredNotBuried — судьба предмета у каждой записи
// надгробия объявлена, и объявление связно.
//
// Перепись печатает ДЕВЯТЬ величин, а не одну. Каждая закрывает свой способ
// получить «ноль находок» ни на чём: записей прочитано · семей · сколько
// объявили «предмета нет», сколько «уехало и стережётся», сколько «уехало и не
// стережётся никем», сколько «осталось здесь и снова стережётся» ·
// репозиториев-преемников в словаре · координат СВЕРЕНО с деревом-преемником и
// сколько НЕ сверялось · какие деревья-преемники были под рукой. Последние две
// врозь намеренно: пропуск, не названный числом, читался бы как проход.
func TestCarriedSubjectIsDeclaredNotBuried(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	rows, err := carriedSubjectRows(root)
	if err != nil {
		t.Fatalf("прочитать записи надгробия: %v", err)
	}

	repos, err := successorRepos(root)
	if err != nil {
		t.Fatalf("собрать словарь репозиториев-преемников: %v", err)
	}
	trees, how, err := carriedSubjectTrees()
	if err != nil {
		t.Fatalf("разобрать %s: %v", carriedSubjectTreesEnv, err)
	}
	var resolve carriedCoordinateResolver
	if len(trees) > 0 {
		resolve = resolveInTrees(trees)
	}

	findings, census := judgeCarriedSubjects(root, rows, repos, resolve)

	t.Logf("осмотрено: записей %s — %d; семей носителей — %d; объявлено %q %d, %q %d, %q %d, "+
		"%q %d; репозиториев-преемников в словаре — %d; координат сверено с деревом-преемником — %d, "+
		"НЕ сверялось — %d; деревья-преемники: %s",
		gateCarrierLedgerName, census.Rows, census.Families,
		subjectFateGone, census.Gone, subjectFateCarried, census.Carried,
		subjectFateUnguarded, census.Unguarded, subjectFateRegained, census.Regained,
		census.Repos, census.Checked, census.Unchecked, how)

	for _, f := range findings {
		t.Error(f)
	}
}
