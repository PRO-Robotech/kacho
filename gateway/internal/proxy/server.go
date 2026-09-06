// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy

import (
	"strings"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
)

// Backends — карта «доменное имя → долгоживущий *grpc.ClientConn».
// Ключи: "iam", "vpc", "compute", "storage", "geo", "loadbalancer", "registry".
type Backends map[string]*grpc.ClientConn

// MethodResolver — re-export of local proxy MethodResolver type signature.
type MethodResolver = methodResolverInternal

// RoutableDomain reports which Backends key a fullMethod needs in order to be
// routed at all, i.e. the `<domain>` segment of
// "/kacho.cloud.<domain>.<...>/<Method>". ok=false means the path is not shaped
// like something this gateway routes, whatever the backend map contains.
//
// Exported because being IN the allowlist and being ROUTABLE are two different
// facts, and until this existed only the first one was checkable from outside.
// The allowlist is computed against the proto descriptors (allowlist/parity_test.go),
// but the map of open backend connections is built by the composition root from a
// hand-written key set, and nothing joined the two: a domain whose key is absent
// resolves to nothing, and the caller is answered by exactly the error that hides
// the admin surface. The wiring gate calls THIS function rather than re-deriving
// the domain, so the gate and the resolver cannot come to disagree about what a
// method needs.
func RoutableDomain(fullMethod string) (string, bool) {
	if !hasContractRoot(fullMethod) {
		return "", false
	}
	// fullMethod = "/kacho.cloud.<domain>.<...>/<Method>"
	parts := strings.SplitN(fullMethod, "/", 3)
	if len(parts) < 3 || parts[1] == "" {
		return "", false
	}
	pkgParts := strings.Split(parts[1], ".")
	if len(pkgParts) < 4 {
		return "", false
	}
	return pkgParts[2], true
}

// Resolver builds a MethodResolver for the unknown-service handler installed
// on the gRPC server. Behaviour:
//
//  1. /kacho.cloud.<rest>   — passes through the allowlist + backend lookup.
//  2. anything else (blocked / Internal* / unknown method) — returns ok=false,
//     which Handler (shimproxy.go) maps to codes.NotFound, NOT Unimplemented.
//     NotFound is load-bearing: it makes Internal* and unknown methods
//     indistinguishable from "method does not exist", so the external listener
//     is not an existence-oracle for admin endpoints ("exists but unimplemented"
//     would leak reconnaissance). Keep parity with the shimproxy.go contract.
//
// THE REFUSAL HERE IS NOT THE WHOLE STORY, and for a while this comment claimed
// it was. This resolver runs at the END of the interceptor chain, so anything
// that answers earlier answers INSTEAD. Authorization did: an Internal* method
// whose permission the caller lacked came back PermissionDenied naming the
// permission, while its neighbours came back "unknown method" — the very
// distinction the paragraph above says does not exist. Nine of the platform's
// seventy-nine Internal* methods behaved that way, all of them ones whose
// authorization target is resolved from the request body.
//
// The uniform answer is therefore produced by route_refusal.go, an interceptor
// mounted AHEAD of authorization and returning byte-for-byte the error Handler
// returns here. This resolver remains the backstop: it is what makes the
// property true for traffic that reaches it, and what the refusal is copied
// from. Changing the message in one place without the other reintroduces the
// difference.
//
// Маршрутизируются только нативные kacho.cloud.* сервисы; backends не
// expose'ят посторонних сервисов.
func Resolver(backends Backends) MethodResolver {
	return func(fullMethod string) (string, grpc.ClientConnInterface, bool) {
		method := fullMethod
		if !hasContractRoot(method) {
			return "", nil, false
		}
		if allowlist.HasInternalSuffix(method) || !allowlist.IsAllowed(method) {
			return "", nil, false
		}
		domain, ok := RoutableDomain(method)
		if !ok {
			return "", nil, false
		}
		conn, ok := backends[domain]
		if !ok {
			return "", nil, false
		}
		return method, conn, true
	}
}

// NewServer creates a gRPC server whose UnknownServiceHandler routes
// kacho.cloud.* traffic to the appropriate backend.
//
// Native services registered on this server (Health, OperationService) take
// precedence over the unknown-service handler, as per gRPC dispatch
// semantics.
func NewServer(resolve MethodResolver, opts ...grpc.ServerOption) *grpc.Server {
	base := []grpc.ServerOption{
		grpc.UnknownServiceHandler(Handler(resolve)),
	}
	return grpc.NewServer(append(base, opts...)...)
}
