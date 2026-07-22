// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// permission_catalog_acr_invariant_test.go — SEC-acr-stepup-refinement (R3).
//
// Locks the step-up allowlist invariant (SEC-ACR-13 / I1 / I2): EXACTLY the 41
// named grant/credential/tenancy-root FQNs carry required_acr_min="2"; every
// other non-exempt RPC carries "1" (routine, AAL1 floor); exempt RPCs carry ""
// (no step-up requirement). Both embedded catalog copies (gateway + iam) are
// byte-identical, and the `permission` field of AccessBindingService/Create is
// NOT changed by the acr addition (net-strengthening: exempt-permission + acr=2).
//
// This is the primary RED→GREEN lock: before the proto refinement 372 RPCs carry
// "2" (blanket step-up); after it, exactly 41 do.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// sensitiveACR2Set — the 41 FQNs that MUST carry required_acr_min="2" after the
// refinement (grant-surface + credential + tenancy-root, domain-agnostic). Any
// drift (an RPC added or dropped) fails this test. Categories A–H per the
// APPROVED acceptance doc.
func sensitiveACR2Set() map[string]struct{} {
	fqns := []string{
		// A — credential mint/destroy (4)
		"kacho.cloud.iam.v1.UserTokenService/Issue",
		"kacho.cloud.iam.v1.UserTokenService/Revoke",
		"kacho.cloud.iam.v1.SAKeyService/Issue",
		"kacho.cloud.iam.v1.SAKeyService/Revoke",
		// B — iam binding grant (4; Create is exempt-permission + acr=2, net-strengthening)
		"kacho.cloud.iam.v1.AccessBindingService/Create",
		"kacho.cloud.iam.v1.AccessBindingService/Update",
		"kacho.cloud.iam.v1.AccessBindingService/Delete",
		"kacho.cloud.iam.v1.AccessBindingService/Revoke",
		// C — compute per-resource grant (22; non-iam grant-surface)
		"kacho.cloud.compute.v1.DiskService/SetAccessBindings",
		"kacho.cloud.compute.v1.DiskService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.DiskPlacementGroupService/SetAccessBindings",
		"kacho.cloud.compute.v1.DiskPlacementGroupService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.FilesystemService/SetAccessBindings",
		"kacho.cloud.compute.v1.FilesystemService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.GpuClusterService/SetAccessBindings",
		"kacho.cloud.compute.v1.GpuClusterService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.HostGroupService/SetAccessBindings",
		"kacho.cloud.compute.v1.HostGroupService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.ImageService/SetAccessBindings",
		"kacho.cloud.compute.v1.ImageService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.InstanceService/SetAccessBindings",
		"kacho.cloud.compute.v1.InstanceService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.PlacementGroupService/SetAccessBindings",
		"kacho.cloud.compute.v1.PlacementGroupService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.SnapshotScheduleService/SetAccessBindings",
		"kacho.cloud.compute.v1.SnapshotScheduleService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.SnapshotService/SetAccessBindings",
		"kacho.cloud.compute.v1.SnapshotService/UpdateAccessBindings",
		"kacho.cloud.compute.v1.instancegroup.InstanceGroupService/SetAccessBindings",
		"kacho.cloud.compute.v1.instancegroup.InstanceGroupService/UpdateAccessBindings",
		// D — group membership grant + group destroy (3; Delete = revoke-by-all, R3/B-2)
		"kacho.cloud.iam.v1.GroupService/AddMember",
		"kacho.cloud.iam.v1.GroupService/RemoveMember",
		"kacho.cloud.iam.v1.GroupService/Delete",
		// E — role policy mutation (2)
		"kacho.cloud.iam.v1.RoleService/Update",
		"kacho.cloud.iam.v1.RoleService/Delete",
		// F — condition policy mutation (2)
		"kacho.cloud.iam.v1.ConditionsService/Update",
		"kacho.cloud.iam.v1.ConditionsService/Delete",
		// G — cluster-admin grant (2)
		"kacho.cloud.iam.v1.InternalClusterService/GrantAdmin",
		"kacho.cloud.iam.v1.InternalClusterService/RevokeAdmin",
		// H — tenancy-root destroy (2)
		"kacho.cloud.iam.v1.AccountService/Delete",
		"kacho.cloud.iam.v1.ProjectService/Delete",
	}
	set := make(map[string]struct{}, len(fqns))
	for _, f := range fqns {
		set[f] = struct{}{}
	}
	return set
}

