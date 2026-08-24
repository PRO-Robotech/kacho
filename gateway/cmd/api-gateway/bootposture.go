// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/pkg/identityposture"
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
//   - identity_provider — the identity posture the process accepted: `external`
//     (a person is checked by the external provider) or `own` (by our own
//     minting). Read by the posture gate off the LIVE process, because the
//     posture decides which start-up demands apply and a values map answers that
//     question with intent: its knobs arrive through envFrom and are read once at
//     start-up, so editing the map changes the map, not the process.
func bootPosture(cfg config.Config, internalListenerMTLS bool, lane identityposture.Provider) observability.BootPosture {
	return observability.BootPosture{
		Service:      "api-gateway",
		AuthMode:     cfg.AuthNMode,
		DBSSLMode:    observability.DBSSLModeNotApplicable,
		PublicMTLS:   cfg.TLSEnabled() && cfg.HybridMTLSEnabled(),
		InternalMTLS: internalListenerMTLS,
		AuthZCheck:   cfg.AuthZEnabled,
		// Посадка личности, ПРИНЯТАЯ процессом: незаданное и негодное значения
		// до этой строки не доживают — страж старта на них не пускает. Значит
		// поле отчитывается об исходе, а не о намерении профиля (задача #1125).
		IdentityProvider: lane.String(),
	}
}
