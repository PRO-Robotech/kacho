// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — HTTP handlers owned by api-gateway directly (not proxied
// to a backend): the logout endpoint and the relay of the sign-in form's verbs.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// SessionRevocationsClient — minimal port the logout handler needs to push
// revocations into kaname. Implemented by an adapter around the generated
// gRPC stub `InternalSessionRevocationsServiceClient.Revoke`. Declared here
// so the handler is unit-testable without spinning up a gRPC server.
//
// The adapter discards the Operation envelope returned by the gRPC stub —
// the logout handler does not poll for completion (Revoke writes
// session_revocations row in the same TX as Operation insert, so by the time
// Revoke returns, downstream pods receiving LISTEN/NOTIFY will already
// invalidate the token).
type SessionRevocationsClient interface {
	Revoke(ctx context.Context, in *iamv1.RevokeRequest) error
}

// VerifiedCaller — identity derived from a cryptographically validated access
// token. It is the ONLY trusted source of the logout subject: a caller may
// revoke exactly their own session(s), never a subject named in the request
// body. Client-supplied `subject`/`token_jti` form fields are ignored.
type VerifiedCaller struct {
	Subject string // token `sub` — the authenticated caller
	JTI     string // token `jti` — the specific session token being surrendered
}

// CallerVerifier — port that validates a presented access token (JWKS
// signature, issuer, audience, expiry) and returns the caller's identity.
// Implemented by an adapter over the gateway's JWKS verifier (the same
// instance used on the principal path). Declared here so the handler is
// unit-testable without a live JWKS endpoint.
//
// nil verifier ⇒ no credential can be authenticated, so every server-side
// revocation fails closed with 401 (only cookie-clearing remains available).
type CallerVerifier interface {
	Verify(ctx context.Context, token string) (*VerifiedCaller, error)
}

// LogoutHandler — POST /oauth/logout
//
//  1. Parse access_token from `Authorization: Bearer|DPoP <token>` OR form-encoded
//     `token` parameter (RFC 7009 section 2.1).
//  2. Authenticate the caller by verifying that token (JWKS signature, issuer,
//     audience, expiry). A presented-but-invalid token is a hard 401; the
//     subject/jti are taken ONLY from the validated token.
//  3. Fail closed: a request that asks to revoke sessions
//     (subject/token_jti/revoke_all) but presents no valid token is refused
//     with 401 — the endpoint never trusts a client-supplied subject, so it
//     cannot be abused to revoke another user's sessions.
//  4. Call kaname `InternalSessionRevocationsService.Revoke` for the caller's
//     own identity — revoke_all_user_tokens=false (single jti) or true (full).
//     This writes OUR revocation record, the one the edge's revocation lane
//     reads on every presentation.
//  5. Issue an ending for every browser session carrier name
//     (`middleware.EndSessionCarriers`). The ending carries no `Domain`, so it
//     matches only a cookie that was issued without one; the precondition this
//     implies is stated in the header of `middleware/session_carrier_names.go`.
//  6. Respond `200 {}`.
//
// There is no provider-side session to end any more (#2734): the previous
// identity provider's session kill — the handler's only outbound call — was
// retired together with the provider, so the handler talks to our identity
// service and to nobody else.
//
// The revocation call is best-effort relative to issuing the carrier ending —
// the user MUST see a successful logout from their side even if the identity
// service is momentarily unreachable. Failures are logged + included in the
// response `warnings` array for debugging but do not surface as HTTP 5xx (that
// would leave the client uncertain whether to retry).
type LogoutHandler struct {
	logger      *slog.Logger
	verifier    CallerVerifier
	revocations SessionRevocationsClient
}

// LogoutHandlerConfig — DI bag.
type LogoutHandlerConfig struct {
	Logger      *slog.Logger
	Verifier    CallerVerifier           // validates the caller's access token; nil ⇒ revocation fails closed (401)
	Revocations SessionRevocationsClient // optional — nil disables revocation
}

// NewLogoutHandler constructs the handler. Logger is required (we never want
// silent failures on a security-critical path).
func NewLogoutHandler(cfg LogoutHandlerConfig) (*LogoutHandler, error) {
	if cfg.Logger == nil {
		return nil, errors.New("logout handler: logger is required")
	}
	return &LogoutHandler{
		logger:      cfg.Logger,
		verifier:    cfg.Verifier,
		revocations: cfg.Revocations,
	}, nil
}

