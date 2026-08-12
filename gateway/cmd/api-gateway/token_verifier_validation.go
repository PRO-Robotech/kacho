// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// token_verifier_validation.go — startup-validation for the RS256/JWKS token
// verifier on the principal path.
package main

import (
	"fmt"
	"strings"
)

// validateProductionTokenVerifierConfig refuses to start when the deploy
// environment is production-class AND the RS256/JWKS verifier could not be
// constructed.
//
// Why a refusal and not the warning that used to stand here: the verifier is
// built from configuration and nothing else — its constructor fails only on an
// empty JWKS address or an empty expected issuer, and it performs no network
// call while being built. A failure there is therefore a MISCONFIGURATION, not
// an outage: repeating the same start with the same configuration can never
// succeed. Absorbed into a warning, it made a permanent misconfiguration the
// normal running mode — the edge kept serving with only the symmetric dev path
// wired, reported itself as configured, and refused nothing for as long as it
// lived. That is the same class as an install step that treats a refused write
// as a normal outcome and carries on.
//
// Environment classes are the ones the neighbouring guards already use
// (validateProductionAuthzConfig / validateProductionRevocationConfig): only the
// explicit dev-class labels tolerate the relaxation, and an empty or misspelt
// label is production-class, so a deploy that forgets to set KACHO_APP_ENV still
// fails closed rather than skipping the guard.
//
// A nil verifierErr means the verifier was constructed: nothing to judge, every
// environment passes.
func validateProductionTokenVerifierConfig(env string, verifierErr error) error {
	if verifierErr == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		// Dev-class — the caller keeps the previous soft pass and emits a WARN.
		return nil
	}
	return fmt.Errorf(
		"token verifier invalid in %q env: the RS256/JWKS verifier was not constructed (%v) — "+
			"without it the edge validates no provider-issued token on the principal path, "+
			"and the failure is configuration, not an outage: the same start can never succeed "+
			"until the JWKS address and expected issuer are set (refuse to start)",
		env, verifierErr,
	)
}
