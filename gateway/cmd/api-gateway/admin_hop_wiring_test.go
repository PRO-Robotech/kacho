// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// A capability nobody calls is the defect this work started from: the
// introspection cache has ALWAYS accepted an HTTPClient, and the composition
// root has never filled it — so the hop carrying a live end-user bearer ran on
// a client that could not be given a trust anchor at all, and no test noticed
// because the field existed and compiled.
//
// main() cannot be exercised from a test (it dials backends and binds
// listeners), so the wiring is asserted where it lives: in the source of the
// composition root. Reading the source is weaker than exercising it, and is
// used deliberately for exactly the property that "it compiles" cannot show —
// that the constructed client REACHES both consumers of the admin hop.
//
// Behaviour of the client itself (verifies that CA, rejects another, refuses to
// start on an unusable bundle) is exercised for real in admin_hop_client_test.go.

func compositionRoot(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("main.go")
	require.NoError(t, err, "composition root must be readable")
	return string(b)
}

func TestCompositionRoot_BuildsTheAdminHopClient(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`newAdminHopClient\(\s*\n?\s*cfg\.HydraAdminCAFile`),
		src,
		"the composition root must build the admin-hop client from the configured trust anchor; "+
			"without it KACHO_HYDRA_ADMIN_CA_FILE is a knob that changes nothing")
}

func TestCompositionRoot_FeedsTheClientToTheIntrospectionHop(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`IntrospectionCacheConfig\{(?s:.*?)HTTPClient:\s*adminHopClient`),
		src,
		"the introspection cache must be given the admin-hop client. This is the hop that carries "+
			"the caller's live bearer on every cache miss; left unset it silently falls back to a "+
			"client with the system root store and no way to pin the internal CA")
}

func TestCompositionRoot_FeedsTheClientToTheLogoutSessionKill(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`LogoutHandlerConfig\{(?s:.*?)HTTPClient:\s*adminHopClient`),
		src,
		"the logout handler addresses the SAME admin API; giving the trust anchor to one consumer "+
			"and not the other would leave sign-out failing on every logout the moment the hop moves to TLS")
}

func TestCompositionRoot_RefusesToStartOnAnUnusableTrustAnchor(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`if ahErr != nil \{\s*\n\s*log\.Fatalf`),
		src,
		"an unusable trust anchor must stop the process at the composition root. Carrying on would "+
			"leave the operator believing the hop is verified against the internal CA while it is not")
}
