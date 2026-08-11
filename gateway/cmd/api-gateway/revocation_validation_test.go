// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Startup-validation tests for the revocation-path configuration implemented in
// revocation_validation.go.
//
// The property under test: a production-class gateway must not boot with the
// revocation path aimed at nothing. Both addresses it needs (token introspection
// and the provider-side session kill) live on an admin API that cannot be
// derived from anything the gateway already knows, so "unset" is a decision the
// operator has to make explicitly — and if they have not made it, the process
// says so at startup instead of discovering it one revoked token at a time.
package main

import (
	"strings"
	"testing"
)

const (
	testIntrospectURL = "http://kacho-umbrella-hydra-admin.kacho.svc:4445/admin/oauth2/introspect"
	testAdminURL      = "http://kacho-umbrella-hydra-admin.kacho.svc:4445"

	// The same two addresses over TLS — the shape a production-class stand is
	// required to carry.
	tlsIntrospectURL = "https://kacho-umbrella-hydra-admin.kacho.svc:4445/admin/oauth2/introspect"
	tlsAdminURL      = "https://kacho-umbrella-hydra-admin.kacho.svc:4445"
)

// Production-class env with no introspection endpoint MUST be refused: with it
// unset the gateway never asks whether a token was revoked, so a token stays
// good for its full lifetime no matter what anyone revokes.
func TestProdRefusesUnsetIntrospectionEndpoint(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IntrospectionURL: "",
		AdminURL:         testAdminURL,
	})
	if err == nil {
		t.Fatalf("expected refusal, got nil")
	}
	// The refusal must name the knob AND say what is off, not merely complain
	// that a string does not parse — an operator reading the pod log has to know
	// which value to supply and why it matters.
	if !strings.Contains(err.Error(), "KACHO_HYDRA_INTROSPECTION_URL is empty") {
		t.Fatalf("the refusal must name the unset knob, got: %v", err)
	}
	if !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("the refusal must say what stops working, got: %v", err)
	}
}

// Same for the admin base: with it unset the logout handler's provider-side
// session kill is silently disabled, so signing out leaves the session alive.
func TestProdRefusesUnsetAdminEndpoint(t *testing.T) {
	err := validateProductionRevocationConfig("prod", RevocationConfig{
		IntrospectionURL: testIntrospectURL,
		AdminURL:         "",
	})
	if err == nil {
		t.Fatalf("expected refusal, got nil")
	}
	if !strings.Contains(err.Error(), "KACHO_HYDRA_ADMIN_URL is empty") {
		t.Fatalf("the refusal must name the unset knob, got: %v", err)
	}
	if !strings.Contains(err.Error(), "session") {
		t.Fatalf("the refusal must say what stops working, got: %v", err)
	}
}

// An empty/unset environment label is production-class — a forgotten label must
// not silently downgrade the guard (same rule as the sibling authz guard).
func TestUnlabelledEnvIsProductionClass(t *testing.T) {
	if err := validateProductionRevocationConfig("", RevocationConfig{}); err == nil {
		t.Fatalf("an unset KACHO_APP_ENV must be treated as production-class, got nil")
	}
}

// Staging is production-class too.
func TestStagingRefusesUnsetEndpoints(t *testing.T) {
	if err := validateProductionRevocationConfig("staging", RevocationConfig{AdminURL: testAdminURL}); err == nil {
		t.Fatalf("expected refusal in staging, got nil")
	}
}

// The explicit dev-class labels tolerate an unconfigured revocation path — the
// local stand may run without the provider's admin API reachable at all.
func TestDevClassToleratesUnsetEndpoints(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		if err := validateProductionRevocationConfig(env, RevocationConfig{}); err != nil {
			t.Fatalf("%s: expected tolerance, got: %v", env, err)
		}
	}
}

// The sanctioned production shape — both addresses over TLS, anchored — boots.
//
// This is the other half of the guard: it must stay SILENT on a configuration
// that is actually correct. A guard that only ever fires is indistinguishable
// from one that fires at random, and the first false refusal gets it removed.
func TestProdAcceptsBothEndpoints(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IntrospectionURL: tlsIntrospectURL,
		AdminURL:         tlsAdminURL,
		AdminCAFile:      "/etc/api-gateway/hydra-admin-ca/ca.crt",
	})
	if err != nil {
		t.Fatalf("expected nil for a fully configured production revocation path, got: %v", err)
	}
}

