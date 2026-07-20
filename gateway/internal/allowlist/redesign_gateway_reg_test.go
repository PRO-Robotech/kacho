// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package allowlist_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
)

// TestGateway_RedesignReg_PublicVsInternal — redesign-2026 new PUBLIC RPCs must be
// in the allowlist and NOT caught by HasInternalSuffix, so the gRPC director routes
// them to their backend on the external listener:
//   - compute MachineTypeService.Get/List (read-only sizing catalog; admin CRUD is
//     InternalMachineTypeService — internal-only, ban #6);
//   - storage ImageService CRUD + ListOperations;
//   - vpc NetworkService.AddCidrBlocks/RemoveCidrBlocks (:verb supernet growth);
//   - iam AccessBindingService.List (unified F11) + Revoke (soft-revoke :verb).
//
// The Internal* counterparts (InternalMachineTypeService admin CRUD,
// InternalImageService.GetInternal infra-projection) MUST NOT be in the allowlist
// and MUST be caught by HasInternalSuffix — they never route onto the external
// endpoint (ban #6). InternalIAMService.GetRoleCompiled is likewise Internal-only.
func TestGateway_RedesignReg_PublicVsInternal(t *testing.T) {
	publicMethods := []string{
		// compute MachineTypeService (public read-only sizing catalog)
		"/kacho.cloud.compute.v1.MachineTypeService/Get",
		"/kacho.cloud.compute.v1.MachineTypeService/List",
		// storage ImageService (public CRUD + ListOperations)
		"/kacho.cloud.storage.v1.ImageService/Get",
		"/kacho.cloud.storage.v1.ImageService/List",
		"/kacho.cloud.storage.v1.ImageService/Create",
		"/kacho.cloud.storage.v1.ImageService/Update",
		"/kacho.cloud.storage.v1.ImageService/Delete",
		"/kacho.cloud.storage.v1.ImageService/ListOperations",
		// vpc NetworkService :verb supernet-growth actions
		"/kacho.cloud.vpc.v1.NetworkService/AddCidrBlocks",
		"/kacho.cloud.vpc.v1.NetworkService/RemoveCidrBlocks",
		// iam AccessBindingService unified List + soft-revoke
		"/kacho.cloud.iam.v1.AccessBindingService/List",
		"/kacho.cloud.iam.v1.AccessBindingService/Revoke",
	}
	for _, m := range publicMethods {

		t.Run("public/"+m, func(t *testing.T) {
			if !allowlist.IsAllowed(m) {
				t.Errorf("публичный redesign-метод %q должен быть в allowlist", m)
			}
			if allowlist.HasInternalSuffix(m) {
				t.Errorf("публичный redesign-метод %q не должен ловиться HasInternalSuffix", m)
			}
		})
	}

	internalMethods := []string{
		// compute InternalMachineTypeService (admin CRUD, :9091)
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Create",
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Update",
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Delete",
		// storage InternalImageService (infra projection, :9091)
		"/kacho.cloud.storage.v1.InternalImageService/GetInternal",
		// iam InternalIAMService.GetRoleCompiled (compiled-role read, :9091) —
		// MUST NEVER reach the external endpoint (task hard requirement).
		"/kacho.cloud.iam.v1.InternalIAMService/GetRoleCompiled",
	}
	for _, m := range internalMethods {

		t.Run("internal/"+m, func(t *testing.T) {
			if allowlist.IsAllowed(m) {
				t.Errorf("Internal redesign-метод %q НЕ должен быть в allowlist (ban #6)", m)
			}
			if !allowlist.HasInternalSuffix(m) {
				t.Errorf("Internal redesign-метод %q должен ловиться HasInternalSuffix", m)
			}
		})
	}
}
