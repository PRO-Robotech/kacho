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

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// End-to-end (through the full authorize → denyDecision → NotFound chain): a
// verb-bearing vpc read-deny surfaces the Kachō contract-tone message
// "<Resource> <id> not found" with the caller-supplied id, byte-matching the
// backend's real NotFound so hide-existence stays indistinguishable from a
// genuine miss — and still leaks no deny reasons.
//
// Catalog entry mirrors the embedded permission_catalog.json (SubnetService/Get:
// v_get + concrete subnet_id scope → hide existence on deny).
const subnetGetEntry = `{"fqn":"kacho.cloud.vpc.v1.SubnetService/Get","permission":"vpc.subnets.get","required_relation":"v_get","scope_extractor":{"object_type":"vpc_subnet","from_request_field":"subnet_id"},"required_acr_min":"2"}`

func TestAuthz_GRPC_VpcReadDeny_ContractNotFoundMessage(t *testing.T) {
	// Well-formed subnet id (prefix "sub" + 17 crockford chars) so the gateway
	// malformed-id short-circuit passes and the request reaches the FGA Check.
	const subnetID = "subabcdefghjkmnpqrst"

	checker := &fakeChecker{allowed: false, reasons: []string{"no path: vpc_subnet:" + subnetID + " has no v_get for user:usr_x"}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, subnetGetEntry), checker)

	_, err := mw.Unary()(withTokenMD("usr_x", "user"),
		&vpcv1.GetSubnetRequest{SubnetId: subnetID},
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.SubnetService/Get"},
		func(ctx context.Context, req any) (any, error) {
			t.Fatal("handler must not be reached on deny")
			return nil, nil
		})
	require.Error(t, err)
	st, _ := status.FromError(err)

	assert.Equal(t, codes.NotFound, st.Code(),
		"verb-bearing vpc read-deny must hide existence as NotFound")
	assert.Equal(t, "Subnet "+subnetID+" not found", st.Message(),
		"hide-existence message must use the Kachō contract tone and match the vpc backend NotFound")
	// No-leak guard preserved (regression lock alongside the message contract).
	assert.NotContains(t, strings.ToLower(st.Message()), "no path")
	assert.NotContains(t, strings.ToLower(st.Message()), "v_get")
}
