// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// licensetiersubject_test.go — ГЕЙТ НА ДЕРЕВЕ для оси четвёртой карты лицензий:
// у каждого объявленного уровня есть предмет, то есть хотя бы один путь дерева,
// который в него разрешается.
//
// Норма, разбор и границы оси живут рядом с суждением — licensemap.go,
// judgeLicenseTierSubjects. Здесь только добыча входа: перечень путей берётся у
// ИНДЕКСА git, как и у соседних лицензионных проверок, потому что вердикт обязан
// быть свойством коммита, а не рабочего каталога.
package repohygiene

import "testing"

// TestLicenseTiersHaveALiveSubjectInTheTree — ось четвёртая на живом дереве.
func TestLicenseTiersHaveALiveSubjectInTheTree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	var paths []string
	for _, line := range gitLsFiles(t, root) {
		if _, rel, ok := parseLsFiles(line); ok {
			paths = append(paths, rel)
		}
	}

	faults, census := judgeLicenseTierSubjects(licenseTiers, licenseTierPathCensus(paths))

	t.Logf("осмотрено: записей индекса %d, находок %d; %s", len(paths), len(faults), census)

	for _, f := range faults {
		t.Errorf("%s.\n"+
			"  ЧТО ДЕЛАТЬ: уровень снимается ВМЕСТЕ со своим предметом — записью в "+
			"licenseTiers, файлом <Prefix>/LICENSE и упоминаниями в шапке licensemap.go. "+
			"Оставленная запись не краснеет и не зеленеет: она молчит.", f)
	}
}
