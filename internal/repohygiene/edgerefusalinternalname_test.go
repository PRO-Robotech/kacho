// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// edgeRefusalComponents — компоненты, которые текст отказа службой назвать не
// может: каталоги `services/` (ВЫВЕДЕНЫ обходом, а не выписаны) и составные
// части края.
func edgeRefusalComponents(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), "services"))
	if err != nil {
		t.Fatalf("services/: %v — перечень служб не установлен", err)
	}
	out := append([]string{}, EdgeComponentNames...)
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
			n++
		}
	}
	if n == 0 {
		t.Fatal("в services/ ни одного каталога — словарь служб пуст, и суд по нему молчал бы о любом имени")
	}
	sort.Strings(out)
	return out
}

// TestEdgeRefusalNamesNoInternalService — ни один текст отказа края наружу не
// называет внутреннюю службу (kacho#3029). Судятся шесть форм записи текста;
// каждая обязана встретиться в дереве.
func TestEdgeRefusalNamesNoInternalService(t *testing.T) {
	t.Parallel()
	components := edgeRefusalComponents(t)
	findings, census := FindEdgeRefusalInternalNames(edgeUnauthTreeSources(t), components)
	total := 0
	for _, f := range EdgeRefusalForms {
		total += census.ByForm[f]
	}
	t.Logf("перепись: файлов края разобрано %d · не разобрано %d · текстов отказа судимо %d (status %d · json %d · map %d · field %d · arg %d · httperror %d) · не разрешено статически %d (status %d · map %d · field %d · httperror %d) · компонентов в словаре %d",
		census.Files, census.Unparsed, total, census.ByForm["status"], census.ByForm["json"], census.ByForm["map"],
		census.ByForm["field"], census.ByForm["arg"], census.ByForm["httperror"], census.Unresolved, census.UnresolvedByForm["status"],
		census.UnresolvedByForm["map"], census.UnresolvedByForm["field"], census.UnresolvedByForm["httperror"], len(components))
	if census.Files == 0 {
		t.Fatal("разобрано ноль файлов края — «ноль находок» неотличимо от «ноль прочитанного»")
	}
	if census.Unparsed != 0 {
		t.Errorf("не разобрано файлов края: %d — они не судятся", census.Unparsed)
	}
	for _, f := range EdgeRefusalForms {
		if census.ByForm[f]+census.UnresolvedByForm[f] == 0 {
			t.Errorf("предпосылка: форма %q не встретилась в дереве ни разу — распознаватель о ней на дереве не проверен; "+
				"форма исчезла — снимите её из перечня тем же изменением", f)
		}
	}
	for _, f := range findings {
		t.Errorf("%s:%d (форма %s): текст отказа называет внутреннее имя %q: %q.\n"+
			"Текст отказа уходит наружу дословно; служба, из которой собран край, ему не принадлежит (kacho#3029).",
			f.File, f.Line, f.Form, f.Name, f.Text)
	}
}
