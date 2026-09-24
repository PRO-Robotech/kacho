// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_emission_expiry_injection_test.go — проба, требующая ЭМИССИИ
// ручки, истекает вместе с её читателем (#2778).
//
// Требование «чарт эмитирует переменную» верно, пока переменную читает процесс.
// Снятие читателя переворачивает его: эмиссия без читателя — принятое и не
// прочитанное значение, а проба, продолжающая её требовать, пришпиливает то,
// что снимающее изменение обязано снять, и краснеет посреди его полосы.
//
// Каждая пара меняет РОВНО ОДИН факт против законного близнеца.
package deploy_test

import (
	"strings"
	"testing"
)

const expiryDeployment = "env:\n  - name: KACHO_X_URL\n    value: x\n"

// Законный близнец: имя объявлено процессом, не снято и эмитируется — молчание.
func TestRevocationEmissionExpiry_LiveKnobEmittedIsSilent(t *testing.T) {
	got := judgeRevocationEmission([]string{"KACHO_X_URL"}, expiryDeployment,
		map[string]bool{"KACHO_X_URL": true}, map[string]string{})
	if len(got) != 0 {
		t.Fatalf("живая эмитируемая ручка объявлена находкой: %q", got)
	}
}

// Ручка снята с процесса (ведомость её называет) — проба обязана сказать, что
// её требование пережило предмет, а не требовать эмиссии дальше.
func TestRevocationEmissionExpiry_RetiredKnobIsNotDemanded(t *testing.T) {
	got := judgeRevocationEmission([]string{"KACHO_X_URL"}, "env: []\n",
		map[string]bool{}, map[string]string{"KACHO_X_URL": "снята вместе с читателем"})
	if len(got) != 1 {
		t.Fatalf("ожидалась ровно одна находка об истёкшем требовании, получено: %q", got)
	}
	if strings.Contains(got[0], "no longer emits") {
		t.Fatalf("проба требует эмиссии СНЯТОЙ ручки — пришпиливает то, что обязано уйти: %q", got[0])
	}
	if !strings.Contains(got[0], "снят") {
		t.Fatalf("находка не называет причину — снятие ручки: %q", got[0])
	}
}

// Ручку не объявляет процесс и не называет ведомость — перечень пробы назвал
// имя, у которого нет читателя: требование эмиссии ложно в обе стороны.
func TestRevocationEmissionExpiry_UndeclaredKnobIsAFinding(t *testing.T) {
	got := judgeRevocationEmission([]string{"KACHO_X_URL"}, expiryDeployment,
		map[string]bool{}, map[string]string{})
	if len(got) != 1 || !strings.Contains(got[0], "не объявляет") {
		t.Fatalf("требование эмиссии ручки без читателя не найдено: %q", got)
	}
}
