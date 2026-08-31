// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func docsRefusedValueOptions(t *testing.T) DocsRefusedValueOptions {
	t.Helper()
	return DocsRefusedValueOptions{
		Root: repoRoot(t),
		// Перечень судимых объявлен явно — это граница полосы, а не свойство
		// дерева; остаток назван в шапке анализатора.
		Services: []DocsRefusedValueService{
			{Name: "compute", CodeDir: "services/compute/internal", DocsDir: "services/compute/docs/content"},
			{Name: "storage", CodeDir: "services/storage/internal", DocsDir: "services/storage/docs/content"},
		},
	}
}

// TestDocsNamingARefusedValueSaysItIsRefused — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`ct2_docs_refused_value_injection_test.go`): здесь только вердикт.
func TestDocsNamingARefusedValueSaysItIsRefused(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditDocsRefusedValue(docsRefusedValueOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премисы: прочитано то, что заведомо есть.
	if census.GoFiles < 50 {
		t.Fatalf("исходников Go прочитано %d — код сервисов не обойден, словарь строить не из чего",
			census.GoFiles)
	}
	if census.StringLiterals < 200 {
		t.Fatalf("строковых литералов собрано %d — разбор исходников не состоялся",
			census.StringLiterals)
	}
	if census.RefusalMsgs == 0 {
		t.Fatalf("ни одно сообщение не несёт связки отказа — либо тон отказов сменился, " +
			"либо распознаватель связки устарел; в обоих случаях вердикт беспредметен")
	}
	if census.RefusedValues == 0 {
		t.Fatalf("отвергаемых значений выведено 0 — судить нечего, «находок ноль» было бы " +
			"неотличимо от «словарь пуст»")
	}
	if census.DocFiles < 20 {
		t.Fatalf("страниц клиентской документации %d — обход пуст, вердикт беспредметен", census.DocFiles)
	}
	if census.Judged == 0 {
		t.Fatalf("упоминаний рассужено 0 — ни одна страница не назвала ни одного отвергаемого " +
			"значения, и сверка не состоялась")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("клиентская страница называет значение, которое условие отвергает синхронно, "+
		"и молчит об этом (отвергаемых значений %d, упоминаний рассужено %d):\n%s\n\n"+
		"Это «принято-и-проигнорировано» с обратной стороны: возможность ОБЪЯВЛЕНА и "+
		"неисполнима. Вызывающий строит запрос по странице и платит круг запроса, а отказ "+
		"не называет страницу, которая ввела в заблуждение. Исходов два: сказать об отказе "+
		"словами самого отказа — либо реализовать возможность. Словарь выводится из "+
		"построителя отказа в коде сервиса; правьте страницу, а не этот список.",
		census.RefusedValues, census.Judged, strings.Join(lines, "\n"))
}
