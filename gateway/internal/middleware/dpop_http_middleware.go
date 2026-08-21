// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dpop_http_middleware.go — HTTP middleware that wires JWT verifier + DPoP
// validator + mTLS-bound validator + step-up gate into the REST request path.
//
// Position in the middleware chain (cmd/api-gateway/main.go):
//
//	HTTPRequestID
//	  HTTPRecovery
//	    AuthInterceptor.HTTP  (dev HMAC + Kratos session)
//	      DPoPMiddleware      ← THIS — production authN path
//	        HTTPAccessLog
//	          HTTPIdempotency
//	            httpMux
//
// When `KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP=true`, every request carrying an
// `Authorization: Bearer|DPoP ...` header runs through:
//
//  1. JWT verifier (Hydra JWKS, alg whitelist, iss/aud/exp).
//  2. If token.cnf.jkt set → DPoP header validation (htm/htu/iat/jti/jkt).
//  3. If token.cnf.x5t#S256 set → mTLS-bound (client cert vs cnf).
//  4. Step-up gate: required ACR / mfa_max_age from permission catalog.
//
// The revocation check is NOT here. It used to be, and that is precisely why it
// never ran: it applies to any presented token, while this middleware is about
// tokens minted bound to a key, and no profile switches this middleware on. It
// now lives on AuthInterceptor, which always runs — see auth_revocation.go.
//
// A rejected credential → 401 with an RFC 6750 `WWW-Authenticate` challenge and
// nothing is forwarded. The principal headers (X-Kacho-Principal-*) are
// then injected exactly as the legacy AuthInterceptor does, so backends see
// a unified shape regardless of whether the token came from dev-HMAC or
// from Hydra.
//
// When disabled (default), this middleware is a no-op pass-through — the
// behaviour for dev environments without Hydra.
package middleware

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// DPoPMiddleware — HTTP middleware orchestrator for production authN.
type DPoPMiddleware struct {
	verifier         *JWTVerifier
	dpop             *DPoPValidator
	mtls             *MTLSBoundValidator
	stepUp           *StepUpGate
	permissionLookup PermissionLookup
	routes           RestRouteResolver

	logger    *slog.Logger
	apiDomain string

	// requireForAllRequests — when true, missing Bearer/DPoP header
	// → 401 (production-strict equivalent for the DPoP path).
	requireForAllRequests bool
}

// PermissionLookup — port-interface that resolves per-RPC requirements
// (required_acr_min, mfa_max_age) keyed by the canonical gRPC FQN
// ("kacho.cloud.vpc.v1.NetworkService/Create"). The catalog implementation
// lives outside the middleware (it is backed by `permission_catalog.json`);
// any source is accepted. An empty fqn (unresolved route) yields the no-op
// requirement.
type PermissionLookup interface {
	Lookup(fqn string) PermissionRequirement
}

// DefaultPermissionLookup — fallback returning PermissionRequirement{ACR=""}
// (no requirement) for any method. Used in dev / when catalog is not wired.
type DefaultPermissionLookup struct{}

// Lookup always returns the no-op requirement.
func (DefaultPermissionLookup) Lookup(_ string) PermissionRequirement {
	return PermissionRequirement{}
}

// catalogPermissionLookup — PermissionLookup backed by the embedded permission
// catalog. It maps a resolved gRPC FQN to its per-RPC ACR floor
// (`required_acr_min`). An unknown FQN (or empty key) resolves to the no-op
// requirement so unmapped routes never fabricate a step-up demand.
type catalogPermissionLookup struct {
	catalog *PermissionCatalog
}

// NewCatalogPermissionLookup wires the step-up gate to the permission catalog.
// A nil catalog degrades to the no-op requirement for every method.
func NewCatalogPermissionLookup(catalog *PermissionCatalog) PermissionLookup {
	return catalogPermissionLookup{catalog: catalog}
}

// Lookup returns the ACR requirement for the given gRPC FQN.
func (c catalogPermissionLookup) Lookup(fqn string) PermissionRequirement {
	if c.catalog == nil || fqn == "" {
		return PermissionRequirement{}
	}
	entry, ok := c.catalog.Lookup(fqn)
	if !ok {
		return PermissionRequirement{}
	}
	return PermissionRequirement{RequiredACRMin: entry.RequiredACRMin}
}

