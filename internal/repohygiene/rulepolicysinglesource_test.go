// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// rulepolicysinglesource_test.go — держатель §8 п. 6 приёмки
// `role-ownership-tier-apart-from-cluster-anchor.md`: политика послабления
// подстановки выводится из строки ОДНИМ местом (задача продукта #1032).
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `rulepolicysinglesource_injection_test.go`.
package repohygiene

import (
	"strings"
	"testing"
)

// rulePolicyCensusFloor — файлов каталога домена, ниже которого обход
// беспредметен. Каталог несёт десятки файлов; порог низкий намеренно — он
// отличает «прочитано мало» от «не прочитано ничего», а не сторожит рост.
const rulePolicyCensusFloor = 5

func TestRulePolicyIsDerivedInOnePlace(t *testing.T) {
	root := repoRoot(t)

	sites, census, err := ScanRulePolicySites(root)
	if err != nil {
		t.Fatalf("обход каталога домена не состоялся, вердикта нет ни по одному файлу: %v", err)
	}

	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	t.Logf("перепись: файлов домена прочитано %d · литералов политики %d · объявлений вывода %d",
		census.FilesRead, census.Literals, census.Derivers)

	if census.FilesRead < rulePolicyCensusFloor {
		t.Fatalf("прочитано файлов %d — обход пуст либо усечён, и молчание гейта было бы "+
			"неотличимо от чистоты", census.FilesRead)
	}
	if census.Literals == 0 {
		t.Fatalf("литералов политики ноль: тип %q переименован или снят, и гейт стережёт "+
			"предмет, которого нет", rulePolicyTypeName)
	}

	if findings := RulePolicyFindings(sites, census); len(findings) > 0 {
		t.Fatalf("политика подстановки объявляется не одним местом:\n%s",
			strings.Join(findings, "\n"))
	}
}
