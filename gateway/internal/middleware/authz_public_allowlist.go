// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz_public_allowlist.go — fixed list of gRPC FQNs admitted at decide()
// step 1, ahead of every other phase.
//
// WHAT MEMBERSHIP ACTUALLY WAIVES. Not "the AuthZ check" — both checks.
// phaseAllowlist is step 1 of decide(); subject extraction, the phase that
// answers 401, is step 4. An entry here therefore returns ALLOW without a
// principal ever being resolved: it is an authN *and* authZ bypass, and it
// applies on every listener, the advertised external TLS edge included. That is
// a stronger thing than the catalog's `<exempt>`, which still demands
// authentication and only skips the FGA Check. Prefer `<exempt>`; reach for
// this list only when the caller cannot have a subject yet.
//
// EVERY ENTRY IS A DECISION AND CARRIES ITS OWN JUSTIFICATION BELOW. An entry
// that names an RPC which does not exist is not harmless dead weight — it reads
// like a reviewed decision, and the next person adding a bypass copies its
// shape. authz_public_allowlist_resolves_test.go fails the build on one.
//
// NOT DOCUMENTED AS A RULE EXCEPTION (state as of this revision). security.md
// carries exactly two documented exemptions from "authN+authZ on every RPC" —
// the iam JWKS route and geo public catalog reads. Neither covers the entries
// below, so their justification lives here and in
// docs/architecture/known-divergences.md §10 rather than being left implicit.
//
// NOTE: OperationService.Get/List are deliberately NOT on this list. They are
// frequently polled but still require authentication — handled via the catalog
// "<exempt>" path (authenticate, skip the FGA Check), never a blanket bypass at
// the edge. Its proto package is "kacho.cloud.operation" (no ".v1."), so a
// "v1"-shaped entry here would never match and would only weaken the list.
//
// The interactive auth flow is NOT here and never was a gRPC surface in this
// repository: login / callback / me / logout are HTTP routes under
// /iam/v1/auth/ (middleware/oidc_auth.go) and OAuth logout is /oauth/logout
// (handler/logout_handler.go). Their pre-auth exemption is isPublicHTTPPath in
// authz_util.go — a separate, live list. Six entries naming a gRPC
// "AuthService"/"BackChannelLogoutService" were removed from here because no
// such service exists in the contract; the exemption they appeared to grant was
// never theirs to grant and is unaffected.
//
// Any additional bypass MUST go through the `authz_overrides.yaml` mechanism
// (auditable, hot-reloadable) instead of being baked into this code path.
package middleware

// DefaultPublicAllowlist returns the curated list of gRPC FQNs that pass
// through the AuthZ middleware without any AuthorizeService.Check call.
//
// Sorted alphabetically — keep that property when adding entries; tests
// rely on it.
func DefaultPublicAllowlist() []string {
	return []string{
		// grpc.health — liveness/readiness probing.
		//
		// What it returns: a SERVING/NOT_SERVING enum aggregated over the
		// gateway's backends (internal/health/health.go). No tenant data, no
		// resource identifiers, no per-owner objects — the same answer for
		// every caller, which is what makes an unauthenticated answer
		// defensible here.
		//
		// Why not `<exempt>`: kubelet probes carry no bearer token, so
		// requiring authentication would fail every probe. Gating the probe on
		// the authz path would also couple liveness to an IAM outage and turn
		// one into a cluster-wide rolling-restart loop.
		//
		// Watch is listed alongside Check although the gateway embeds
		// UnimplementedHealthServer and therefore answers Unimplemented: the
		// method exists in the served contract, and listing it keeps a future
		// implementation from silently arriving behind a bypass.
		"grpc.health.v1.Health/Check",
		"grpc.health.v1.Health/Watch",

		// grpc.reflection — schema enumeration for grpcurl and similar CLIs.
		//
		// SCOPE, STATED ACCURATELY. An earlier comment here claimed reflection
		// was "only available cluster-internal anyway". It is not:
		// reflection.Register(grpcSrv) in cmd/api-gateway/main.go registers on
		// the same *grpc.Server that is served on the advertised external TLS
		// listener, so these two FQNs are answerable unauthenticated from the
		// edge. The comment is corrected rather than deleted because a
		// security note that contradicts the code invites the next reader to
		// "fix" the code to match it.
		//
		// What it returns: descriptors for services registered NATIVELY on the
		// gateway — OperationService, Health and ServerReflection itself.
		// Backend services (iam/vpc/compute/…) are transparently proxied, not
		// registered here, so they are not enumerable through this surface;
		// and every descriptor it does return is already published in the
		// public proto tree. No tenant data and no per-owner objects.
		//
		// This is the weakest justification of the four and is recorded as an
		// open question in docs/architecture/known-divergences.md §10: unlike
		// health, reflection is developer convenience rather than an
		// operational necessity, and restricting it to the cluster-internal
		// listener would cost only edge-side grpcurl.
		"grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
		"grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",

		// SECURITY: Internal* FQNs are deliberately NOT on this global allowlist.
		//
		// The REST path serves both the internal and the advertised external
		// TLS listener from the SAME *http.Server, so a global FQN allowlist
		// would short-circuit decide() to ALLOW even for an UNAUTHENTICATED
		// caller hitting these RPCs from the edge — an authz-oracle /
		// user-enumeration / user-mutation priv-esc.
		//
		// Internal callers (api-gateway auth-interceptor self-call, admin tooling
		// via port-forward, kacho-iam subject-change drainer) still carry no
		// external user JWT — but they arrive on the cluster-internal listener.
		// They are admitted by the LISTENER-ORIGIN gate in decide()
		// (allowlist.HasInternalSuffix + !listenerorigin.IsExternal) instead of a
		// blanket FQN bypass, so an external caller of these is authN-required /
		// rejected while an internal caller still passes. Defense-in-depth: the
		// REST dispatcher additionally 404s Internal* paths on the external
		// listener (restmux.NewMux), and the gRPC routing layer blocks Internal*
		// gRPC everywhere (HasInternalSuffix).
	}
}
