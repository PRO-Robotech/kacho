// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "testing"

// TestMachineTypeFamily_CPUGuaranteeApplies — COMP-1 F2/F8: cpuGuaranteePercent is
// meaningful only for CPU-flavor families; for GPU it is accepted-and-ignored.
func TestMachineTypeFamily_CPUGuaranteeApplies(t *testing.T) {
	cases := []struct {
		f    MachineTypeFamily
		want bool
	}{
		{MachineTypeFamilyStandard, true},
		{MachineTypeFamilyCompute, true},
		{MachineTypeFamilyMemory, true},
		{MachineTypeFamilyGPU, false},
		{MachineTypeFamilyUnspecified, false},
	}
	for _, c := range cases {
		if got := c.f.CPUGuaranteeApplies(); got != c.want {
			t.Errorf("family %d CPUGuaranteeApplies() = %v, want %v", c.f, got, c.want)
		}
	}
}

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
