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
	testIntrospectURL = "http://kacho-umbrella-hydra-admin.kacho.svc.cluster.local:4445/admin/oauth2/introspect"
	testAdminURL      = "http://kacho-umbrella-hydra-admin.kacho.svc.cluster.local:4445"
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

// Both addresses set → production boots.
func TestProdAcceptsBothEndpoints(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IntrospectionURL: testIntrospectURL,
		AdminURL:         testAdminURL,
	})
	if err != nil {
		t.Fatalf("expected nil for a fully configured production revocation path, got: %v", err)
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
