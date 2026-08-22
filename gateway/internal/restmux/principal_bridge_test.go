// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// annotate runs the REAL REST→gRPC bridge (the mux's IncomingHeaderMatcher plus
// the WithMetadata annotator, exactly as NewMux wires them) over an HTTP request
// and returns the gRPC metadata that would reach the backend. This is the
// observable that matters: "what does compute/vpc actually see in
// metadata.FromIncomingContext".
func annotate(t *testing.T, r *http.Request) metadata.MD {
	t.Helper()
	mux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(principalHeaderMatcher),
		runtime.WithMetadata(principalMetadata),
	)
	ctx, err := runtime.AnnotateContext(context.Background(), mux, r,
		"/kacho.cloud.compute.v1.InstanceService/Get")
	if err != nil {
		t.Fatalf("AnnotateContext: %v", err)
	}
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("no outgoing metadata produced by the REST→gRPC bridge")
	}
	return md
}

// TestBridge_RefusesClientForgedKachoMetadata — the bridge must not carry ANY
// `x-kacho-…` metadata that the gateway did not itself produce.
//
// grpc-gateway's DefaultHeaderMatcher bridges every `Grpc-Metadata-<X>` header
// to metadata key `<x>`. That is how `Grpc-Metadata-X-Kacho-Admin: true` reached
// kacho-compute as `x-kacho-admin: true` and raised TenantCtx.Admin on the PUBLIC
// listener (→ operation-ownership bypass → foreign resource readable).
//
// Belt-and-braces with the auth-middleware strip: the strip removes these
// headers before the mux runs, but the mux must not be the component that would
// have carried them. A future middleware-ordering slip must not re-open the hole.
func TestBridge_RefusesClientForgedKachoMetadata(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/compute/v1/operations/epd1", nil)
	forged := map[string]string{
		"Grpc-Metadata-X-Kacho-Admin":            "true",
		"Grpc-Metadata-X-Kacho-Project-Id":       "prj-victim",
		"Grpc-Metadata-X-Kacho-Actor":            "attacker",
		"Grpc-Metadata-X-Kacho-Not-Yet-Invented": "true",
	}
	for k, v := range forged {
		r.Header.Set(k, v)
	}

	md := annotate(t, r)

	for _, key := range []string{
		"x-kacho-admin", "x-kacho-project-id", "x-kacho-actor",
		"x-kacho-not-yet-invented",
	} {
		if vals := md.Get(key); len(vals) != 0 {
			t.Errorf("REST→gRPC bridge carried client-forged %s = %v", key, vals)
		}
	}
}

// TestBridge_CarriesGatewayDerivedIdentity — regression: narrowing the bridge
// must NOT drop what the gateway legitimately forwards. The auth / DPoP
// middleware sets these AFTER validating a credential; backends depend on them
// (principal → FGA subject, acr → iam step-up floor on the internal re-dial).
func TestBridge_CarriesGatewayDerivedIdentity(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/compute/v1/instances", nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr-alice")
	r.Header.Set(principalmeta.HeaderPrincipalDisplay, "Alice")
	r.Header.Set(principalmeta.HeaderTokenACR, "2")
	r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalID, "usr-alice")
	r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalDisplay, "Alice")
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenACR, "2")
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenJti, "jti-1")
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenScope, "openid")

	md := annotate(t, r)

	for key, want := range map[string]string{
		principalmeta.MetaPrincipalType: "user",
		principalmeta.MetaPrincipalID:   "usr-alice",
		// Имя приезжает ДВОИЧНЫМ ключом: обычный роняет вызов на первом же
		// не-латинском символе. Здесь стоял обычный, и он держался ТОЛЬКО
		// мостом — то есть второй копией значения; мост её больше не делает
		// (#930), а единственным производителем остался аннотатор.
		principalmeta.MetaPrincipalDisplayBin: "Alice",
		principalmeta.MetaTokenACR:            "2",
		principalmeta.MetaTokenJti:            "jti-1",
		principalmeta.MetaTokenScope:          "openid",
	} {
		vals := md.Get(key)
		if len(vals) == 0 {
			t.Errorf("gateway-derived %s dropped by the bridge", key)
			continue
		}
		if vals[0] != want {
			t.Errorf("gateway-derived %s = %q, want %q", key, vals[0], want)
		}
	}
}

// TestBridge_LeavesNonKachoHeadersAlone — regression: the narrowing is scoped to
// the reserved `x-kacho-` namespace. Everything else keeps grpc-gateway's
// standard behaviour (permanent HTTP headers + the `Grpc-Metadata-` bridge).
func TestBridge_LeavesNonKachoHeadersAlone(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/compute/v1/instances", nil)
	r.Header.Set("Grpc-Metadata-X-Tenant-Hint", "hint")
	r.Header.Set("Authorization", "Bearer tok")

	md := annotate(t, r)

	if vals := md.Get("x-tenant-hint"); len(vals) == 0 || vals[0] != "hint" {
		t.Errorf("non-kacho Grpc-Metadata- header no longer bridged: %v", vals)
	}
	if vals := md.Get("authorization"); len(vals) == 0 || vals[0] != "Bearer tok" {
		t.Errorf("permanent HTTP header Authorization no longer bridged: %v", vals)
	}
}
