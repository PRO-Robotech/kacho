// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// TestRegistry_REG_1_15_DefaultRepositoryVisibilityJSONName — F5 rename lock: tenant
// JSON-контракт несёт camelCase `defaultRepositoryVisibility` (REG-1 F5), НЕ прежнее
// `defaultVisibility`. Ре-интродукция старого имени сломает клиентский контракт.
// # verifies REG-1-15
func TestRegistry_REG_1_15_DefaultRepositoryVisibilityJSONName(t *testing.T) {
	uc := newUC(&mockRepo{}, &mockZot{}, &mockIAM{}, newMemOps())
	reg := &domain.Registry{
		ID:                "reg-c9k2000000000",
		ProjectID:         "prj-7h3n",
		Name:              "payments",
		DefaultVisibility: domain.VisibilityPublic,
		Status:            domain.RegistryStatusActive,
	}
	js, err := protojson.Marshal(uc.ProtoRegistry(reg))
	require.NoError(t, err)
	body := string(js)

	require.Contains(t, body, `"defaultRepositoryVisibility":"PUBLIC"`,
		"F5: renamed camelCase field present with mapped value")
	require.False(t, strings.Contains(body, `"defaultVisibility"`),
		"F5: old field name must NOT appear (rename, not alias)")
	// F1 field-absence lock (REG-1-02): нет globalSlug/displayName/top-level visibility.
	require.NotContains(t, body, "globalSlug", "F1: globalSlug field must be absent")
	require.NotContains(t, body, "displayName", "F1: displayName field must be absent")
	require.NotContains(t, body, `"visibility"`, "F1: registry carries no top-level visibility")
}