// TestPermissionCatalog_ACR_41SetInvariant — SEC-ACR-13 / I1: the set of FQNs
// carrying required_acr_min="2" is EXACTLY the 41 named sensitive RPCs.
func TestPermissionCatalog_ACR_41SetInvariant(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	sensitive := sensitiveACR2Set()
	require.Len(t, sensitive, 41, "the acceptance-doc sensitive set must contain exactly 41 FQNs")

	got2 := map[string]struct{}{}
	for _, fqn := range c.FQNs() {
		e, ok := c.Lookup(fqn)
		require.True(t, ok)
		if e.RequiredACRMin == "2" {
			got2[fqn] = struct{}{}
		}
	}

	// Every named sensitive FQN carries "2", and NOTHING else does.
	for fqn := range sensitive {
		e, ok := c.Lookup(fqn)
		require.True(t, ok, "sensitive FQN missing from catalog: %s", fqn)
		assert.Equal(t, "2", e.RequiredACRMin, "sensitive FQN must carry acr=2: %s", fqn)
	}
	for fqn := range got2 {
		_, want := sensitive[fqn]
		assert.True(t, want, "FQN carries acr=2 but is NOT in the sensitive-41 allowlist (over-inclusion): %s", fqn)
	}
	assert.Len(t, got2, 41, "exactly 41 FQNs must carry required_acr_min=2")
}

// TestPermissionCatalog_ACR_ComplementNotTwo — SEC-ACR-13 / I1: explicit
// regression points that MUST NOT carry "2" (they were downgraded to "1").
func TestPermissionCatalog_ACR_ComplementNotTwo(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	routine := []string{
		// B6 author-inert create → routine
		"kacho.cloud.iam.v1.RoleService/Create",
		"kacho.cloud.iam.v1.ConditionsService/Create",
		"kacho.cloud.iam.v1.GroupService/Create",
		// per-resource ListAccessBindings — reads → routine
		"kacho.cloud.compute.v1.InstanceService/ListAccessBindings",
		"kacho.cloud.iam.v1.AccessBindingService/ListByScope",
		"kacho.cloud.iam.v1.AccessBindingService/ListAssignableRoles",
		"kacho.cloud.iam.v1.AccessBindingService/ListBySubject",
		// B3 subject-delete → routine
		"kacho.cloud.iam.v1.ServiceAccountService/Delete",
		"kacho.cloud.iam.v1.UserService/Delete",
		// B5 non-iam Internal*-admin (sample) → routine
		"kacho.cloud.geo.v1.InternalRegionService/Create",
		"kacho.cloud.vpc.v1.InternalAddressPoolService/Create",
		"kacho.cloud.compute.v1.InternalMachineTypeService/Create",
		// B4 cluster reads → routine
		"kacho.cloud.iam.v1.InternalClusterService/Get",
		"kacho.cloud.iam.v1.InternalClusterService/ListAdmins",
		// routine resource lifecycle
		"kacho.cloud.vpc.v1.NetworkService/Create",
		"kacho.cloud.compute.v1.InstanceService/Create",
		// group non-destructive lifecycle (Delete is sensitive, these are not)
		"kacho.cloud.iam.v1.GroupService/AddMember", // sanity: this IS "2" (asserted below, negative-control excluded)
	}
	for _, fqn := range routine {
		if fqn == "kacho.cloud.iam.v1.GroupService/AddMember" {
			continue // control — AddMember is sensitive; asserted in the 41-set test
		}
		e, ok := c.Lookup(fqn)
		require.True(t, ok, "routine FQN missing from catalog: %s", fqn)
		assert.NotEqual(t, "2", e.RequiredACRMin, "routine FQN must NOT carry acr=2: %s (got %q)", fqn, e.RequiredACRMin)
		assert.Equal(t, "1", e.RequiredACRMin, "routine non-exempt FQN must carry acr=1 (AAL1 floor): %s", fqn)
	}
}

