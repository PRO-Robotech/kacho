// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// auth_machine_binding_test.go — machine principals must present a
// sender-constrained token where binding is required.
//
// Why this sits in auth.go and not in the DPoP middleware. The `cnf` machinery
// (DPoPMiddleware on REST, CnfBindingInterceptor on gRPC) is gated behind
// KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP, which defaults to false and — until this
// change — was emitted by no chart template and set in no values file. So the
// only path that ever inspected `cnf` was unreachable on a deployed stand, and
// a machine token was an ordinary, replayable bearer: the asymmetric signature
// protects the long-lived KEY, not the token minted from it (CWE-294).
// AuthInterceptor is the one authN middleware that always runs, so the control
// lives here.
//
// Staged by construction: the requirement is OFF unless explicitly enabled,
// because no OAuth2 client is yet registered to mint bound tokens — turning it
// on before issuance lands would reject every service-account token. The
// human/interactive path is never affected.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// bindingVerifier — a TokenVerifier returning a canned VerifiedToken, so the
// test drives the cnf/principal shape directly rather than minting real JWTs.
type bindingVerifier struct{ tok *VerifiedToken }

func (v *bindingVerifier) Verify(_ context.Context, raw string) (*VerifiedToken, error) {
	t := *v.tok
	t.Raw = raw
	return &t, nil
}

// machineToken / humanToken — verified tokens differing only in principal type.
func machineToken(bound bool) *VerifiedToken {
	t := &VerifiedToken{
		Subject: "sva0000000000000000a",
		Claims: map[string]any{
			"kacho_principal_type": "service_account",
			"kacho_principal_id":   "sva0000000000000000a",
		},
	}
	if bound {
		t.Cnf = TokenConfirmation{Jkt: "thumbprint", HasJkt: true}
	} else {
		t.Cnf = TokenConfirmation{IsBearer: true}
	}
	return t
}

func humanToken(bound bool) *VerifiedToken {
	t := &VerifiedToken{
		Subject: "usr0000000000000000a",
		Claims: map[string]any{
			"kacho_principal_type": "user",
			"kacho_principal_id":   "usr0000000000000000a",
		},
	}
	if bound {
		t.Cnf = TokenConfirmation{Jkt: "thumbprint", HasJkt: true}
	} else {
		t.Cnf = TokenConfirmation{IsBearer: true}
	}
	return t
}

// asymBearer — a syntactically asymmetric JWT so isAsymmetricJWT routes the
// request down the Hydra-JWT path. The stub verifier ignores the contents.
const asymBearer = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ4In0.sig"

func bindingInterceptor(t *testing.T, vt *VerifiedToken, require bool) *AuthInterceptor {
	t.Helper()
	a := NewAuthInterceptor(AuthModeProduction, "", nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	a = a.WithVerifier(&bindingVerifier{tok: vt})
	return a.WithRequireMachineTokenBinding(require)
}

func doREST(t *testing.T, a *AuthInterceptor) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	served := false
	h := a.HTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true }))
	req := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	req.Header.Set("Authorization", "Bearer "+asymBearer)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, served
}

// ── REST: the executed production path ───────────────────────────────────────

// TestREST_MachineUnboundToken_RejectedWhenBindingRequired — the core control.
// Without it a leaked machine token is replayable by anyone who holds the
// bytes; the asymmetric key that minted it is irrelevant to that replay.
func TestREST_MachineUnboundToken_RejectedWhenBindingRequired(t *testing.T) {
	a := bindingInterceptor(t, machineToken(false), true)
	rec, served := doREST(t, a)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401 for an unbound machine token", rec.Code)
	}
	if served {
		t.Error("the request must not reach the backend")
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("a 401 must carry an RFC 6750 challenge")
	}
}

// TestREST_MachineBoundToken_Accepted — a machine token that IS
// sender-constrained passes; the control demands binding, not abstinence.
func TestREST_MachineBoundToken_Accepted(t *testing.T) {
	a := bindingInterceptor(t, machineToken(true), true)
	rec, served := doREST(t, a)

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("a cnf-bound machine token must be accepted; got 401")
	}
	if !served {
		t.Error("the request must reach the backend")
	}
}

// TestREST_HumanUnboundToken_AcceptedWhenBindingRequired — the staged rollout
// contract: requiring binding for machines must NOT break the interactive path,
// where the browser flow issues plain bearers.
func TestREST_HumanUnboundToken_AcceptedWhenBindingRequired(t *testing.T) {
	a := bindingInterceptor(t, humanToken(false), true)
	rec, served := doREST(t, a)

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("the human path must be unaffected by the machine-binding requirement; got 401")
	}
	if !served {
		t.Error("the human request must reach the backend")
	}
}

// TestREST_MachineUnboundToken_AcceptedWhenNotRequired — default-off. Enabling
// before the provider mints bound tokens would reject every service account, so
// the requirement must be inert until switched on.
func TestREST_MachineUnboundToken_AcceptedWhenNotRequired(t *testing.T) {
	a := bindingInterceptor(t, machineToken(false), false)
	rec, served := doREST(t, a)

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("with the requirement off an unbound machine token must still pass; got 401")
	}
	if !served {
		t.Error("the request must reach the backend when the requirement is off")
	}
}

