// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/pkg/observability"
)

// bootPosture is the api-gateway's self-report of the posture the process
// actually booted with (see observability.BootPosture: the gate must assert on
// an observed fact, not on stored configuration).
//
// Where the values come from:
//   - auth_mode     — cfg.AuthNMode, the mode the auth interceptor is built with
//     (validateProductionAuthzConfig has already refused a relaxed posture under
//     a production-class KACHO_APP_ENV).
//   - db_sslmode    — the gateway owns no database, hence the literal "n/a".
//   - public_mtls   — client-certificate verification on the EXTERNAL listener.
//     It is only real when the TLS listener actually starts (cert+key+addr
//     present) AND hybrid mTLS is enabled: without the TLS listener the hybrid
//     flag verifies nothing, so reporting it alone would be a values-file wish
//     rather than the wired listener state.
//   - internal_mtls — the RESOLVED state of the internal gRPC listener
//     (buildInternalListenerSecurity), not the raw enable flag.
//   - authz_check   — whether the per-RPC authz middleware enforces. With
//     KACHO_API_GATEWAY_AUTHZ_ENABLED=false it mounts as a pass-through, i.e. no
//     per-RPC Check happens at all.
func bootPosture(cfg config.Config, internalListenerMTLS bool) observability.BootPosture {
	return observability.BootPosture{
		Service:      "api-gateway",
		AuthMode:     cfg.AuthNMode,
		DBSSLMode:    observability.DBSSLModeNotApplicable,
		PublicMTLS:   cfg.TLSEnabled() && cfg.HybridMTLSEnabled(),
		InternalMTLS: internalListenerMTLS,
		AuthZCheck:   cfg.AuthZEnabled,
	}
}