// TestPermissionCatalog_ACR_GroupDeleteSensitive — R3/B-2: GroupService/Delete
// is revoke-by-all → sensitive ("2"); the non-destructive group lifecycle is not.
func TestPermissionCatalog_ACR_GroupDeleteSensitive(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	del, ok := c.Lookup("kacho.cloud.iam.v1.GroupService/Delete")
	require.True(t, ok)
	assert.Equal(t, "2", del.RequiredACRMin, "GroupService/Delete is revoke-by-all → sensitive (R3/B-2)")

	for _, fqn := range []string{
		"kacho.cloud.iam.v1.GroupService/Create",
		"kacho.cloud.iam.v1.GroupService/ListMembers",
	} {
		e, ok := c.Lookup(fqn)
		require.True(t, ok)
		assert.NotEqual(t, "2", e.RequiredACRMin, "non-destructive group lifecycle must be routine: %s", fqn)
	}
}

// TestPermissionCatalog_ACR_CreateNetStrengthening — SEC-ACR-06 / B1 / I4:
// AccessBindingService/Create carries acr="2" WHILE permission stays "<exempt>"
// (orthogonal fields; StepUpGate enforces, FGA scope-Check stays skipped).
func TestPermissionCatalog_ACR_CreateNetStrengthening(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	e, ok := c.Lookup("kacho.cloud.iam.v1.AccessBindingService/Create")
	require.True(t, ok)
	assert.Equal(t, "2", e.RequiredACRMin, "Create must gain acr=2 (close create-instead-of-Update bypass)")
	assert.Equal(t, "<exempt>", e.Permission, "Create permission must stay <exempt> (acr/permission are orthogonal — net-strengthening)")
	assert.True(t, e.IsExempt(), "Create must remain FGA-exempt")
}

// TestPermissionCatalog_ACR_CountsAndByteIdentity — SEC-ACR-13 / I2: the whole
// catalog splits 41×"2" / 332×"1" / 65×"" = 438, and both embedded copies
// (gateway + iam) are byte-identical.
func TestPermissionCatalog_ACR_CountsAndByteIdentity(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	var n2, n1, nEmpty int
	for _, fqn := range c.FQNs() {
		e, _ := c.Lookup(fqn)
		switch e.RequiredACRMin {
		case "2":
			n2++
		case "1":
			n1++
		case "":
			nEmpty++
		default:
			t.Fatalf("unexpected required_acr_min %q on %s", e.RequiredACRMin, fqn)
		}
	}
	assert.Equal(t, 41, n2, "sensitive count")
	assert.Equal(t, 332, n1, "routine count")
	assert.Equal(t, 65, nEmpty, "no-requirement (exempt) count")
	assert.Equal(t, 438, n2+n1+nEmpty, "catalog total")

	// Byte-identity of the two embedded copies.
	gw := middleware.EmbeddedPermissionCatalogJSON()
	iamPath := iamCatalogPath(t)
	iamBytes, err := os.ReadFile(iamPath)
	require.NoError(t, err, "read iam embedded catalog copy")
	assert.Equal(t, string(gw), string(iamBytes),
		"gateway and iam embedded permission_catalog.json copies must be byte-identical")
}

// iamCatalogPath resolves the iam embedded catalog copy relative to THIS test
// source file (robust to the test's working directory).
func iamCatalogPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	// this file: <repo>/gateway/internal/middleware/permission_catalog_acr_invariant_test.go
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	return filepath.Join(repoRoot, "services", "iam", "internal", "apps", "kacho",
		"seed", "embedded", "permission_catalog.json")
}