// TLS with nothing to verify against is refused as well. Moving the address to
// https and stopping there is the well-meant half-step that takes the API down:
// the provider's in-cluster certificate comes from the internal CA, this process
// trusts the system roots, and the introspection layer files an unknown-authority
// handshake as a PERMANENT misconfiguration — after which it refuses every
// request instead of waving them through.
func TestProdRefusesTLSHopWithoutTrustAnchor(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IntrospectionURL: tlsIntrospectURL,
		AdminURL:         tlsAdminURL,
		AdminCAFile:      "",
	})
	if err == nil {
		t.Fatalf("expected refusal for an https hop with no pinned anchor, got nil")
	}
	if !strings.Contains(err.Error(), "KACHO_HYDRA_ADMIN_CA_FILE") {
		t.Fatalf("the refusal must name the anchor knob, got: %v", err)
	}
}

// A dev-class stand may legitimately have the provider's admin API on plaintext
// (no internal CA, port-forward, no cert at all). The transport requirement rides
// the same dev-class exemption as the rest of this guard — stated as its own case
// so the exemption is a decision on record rather than a side effect.
func TestDevClassToleratesPlaintextHop(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		if err := validateProductionRevocationConfig(env, RevocationConfig{
			IntrospectionURL: testIntrospectURL,
			AdminURL:         testAdminURL,
		}); err != nil {
			t.Fatalf("%s: expected tolerance for a plaintext hop, got: %v", env, err)
		}
	}
}

// An address that points at the PUBLIC OAuth2 API rather than the admin one is
// refused by shape. This is the original defect: the endpoint was derived from
// the issuer, so it addressed a server that does not serve introspection at all.
// Refusing the shape turns a silent runtime fail-open into a startup message.
func TestProdRefusesNonAdminIntrospectionPath(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IntrospectionURL: "https://hydra.api.kacho.cloud/oauth2/introspect",
		AdminURL:         testAdminURL,
	})
	if err == nil {
		t.Fatalf("expected refusal for a public-API introspection path, got nil")
	}
	if !strings.Contains(err.Error(), "/admin/oauth2/introspect") {
		t.Fatalf("the refusal must name the path the admin API actually serves, got: %v", err)
	}
}

// ─── the hop must not carry a credential in the clear ────────────────────────
//
// What rides this hop decided these two tests. Introspection asks about a bearer
// by SENDING it, on every cache miss, for every authenticated request — so the
// wire carries a live end-user credential, not merely an administrative call. A
// bearer is a bearer: whoever reads it off the wire can use it until it expires.
//
// Plaintext was accepted here for the whole life of the guard: the scheme check
// admitted "http" and "https" alike, so a production-class stand booted with the
// hop in the clear and nothing said so.

// Production-class + a plaintext introspection address MUST be refused.
func TestProdRefusesPlaintextIntrospectionHop(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IntrospectionURL: testIntrospectURL, // http://…
		AdminURL:         tlsAdminURL,
	})
	if err == nil {
		t.Fatalf("expected refusal for a plaintext introspection hop, got nil")
	}
	// The refusal is operator diagnostics: it must name the knob to change.
	if !strings.Contains(err.Error(), "KACHO_HYDRA_INTROSPECTION_URL") {
		t.Fatalf("the refusal must name the knob, got: %v", err)
	}
}

// Production-class + a plaintext admin base MUST be refused.
func TestProdRefusesPlaintextAdminHop(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IntrospectionURL: tlsIntrospectURL,
		AdminURL:         testAdminURL, // http://…
	})
	if err == nil {
		t.Fatalf("expected refusal for a plaintext admin hop, got nil")
	}
	if !strings.Contains(err.Error(), "KACHO_HYDRA_ADMIN_URL") {
		t.Fatalf("the refusal must name the knob, got: %v", err)
	}
}

// A malformed address is refused too — an operator who set the knob to a
// hostname without a scheme gets told at startup, not on the first request.
func TestProdRefusesUnparseableIntrospectionURL(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IntrospectionURL: "kacho-umbrella-hydra-admin:4445/admin/oauth2/introspect",
		AdminURL:         testAdminURL,
	})
	if err == nil {
		t.Fatalf("expected refusal for a schemeless introspection address, got nil")
	}
}
