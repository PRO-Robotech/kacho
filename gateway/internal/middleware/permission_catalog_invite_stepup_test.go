// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// permission_catalog_invite_stepup_test.go — UserService/Invite is a
// PRIVILEGE-GRANT surface and must carry the step-up floor.
//
// InviteUserRequest carries an OPTIONAL `project_id` + `role_id` pair, and the
// Invite use-case creates the AccessBinding ATOMICALLY with the invite row. That
// makes Invite a grant-issuing RPC in exactly the sense the step-up policy names
// (credential mint / privilege grant / irreversible destroy → required_acr_min=2;
// routine resource CRUD stays at the AAL1 floor "1"). A grant path reachable at
// acr=1 while AccessBindingService/Create — the very RPC Invite inlines — demands
// acr=2 is a step-up bypass: the same privilege is handed out through the cheaper
// door.
//
// Both embedded catalog copies (gateway + iam) are generated from proto, so this
// pins the proto annotation through the runtime artefact the middleware actually
// reads.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestPermissionCatalog_UserInvite_RequiresStepUp — UserService/Invite carries
// required_acr_min="2" (grant-surface parity with AccessBindingService/Create).
func TestPermissionCatalog_UserInvite_RequiresStepUp(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	inv, ok := c.Lookup("kaname.cloud.iam.v1.UserService/Invite")
	require.True(t, ok, "UserService/Invite must exist in the catalog")
	assert.Equal(t, "2", inv.RequiredACRMin,
		"Invite creates an AccessBinding atomically (project_id+role_id) → privilege grant → step-up required")

	// Parity anchor: the RPC Invite inlines already demands step-up. If that
	// changes, the comparison above stops meaning what it says.
	ab, ok := c.Lookup("kaname.cloud.iam.v1.AccessBindingService/Create")
	require.True(t, ok)
	assert.Equal(t, "2", ab.RequiredACRMin,
		"AccessBindingService/Create is the grant-surface anchor Invite must match")

	// Non-grant User lifecycle stays routine — the fix must not blanket-raise
	// the whole service (step-up is sensitive-only, not blanket).
	for _, fqn := range []string{
		"kaname.cloud.iam.v1.UserService/Get",
		"kaname.cloud.iam.v1.UserService/Update",
		"kaname.cloud.iam.v1.UserService/Delete",
	} {
		e, ok := c.Lookup(fqn)
		require.True(t, ok, "missing from catalog: %s", fqn)
		assert.Equal(t, "1", e.RequiredACRMin,
			"routine User lifecycle must stay at the AAL1 floor: %s", fqn)
	}
}
