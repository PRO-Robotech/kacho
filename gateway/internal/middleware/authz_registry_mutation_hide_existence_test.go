// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// Security regression lock (security.md #6 / hardening-invariant #5): an
// authenticated deny on a verb-bearing registry MUTATION (RegistryService.Update
// / .Delete on a concrete registry_registry scope) must be surfaced as an OPAQUE
// hide-existence NotFound — never PermissionDenied with the iam enforcement-Check
// deny reason echoed into the client response. That reason
// (authorize_service.go builds "subject … lacks relation \"admin\" on …; current
// direct relations: [viewer, v_update]" and InternalIAMService.Check returns it
// as CheckResponse.reason) is a relations/existence oracle: it confirms the
// resource exists and enumerates the subject's direct grants.
//
// Observed prod leak (registry-authz newman update-viewer / delete-viewer): the
// gateway chose writeHTTPDeny/buildGRPCDenyStatus (403 + `deny_reasons` in the
// ErrorInfo/PreconditionFailure details) because the RegistryService/Update and
// /Delete catalog entries were NOT marked hide-existence — only the /Get read was
// hidden (via the v_get heuristic). The registry backend is already opaque
// (IAMCheckClient drops CheckResponse.reason); the client-facing leak is the
// gateway echo, closed by marking these two RPCs hide_existence in the proto →
// catalog so denyDecision returns outcomeNotFound.
//
// This test drives the FULL gateway chain (decide → denyDecision → gRPCStatus)
// against the REAL embedded permission catalog, so it locks BOTH the wiring
// (registry Update/Delete are hide-existence in the shipped catalog) and the
// observable (no deny-reason tokens reach the client).

// registryLeakReason mirrors the exact FGA relation-enumeration reason iam's
// enforcement Check returns for the viewer-deny — the string that must never
// reach the client.
const registryLeakReason = `subject user:usr_x lacks relation "admin" on registry_registry:regabcdefghjkmnpqrst; current direct relations: [viewer, v_update]`

// oracleTokens — substrings that, if present anywhere in the client-facing
// status (message OR serialized details), constitute a relations/existence leak.
var oracleTokens = []string{"deny_reasons", "direct relations", "current direct", "lacks relation"}

func TestAuthz_GRPC_RegistryMutationDeny_OpaqueHideExistence(t *testing.T) {
	// Well-formed registry id (prefix "reg" + 17 crockford chars) so the gateway
	// malformed-id short-circuit passes and the request reaches the FGA Check.
	const registryID = "regabcdefghjkmnpqrst"

	// The REAL shipped catalog is the source of truth: the wiring lock lives here.
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	cases := []struct {
		name   string
		fqn    string
		req    any
		method string
	}{
		{
			name:   "Update",
			fqn:    "kacho.cloud.registry.v1.RegistryService/Update",
			req:    &registryv1.UpdateRegistryRequest{RegistryId: registryID},
			method: "/kacho.cloud.registry.v1.RegistryService/Update",
		},
		{
			name:   "Delete",
			fqn:    "kacho.cloud.registry.v1.RegistryService/Delete",
			req:    &registryv1.DeleteRegistryRequest{RegistryId: registryID},
			method: "/kacho.cloud.registry.v1.RegistryService/Delete",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Wiring lock: the shipped catalog must mark this mutation hide-existence.
			entry, ok := catalog.Lookup(tc.fqn)
			require.True(t, ok, "catalog must contain %s", tc.fqn)
			assert.True(t, entry.HidesExistenceOnDeny(tc.fqn),
				"%s deny must hide existence (no PermissionDenied + no deny_reasons echo)", tc.fqn)

			checker := &fakeChecker{allowed: false, reasons: []string{registryLeakReason}}
			mw := buildAuthzMiddleware(t, catalog, checker)

			_, callErr := mw.Unary()(withTokenMD("usr_x", "user"), tc.req,
				&grpc.UnaryServerInfo{FullMethod: tc.method},
				func(ctx context.Context, req any) (any, error) {
					t.Fatal("handler must not be reached on deny")
					return nil, nil
				})
			require.Error(t, callErr)

			st, _ := status.FromError(callErr)
			assert.Equal(t, codes.NotFound, st.Code(),
				"verb-bearing registry mutation deny must hide existence as NotFound, not PermissionDenied")

			// The FULL client-facing status (message + all details) must carry NONE
			// of the relation/existence oracle tokens. Serialize the status proto so
			// the assertion also covers the PreconditionFailure / ErrorInfo details
			// where the gateway would otherwise put deny_reasons.
			raw, mErr := json.Marshal(st.Proto())
			require.NoError(t, mErr)
			blob := strings.ToLower(string(raw))
			for _, tok := range oracleTokens {
				assert.NotContainsf(t, blob, tok,
					"client-facing deny leaked oracle token %q: %s", tok, string(raw))
			}
		})
	}
}
