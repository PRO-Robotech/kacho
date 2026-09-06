// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_acr_claim_placement_test.go — the assurance level a human actually
// authenticated with must REACH the step-up gate, whatever placement the
// provider used to carry it.
//
// Why this file exists, stated plainly so nobody deletes it as noise: our own
// token hook emits the level as `kaname_acr` INSIDE the enrichment map, while the
// verifier used to read only the standard top-level `acr`. Hydra promotes to the
// top level only what `oauth2.allowed_top_level_claims` whitelists, and neither
// `acr` nor `kaname_acr` is on that list on any deployed profile — so the level
// arrived at the edge, in the signed token, and was dropped on the floor. The
// gate then ranked every human caller at 0 and refused every RPC with a floor.
//
// The neighbouring step-up tests could not see this: stepup_gate_test.go builds
// a VerifiedToken literal (extraction bypassed entirely), and
// stepup_alwayson_test.go / stepup_wiring_test.go mint `acr` at the TOP level —
// the one placement the product already handled. A fixture more lenient than the
// wire is how a claim can be write-only for its whole life. These cases mint the
// level ONLY where the product actually puts it.
//
// Ground truth for the two wire shapes:
//   - the hook returns `session.access_token = {"ext_claims": {...}}`
//     (services/iam token_hook_handler);
//   - Hydra whitelists `ext_claims` on the dev profile ⇒ the map is ALSO
//     mirrored to the top level (deploy/helm/umbrella/values.dev.yaml);
//   - the prod profile whitelists nothing ⇒ the map stays under the provider's
//     own `ext` wrapper. `services/registry/.../jwks/verifier.go` decodes
//     exactly that nested shape, which is the corroboration that it is real.

