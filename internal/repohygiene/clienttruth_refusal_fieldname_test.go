// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestResourceRefusalNamesAContractField — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_refusal_fieldname_injection_test.go`): здесь только вердикт.
func TestResourceRefusalNamesAContractField(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditRefusalFieldNames(DefaultRefusalFieldNameOptions(repoRoot(t)), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: обход не пуст. «Находок ноль» обязано быть отличимо от
	// «прочитано ноль» — иначе вердикт беспредметен.
	if census.PackagesWithVocabulary < 20 {
		t.Fatalf("пакетов ресурса со словарём %d — дерево не обойдено, судить не по чему",
			census.PackagesWithVocabulary)
	}
	if census.VocabularyNames < 100 {
		t.Fatalf("имён в словарях %d — дескрипторы не слинкованы, словарь пуст",
			census.VocabularyNames)
	}
	if census.Judged < 20 {
		t.Fatalf("рассужено имён %d — производитель имени не распознан", census.Judged)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("  " + f.String() + "\n")
		}
		t.Fatalf("имена полей в отказах, которых нет в контракте ресурса (%d):\n%s",
			len(findings), b.String())
	}
}
