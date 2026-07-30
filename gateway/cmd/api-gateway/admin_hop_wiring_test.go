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

// The guard that refuses the start must be shown the value it judges.
//
// This one cost a production rollout. `validateProductionRevocationConfig`
// decides "the admin hop is https, so a trust anchor MUST be pinned" by reading
// RevocationConfig.AdminCAFile — and the composition root built that struct
// from the two URLs only, leaving AdminCAFile at its zero value. So in a
// production-class environment the guard concluded "no anchor pinned" NO MATTER
// WHAT was configured: the chart set KACHO_HYDRA_ADMIN_CA_FILE, the secret was
// mounted, the file was there, envconfig had parsed it into cfg — and the
// process still refused to start, naming the very knob that was set.
//
// Nothing in the suite could see it. The validator's own unit tests pass the
// field directly, so they prove the FUNCTION and say nothing about the CALL;
// the wiring tests above cover the client, which is a different consumer of the
// same knob. An unsatisfiable boot-guard is worse than a missing one: it fails
// closed on a correct configuration, and the failure text sends the reader to
// fix something that is already right.
func TestCompositionRoot_FeedsTheTrustAnchorToTheRevocationGuard(t *testing.T) {
	src := compositionRoot(t)
	require.Regexp(t,
		regexp.MustCompile(`RevocationConfig\{(?s:[^}]*?)AdminCAFile:\s*cfg\.HydraAdminCAFile`),
		src,
		"the revocation startup-guard must be given cfg.HydraAdminCAFile. It REFUSES THE START "+
			"when the admin hop is https and this field is empty, so a call site that omits it "+
			"makes the guard unsatisfiable: no configuration can pass it, and the error names "+
			"the knob the operator has already set")
}
