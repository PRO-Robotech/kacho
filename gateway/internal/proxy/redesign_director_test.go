// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// TestResolver_RedesignReg_PublicVsInternal — redesign-2026 new public RPCs
// resolve through the gRPC director to their owning backend (domain-prefix
// `kacho.cloud.<domain>.v1.*`): compute MachineTypeService, storage ImageService,
// vpc NetworkService CIDR verbs, iam AccessBindingService List/Revoke. The
// Internal* counterparts (InternalMachineTypeService, InternalImageService,
// InternalIAMService.GetRoleCompiled) are blocked by HasInternalSuffix — they
// never resolve and never reach a backend on the external listener (ban #6).
func TestResolver_RedesignReg_PublicVsInternal(t *testing.T) {
	backends := makeTestBackends(t, []string{"iam", "vpc", "compute", "storage"})
	resolve := proxy.Resolver(backends)

	public := []struct{ method, domain string }{
		{"/kacho.cloud.compute.v1.MachineTypeService/Get", "compute"},
		{"/kacho.cloud.compute.v1.MachineTypeService/List", "compute"},
		{"/kacho.cloud.storage.v1.ImageService/Get", "storage"},
		{"/kacho.cloud.storage.v1.ImageService/List", "storage"},
		{"/kacho.cloud.storage.v1.ImageService/Create", "storage"},
		{"/kacho.cloud.storage.v1.ImageService/Update", "storage"},
		{"/kacho.cloud.storage.v1.ImageService/Delete", "storage"},
		{"/kacho.cloud.storage.v1.ImageService/ListOperations", "storage"},
		{"/kacho.cloud.vpc.v1.NetworkService/AddCidrBlocks", "vpc"},
		{"/kacho.cloud.vpc.v1.NetworkService/RemoveCidrBlocks", "vpc"},
		{"/kacho.cloud.iam.v1.AccessBindingService/List", "iam"},
		{"/kacho.cloud.iam.v1.AccessBindingService/Revoke", "iam"},
	}
	for _, tc := range public {
		_, conn, ok := resolve(tc.method)
		if !ok || conn != backends[tc.domain] {
			t.Errorf("public redesign-метод %q должен резолвиться на %s-backend (ok=%v)", tc.method, tc.domain, ok)
		}
	}

	internal := []string{
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Create",
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Update",
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Delete",
		"/kacho.cloud.storage.v1.InternalImageService/GetInternal",
		"/kacho.cloud.iam.v1.InternalIAMService/GetRoleCompiled",
	}
	for _, m := range internal {
		if _, _, ok := resolve(m); ok {
			t.Errorf("Internal redesign-метод %q должен быть заблокирован (ban #6)", m)
		}
	}
}
