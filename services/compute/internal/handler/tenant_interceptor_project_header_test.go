// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
)

// TestTenantFromMetadata_ReadsProjectIDHeader locks the post-project-rename wire
// contract: caller project-scope is carried by the canonical `x-kacho-project-id`
// header (matching the renamed project_id model and the kacho-vpc sibling), NOT
// the vestigial `x-kacho-folder-id`. Resource access is NOT gated on this scope
// (that is the per-RPC FGA Check + listauthz); it feeds IsAnonymous, so reading
// the wrong header name would make a scoped caller look anonymous.
func TestTenantFromMetadata_ReadsProjectIDHeader(t *testing.T) {
	md := metadata.New(map[string]string{
		"x-kacho-project-id": "p1",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	trusted := tenantFromMetadata(ctx, true, true /*internal listener*/)
	assert.Contains(t, trusted.ProjectIDs, "p1",
		"trusted peer's x-kacho-project-id must populate ProjectIDs")
	assert.False(t, trusted.IsAnonymous(),
		"a caller with a trusted project-scope is not anonymous for the AuthN gate")
}

// TestTenantFromMetadata_IgnoresLegacyFolderHeader ensures the legacy
// `x-kacho-folder-id` header is no longer honoured — it must not grant scope.
func TestTenantFromMetadata_IgnoresLegacyFolderHeader(t *testing.T) {
	md := metadata.New(map[string]string{
		"x-kacho-folder-id": "f1",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	trusted := tenantFromMetadata(ctx, true, true /*internal listener*/)
	assert.Empty(t, trusted.ProjectIDs,
		"legacy x-kacho-folder-id must not populate ProjectIDs")
}
