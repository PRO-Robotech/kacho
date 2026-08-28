// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func iamKnowsNoEdgeOptions(t *testing.T) IamKnowsNoEdgeOptions {
	t.Helper()
	return IamKnowsNoEdgeOptions{
		Root:       repoRoot(t),
		GoRoot:     "services/iam",
		ChartRoots: []string{"deploy/helm/umbrella/charts/kacho-iam"},
	}
}

// TestIamIsNotTypedByItsConsumer — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`iamknowsnoedge_injection_test.go`): здесь только вердикт.
func TestIamIsNotTypedByItsConsumer(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditIamKnowsNoEdge(iamKnowsNoEdgeOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок» было бы
	// достижимо пустым обходом — то есть неотличимо от «ноль прочитанного».
	if census.GoFiles < 200 || census.ChartFiles < 3 {
		t.Fatalf("файлов Go %d, файлов чарта %d — обход пуст, вердикт беспредметен",
			census.GoFiles, census.ChartFiles)
	}

	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("\n  " + f.String())
	}
	t.Fatalf("владелец прав знает своего потребителя — находок %d:%s\n\n"+
		"iam объявлен листом графа рёбер: его зовут, он не зовёт никого. Соединение "+
		"открывает ПОТРЕБИТЕЛЬ (край), поэтому ни типа края, ни его адреса у владельца "+
		"прав быть не должно. Обязательная ручка адреса вдобавок означает отказ старта "+
		"там, где края нет вовсе.", len(findings), b.String())
}
