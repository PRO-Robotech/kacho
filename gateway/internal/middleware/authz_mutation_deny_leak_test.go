// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestAuthz_MutationDeny_NoDenyReasonLeak — #64 registry-authz finding. A mutation
// deny correctly STAYS PermissionDenied(7) (not hidden as 404), but its details
// must NOT echo the raw deny reason nor a `deny_reasons` metadata key — both are
// authz/existence oracles (e.g. "no path: account:acc_secret has no v_get" reveals
// the resource + why). The machine-readable violation TYPE (category) is preserved
// for step-up UX; only the leaky Description/metadata are removed.
func TestAuthz_MutationDeny_NoDenyReasonLeak(t *testing.T) {
	checker := &fakeChecker{allowed: false, reasons: []string{"no path: account:acc_secret has no v_delete for user:usr_x"}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, accountDeleteEntry), checker)
	_, err := mw.Unary()(withTokenMD("usr_x", "user"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.iam.v1.AccountService/Delete"},
		func(ctx context.Context, req any) (any, error) { return nil, nil })
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code(), "mutation deny stays 403")
	for _, d := range st.Details() {
		s := strings.ToLower(toString(d))
		assert.NotContains(t, s, "deny_reasons", "403 deny detail must not carry deny_reasons metadata")
		assert.NotContains(t, s, "acc_secret", "403 deny detail must not echo the raw resource in the reason")
		assert.NotContains(t, s, "v_delete", "403 deny detail must not echo the FGA relation in the reason")
	}
	_ = context.Background()
}
