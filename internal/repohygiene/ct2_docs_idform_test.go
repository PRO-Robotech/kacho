// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func docsIDFormOptions(t *testing.T) DocsIDFormOptions {
	t.Helper()
	return DocsIDFormOptions{
		Root:       repoRoot(t),
		ModulePath: "github.com/PRO-Robotech/kacho",
		// Судятся ВСЕ клиентские деревья документации. Границы больше нет:
		// прежде судились два домена, и остаток был назван честно; #1723
		// перемерил остальные шесть — находок в них НОЛЬ, и это сказано числом,
		// а не умолчанием (перепись печатает страницы и рассуженные токены по
		// всему набору). Перечень выписан, а не выведен обходом `services/*`,
		// намеренно: домен без клиентской документации обязан быть виден как
		// решение, а не как пропуск обхода.
		JudgedDomains: []string{
			"gateway/docs/content",
			"services/compute/docs/content",
			"services/geo/docs/content",
			"services/iam/docs/content",
			"services/nlb/docs/content",
			"services/registry/docs/content",
			"services/storage/docs/content",
			"services/vpc/docs/content",
		},
	}
}

// TestDocsIDFormMatchesWhatTheCodeMints — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`ct2_docs_idform_injection_test.go`): здесь только вердикт.
func TestDocsIDFormMatchesWhatTheCodeMints(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditDocsIDForm(docsIDFormOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премисы: прочитано то, что заведомо есть. Без них «ноль находок»
	// достигалось бы пустым обходом.
	if census.GoFiles < 200 {
		t.Fatalf("исходников Go прочитано %d — дерево не обойдено, словарь чеканки строить не из чего",
			census.GoFiles)
	}
	if census.ConstLiterals < 100 {
		t.Fatalf("строковых констант собрано %d — разбор объявлений не состоялся", census.ConstLiterals)
	}
	if census.MintCalls < 10 {
		t.Fatalf("вызовов чеканки найдено %d — обход не видит `NewID`/`NewHyphenID`", census.MintCalls)
	}
	if census.Prefixes < 10 {
		t.Fatalf("префиксов в словаре %d — резолв аргументов не работает, судить нечем", census.Prefixes)
	}
	// Обе формы обязаны быть представлены: словарь, знающий одну, объявил бы
	// вторую нарушением на всяком её вхождении.
	var sawConcat, sawHyphen bool
	for _, pf := range census.PrefixForms {
		if strings.Contains(pf, idFormConcat) {
			sawConcat = true
		}
		if strings.Contains(pf, idFormHyphen) {
			sawHyphen = true
		}
	}
	if !sawConcat || !sawHyphen {
		t.Fatalf("в словаре представлены не обе формы (слитная=%v дефисная=%v) — "+
			"вердикт был бы о выборе анализатора, а не о дереве", sawConcat, sawHyphen)
	}
	if census.DocFiles < 100 {
		t.Fatalf("страниц клиентской документации %d — обход неполон, вердикт беспредметен", census.DocFiles)
	}
	if census.Judged == 0 {
		t.Fatalf("токенов рассужено 0 при %d встреченных — сверка не состоялась", census.TokensSeen)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("клиентская страница показывает идентификатор в форме, которой код не чеканит "+
		"(токенов встречено %d, рассужено %d):\n%s\n\n"+
		"Форму выбирает генератор, а не читатель: `ids.NewID` даёт слитную, "+
		"`ids.NewHyphenID` — дефисную. Разбор `validate.ResourceID` судит только префикс "+
		"и тело внутрь не смотрит, поэтому чужая форма проходит проверку и умирает дальше — "+
		"промахом по несуществующему ресурсу либо отказом предусловия у соседа, то есть "+
		"отказом не о том. Словарь выводится из вызовов чеканки; правьте страницу, а не этот список.",
		census.TokensSeen, census.Judged, strings.Join(lines, "\n"))
}
