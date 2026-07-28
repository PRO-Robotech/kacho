// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package main — startup-validation for the revocation path.
//
// Verifying a bearer's signature proves who minted it and when it expires. It
// does NOT prove the token is still good: a sign-out, a revoked machine key or a
// back-channel logout all leave a perfectly valid signature behind. The only
// authority on that is the identity provider, reached over its ADMIN API — and
// the gateway therefore needs two addresses it cannot work out for itself:
//
//   - the token-introspection endpoint, asked per request (through a short-TTL
//     cache) before the request is let through;
//   - the admin base, used by the logout handler to kill the provider-side
//     session so a sign-out actually ends the session everywhere.
//
// Neither can be derived. Both live on a Service and port distinct from the
// public issuer, and an address derived from the issuer names a server that does
// not serve them. That failure is invisible from the outside: the check runs, is
// answered by something that is not the endpoint, and — unless the process is
// told to treat that as a misconfiguration — the request proceeds.
//
// So an unset address is not a neutral default, it is the control switched off.
// This guard refuses to start a production-class gateway in that state, and it
// refuses an address whose shape shows it points at the public API instead of
// the admin one, because that is the exact mistake the derived default made.
package main

import (
	"fmt"
	"net/url"
	"strings"
)

// introspectionAdminPath — the path the identity provider's admin API serves
// token introspection on. Pinned here so an address aimed at the public OAuth2
// API (which serves no introspection at all) is refused at startup rather than
// failing on every request afterwards.
const introspectionAdminPath = "/admin/oauth2/introspect"

// RevocationConfig is the cross-section of the configuration this guard reads:
// the two admin-API addresses the revocation path depends on.
//
// Kept as a small value-type at the composition root — like AuthzMiddlewareConfig
// — so the validator is testable without pulling the middleware/clients graph
// into a `package main` test binary.
type RevocationConfig struct {
	// IntrospectionURL — resolved token-introspection endpoint (empty ⇒ unset).
	IntrospectionURL string
	// AdminURL — resolved admin API base for the logout session-kill (empty ⇒ unset).
	AdminURL string
}

// validateProductionRevocationConfig refuses to start when the deploy
// environment is production-class AND the revocation path is unconfigured or
// misaddressed.
//
// Only the explicit dev-class labels ("dev" / "local" / "test") tolerate an
// unconfigured path — a local stand may run with no reachable admin API at all.
// Every OTHER value, including an empty/unset label, is production-class and is
// validated: a deploy that forgets KACHO_APP_ENV still fails closed rather than
// silently skipping the guard, exactly as the sibling authz and internal-listener
// guards do.
func validateProductionRevocationConfig(env string, cfg RevocationConfig) error {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		return nil
	}

	var problems []string
	if strings.TrimSpace(cfg.IntrospectionURL) == "" {
		problems = append(problems,
			"KACHO_HYDRA_INTROSPECTION_URL is empty — with no introspection endpoint the "+
				"gateway never asks whether an access token was revoked, so a revoked token "+
				"keeps working until it expires on its own")
	} else if err := validateAdminEndpoint(cfg.IntrospectionURL, introspectionAdminPath); err != nil {
		problems = append(problems, "KACHO_HYDRA_INTROSPECTION_URL "+err.Error())
	}

	if strings.TrimSpace(cfg.AdminURL) == "" {
		problems = append(problems,
			"KACHO_HYDRA_ADMIN_URL is empty — the logout handler's provider-side session "+
				"kill is disabled when it is unset, so signing out leaves the session alive "+
				"at the identity provider")
	} else if err := validateAdminEndpoint(cfg.AdminURL, ""); err != nil {
		problems = append(problems, "KACHO_HYDRA_ADMIN_URL "+err.Error())
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf(
		"revocation path invalid in %q env: %s (refuse to start)",
		env, strings.Join(problems, "; "),
	)
}

// validateAdminEndpoint checks that an address is a usable absolute URL and,
// when wantPath is non-empty, that it addresses that exact path. The path check
// is what separates the admin API from the public one: the public OAuth2 API
// serves `/oauth2/introspect` to nobody, and an address ending there is the
// derived default this guard exists to catch.
func validateAdminEndpoint(raw, wantPath string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("is not a valid URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must be an absolute http(s) URL, got %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("has no host: %q", raw)
	}
	if wantPath != "" && strings.TrimRight(u.Path, "/") != wantPath {
		return fmt.Errorf(
			"must address the admin API path %q, got %q — the public OAuth2 API does not "+
				"serve introspection, and an address pointing there answers every check with "+
				"a non-answer", wantPath, u.Path)
	}
	return nil
}
