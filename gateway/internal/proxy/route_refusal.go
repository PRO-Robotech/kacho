// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// route_refusal.go — the external listener answers "there is no such method"
// for every Internal*Service BEFORE anyone is asked about permissions.
//
// WHY THIS EXISTS. Resolver (server.go) already refuses to route Internal*, and
// its refusal is deliberately NotFound rather than Unimplemented so that an
// admin method is indistinguishable from a method that does not exist. But the
// refusal is taken by the UnknownServiceHandler, which sits at the END of the
// interceptor chain — after authorization. For any Internal* method whose
// permission the caller lacks, authorization answered first, with
// PermissionDenied AND THE NAME OF THE PERMISSION. Seventy of the platform's
// seventy-nine Internal* methods answered the indistinguishable "unknown
// method"; nine answered with their permission name. The shape of the answer
// told an outside caller which internal methods exist and what their permissions
// are called — precisely the reconnaissance the NotFound contract was written to
// deny. (An external probe also could not tell "not routed here" from "routed
// but not permitted", so the isolation invariant could not be demonstrated for
// those nine at all.)
//
// WHERE IT GOES IN THE CHAIN. Immediately BEFORE authorization and AFTER
// authentication — not at the very front. The answer must match what a method
// that does not exist would produce FOR THE SAME CALLER, and an unauthenticated
// caller gets Unauthenticated from the authN interceptor for any method at all.
// Refusing ahead of authN would hand that caller NotFound for Internal* and
// Unauthenticated for everything else, which is the same leak wearing different
// clothes.
//
// SCOPE. Internal*Service ONLY. Resolver refuses more than that (unknown
// domains, non-allowlisted methods), but those refusals must stay where they
// are: the natively-registered services on this server — health, the operation
// proxy, reflection — are dispatched by the service registry and never reach
// Resolver, so an interceptor applying the full predicate would refuse them.
package proxy

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
)

// IsInternalRoute reports whether fullMethod names an Internal*Service method,
// i.e. one the external listener must answer for as if it did not exist.
//
// Exported so the refusal and its regression test read the same predicate: a
// test carrying its own copy would keep passing while the two drifted apart.
func IsInternalRoute(fullMethod string) bool {
	return allowlist.HasInternalSuffix(fullMethod)
}

// refuseRoute is the single source of the refusal error. It MUST stay
// byte-identical to the one Handler (shimproxy.go) returns for a method the
// Resolver declines — the whole point is that the two are indistinguishable.
func refuseRoute(fullMethod string) error {
	return status.Errorf(codes.NotFound, "unknown method: %s", fullMethod)
}

// UnaryRefuseInternalRoute refuses the route for Internal*Service on unary RPCs.
func UnaryRefuseInternalRoute() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		if info != nil && IsInternalRoute(info.FullMethod) {
			return nil, refuseRoute(info.FullMethod)
		}
		return handler(ctx, req)
	}
}

// StreamRefuseInternalRoute refuses the route for Internal*Service on streaming
// RPCs. The gateway forwards domain traffic through UnknownServiceHandler, which
// grpc-go dispatches as a stream, so THIS is the interceptor that carries the
// property for proxied traffic — the unary one covers natively-registered
// services and is kept for parity rather than left as an asymmetry.
func StreamRefuseInternalRoute() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo,
		handler grpc.StreamHandler) error {
		method := ""
		if info != nil {
			method = info.FullMethod
		}
		if m, ok := grpc.MethodFromServerStream(ss); ok {
			method = m
		}
		if IsInternalRoute(method) {
			return refuseRoute(method)
		}
		return handler(srv, ss)
	}
}