// TestREST_MachineBoundToken_StillInjectsPrincipal — the guard must not disturb
// the identity it gates: a passing machine request still carries its principal
// headers downstream.
func TestREST_MachineBoundToken_StillInjectsPrincipal(t *testing.T) {
	a := bindingInterceptor(t, machineToken(true), true)

	var gotType, gotID string
	h := a.HTTP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get(principalmeta.HeaderPrincipalType)
		gotID = r.Header.Get(principalmeta.HeaderPrincipalID)
	}))
	req := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	req.Header.Set("Authorization", "Bearer "+asymBearer)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotType != "service_account" || gotID != "sva0000000000000000a" {
		t.Errorf("principal = (%q,%q); want the machine principal", gotType, gotID)
	}
}

// ── gRPC: parity on the native surface ───────────────────────────────────────

func doGRPC(t *testing.T, a *AuthInterceptor) error {
	t.Helper()
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+asymBearer))
	_, err := a.authorize(ctx, "/kacho.cloud.vpc.v1.NetworkService/List")
	return err
}

// TestGRPC_MachineUnboundToken_RejectedWhenBindingRequired — the same control
// on the native gRPC surface. A gap on either surface makes the other pointless:
// the replayer simply uses the unguarded one.
func TestGRPC_MachineUnboundToken_RejectedWhenBindingRequired(t *testing.T) {
	err := doGRPC(t, bindingInterceptor(t, machineToken(false), true))
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v (err=%v); want Unauthenticated", status.Code(err), err)
	}
}

// TestGRPC_MachineBoundToken_Accepted — bound machine tokens pass on gRPC too.
func TestGRPC_MachineBoundToken_Accepted(t *testing.T) {
	if err := doGRPC(t, bindingInterceptor(t, machineToken(true), true)); err != nil {
		t.Fatalf("a bound machine token must be accepted: %v", err)
	}
}

// TestGRPC_HumanUnboundToken_Accepted — human path unaffected on gRPC.
func TestGRPC_HumanUnboundToken_Accepted(t *testing.T) {
	if err := doGRPC(t, bindingInterceptor(t, humanToken(false), true)); err != nil {
		t.Fatalf("the human path must be unaffected: %v", err)
	}
}

// TestGRPC_MachineUnboundToken_AcceptedWhenNotRequired — default-off on gRPC.
func TestGRPC_MachineUnboundToken_AcceptedWhenNotRequired(t *testing.T) {
	if err := doGRPC(t, bindingInterceptor(t, machineToken(false), false)); err != nil {
		t.Fatalf("with the requirement off the token must pass: %v", err)
	}
}

// ── lookup-fallback branch ───────────────────────────────────────────────────

// lookupStub — a SubjectLookuper resolving a fixed subject, used to reach the
// branch taken when a verified token carries no kacho_principal_* claims.
type lookupStub struct{ typ, id string }

func (l *lookupStub) LookupByExternalID(_ context.Context, _ string) (Subject, error) {
	return Subject{Type: l.typ, ID: l.id, DisplayName: ""}, nil
}

// claimlessToken — verified, but without kacho_principal_* claims, so the
// principal comes from the lookup instead of the token.
func claimlessToken() *VerifiedToken {
	return &VerifiedToken{
		Subject: "external-subject",
		Claims:  map[string]any{},
		Cnf:     TokenConfirmation{IsBearer: true},
	}
}

func lookupInterceptor(t *testing.T, typ string) *AuthInterceptor {
	t.Helper()
	a := NewAuthInterceptor(AuthModeProduction, "", &lookupStub{typ: typ, id: "resolved-id"},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return a.WithVerifier(&bindingVerifier{tok: claimlessToken()}).
		WithRequireMachineTokenBinding(true)
}

// TestREST_MachineResolvedByLookup_UnboundRejected — guarding only the claims
// path would leave the lookup fallback as the way in for exactly the credential
// this control constrains.
func TestREST_MachineResolvedByLookup_UnboundRejected(t *testing.T) {
	rec, served := doREST(t, lookupInterceptor(t, "service_account"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401 — a machine principal resolved by lookup is still a machine", rec.Code)
	}
	if served {
		t.Error("the request must not reach the backend")
	}
}

// TestREST_HumanResolvedByLookup_Accepted — the same fallback for a human is
// untouched.
func TestREST_HumanResolvedByLookup_Accepted(t *testing.T) {
	rec, served := doREST(t, lookupInterceptor(t, "user"))
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("a human resolved by lookup must not be rejected; got 401")
	}
	if !served {
		t.Error("the request must reach the backend")
	}
}

// TestGRPC_MachineResolvedByLookup_UnboundRejected — gRPC parity.
func TestGRPC_MachineResolvedByLookup_UnboundRejected(t *testing.T) {
	if code := status.Code(doGRPC(t, lookupInterceptor(t, "service_account"))); code != codes.Unauthenticated {
		t.Fatalf("code = %v; want Unauthenticated", code)
	}
}

// TestGRPC_HumanResolvedByLookup_Accepted — gRPC human parity.
func TestGRPC_HumanResolvedByLookup_Accepted(t *testing.T) {
	if err := doGRPC(t, lookupInterceptor(t, "user")); err != nil {
		t.Fatalf("a human resolved by lookup must be accepted: %v", err)
	}
}
