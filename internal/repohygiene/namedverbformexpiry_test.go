// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// namedverbformexpiry_test.go — ГЕЙТ: отсрочка `#1844` истекает САМА.
//
// Поимённая форма права роли (ключ `verbs:`) возвращается ТОЛЬКО вместе с
// проверкой её полноты. Пока форма отвергается сентинелом, шести проб полноты
// нет по построению — и это законно; в день, когда сентинел перестанет
// возвращаться, а проб по-прежнему не окажется, гейт называет недостающие
// сценарии поимённо.
//
// Предмет, довод в пользу пары (а не любой её половины) и границы разбора —
// в шапке `namedverbformexpiry.go`; здесь они не пересказываются.
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `namedverbformexpiry_injection_test.go`.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// manifestPackageDir — пакет, где живёт пред-разборная проверка снятого ключа.
	manifestPackageDir = "services/iam/internal/manifest/"
	// manifestCensusFloor — не-тестовых файлов пакета, ниже которого обход
	// беспредметен: пакет переехал либо снят, и гейт стережёт каталог, которого
	// больше нет.
	manifestCensusFloor = 5
)

// scenarioProbeNames — имена проб сценариев, найденные по ВСЕМУ дереву.
//
// По всему, а не по одному пакету: приёмка не назначает шести пробам дома, и
// гейт, ищущий их в одном каталоге, объявил бы отсутствующими те, что заведут
// рядом.
func scenarioProbeNames(t *testing.T, tt *trackedTree, root string) ([]string, int) {
	t.Helper()
	var (
		names  []string
		parsed int
	)
	var rels []string
	for rel := range tt.files {
		if strings.HasSuffix(rel, "_test.go") {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		if !strings.Contains(string(src), "TestMODRL") {
			parsed++
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, rel, src, 0)
		if perr != nil {
			t.Fatalf("разбор %s: %v", rel, perr)
		}
		parsed++
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "TestMODRL") {
				names = append(names, fn.Name.Name)
			}
		}
	}
	sort.Strings(names)
	return names, parsed
}

// TestNamedVerbFormReturnsOnlyWithItsCompletenessCheck — сам гейт.
func TestNamedVerbFormReturnsOnlyWithItsCompletenessCheck(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var (
		manifestFiles int
		lits, idents  int
		returns       int
	)
	var rels []string
	for rel := range tt.files {
		if !strings.HasPrefix(rel, manifestPackageDir) || !strings.HasSuffix(rel, ".go") {
			continue
		}
		if strings.HasSuffix(rel, "_test.go") || strings.Count(rel[len(manifestPackageDir):], "/") > 0 {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		c, err := ScanVerbFormSentinel(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		manifestFiles++
		lits += c.CompositeLits
		idents += c.Idents
		returns += c.SentinelReturns
	}

	probeNames, testFiles := scenarioProbeNames(t, tt, root)
	missing := MissingScenarioProbes(probeNames)
	finding := NamedVerbFormFinding(returns, probeNames)

	t.Logf("перепись: не-тестовых файлов %s разобрано %d (литералов %d, идентификаторов %d) · "+
		"возвратов сентинела %s — %d · тестовых файлов дерева осмотрено %d · "+
		"проб MOD-RL найдено %d %v · сценариев отсрочки %d, из них без пробы %d %v",
		manifestPackageDir, manifestFiles, lits, idents,
		RoleRuleVerbsSentinel, returns, testFiles,
		len(probeNames), probeNames, len(NamedVerbScenarios), len(missing), missing)

	// ── ПРЕДПОСЫЛКИ ОБХОДА ───────────────────────────────────────────────────
	if manifestFiles < manifestCensusFloor {
		t.Fatalf("перепись обвалилась: не-тестовых файлов %s разобрано %d при пороге %d — "+
			"пакет переехал либо снят, и гейт стережёт каталог, которого больше нет",
			manifestPackageDir, manifestFiles, manifestCensusFloor)
	}
	if lits == 0 {
		t.Fatalf("в пакете %s не прочитано ни одного составного литерала — ось разбора "+
			"беспредметна, и «возвратов ноль» получено даром", manifestPackageDir)
	}
	if testFiles == 0 {
		t.Fatal("обход тестовых файлов дерева пуст — перепись проб беспредметна, и " +
			"«проб нет» неотличимо от «ничего не прочитано»")
	}
	if len(NamedVerbScenarios) == 0 {
		t.Fatal("перечень сценариев отсрочки пуст — требовать нечего, и молчание гейта " +
			"было бы сказано ни о чём")
	}

	// ── НАХОДКА — ПАРА, а не половина ────────────────────────────────────────
	//
	// Решение принимает `NamedVerbFormFinding` — ТА ЖЕ функция, которую гоняет
	// инъекция. Молчание у неё две законные причины: форма отвергается либо
	// вернулась вместе со своей проверкой.
	if len(finding) == 0 {
		return
	}
	t.Fatalf("ключ `verbs:` правила роли больше не отвергается сентинелом %s (возвратов в %s — "+
		"ноль), а проб полноты по-прежнему нет у %d сценариев из %d: %v\n\n"+
		"Поимённый перечень действий возвращается ТОЛЬКО вместе с проверкой его полноты по "+
		"классу (#1844). Принять перечень имён, не умея проверить полноту, значит свести его "+
		"к классу МОЛЧА — то есть выдать право ШИРЕ просимого: замер приёмки называет 55 "+
		"вхождений из 92 в черновике vpc, совпадающих с именем класса.\n"+
		"Исходов два: завести пробы названных сценариев ЛИБО вернуть отказ сентинелом. "+
		"Третьего — «принять форму и доделать проверку позже» — нет.\n"+
		"Найдено проб MOD-RL: %v",
		RoleRuleVerbsSentinel, manifestPackageDir, len(missing), len(NamedVerbScenarios),
		finding, probeNames)
}
