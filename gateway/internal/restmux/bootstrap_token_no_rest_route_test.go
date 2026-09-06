// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// bootstrap_token_no_rest_route_test.go — the bootstrap-admin token mint must NOT
// be reachable over REST, on ANY listener.
//
// InternalBootstrapTokenService/MintBootstrapToken hands out a Hydra-signed RS256
// Bearer for a cluster `system_admin` ServiceAccount. Its catalog permission is
// `<exempt>`, and the gateway admits `<exempt>` Internal* RPCs that arrive on the
// cluster-internal listener WITHOUT extracting a principal — so a REST route for
// it is a CREDENTIAL-FREE control-plane takeover: the internal REST listener is
// plain HTTP/1.1 (no TLS, no client cert — see cmd/api-gateway/main.go), so any
// pod that can reach the `internal-rest` Service port, or anyone with a
// port-forward, could POST an empty body and receive a cluster-admin token.
//
// "The mTLS listener boundary is the gate" is FALSE for this path: network
// position is not a credential (security.md — "internal = trusted" is a forbidden
// assumption). The mint keeps exactly one door: a DIRECT mTLS gRPC dial to iam
// :9091, where authzguard's per-RPC SPIFFE allow-list checks the caller's VERIFIED
// CLIENT CERTIFICATE.
//
// This test pins the absence of the REST door behaviourally: the route must not
// exist even on the internal listener (404 = no route, as opposed to the
// backend-unreachable error a REGISTERED internal route yields against the dead
// 127.0.0.1:1 backend — see TestInternalListener_ServesInternalPaths).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// bootstrapMintRESTPaths — every REST spelling the mint could be reachable
// under: the former `google.api.http` binding and the grpc-gateway
// `generate_unbound_methods` default route.
var bootstrapMintRESTPaths = []struct{ method, path string }{
	{"POST", "/iam/v1/internal/bootstrapToken:mint"},
	{"POST", "/kaname.cloud.iam.v1.InternalBootstrapTokenService/MintBootstrapToken"},
}

// TestBootstrapMint_NoRESTRoute_OnInternalListener — the mint is unrouted on the
// cluster-internal listener (the only listener that serves Internal* REST).
func TestBootstrapMint_NoRESTRoute_OnInternalListener(t *testing.T) {
	h, err := NewMux(context.Background(), muxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	for _, tc := range bootstrapMintRESTPaths {
		t.Run("INT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(listenerorigin.WithInternal(req.Context()))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("mint REST path %s %s on INTERNAL listener: got %d, want 404 "+
					"(CRITICAL: credential-free cluster-admin token mint exposed over REST)",
					tc.method, tc.path, rec.Code)
			}
		})
	}
}

// TestBootstrapMint_NoRESTRoute_OnExternalListener — and, a fortiori, unrouted on
// the external listener.
func TestBootstrapMint_NoRESTRoute_OnExternalListener(t *testing.T) {
	h, err := NewMux(context.Background(), muxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	for _, tc := range bootstrapMintRESTPaths {
		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("mint REST path %s %s on EXTERNAL listener: got %d, want 404",
					tc.method, tc.path, rec.Code)
			}
		})
	}
}
