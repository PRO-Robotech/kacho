// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// external_grpc_services.go — the single site that decides WHAT the api-gateway
// serves natively on its externally-reachable gRPC server.
//
// The same *grpc.Server object is served on the plaintext cmux listener the
// ingress targets AND on the advertised external TLS listener (see main(): two
// Serve goroutines, one server). Both are external/unmarked, so anything
// registered here is answerable from the edge. Everything else that reaches this
// server is transparently proxied to a backend by the UnknownServiceHandler
// installed in proxy.NewServer.
//
// Registration lives in a named function rather than inline in main() so the
// edge's native surface is one readable list AND is addressable by a test: what
// the edge exposes is a security property, and a property nothing can assert is
// a property nothing holds.
package main

import (
	"google.golang.org/grpc"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	"github.com/PRO-Robotech/kacho/gateway/internal/health"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// registerExternalGRPCServices registers every gRPC service the api-gateway
// answers itself on the externally-reachable listeners.
//
// SECURITY — server-reflection is deliberately absent, and adding it here is the
// thing this function's test forbids. Reflection's ListServices answers from
// GetServiceInfo() and would therefore look harmlessly short, but symbol lookup
// (FileContainingSymbol / FileByFilename) resolves against
// protoregistry.GlobalFiles, which this binary populates with every backend
// descriptor it links. Serving it here would put an unauthenticated,
// unauthorized surface on the advertised edge — outside both exemptions
// security.md documents — and hand any caller a free amplification lever. The
// operator-facing copy lives on the cluster-internal listener behind mTLS
// (internal_grpc_listener.go), which is where admin surface belongs.
func registerExternalGRPCServices(
	srv *grpc.Server,
	backends proxy.Backends,
	opsProxy operationpb.OperationServiceServer,
) {
	// grpc.health.v1.Health — gateway self-liveness. Answers a constant SERVING
	// for the gateway itself; it does not read the request's service name.
	health.RegisterGRPCHealth(srv, backends)

	// kacho.cloud.operation.OperationService — polled by every async client.
	// Routed here natively (not through the transparent proxy) because the
	// gateway fans the poll out across backends itself.
	operationpb.RegisterOperationServiceServer(srv, opsProxy)
}
