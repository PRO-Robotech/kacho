// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"strings"
	"testing"
)

// surfacedecision_injection_test.go — доказательство, что ведомости решений о
// поверхности СПОСОБНЫ упасть, и что их запись не глушит соседний исход.
//
// Каждая инъекция меняет РОВНО ОДИН факт против законного близнеца и требует
// находку с координатой; близнец обязан молчать. Входы — того же вида, что даёт
// дерево: имена служб, счёт List*, наличие каталогов.

const injGate = "TestSomeGate"

func injRecord(body string) func(string) (string, error) {
	return func(rel string) (string, error) {
		if rel == "missing.md" {
			return "", errors.New("нет файла")
		}
		return body, nil
	}
}

func legalDecision() surfaceDecision {
	return surfaceDecision{Service: "quiet", Issue: 1, Record: "doc.md", Because: "нет RPC"}
}

func requireOneSurfaceFinding(t *testing.T, name string, got []string, want string) {
	t.Helper()
	if len(got) != 1 || !strings.Contains(got[0], want) {
		t.Fatalf("%s: ждали ровно одну находку с %q, получили %d: %v", name, want, len(got), got)
	}
}

func TestSurfaceDecisionRecordFindings_EachFieldCanFail(t *testing.T) {
	t.Parallel()
	svcs := []string{"quiet", "vpc"}
	read := injRecord("решение держит " + injGate)

	if got := surfaceDecisionRecordFindings([]surfaceDecision{legalDecision()}, svcs, injGate, read); len(got) != 0 {
		t.Fatalf("законная запись дала находки: %v", got)
	}

	cases := []struct {
		name   string
		mutate func(*surfaceDecision)
		read   func(string) (string, error)
		want   string
	}{
		{"служба вне дерева", func(d *surfaceDecision) { d.Service = "gone" }, read, "нечего исключать"},
		{"пустая причина", func(d *surfaceDecision) { d.Because = " " }, read, "без причины"},
		{"нет задачи", func(d *surfaceDecision) { d.Issue = 0 }, read, "не называет задачи"},
		{"нет документа", func(d *surfaceDecision) { d.Record = "" }, read, "не называет документа"},
		{"документ не читается", func(d *surfaceDecision) { d.Record = "missing.md" }, read, "не читается"},
		{"документ о другом", func(*surfaceDecision) {}, injRecord("про что-то другое"), "не называет гейт"},
	}
	for _, c := range cases {
		d := legalDecision()
		c.mutate(&d)
		requireOneSurfaceFinding(t, c.name, surfaceDecisionRecordFindings([]surfaceDecision{d}, svcs, injGate, c.read), c.want)
	}

	dup := []surfaceDecision{legalDecision(), legalDecision()}
	requireOneSurfaceFinding(t, "дубль", surfaceDecisionRecordFindings(dup, svcs, injGate, read), "дважды")
}

func TestListingSurfaceDecision_ExpiresAndDoesNotHideOthers(t *testing.T) {
	t.Parallel()
	ledger := []surfaceDecision{legalDecision()}
	svcs := []string{"quiet", "vpc"}

	// Близнец: записанная служба без списка, у соседа список есть — молчит.
	if got := listingSurfaceFindings(svcs, map[string]int{"quiet": 0, "vpc": 3}, ledger); len(got) != 0 {
		t.Fatalf("законное дерево дало находки: %v", got)
	}
	// Запись пережила предмет: у службы появился List*.
	requireOneSurfaceFinding(t, "истечение по успеху",
		listingSurfaceFindings(svcs, map[string]int{"quiet": 1, "vpc": 3}, ledger), "outlived")
	// Запись не глушит соседа: незаписанная служба без списка — находка.
	requireOneSurfaceFinding(t, "сосед без записи",
		listingSurfaceFindings(svcs, map[string]int{"quiet": 0, "vpc": 0}, ledger), "vpc")
}

func TestAnalyserCoverageDecision_RecordedIsNeitherMissingNorRun(t *testing.T) {
	t.Parallel()
	ledger := []surfaceDecision{legalDecision()}
	svcs := []string{"quiet", "vpc"}
	has := func(present ...string) func(string) bool {
		return func(s string) bool {
			for _, p := range present {
				if p == s {
					return true
				}
			}
			return false
		}
	}

	if got := analyserCoverageFindings(svcs, has("vpc"), ledger); len(got) != 0 {
		t.Fatalf("законное дерево дало находки: %v", got)
	}
	requireOneSurfaceFinding(t, "сосед без анализатора",
		analyserCoverageFindings(svcs, has(), ledger), "vpc")
	requireOneSurfaceFinding(t, "анализатор у записанной службы",
		analyserCoverageFindings(svcs, has("vpc", "quiet"), ledger), "quiet")

	if got := ciRunFindings(svcs, []string{"vpc"}, ledger); len(got) != 0 {
		t.Fatalf("законная провязка конвейера дала находки: %v", got)
	}
	requireOneSurfaceFinding(t, "сосед вне конвейера", ciRunFindings(svcs, nil, ledger), "vpc")
	requireOneSurfaceFinding(t, "конвейер гонит записанную", ciRunFindings(svcs, []string{"vpc", "quiet"}, ledger), "quiet")
	requireOneSurfaceFinding(t, "конвейер гонит несуществующую", ciRunFindings(svcs, []string{"vpc", "ghost"}, ledger), "ghost")
}

func TestUseCasePremiseDecision_ExpiresAndDoesNotHideOthers(t *testing.T) {
	t.Parallel()
	ledger := []surfaceDecision{legalDecision()}
	svcs := []string{"services/quiet", "services/vpc"}

	legal := map[string]useCaseLayerShape{
		"services/quiet": {},
		"services/vpc":   {apps: true, api: true, pkgs: 8},
	}
	if got, total := useCasePremiseFindings(svcs, legal, ledger); len(got) != 0 || total != 8 {
		t.Fatalf("законное дерево: находки %v, пакетов %d (ждали 8)", got, total)
	}

	grown := map[string]useCaseLayerShape{
		"services/quiet": {apps: true},
		"services/vpc":   {apps: true, api: true, pkgs: 8},
	}
	got, _ := useCasePremiseFindings(svcs, grown, ledger)
	requireOneSurfaceFinding(t, "истечение по успеху", got, "пережила")

	bare := map[string]useCaseLayerShape{
		"services/quiet": {},
		"services/vpc":   {},
	}
	got, _ = useCasePremiseFindings(svcs, bare, ledger)
	requireOneSurfaceFinding(t, "сосед без слоя", got, "services/vpc")

	empty := map[string]useCaseLayerShape{
		"services/quiet": {},
		"services/vpc":   {apps: true, api: true},
	}
	got, _ = useCasePremiseFindings(svcs, empty, ledger)
	requireOneSurfaceFinding(t, "сосед с пустым api", got, "ни одного пакета")
}