import (
	"context"
	"net/http"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// ceremonyEnrichment — the subset of the hook's enrichment map that the edge
// reads, carrying the level under the name our own hook stamps it with.
func ceremonyEnrichment(acr string) map[string]any {
	return map[string]any{
		"kaname_principal_type": "user",
		"kaname_principal_id":   "usr_alice_acc_a1b2",
		"kaname_user_id":        "usr_alice_acc_a1b2",
		"kaname_acr":            acr,
	}
}

// promotedCeremonyClaims — the dev-profile wire shape, verbatim as observed on
// the stand: the enrichment map mirrored to the top level, and NO top-level
// `acr` (Hydra does not promote it).
func promotedCeremonyClaims(acr string) jwt.MapClaims {
	c := standardClaims()
	delete(c, "acr")
	c["ext_claims"] = ceremonyEnrichment(acr)
	return c
}

// nestedCeremonyClaims — the prod-profile wire shape: nothing is promoted, so
// the enrichment map stays under the provider's `ext` wrapper.
func nestedCeremonyClaims(acr string) jwt.MapClaims {
	c := standardClaims()
	delete(c, "acr")
	c["ext"] = map[string]any{
		"sid":        "bf756cf9-78bd-41dd-a36a-242a99a8dfdc",
		"ext_claims": ceremonyEnrichment(acr),
	}
	return c
}

// verifyClaims signs the given claim set with the fixture key and runs it
// through the real verifier.
func verifyClaims(t *testing.T, fix *jwksFixture, claims jwt.MapClaims) *middleware.VerifiedToken {
	t.Helper()
	vt, err := rs256Verifier(t, fix).Verify(context.Background(), fix.sign(t, claims))
	require.NoError(t, err)
	return vt
}

// --- 1. dev profile: enrichment map promoted to the top level ---------------

func TestVerifiedTokenACR_ReadsPromotedEnrichmentMap(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")

	vt := verifyClaims(t, fix, promotedCeremonyClaims("2"))

	assert.Equal(t, "2", vt.ACR,
		"the level our own token hook emits (kaname_acr, inside the enrichment map) "+
			"must reach the step-up gate; reading only the standard top-level `acr` "+
			"discards it, because Hydra promotes neither `acr` nor `kaname_acr`")
}

// --- 2. prod profile: enrichment map NOT promoted, nested under `ext` -------

func TestVerifiedTokenACR_ReadsNestedEnrichmentMap(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")

	vt := verifyClaims(t, fix, nestedCeremonyClaims("2"))

	assert.Equal(t, "2", vt.ACR,
		"the prod profile whitelists no top-level claims at all, so the enrichment "+
			"map arrives nested under `ext`; a reader that only knows the promoted "+
			"placement is blind on exactly the profile that matters")
}

// --- 3. precedence: the standard OIDC claim is authoritative ---------------

func TestVerifiedTokenACR_StandardClaimWinsOverEnrichmentMap(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")

	claims := promotedCeremonyClaims("3")
	claims["acr"] = "1" // provider asserts the level itself

	vt := verifyClaims(t, fix, claims)

	assert.Equal(t, "1", vt.ACR,
		"`acr` is the standard OIDC claim; when the provider emits it, it is the "+
			"authoritative statement about the ceremony and the enrichment mirror is "+
			"only a fallback — never the other way round")
}

// --- 4. fail-closed: no level anywhere stays empty, ranked 0 ---------------

// The negative alone would be green on a completely dead extractor, so it is
// paired with the positive control in the same case: same verifier, same
// fixture, one carries the level and one does not, and they must differ.
func TestVerifiedTokenACR_AbsentEverywhere_StaysEmpty_PairedWithPresent(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")

	absent := standardClaims()
	delete(absent, "acr")
	absent["ext_claims"] = map[string]any{
		"kaname_principal_type": "user",
		"kaname_principal_id":   "usr_alice_acc_a1b2",
	}

	assert.Equal(t, "", verifyClaims(t, fix, absent).ACR,
		"a token asserting no level anywhere must yield no level — the gate ranks "+
			"that 0 and denies; nothing outside the signed token may supply one")
	assert.Equal(t, "2", verifyClaims(t, fix, promotedCeremonyClaims("2")).ACR,
		"positive control: the same verifier on the same fixture DOES extract a "+
			"level when the token carries one")
}

// --- 5. the whole point: the level reaches the always-mounted step-up gate ---

// A credential-mint RPC declares required_acr_min=2. A real interactive-login
// bearer — level carried only in the enrichment map, exactly as captured from
// the stand — must PASS it. Before the fix this same token was refused with
// `presented ACR 0`, so no human could reach any RPC with a floor.
func TestStepUp_CeremonyToken_LevelOnlyInEnrichmentMap_PassesFloor(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	claims := promotedCeremonyClaims("2")
	// The principal claims are promoted on this profile too — keep the shape
	// faithful so the case exercises the token the stand actually issues.
	claims["kaname_principal_type"] = "user"
	claims["kaname_principal_id"] = "usr_alice_acc_a1b2"

	rec, _, hit := serveREST(t, auth, http.MethodPost,
		"https://api.kacho.cloud/iam/v1/users/usr-abc/tokens",
		fix.sign(t, claims))

	assert.Equal(t, http.StatusOK, rec.Code,
		"a human who completed the elevated ceremony must reach a floor-2 RPC; "+
			"WWW-Authenticate says: "+rec.Header().Get("WWW-Authenticate"))
	assert.True(t, hit, "the backend must be reached")
}

// The paired denial, on the same route through the same layer: strip the level
// and the floor must still refuse. This is what proves the case above measures
// the level rather than a gate that stopped gating.
func TestStepUp_CeremonyToken_NoLevelAnywhere_StillRefused(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	claims := promotedCeremonyClaims("2")
	claims["kaname_principal_type"] = "user"
	claims["kaname_principal_id"] = "usr_alice_acc_a1b2"
	delete(claims["ext_claims"].(map[string]any), "kaname_acr")

	rec, _, hit := serveREST(t, auth, http.MethodPost,
		"https://api.kacho.cloud/iam/v1/users/usr-abc/tokens",
		fix.sign(t, claims))

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"no level asserted anywhere in the signed token ⇒ rank 0 ⇒ the floor refuses")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "insufficient_user_authentication")
	assert.False(t, hit, "the backend must not be reached")
}

// --- 6. the enrichment map is read the same way for every claim in it -------

// The gate's machine-principal exemption reads `kaname_principal_type` out of the
// SAME map by the same resolution. Pinning it on the nested placement keeps the
// two from drifting apart: a resolution that finds the level but not the
// principal type would leave every service account ranked as an interactive
// human on the profile where nothing is promoted, i.e. permanently below a floor
// no machine can ever satisfy.
func TestVerifiedToken_NestedEnrichmentMap_ResolvesPrincipalClaimsToo(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")

	claims := nestedCeremonyClaims("0")
	claims["ext"].(map[string]any)["ext_claims"].(map[string]any)["kaname_principal_type"] = "service_account"
	claims["ext"].(map[string]any)["ext_claims"].(map[string]any)["kaname_principal_id"] = "sva_robot_acc_a1b2"

	vt := verifyClaims(t, fix, claims)

	require.NotNil(t, vt.ExtClaims,
		"the enrichment map nested under `ext` is the same signed map as the promoted one")
	assert.Equal(t, "service_account", vt.ExtClaims["kaname_principal_type"])
	assert.Equal(t, "sva_robot_acc_a1b2", vt.ExtClaims["kaname_principal_id"])
}
