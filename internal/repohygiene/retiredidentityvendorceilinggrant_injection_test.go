// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Инъекции решения владельца о потолке (`vendorApplyGrants`), в обе стороны, на
// синтетическом дереве: строки здесь не несут имени издателя — его роль играет
// метка `vendorx`, а отпечаток считается от неё же.
const grantLine = `- "vendorx-1.0.0.tgz:templates/rbac.yaml"`

func grantFixture(withLine, withArchive bool, extra ...string) (
	[]vendorCeilingGrant, map[string]vendorTreeCorpus, map[string][]vendorBinding,
	map[string]int, map[string]vendorTreeDelta, map[string]vendorTreeCensus,
) {
	grants := []vendorCeilingGrant{{Tree: "kacho", File: ".trivyignore.yaml",
		TextSHA256: vendorTextSHA256(grantLine), SubjectDir: "deploy/helm/umbrella/charts",
		Decision: "опыт", Removal: "опыт"}}
	corpus := vendorTreeCorpus{Bodies: map[string]string{}, Blobs: map[string]string{}}
	if withArchive {
		corpus.Archives = []vendorArchive{{Name: "deploy/helm/umbrella/charts/vendorx-1.0.0.tgz"}}
	}
	var bindings []vendorBinding
	if withLine {
		bindings = append(bindings, vendorBinding{Tree: "kacho", File: ".trivyignore.yaml",
			Line: 7, Axis: vendorAxisName, Text: grantLine})
	}
	for i, text := range extra {
		bindings = append(bindings, vendorBinding{Tree: "kacho", File: ".trivyignore.yaml",
			Line: 20 + i, Axis: vendorAxisName, Text: text})
	}
	head := map[string]vendorTreeCorpus{"kacho": corpus}
	byTree := map[string][]vendorBinding{"kacho": bindings}
	ceilings := map[string]int{"kacho": 10}
	deltas := map[string]vendorTreeDelta{"kacho": {Added: append([]vendorBinding(nil), bindings...)}}
	census := map[string]vendorTreeCensus{"kacho": {}}
	return grants, head, byTree, ceilings, deltas, census
}

// Законный близнец: строка гранта есть, архив на месте — потолок +1, прирост пуст.
func TestVendorGrantInjection_LiveGrantRaisesTheCeilingByItsOneLine(t *testing.T) {
	t.Parallel()
	g, head, byTree, ceil, deltas, census := grantFixture(true, true)
	if f := vendorApplyGrants(g, head, byTree, ceil, deltas, census); len(f) != 0 {
		t.Fatalf("живой грант дал находку: %v", f)
	}
	if ceil["kacho"] != 11 || census["kacho"].Granted != 1 || len(deltas["kacho"].Added) != 0 {
		t.Fatalf("потолок %d, извинено %d, прирост %d — ждали 11, 1, 0",
			ceil["kacho"], census["kacho"].Granted, len(deltas["kacho"].Added))
	}
}

// Грант извиняет ТОЛЬКО свою строку: соседняя привязка в том же файле остаётся ростом.
func TestVendorGrantInjection_AnotherLineInTheSameFileIsNotExcused(t *testing.T) {
	t.Parallel()
	g, head, byTree, ceil, deltas, census := grantFixture(true, true,
		`- "vendorx-1.0.0.tgz:templates/other.yaml"`)
	vendorApplyGrants(g, head, byTree, ceil, deltas, census)
	if ceil["kacho"] != 11 || len(deltas["kacho"].Added) != 1 {
		t.Fatalf("потолок %d, прирост %d — ждали 11 и 1: соседняя строка извинена молча",
			ceil["kacho"], len(deltas["kacho"].Added))
	}
}

// Строки гранта больше нет — грант без привязки, находка «снять грант».
func TestVendorGrantInjection_GrantWithoutItsLineIsFound(t *testing.T) {
	t.Parallel()
	g, head, byTree, ceil, deltas, census := grantFixture(false, true)
	f := vendorApplyGrants(g, head, byTree, ceil, deltas, census)
	if len(f) != 1 || !strings.Contains(f[0].String(), "нет строки с отпечатком") || ceil["kacho"] != 10 {
		t.Fatalf("грант без строки: находки %v, потолок %d", f, ceil["kacho"])
	}
}

// Архив подчарта ушёл (релиз снятия), строка осталась — находка «снять запись и грант».
func TestVendorGrantInjection_GrantOutlivingItsSubchartIsFound(t *testing.T) {
	t.Parallel()
	g, head, byTree, ceil, deltas, census := grantFixture(true, false)
	f := vendorApplyGrants(g, head, byTree, ceil, deltas, census)
	if len(f) != 1 || !strings.Contains(f[0].String(), "подчарт снят") || ceil["kacho"] != 10 {
		t.Fatalf("грант без подчарта: находки %v, потолок %d", f, ceil["kacho"])
	}
}
