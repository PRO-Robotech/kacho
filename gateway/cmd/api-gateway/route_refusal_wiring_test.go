// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The route refusal is only correct IN A POSITION: after authentication, before
// authorization. Both neighbours matter, and neither is visible from the
// interceptor's own package — the order lives here, in the composition root.
// Behaviour of the refusal itself is exercised for real in
// gateway/internal/proxy/route_refusal_test.go.

func TestCompositionRoot_RefusesTheInternalRouteBeforeAuthorization(t *testing.T) {
	src := compositionRoot(t)

	refusal := strings.Index(src, "proxy.StreamRefuseInternalRoute()")
	require.Greater(t, refusal, -1,
		"the external listener must mount the Internal* route refusal; without it an Internal* method "+
			"whose permission the caller lacks answers PermissionDenied naming the permission, while its "+
			"neighbours answer an indistinguishable «unknown method»")

	authz := strings.Index(src, "authzMW.Stream()")
	require.Greater(t, authz, -1, "authorization interceptor not found — this test can no longer judge the order")

	require.Less(t, refusal, authz,
		"the route refusal must be mounted BEFORE authorization. Mounted after it, authorization answers "+
			"first and the answer names the permission — an existence-oracle for the admin surface, and a "+
			"probe that cannot tell «not routed here» from «routed but not permitted»")
}

func TestCompositionRoot_RefusesTheInternalRouteAfterAuthentication(t *testing.T) {
	src := compositionRoot(t)

	authn := strings.Index(src, "authInterceptor.Stream()")
	require.Greater(t, authn, -1, "authN interceptor not found — this test can no longer judge the order")
	refusal := strings.Index(src, "proxy.StreamRefuseInternalRoute()")
	require.Greater(t, refusal, -1, "route refusal not mounted")

	require.Less(t, authn, refusal,
		"the route refusal must be mounted AFTER authentication. Ahead of it, an unauthenticated caller "+
			"would get NotFound for Internal* and Unauthenticated for every other method — the same leak "+
			"in different clothes")
}

func TestCompositionRoot_RefusalIsMountedOnBothCallShapes(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t, regexp.MustCompile(`grpcUnaryInterceptors\s*=\s*append\([^)]*proxy\.UnaryRefuseInternalRoute\(\)`), src,
		"unary shape must carry the refusal too")
	require.Regexp(t, regexp.MustCompile(`grpcStreamInterceptors\s*=\s*append\([^)]*proxy\.StreamRefuseInternalRoute\(\)`), src,
		"the STREAM shape is the load-bearing one: proxied domain traffic goes through UnknownServiceHandler, "+
			"which grpc-go dispatches as a stream")
}
