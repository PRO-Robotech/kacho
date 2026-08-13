// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "testing"

// Проба предиката `CPUGuaranteeApplies` снята вместе с ним самим: предикат
// объявлял правило («доля гарантированного CPU осмысленна не для всех семейств»),
// которого не применял ни один путь запроса. Проба закрепляла ОТВЕТ предиката, а
// не поведение продукта, и оставалась бы зелёной, даже если бы правило никогда не
// исполнялось — что и было. Решение о самом правиле (приём и игнорирование для
// семейства ускорителей) записано приёмкой COMP-1, сценарий COMP-1-08, и этой
// правкой не затрагивается.

// TestMachineTypeFamily_Valid — Create requires a concrete family (not UNSPECIFIED).
func TestMachineTypeFamily_Valid(t *testing.T) {
	if MachineTypeFamilyUnspecified.Valid() {
		t.Error("FAMILY_UNSPECIFIED must be invalid")
	}
	for _, f := range []MachineTypeFamily{MachineTypeFamilyStandard, MachineTypeFamilyCompute, MachineTypeFamilyMemory, MachineTypeFamilyGPU} {
		if !f.Valid() {
			t.Errorf("family %d must be valid", f)
		}
	}
	if MachineTypeFamily(99).Valid() {
		t.Error("out-of-range family must be invalid")
	}
}

// TestMachineTypeStatus_Bookable — COMP-1 F2/F7: AVAILABLE + DEPRECATED are
// bookable on Instance.Create; RETIRED is rejected.
func TestMachineTypeStatus_Bookable(t *testing.T) {
	if !MachineTypeStatusAvailable.Bookable() {
		t.Error("AVAILABLE must be bookable")
	}
	if !MachineTypeStatusDeprecated.Bookable() {
		t.Error("DEPRECATED must be bookable (compat window)")
	}
	if MachineTypeStatusRetired.Bookable() {
		t.Error("RETIRED must NOT be bookable")
	}
	if MachineTypeStatusUnspecified.Bookable() {
		t.Error("UNSPECIFIED must NOT be bookable")
	}
}
