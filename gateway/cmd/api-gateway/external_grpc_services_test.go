// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// external_grpc_services_test.go — the gate on WHAT the advertised edge answers
// natively.
//
// WHY A GATE. "Reflection is not on the external listener" is a claim about a
// single call in a 900-line composition root. Nothing was able to fail when it
// was there, which is why it stayed: the comment beside it asserted that only
// natively-registered services were visible, and that assertion was true of
// ListServices and false of symbol lookup. A note cannot hold a property; a
// failing test can.
//
// WHY IT READS THE SERVER AND NOT THE SOURCE. The subject is the *grpc.Server
// object main() serves on the ingress-facing and TLS listeners. GetServiceInfo()
// is that object's own account of what it dispatches, so an added
// reflection.Register anywhere in the wiring reddens this — including one added
// through a helper, which a grep for the call site would miss.
package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/gateway/internal/opsproxy"
)

// reflectionServicePrefix — the proto package prefix shared by every version of
// server-reflection (v1, v1alpha, and any future one). Matching the prefix
// rather than the two known names is deliberate: grpc-go's Register has added a
// version before and would otherwise slip in unnoticed.
const reflectionServicePrefix = "grpc.reflection."

// externalSurface builds the edge's native gRPC surface exactly as main() does
// and returns the server's own account of it.
//
// Built with a plain grpc.NewServer, NOT proxy.NewServer: the transparent
// UnknownServiceHandler is what forwards everything else to a backend, and it
// answers unknown methods by dialing rather than with Unimplemented. Leaving it
// out is what lets a probe distinguish "the edge answers this itself" from "the
// edge would forward it".
func externalSurface(t *testing.T) (*grpc.Server, map[string]grpc.ServiceInfo) {
	t.Helper()
	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)
	registerExternalGRPCServices(srv, nil, opsproxy.New(nil))
	return srv, srv.GetServiceInfo()
}

// TestExternalGRPCSurface_ServesNoReflection — THE GATE.
//
// Server-reflection on the advertised edge is an unauthenticated, unauthorized
// surface: it is entry 1 of DefaultPublicAllowlist, which phaseAllowlist
// consults at decide() step 1, three phases ahead of the 401. It falls under
// neither exemption security.md documents. Its answer is not tenant data — the
// proto tree is public — so the cost is the exemption itself plus a free
// amplification lever, and neither is worth edge-side grpcurl.
func TestExternalGRPCSurface_ServesNoReflection(t *testing.T) {
	_, info := externalSurface(t)

	// ── Premise: the surface was actually built. ───────────────────────────
	// An empty map would make the negative assertion below pass while proving
	// nothing — "found no reflection" and "found no services" print the same ok.
	require.NotEmpty(t, info,
		"GATE CANNOT RUN: the edge registered no services at all, so a pass below would mean 'read nothing'")

	// ── Paired positive: the things the edge IS supposed to answer. ────────
	// Without these, deleting registerExternalGRPCServices' body would turn this
	// gate green — the strongest possible false pass.
	require.Contains(t, info, "grpc.health.v1.Health",
		"the edge must keep answering health: kubelet probes and the readiness fan-out depend on it")
	require.Contains(t, info, "kacho.cloud.operation.OperationService",
		"the edge must keep answering OperationService natively: every async client polls it")

	// ── The property. ─────────────────────────────────────────────────────
	var reflective []string
	for svc := range info {
		if strings.HasPrefix(svc, reflectionServicePrefix) {
			reflective = append(reflective, svc)
		}
	}
	require.Empty(t, reflective,
		"server-reflection is registered on the externally-reachable gRPC server (%v). "+
			"The same *grpc.Server is served on the ingress-facing cmux listener and on the advertised "+
			"TLS listener, and reflection sits on the authN+authZ bypass list, so this is an unauthenticated "+
			"surface on the edge. Symbol lookup resolves against protoregistry.GlobalFiles — the whole linked "+
			"descriptor set — so a short ListServices answer does not bound what it returns. "+
			"Operator tooling belongs on the cluster-internal listener, behind mTLS: internal_grpc_listener.go",
		reflective)
}

// TestExternalGRPCSurface_IsExactlyTheDocumentedSet — the surface is small
// enough to enumerate, so enumerate it.
//
// The reflection gate above forbids one family by name. This one asserts the
// whole set, which is what catches the NEXT thing registered at the edge without
// a decision — a shape no name-based check can anticipate.
func TestExternalGRPCSurface_IsExactlyTheDocumentedSet(t *testing.T) {
	_, info := externalSurface(t)

	want := map[string]string{
		"grpc.health.v1.Health":                  "gateway self-liveness; constant SERVING, ignores the requested service name",
		"kacho.cloud.operation.OperationService": "async poll surface, fanned out across backends by the gateway",
	}

	for svc := range info {
		if _, ok := want[svc]; !ok {
			t.Errorf("%q is registered natively on the externally-reachable gRPC server but is not in the "+
				"documented edge surface. Anything registered here is answerable from the advertised TLS "+
				"listener and from the ingress-facing plaintext listener. Either record the decision in "+
				"gateway/docs/engineering/architecture/known-divergences.md §10 and add it to this set, or register it "+
				"on the cluster-internal listener instead.", svc)
		}
	}
	for svc, why := range want {
		require.Contains(t, info, svc,
			"%q disappeared from the edge surface (%s) — that is a posture change, not a cleanup", svc, why)
	}
}