// DPoPMiddlewareConfig — DI bag.
type DPoPMiddlewareConfig struct {
	Verifier         *JWTVerifier
	DPoP             *DPoPValidator
	MTLS             *MTLSBoundValidator
	StepUp           *StepUpGate
	PermissionLookup PermissionLookup
	// RestRouter resolves the incoming (method, path) to the canonical gRPC
	// FQN used as the PermissionLookup key. When nil the step-up gate has no
	// FQN to resolve and therefore imposes no per-RPC ACR requirement.
	RestRouter RestRouteResolver
	Logger     *slog.Logger
	APIDomain  string

	// RequireForAllRequests — production-strict; reject anonymous traffic.
	RequireForAllRequests bool
}

// NewDPoPMiddleware constructs the orchestrator. Verifier + DPoP + StepUp
// are required; permissionLookup is optional.
func NewDPoPMiddleware(cfg DPoPMiddlewareConfig) (*DPoPMiddleware, error) {
	if cfg.Verifier == nil {
		return nil, errors.New("dpop middleware: Verifier is required")
	}
	if cfg.DPoP == nil {
		return nil, errors.New("dpop middleware: DPoP validator is required")
	}
	if cfg.StepUp == nil {
		return nil, errors.New("dpop middleware: StepUp gate is required")
	}
	if cfg.MTLS == nil {
		cfg.MTLS = NewMTLSBoundValidator()
	}
	if cfg.PermissionLookup == nil {
		cfg.PermissionLookup = DefaultPermissionLookup{}
	}
	if cfg.Logger == nil {
		return nil, errors.New("dpop middleware: Logger is required")
	}
	if cfg.APIDomain == "" {
		return nil, errors.New("dpop middleware: APIDomain is required")
	}
	return &DPoPMiddleware{
		verifier:              cfg.Verifier,
		dpop:                  cfg.DPoP,
		mtls:                  cfg.MTLS,
		stepUp:                cfg.StepUp,
		permissionLookup:      cfg.PermissionLookup,
		routes:                cfg.RestRouter,
		logger:                cfg.Logger,
		apiDomain:             cfg.APIDomain,
		requireForAllRequests: cfg.RequireForAllRequests,
	}, nil
}

// Wrap returns an http.Handler middleware.
func (m *DPoPMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always skip on health / auth-flow endpoints — those run pre-auth.
		// Single source of truth for the pre-auth allow-list is
		// isPublicHTTPPath (authz_util.go), shared with the authz middleware so
		// the two layers can never drift.
		path := r.URL.Path
		if isPublicHTTPPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		// Determine scheme (Bearer vs DPoP) — both ride on Authorization header.
		auth := r.Header.Get("Authorization")
		token, scheme := splitAuthScheme(auth)
		dpopHeader := r.Header.Get("DPoP")

		// 1. No Authorization header → respect requireForAllRequests; otherwise pass.
		if token == "" {
			if m.requireForAllRequests {
				m.challenge(w, r, http.StatusUnauthorized,
					`Bearer error="invalid_token", error_description="missing access token"`, nil)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// 2. Verify access token (signature + iss/aud/exp/nbf/iat).
		verified, err := m.verifier.Verify(r.Context(), token)
		if err != nil {
			m.logger.Warn("dpop-mw: jwt verify failed", "err", err, "path", path)
			m.challenge(w, r, http.StatusUnauthorized,
				`Bearer error="invalid_token", error_description="`+sanitizeErr(err)+`"`, nil)
			return
		}

		// 3. Sender-constrained checks.
		switch {
		case verified.Cnf.HasJkt:
			req := DPoPRequest{
				Method:     r.Method,
				URL:        absoluteRequestURL(r, m.apiDomain),
				DPoPHeader: dpopHeader,
			}
			if err := m.dpop.Validate(verified, req); err != nil {
				m.logger.Warn("dpop-mw: dpop validate failed", "err", err, "path", path)
				m.challenge(w, r, http.StatusUnauthorized,
					`DPoP error="invalid_dpop_proof", error_description="`+sanitizeErr(err)+`"`, nil)
				return
			}
		case verified.Cnf.HasX5tS:
			var connState *tls.ConnectionState
			if r.TLS != nil {
				connState = r.TLS
			}
			if err := m.mtls.Validate(verified, connState, nil); err != nil {
				m.logger.Warn("dpop-mw: mtls validate failed", "err", err, "path", path)
				m.challenge(w, r, http.StatusUnauthorized,
					`Bearer error="invalid_token", error_description="`+sanitizeErr(err)+`"`, nil)
				return
			}
		default:
			// Plain bearer — accepted when scheme=Bearer; reject when scheme=DPoP
			// (mismatched expectation: client signalled DPoP, but token has no jkt).
			if strings.EqualFold(scheme, "DPoP") {
				m.challenge(w, r, http.StatusUnauthorized,
					`DPoP error="invalid_token", error_description="access token has no cnf.jkt"`, nil)
				return
			}
		}

		// 4. Step-up gate. Resolve the canonical gRPC FQN from the REST
		//    (method, path) so the catalog-backed lookup keys on a real entry
		//    (an unresolved route yields the empty FQN → no requirement).
		req := m.permissionLookup.Lookup(m.resolveFQN(r.Method, path))
		if err := m.stepUp.Check(verified, req); err != nil {
			challenge := BuildStepUpChallenge(req, verified.ACR)
			m.logger.Info("dpop-mw: step-up required",
				"path", path, "presented_acr", verified.ACR, "required", req.RequiredACRMin)
			m.challenge(w, r, http.StatusUnauthorized, challenge, nil)
			return
		}

		// 5. Inject principal headers — backends consume via corelib's
		//    PrincipalExtractInterceptor.
		injectVerifiedTokenHeaders(r, verified)

		next.ServeHTTP(w, r)
	})
}

