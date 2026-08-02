// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// render_reflection_test.go — render-guard for the retired reflection switch.
//
// gRPC server-reflection moved off the externally-reachable listener onto the
// cluster-internal one, where it is served exactly when that listener enforces
// mTLS and the caller allow-list. The standalone knob that used to steer it
// (`internalListener.reflection` → KACHO_API_GATEWAY_INTERNAL_GRPC_REFLECTION)
// is gone, because its dangerous setting was reachable in one step: on, while
// the listener runs insecure and mounts no authorising interceptors.
//
// A removed knob leaves a stale value behind in somebody's overlay. "Stale
// values are ignored" is the kind of claim that is true until a template grows a
// second reference, so it is asserted by rendering WITH the retired value set
// rather than left to the reader's confidence.
package deploy_test

import (
	"strings"
	"testing"
)

// retiredReflectionEnv — the env var the process no longer reads. Named once so
// the guard and its message cannot drift apart.
const retiredReflectionEnv = "KACHO_API_GATEWAY_INTERNAL_GRPC_REFLECTION"

// TestRender_RetiredReflectionKnobIsInert — set the retired value on every
// posture and require that nothing reaches the PodSpec.
//
// Both postures are rendered on purpose. The insecure one is where the knob was
// actually dangerous; the mTLS one is where a template author would most
// plausibly reinstate it while "restoring debugging".
func TestRender_RetiredReflectionKnobIsInert(t *testing.T) {
	for _, tc := range []struct {
		name string
		sets []string
	}{
		{
			name: "insecure internal listener (dev/local posture)",
			sets: []string{"internalListener.reflection=true"},
		},
		{
			name: "mTLS internal listener (production posture)",
			sets: []string{
				"mtls.enable=true",
				"internalListener.mtls.enable=true",
				"internalListener.reflection=true",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := helmTemplate(t, tc.sets...)

			// Premise: the render produced a PodSpec at all. Without this, a
			// broken template that emitted nothing would satisfy the negative
			// assertion below and read as a pass.
			mustContain(t, out, "kind: Deployment")
			mustContain(t, out, "KACHO_API_GATEWAY_LISTEN_ADDR")

			if strings.Contains(out, retiredReflectionEnv) {
				t.Fatalf("%s is still rendered when an overlay sets the retired internalListener.reflection "+
					"value. The process no longer reads it, so an emitted variable is at best noise and at "+
					"worst a knob someone re-wires: reflection follows internalListener.mtls.enable and "+
					"nothing else.", retiredReflectionEnv)
			}
		})
	}
}

// TestRender_ReflectionHasNoSwitchOfItsOwn — the paired positive.
//
// The guard above forbids one variable name; on its own it would also pass on a
// chart that stopped rendering the internal listener entirely. This asserts the
// listener the reflection surface now lives on is still wired in the posture
// that authorises callers — so "no reflection knob" cannot be satisfied by "no
// internal listener".
func TestRender_ReflectionHasNoSwitchOfItsOwn(t *testing.T) {
	out := helmTemplate(t,
		"mtls.enable=true",
		"internalListener.mtls.enable=true",
	)

	mustContain(t, out, "KACHO_API_GATEWAY_INTERNAL_GRPC_MTLS_ENABLE")
	mustContain(t, out, "KACHO_API_GATEWAY_INTERNAL_GRPC_ALLOWED_SPIFFE")

	if strings.Contains(out, retiredReflectionEnv) {
		t.Fatalf("%s must not be rendered in any posture", retiredReflectionEnv)
	}
}