// ServeHTTP implements net/http.Handler.
func (h *LogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}

	// 1. Extract token (header OR form param `token`).
	rawToken := extractAccessToken(r)
	// Form parse — RFC 7009 allows `token=...` body.
	_ = r.ParseForm()
	if rawToken == "" {
		rawToken = strings.TrimSpace(r.Form.Get("token"))
	}

	// A "server-side revoke" is any request that asks the gateway to invalidate
	// sessions/tokens in iam (as opposed to merely clearing the caller's
	// own browser cookies). Historically the target subject/jti were read from
	// the request body, which let an unauthenticated caller revoke ANY user.
	// These client-supplied targets are no longer trusted — the identity is
	// derived exclusively from a validated access token.
	revokeRequested := strings.TrimSpace(r.Form.Get("subject")) != "" ||
		strings.TrimSpace(r.Form.Get("token_jti")) != "" ||
		r.Form.Get("revoke_all") == "true"
	revokeAll := r.Form.Get("revoke_all") == "true"

	// 2. Authenticate the caller from the presented access token. A token that
	//    is present but fails verification is a hard 401 — we never fall back
	//    to trusting the request body.
	var caller *VerifiedCaller
	if rawToken != "" && h.verifier != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		vc, verr := h.verifier.Verify(ctx, rawToken)
		cancel()
		if verr != nil {
			h.logger.Warn("logout: access-token verification failed", "err", verr)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_token"})
			return
		}
		caller = vc
	}

	// 3. Fail closed: revoking sessions requires a proven identity. Without a
	//    validated token we refuse the server-side revocation entirely and
	//    never act on a client-supplied subject.
	if revokeRequested && caller == nil {
		h.logger.Warn("logout: revocation requested without a validated access token — refused")
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication required to revoke sessions"})
		return
	}

	// 4. Revoke ONLY the authenticated caller's own session(s). Errors are
	//    collected, not fatal — the user must still see a successful logout.
	var revocErrs []string
	if caller != nil && h.revocations != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		req := &iamv1.RevokeRequest{
			TokenJti:            caller.JTI,
			UserId:              caller.Subject,
			Reason:              "user-logout",
			RevokeAllUserTokens: revokeAll,
			TtlExpiresAt:        timestamppb.New(time.Now().Add(30 * 24 * time.Hour)),
		}
		if err := h.revocations.Revoke(ctx, req); err != nil {
			h.logger.Warn("logout: revocations.Revoke failed", "err", err, "subject", caller.Subject)
			revocErrs = append(revocErrs, fmt.Sprintf("revocations: %v", err))
		}
		cancel()
	}

	// 5. Issue the ending of every browser session carrier name. Always done,
	//    even for a token-less request, so a user can drop their browser session.
	//    The NAMES live in ONE declaration (`middleware.EndSessionCarriers`,
	//    F4d-26): this handler and the refusal path of the identity lane issue the
	//    same endings the same way, otherwise "logout" and "refusal" would leave
	//    the same browser in different states. A second list here is what the
	//    gate `session_carrier_names_gate_test.go` calls a finding. Issuing an
	//    ending is not the same as removing the cookie: the ending carries no
	//    `Domain`, and the precondition that follows is stated in the header of
	//    `middleware/session_carrier_names.go`.
	middleware.EndSessionCarriers(w)

	out := map[string]any{"ok": true}
	if len(revocErrs) > 0 {
		// 207-style multi-status; we keep 200 to not block the user.
		out["warnings"] = revocErrs
	}
	if rawToken == "" {
		out["note"] = "no access_token presented; session carrier endings issued"
	}
	writeJSON(w, http.StatusOK, out)
}

// extractAccessToken pulls the bearer/DPoP token from the Authorization header.
// Returns "" if absent.
func extractAccessToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	for _, scheme := range []string{"Bearer ", "DPoP ", "bearer ", "dpop "} {
		if strings.HasPrefix(auth, scheme) {
			return strings.TrimSpace(auth[len(scheme):])
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
