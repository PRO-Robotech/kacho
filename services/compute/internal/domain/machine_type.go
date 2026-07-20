// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "time"

// MachineTypeFamily — coarse machine-type class (COMP-1 F7). Mirrors
// computev1.MachineType_Family by ordinal so protoconv maps it trivially.
type MachineTypeFamily int32

// Значения MachineTypeFamily зеркалят computev1.MachineType_Family.
const (
	MachineTypeFamilyUnspecified MachineTypeFamily = iota // FAMILY_UNSPECIFIED
	MachineTypeFamilyStandard                             // STANDARD
	MachineTypeFamilyCompute                              // COMPUTE
	MachineTypeFamilyMemory                               // MEMORY
	MachineTypeFamilyGPU                                  // GPU
)

// CPUGuaranteeApplies reports whether cpuGuaranteePercent is meaningful for this
// family (COMP-1 F2/F8): only the CPU-flavor families STANDARD/COMPUTE/MEMORY.
// For GPU the field is accepted-and-ignored (does not modulate effectiveResources).
func (f MachineTypeFamily) CPUGuaranteeApplies() bool {
	switch f {
	case MachineTypeFamilyStandard, MachineTypeFamilyCompute, MachineTypeFamilyMemory:
		return true
	default:
		return false
	}
}

// Valid reports whether the family is one of the concrete classes (not UNSPECIFIED
// and within range). MachineType.Create requires a concrete family (F7).
func (f MachineTypeFamily) Valid() bool {
	return f >= MachineTypeFamilyStandard && f <= MachineTypeFamilyGPU
}

// machineTypeFamilyByName — canonical family vocabulary (uppercase wire names,
// COMP-1 F7/F19 filter=family=). Owned by the domain so the router/handler does
// not carry a duplicate string↔enum map.
var machineTypeFamilyByName = map[string]MachineTypeFamily{
	"STANDARD": MachineTypeFamilyStandard,
	"COMPUTE":  MachineTypeFamilyCompute,
	"MEMORY":   MachineTypeFamilyMemory,
	"GPU":      MachineTypeFamilyGPU,
}

// ParseMachineTypeFamily maps a wire family name ("GPU") to the enum. ok=false for
// an unknown / empty string — the caller rejects it as INVALID_ARGUMENT.
func ParseMachineTypeFamily(s string) (MachineTypeFamily, bool) {
	f, ok := machineTypeFamilyByName[s]
	return f, ok
}

// MachineTypeStatus — catalog availability (COMP-1 F7). Mirrors
// computev1.MachineType_Status by ordinal.
type MachineTypeStatus int32

// Значения MachineTypeStatus зеркалят computev1.MachineType_Status.
const (
	MachineTypeStatusUnspecified MachineTypeStatus = iota // STATUS_UNSPECIFIED
	MachineTypeStatusAvailable                            // AVAILABLE
	MachineTypeStatusDeprecated                           // DEPRECATED
	MachineTypeStatusRetired                              // RETIRED
)

// Valid reports whether the status is within the enum range.
func (s MachineTypeStatus) Valid() bool {
	return s >= MachineTypeStatusUnspecified && s <= MachineTypeStatusRetired
}

// Bookable reports whether an Instance may be created against a machine type in
// this status (COMP-1 F2/F7): AVAILABLE and DEPRECATED are bookable (DEPRECATED is
// discouraged but kept for compat); RETIRED is rejected on Instance.Create.
func (s MachineTypeStatus) Bookable() bool {
	return s == MachineTypeStatusAvailable || s == MachineTypeStatusDeprecated
}

// EffectiveResources — authoritative size mirror derived from the catalog entry
// (COMP-1 F2/F7). Memory is expressed in MiB (mebibytes), NOT bytes.
type EffectiveResources struct {
	VCPU      int32
	MemoryMiB int64
	GPUs      int32
	GPUType   string
}

// MachineType — the sync sizing catalog resource (COMP-1 F7). Flat control-plane
// record; a tenant picks a size before launch. GPU count is expressed by catalog
// granularity (gpu-a100-1/-2/-4/-8), not by a request field. EffectiveResources /
// AvailableZones are server-derived (output-only on the wire).
type MachineType struct {
	ID                 string
	Name               string
	Description        string
	Family             MachineTypeFamily
	EffectiveResources EffectiveResources
	AvailableZones     []string
	Status             MachineTypeStatus
	Labels             map[string]string
	CreatedAt          time.Time
}
