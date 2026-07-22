// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stepup_gate.go — RFC 9470 (OAuth 2.0 Step-up Authentication Challenge) +
// OpenID Connect Core 1.0 §3.1.2.1 ACR enforcement.
//
// Given a permission catalog (`<rpc-fqn> → required_acr_min` + optional
// `mfa_max_age`), the gate decides whether to allow a verified token through.
// On failure it emits an RFC 6750-style challenge that signals the UI to run
// a re-authentication ceremony with elevated `acr_values`.
//
// ACR ordering:
//
//	"0" (anonymous) < "1" (password-only) < "2" (Passkey/UV-preferred) <
//	"3" (Passkey/UV-required, hardware-bound)
//
// `mfa_max_age` enforces a sliding freshness window on `auth_time` — a token
// older than the window is rejected even if its `acr` is high enough,
// forcing a re-auth.
package middleware

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors — mapped to WWW-Authenticate by the HTTP handler.
var (
	ErrStepUpRequired = errors.New("insufficient_user_authentication: higher ACR required")
	ErrMFAStale       = errors.New("insufficient_user_authentication: MFA assertion is stale")
)

// PermissionRequirement — per-RPC authentication policy. Resolved by the
// permission catalog.
//
// An empty RequiredACRMin means NO step-up requirement: Check fails OPEN on it
// (the `if req.RequiredACRMin != ""` guard below skips the acr floor). This is
// intentional — catalog COMPLETENESS (every RPC has an entry) and the catalog's
// per-RPC authz Check are enforced upstream by the authz middleware
// ("no entry for method" → AUTHZ_DENIED), so the step-up layer need not
// re-derive a default here. The generator injects an explicit required_acr_min
// for every NON-exempt RPC (default "2"), so a genuine empty value at runtime is
// either an exempt RPC (gated by in-handler ReBAC + FGA-exempt posture) or an
// explicit routine downgrade — never an accidental un-gated privileged RPC.
type PermissionRequirement struct {
	RequiredACRMin string        // "" → no step-up requirement (Check fails open on it)
	MFAMaxAge      time.Duration // 0 → no requirement
}

// StepUpGate — stateless evaluator.
type StepUpGate struct {
	now func() time.Time
}

// NewStepUpGate constructs an evaluator with the given clock (defaults to
// time.Now). Inject a synthetic clock for tests.
func NewStepUpGate(now func() time.Time) *StepUpGate {
	if now == nil {
		now = time.Now
	}
	return &StepUpGate{now: now}
}

// Check enforces both `acr` floor and `auth_time` freshness. Returns nil on
// pass, a sentinel + descriptive error on fail.
func (g *StepUpGate) Check(token *VerifiedToken, req PermissionRequirement) error {
	if token == nil {
		return errors.New("stepup: token required")
	}

	// O-1 (#58): service-account principals are acr-EXEMPT by design — parity with
	// the iam :9091 acr-floor (authzguard/acr_floor.go, fgaproxy.go; security.md
	// §4.1.2). A service principal has no interactive MFA and can NEVER satisfy an
	// acr>=1 floor, so gating it on ACR would permanently 401 every SA — including
	// the bootstrap-admin SA (#58) — out of the acr-gated seed RPCs
	// (UserTokenService.Issue / SAKeyService.Issue). The exemption is NARROW: it
	// keys strictly on kacho_principal_type == "service_account" (a `user`
	// principal is NEVER exempt — mechanism-lock test), and it lifts ONLY the
	// ACR/MFA-freshness floor — the downstream FGA authz Check (authz.go) still
	// runs, so this grants no permission, it only stops demanding an assurance
	// level a service principal structurally cannot produce.
	if isServiceAccountPrincipal(token) {
		return nil
	}

	if req.RequiredACRMin != "" {
		gotRank := acrRank(token.ACR)
		wantRank := acrRank(req.RequiredACRMin)
		if gotRank < wantRank {
			return fmt.Errorf("%w: presented=%q required=%q", ErrStepUpRequired, token.ACR, req.RequiredACRMin)
		}
	}

	if req.MFAMaxAge > 0 {
		if token.AuthTime.IsZero() {
			return fmt.Errorf("%w: auth_time missing in token", ErrMFAStale)
		}
		age := g.now().Sub(token.AuthTime)
		if age > req.MFAMaxAge {
			return fmt.Errorf("%w: age=%s max=%s", ErrMFAStale, age, req.MFAMaxAge)
		}
	}
	return nil
}

// principalTypeServiceAccount — the kacho_principal_type claim value stamped by
// the iam token-hook for a client_credentials service-account token.
const principalTypeServiceAccount = "service_account"

// isServiceAccountPrincipal reports whether the verified token belongs to a
// service-account principal (acr-exempt, O-1). Reads the kacho_principal_type
// claim from the top level or the nested ext_claims (verifiedClaim). Only an
// EXACT "service_account" match exempts — an empty/absent type or a `user` type
// is never exempt.
func isServiceAccountPrincipal(token *VerifiedToken) bool {
	return verifiedClaim(token, "kacho_principal_type") == principalTypeServiceAccount
}

// acrRank maps ACR strings to a comparable integer. Unknown values resolve
// to 0 (anonymous) — fail-closed when policy expects ≥ 1.
func acrRank(acr string) int {
	switch acr {
	case "3":
		return 3
	case "2":
		return 2
	case "1":
		return 1
	case "0", "":
		return 0
	default:
		return 0
	}
}

// BuildStepUpChallenge produces an RFC 9470 §3 / RFC 6750 challenge header
// value for the given requirement. Returned string is suitable for direct
// use as the value of `WWW-Authenticate`.
//
// Examples:
//
//	BuildStepUpChallenge(PermissionRequirement{RequiredACRMin: "3"})
//	  → `Bearer error="insufficient_user_authentication",
//	     error_description="Required ACR 3 for this resource; presented ACR 2",
//	     acr_values="3"`
//
// `presentedACR` may be empty (anonymous / no token).
func BuildStepUpChallenge(req PermissionRequirement, presentedACR string) string {
	desc := fmt.Sprintf("Required ACR %s for this resource; presented ACR %s",
		req.RequiredACRMin, defaultIfEmpty(presentedACR, "0"))
	out := `Bearer error="insufficient_user_authentication", error_description="` + desc + `"`
	if req.RequiredACRMin != "" {
		out += `, acr_values="` + req.RequiredACRMin + `"`
	}
	if req.MFAMaxAge > 0 {
		out += fmt.Sprintf(`, max_age="%d"`, int(req.MFAMaxAge.Seconds()))
	}
	return out
}

func defaultIfEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
