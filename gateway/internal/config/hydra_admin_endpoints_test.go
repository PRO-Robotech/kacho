// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// hydra_admin_endpoints_test.go — the two endpoints the revocation path needs
// live on the identity provider's ADMIN API, and neither may be inferred.
//
// A derived value here is not a convenience: it aims a security control at a
// guessed address. Both endpoints are reachable only inside the cluster, on a
// different Service and port from the public issuer, so an address derived from
// the issuer answers with something that is not the endpoint at all — and the
// caller cannot tell that apart from "the token is fine".
//
// So the contract is: unset means UNSET. The composition root refuses to start a
// production-class gateway on an unset address (see the boot guard in package
// main), which is a statement an operator can act on, unlike a silent fallback.
package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// An unset introspection endpoint stays unset. Deriving it from the issuer aims
// the revocation check at the PUBLIC OAuth2 API, where the endpoint does not
// exist — the check then answers "could not ask" on every request, forever.
func TestResolvedHydraIntrospectionURL_UnsetIsNotDerived(t *testing.T) {
	cfg := config.Config{APIDomain: "api.kacho.cloud"}
	require.Empty(t, cfg.ResolvedHydraIntrospectionURL(),
		"an unset introspection endpoint must stay unset; deriving one from the issuer "+
			"points the revocation check at an address that cannot serve it")

	cfg.HydraIssuer = "https://hydra.api.kacho.cloud"
	require.Empty(t, cfg.ResolvedHydraIntrospectionURL(),
		"an explicit issuer is still not an introspection endpoint — introspection lives "+
			"on the admin API, on another Service and port")
}

// The explicit value is returned verbatim.
func TestResolvedHydraIntrospectionURL_ExplicitIsVerbatim(t *testing.T) {
	const want = "http://kacho-umbrella-hydra-admin.kacho.svc:4445/admin/oauth2/introspect"
	cfg := config.Config{
		APIDomain:             "api.kacho.cloud",
		HydraIssuer:           "https://hydra.api.kacho.cloud",
		HydraIntrospectionURL: want,
	}
	require.Equal(t, want, cfg.ResolvedHydraIntrospectionURL())
}

// Same rule for the admin base the logout handler uses to kill the provider-side
// session. Its own doc says an empty value disables the session-kill; a derived
// one does something worse — it POSTs the kill to whatever answers on the public
// issuer host, and then reports success or failure about the wrong server.
func TestResolvedHydraAdminURL_UnsetIsNotDerived(t *testing.T) {
	cfg := config.Config{APIDomain: "api.kacho.cloud", HydraIssuer: "https://hydra.api.kacho.cloud"}
	require.Empty(t, cfg.ResolvedHydraAdminURL(),
		"an unset admin base must stay unset; the issuer host does not serve the admin API")
}

// The explicit admin base is returned verbatim.
func TestResolvedHydraAdminURL_ExplicitIsVerbatim(t *testing.T) {
	const want = "http://kacho-umbrella-hydra-admin.kacho.svc:4445"
	cfg := config.Config{APIDomain: "api.kacho.cloud", HydraAdminURL: want}
	require.Equal(t, want, cfg.ResolvedHydraAdminURL())
}
