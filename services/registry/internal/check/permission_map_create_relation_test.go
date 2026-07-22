// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/services/registry/internal/check"
)

// TestPermissionMap_Create_EditorTierOnProject — RegistryService.Create is create-child:
// the in-service gate MUST match the proto annotation + api-gateway permission-catalog
// (required_relation="editor", scope project). Previously the in-service map used
// `v_create` on the project, which the iam reconciler does NOT materialize for an
// `edit` role (edit@project materializes editor + v_get/v_list/v_update on the project,
// but NOT v_create — that is an account-level "create the project" verb). So a
// project-editor was allowed by the gateway (editor) yet denied by the registry
// in-service check (v_create) → 403 on Create in its OWN project.
//
// Both listeners must resolve the SAME decision (defense-in-depth). Lock the relation
// to "editor" so the in-service gate cannot drift back to a verb the parent scope never
// grants an editor.
func TestPermissionMap_Create_EditorTierOnProject(t *testing.T) {
	m := check.PermissionMap()
	e, ok := m["/kacho.cloud.registry.v1.RegistryService/Create"]
	require.True(t, ok, "Create must be mapped")
	require.Equal(t, "editor", e.Relation,
		"Create is create-child → editor@project (proto required_relation + gateway catalog), NOT v_create")

	// Extractor still resolves the PARENT project (create-child scope).
	require.NotNil(t, e.Extract, "Create must carry a parent-project extractor")
	objType, objID, err := e.Extract(&registryv1.CreateRegistryRequest{ProjectId: "prjhc59hycvx38q2pxr4"})
	require.NoError(t, err)
	require.Equal(t, "project", objType, "Create scopes on the parent project")
	require.Equal(t, "prjhc59hycvx38q2pxr4", objID)
}