// splitAuthScheme returns (token, scheme) where scheme ∈ {"Bearer","DPoP"}.
func splitAuthScheme(auth string) (token, scheme string) {
	if auth == "" {
		return "", ""
	}
	if v, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return v, "Bearer"
	}
	if v, ok := strings.CutPrefix(auth, "bearer "); ok {
		return v, "Bearer"
	}
	if v, ok := strings.CutPrefix(auth, "DPoP "); ok {
		return v, "DPoP"
	}
	if v, ok := strings.CutPrefix(auth, "dpop "); ok {
		return v, "DPoP"
	}
	return "", ""
}

// absoluteRequestURL reconstructs the canonical URL the client used to address
// this request. The DPoP htu contract is client-must-match-server: the client
// computed htu from the exact Host it sent, so we accept r.Host verbatim and
// never substitute the configured apiDomain when it differs — doing so would
// make the gateway-side htu diverge from the client's and 401 every DPoP-bound
// request behind an ingress that forwards a Host header != apiDomain. apiDomain
// is used only as a fallback to fill an empty r.Host. This mirrors the
// canonicalisation canonicalHTU performs on both sides.
func absoluteRequestURL(r *http.Request, apiDomain string) string {
	scheme := "https"
	// Strict canonicalisation — DPoP htu must equal the URL the client
	// actually sent. We accept r.Host as-is; the client computed htu from
	// the same URL. (See RFC 9449 section 4.3: "the htu claim contains the HTTP
	// URI used for the request").
	host := r.Host
	if host == "" {
		host = apiDomain
	}
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		// On plain HTTP listener (cluster-internal), accept http scheme. The
		// canonicalHTU helper normalises this consistently on both sides.
		scheme = "http"
	}
	return scheme + "://" + host + r.URL.Path
}

// resolveFQN maps an incoming REST (method, path) to the canonical gRPC FQN
// used as the permission-catalog key, via the generated REST route table. When
// no router is wired or the route does not match a known template it returns
// the empty string, which the catalog-backed PermissionLookup treats as "no
// requirement" — an unmapped route must never fabricate a step-up demand.
func (m *DPoPMiddleware) resolveFQN(method, path string) string {
	if m.routes == nil {
		return ""
	}
	if fqn, ok := m.routes.Resolve(method, path); ok {
		return fqn
	}
	return ""
}

