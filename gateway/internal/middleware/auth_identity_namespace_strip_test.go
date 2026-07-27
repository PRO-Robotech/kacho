// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// forgeableNamespaceHeaders — the identity-namespace headers a client must never
// be able to place on the wire. The list deliberately mixes:
//
//   - headers a BACKEND actually reads today and that the previous
//     name-enumerated strip did NOT cover (`x-kacho-admin` → compute/vpc
//     TenantCtx.Admin, `x-kacho-project-id` → TenantCtx.ProjectIDs,
//     `x-kacho-actor` → audit actor);
//   - headers the strip already covered (principal / token) — regression, they
//     must stay covered;
//   - a header nobody reads yet (`x-kacho-not-yet-invented`). It stands in for
//     the NEXT identity header somebody adds to a backend. A name-enumerated
//     strip cannot cover it by construction; a namespace-wide strip covers it
//     for free. This is the case that turns "we forgot one" from a security
//     incident into a non-event.
//
// Both surface forms are exercised: the bare `X-Kacho-…` header and the
// `Grpc-Metadata-X-Kacho-…` form, because grpc-gateway's DefaultHeaderMatcher
// bridges ANY `Grpc-Metadata-`-prefixed header into backend gRPC metadata.
var forgeableNamespaceHeaders = []string{
	"X-Kacho-Admin",
	"Grpc-Metadata-X-Kacho-Admin",
	"X-Kacho-Project-Id",
	"Grpc-Metadata-X-Kacho-Project-Id",
	"X-Kacho-Actor",
	"Grpc-Metadata-X-Kacho-Actor",
	"X-Kacho-Principal-Id",
	"Grpc-Metadata-X-Kacho-Principal-Type",
	"X-Kacho-Token-Acr",
	"Grpc-Metadata-X-Kacho-Token-Acr",
	"X-Kacho-Not-Yet-Invented",
	"Grpc-Metadata-X-Kacho-Not-Yet-Invented",
}

// TestAuthHTTP_StripsEntireKachoIdentityNamespace — the REST auth path must
// sanitise the WHOLE `x-kacho-` namespace, not a hand-written list of families.
//
// Live escalation this locks: a client sent `Grpc-Metadata-X-Kacho-Admin: true`,
// the REST→gRPC bridge forwarded it verbatim as gRPC metadata `x-kacho-admin`,
// and kacho-compute raised TenantCtx.Admin on its PUBLIC listener — which in turn
// lifted the operation-ownership predicate, and an Operation response carries the
// created resource in full. A 403 became a 200 on somebody else's resource.
func TestAuthHTTP_StripsEntireKachoIdentityNamespace(t *testing.T) {
	auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", nil, authTestLogger())
	var seen http.Header
	h := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodPost, "/compute/v1/instances", nil)
	for _, k := range forgeableNamespaceHeaders {
		r.Header.Set(k, "true")
	}
	// A non-identity header must survive — the strip is namespace-scoped, not a
	// blanket header purge.
	r.Header.Set("X-Request-Id", "req-1")
	r.Header.Set("Content-Type", "application/json")

	h.ServeHTTP(httptest.NewRecorder(), r)

	for _, k := range forgeableNamespaceHeaders {
		if got := seen.Get(k); got != "" {
			t.Errorf("forged inbound %s not stripped (got %q)", k, got)
		}
	}
	if seen.Get("X-Request-Id") != "req-1" {
		t.Errorf("non-identity header X-Request-Id was dropped (got %q)", seen.Get("X-Request-Id"))
	}
	if seen.Get("Content-Type") != "application/json" {
		t.Errorf("non-identity header Content-Type was dropped (got %q)", seen.Get("Content-Type"))
	}
}

// TestAuthUnary_StripsEntireKachoIdentityNamespace — same invariant on the
// native-gRPC edge of the gateway. A client dialling the gateway's gRPC listener
// sets metadata keys directly (no `grpc-metadata-` bridge), so the namespace
// strip must key on the bare metadata name too.
func TestAuthUnary_StripsEntireKachoIdentityNamespace(t *testing.T) {
	auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", nil, authTestLogger())
	md := metadata.New(map[string]string{
		"x-kacho-admin":            "true",
		"x-kacho-project-id":       "prj-victim",
		"x-kacho-actor":            "attacker",
		"x-kacho-principal-id":     "usr-victim",
		"x-kacho-token-acr":        "3",
		"x-kacho-not-yet-invented": "true",
		"authorization":            "", // non-namespace key, must survive
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var seen metadata.MD
	_, err := auth.Unary()(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InstanceService/Get"},
		func(c context.Context, _ any) (any, error) {
			seen, _ = metadata.FromIncomingContext(c)
			return nil, nil
		})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	for _, k := range []string{
		"x-kacho-admin", "x-kacho-project-id", "x-kacho-actor",
		"x-kacho-token-acr", "x-kacho-not-yet-invented",
	} {
		if vals := seen.Get(k); len(vals) != 0 {
			t.Errorf("forged incoming metadata %s not stripped (got %v)", k, vals)
		}
	}
	if _, ok := seen["authorization"]; !ok {
		t.Error("non-namespace metadata key `authorization` was stripped")
	}
	// The gateway re-injects its own trusted principal after the strip — the
	// strip must not leave the backend identity-less.
	if got := seen.Get("x-kacho-principal-id"); len(got) == 0 || got[0] == "usr-victim" {
		t.Errorf("gateway-derived principal missing or client-forged value survived: %v", got)
	}
}