// grpcMethodForPath converts a REST path (`/iam/v1/users/abc`) to a key that is
// deliberately NOT a catalog FQN. It is the last-resort fallback for the authz
// middleware when the generated RestRouteResolver matches no route, and its
// result is meant to be treated as an unknown method — deny-by-default.
//
// The doubled leading slash is what guarantees that: `path` already starts with
// "/", so the result can never equal a catalog key (`<pkg>.<Service>/<Method>`,
// no leading slash) nor be classified by allowlist.HasInternalSuffix, which
// tokenises on the first slash.
//
// # Why this is load-bearing rather than sloppy, and what it costs
//
// `buf.gen.yaml` sets `generate_unbound_methods=true`, so every RPC WITHOUT a
// `google.api.http` annotation still gets a grpc-gateway default route at
// `POST /<proto.package>.<Service>/<Method>` — a path shaped exactly like a
// catalog key with one slash prepended. Roughly nineteen such routes are served
// on the cluster-internal REST mux. Because the generated route table is built
// from http annotations only, none of them resolves through RestRouteResolver,
// they all land here, and the doubled slash makes the catalog lookup miss →
// every one of them answers PermissionDenied. That surface exists and does not
// work, which is a real gap — but a fail-CLOSED one.
//
// Do NOT "fix" it by returning strings.TrimPrefix(path, "/"). That single change
// would make those paths resolve to their catalog rows, and several of the rows
// are `<exempt>` — including InternalIAMService RegisterResource /
// UnregisterResource, which WRITE AUTHORIZATION TUPLES.
// phaseInternalOriginExempt admits an exempt Internal* RPC on the internal
// listener WITHOUT extracting a principal, on network position alone; its own
// doc states the rule this would break — an RPC that grants privilege must have
// NO REST route at all, not an exempt one (as MintBootstrapToken already does).
// So resolution must be restored only TOGETHER WITH removing the REST
// registration of the privilege-granting Internal RPCs, in that order. Restoring
// it first trades a dead surface for an unauthenticated tuple writer.
func grpcMethodForPath(path string) string {
	// Strip leading slash, split into segments.
	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return path
	}
	return "/" + path
}

// sanitizeErr returns a single-line human description suitable for HTTP
// header value. Strips quotation marks + control chars (RFC 6750 section 3 forbids
// quoted-strings with embedded `"`).
func sanitizeErr(err error) string {
	s := err.Error()
	s = strings.ReplaceAll(s, "\"", "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > 256 {
		s = s[:256]
	}
	return s
}

// challenge writes a 401 with a single WWW-Authenticate header + JSON body.
func (m *DPoPMiddleware) challenge(w http.ResponseWriter, _ *http.Request, status int, wwwAuth string, extra map[string]any) {
	w.Header().Set("WWW-Authenticate", wwwAuth)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{
		"code":    status,
		"message": "authentication failed",
	}
	for k, v := range extra {
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}

// injectVerifiedTokenHeaders adds X-Kacho-Principal-* headers from a verified
// JWT. The downstream restmux WithMetadata callback then forwards them as
// gRPC metadata.
func injectVerifiedTokenHeaders(r *http.Request, t *VerifiedToken) {
	if t == nil {
		return
	}
	// WHO a token names is decided in exactly one place — the same one the
	// legacy auth.HTTP Hydra path uses — so the two cannot disagree about
	// identity (CWE-287 / OWASP A07). They already had: this comment used to
	// claim that parity while the code beneath it back-filled a missing
	// identifier from the raw OIDC `sub`, which principalFromVerifiedToken
	// refuses outright.
	//
	// The claim set that exposed the difference states a principal TYPE and no
	// principal IDENTIFIER. The omission is deliberate — withholding the
	// identifier is precisely why such a token authorizes nothing — and `sub` is
	// not a stand-in for it: it is the provider's name for its own subject, not
	// this platform's name for a principal. Supplied in the identifier's place
	// it is indistinguishable downstream from a claimed one, so subject
	// extraction matches on its FIRST rule, the one reserved for a token that
	// said outright who it is, and a request that named nobody arrives as
	// somebody.
	//
	// So absence travels as absence: no principal header is written at all. That
	// is not a loss of identity for tokens that legitimately carry only `sub` —
	// the authN layer ahead of this one runs first and resolves such a `sub`
	// through a real lookup; leaving its headers standing is the whole point.
	if pType, pID, _, err := principalFromVerifiedToken(t); err == nil {
		r.Header.Set(principalmeta.HeaderPrincipalType, pType)
		r.Header.Set(principalmeta.HeaderPrincipalID, pID)
		principalmeta.SetPrincipalDisplay(r.Header, "") // not forwarded on this path
		// Legacy grpc-gateway convention fallback.
		r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalType, pType)
		r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalID, pID)
	}

	// The token's own context — ACR, jti, scope, exp — describes the CREDENTIAL,
	// never who anyone is, so it travels either way: the layer ahead may have
	// resolved a principal properly, and dropping its audit and step-up context
	// along with the fabricated name would trade one defect for another.
	//
	// Written by the shared producer in auth_stepup.go, which the ALWAYS-MOUNTED
	// authN layer calls too. This middleware was the header's only producer, and
	// it mounts behind a toggle no profile sets — so the cluster-internal floor,
	// which decides on the acr forwarded from here, read an absent value on every
	// request. One producer is right; one producer that never runs is not.
	setTokenContextHeaders(r, t)
}
