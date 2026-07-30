// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// jwt_verifier.go — JWKS-cached JWT verifier for Hydra-issued access tokens.
//
// Pipeline (RFC 8725 hardening applied):
//  1. Parse JWT header → require `kid` + alg ∈ {RS256, ES256, EdDSA}; reject
//     `alg=none` / `HS*` BEFORE key resolution (algorithm-confusion mitigation).
//  2. JWKS fetch (cached); resolve `kid` → JWK; enforce per-kid alg pinning
//     (JWT alg MUST equal JWK alg or kty-derived alg).
//  3. Convert JWK → crypto.PublicKey; verify signature via golang-jwt/jwt/v5.
//  4. Validate `iss` (exact match), `aud` (contains expected), `exp`/`nbf`/`iat`
//     with configurable clock-skew (default ±30s).
//  5. Extract custom claims: `acr`, `amr`, `auth_time`, `cnf` (jkt | x5t#S256),
//     `scope`, `ext_claims` (kacho_*).
//
// JWKS cache: in-memory, TTL configurable. Refresh is LAZY, on the request path
// (no background ticker / prewarm exists — do not read the word "background"
// into it), and has two distinct triggers: the snapshot went stale by time, and
// the signer named a `kid` the snapshot does not carry. The second one is what
// absorbs a mid-grace-window key rotation, and it deliberately ignores the TTL:
// a snapshot can be perfectly fresh and already incomplete. Its cost is bounded
// by forcedRefreshMinInterval so an unknown `kid` cannot become a load
// amplifier.
//
// Thread safety: methods are safe for concurrent use; cache uses sync.RWMutex.
package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// VerifiedToken — output of JWTVerifier.Verify. Carries all claims required by
// downstream middleware (DPoP binding check, step-up gate, principal
// injection).
type VerifiedToken struct {
	Raw       string // original compact JWT (for downstream introspection / audit)
	Kid       string
	Alg       string
	Subject   string
	Issuer    string
	Audience  []string
	IssuedAt  time.Time
	ExpiresAt time.Time
	NotBefore time.Time
	JTI       string

	// Authentication context — drives step-up gating.
	ACR      string // "0" | "1" | "2" | "3"
	AMR      []string
	AuthTime time.Time

	// Sender-constrained binding — exactly one of Jkt / X5tS256 may be set.
	// Empty → bearer token (legacy).
	Cnf TokenConfirmation

	Scope string

	// ExtClaims — Kachō custom claims emitted by Hydra token_hook (kacho_*).
	ExtClaims map[string]any

	// Raw claims for callers that need fields we did not explicitly extract.
	Claims jwt.MapClaims
}

// TokenConfirmation — RFC 7800 §3 confirmation method. Either DPoP-bound
// (jkt) or mTLS-bound (x5t#S256). Mutually exclusive in practice.
type TokenConfirmation struct {
	Jkt      string // RFC 9449 §6.1 — DPoP JWK SHA-256 thumbprint (base64url)
	X5tS256  string // RFC 8705 §3 — client certificate SHA-256 thumbprint
	HasJkt   bool
	HasX5tS  bool
	IsBearer bool // no cnf claim present → plain bearer token
}

// JWTVerifier — RFC 8725-hardened access-token validator.
type JWTVerifier struct {
	jwks             *JWKSCache
	expectedIssuer   string
	expectedAudience string
	clockSkew        time.Duration

	// allowMissingAudience — for tests / dev mode where Hydra may not yet
	// inject the gateway audience.
	allowMissingAudience bool
}

// JWTVerifierConfig — construction parameters.
type JWTVerifierConfig struct {
	JWKSURL              string
	JWKSCacheTTL         time.Duration
	JWKSFetchTimeout     time.Duration
	HTTPClient           *http.Client // optional; nil → default
	ExpectedIssuer       string
	ExpectedAudience     string
	ClockSkew            time.Duration
	AllowMissingAudience bool
}

// NewJWTVerifier constructs a verifier wired to the given JWKS endpoint.
func NewJWTVerifier(cfg JWTVerifierConfig) (*JWTVerifier, error) {
	if cfg.JWKSURL == "" {
		return nil, errors.New("jwt verifier: JWKSURL is required")
	}
	if cfg.ExpectedIssuer == "" {
		return nil, errors.New("jwt verifier: ExpectedIssuer is required")
	}
	if cfg.JWKSCacheTTL <= 0 {
		cfg.JWKSCacheTTL = 5 * time.Minute
	}
	if cfg.JWKSFetchTimeout <= 0 {
		cfg.JWKSFetchTimeout = 5 * time.Second
	}
	if cfg.ClockSkew <= 0 {
		cfg.ClockSkew = 30 * time.Second
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.JWKSFetchTimeout}
	}
	return &JWTVerifier{
		jwks: NewJWKSCache(cfg.JWKSURL, cfg.JWKSCacheTTL, httpClient),

		expectedIssuer:       cfg.ExpectedIssuer,
		expectedAudience:     cfg.ExpectedAudience,
		clockSkew:            cfg.ClockSkew,
		allowMissingAudience: cfg.AllowMissingAudience,
	}, nil
}

// JWKSCache — thread-safe TTL cache for a single JWKS endpoint. Refreshes on
// miss or stale; force-refresh on unknown `kid` (TTL not consulted) to absorb
// mid-grace-window rotations, rate-limited by forcedRefreshMinInterval.
type JWKSCache struct {
	url        string
	ttl        time.Duration
	httpClient *http.Client

	// fetchMu single-flights the HTTP fetch WITHOUT holding mu across the
	// blocking round-trip, so a slow JWKS endpoint never stalls concurrent
	// token verifications (they keep taking mu.RLock while a fetch is in flight).
	fetchMu sync.Mutex

	mu        sync.RWMutex
	set       *JWKSet
	fetchedAt time.Time
	// forcedAt — время последней ПОПЫТКИ вынужденного перезапроса (повод —
	// неизвестный kid). Отмечается до сетевого вызова, а не после успеха: иначе
	// неотвечающий источник ключей опрашивался бы на каждый запрос.
	forcedAt time.Time
}

// forcedRefreshMinInterval — минимальный интервал между ВЫНУЖДЕННЫМИ
// перезапросами набора ключей (повод — неизвестный kid, а не истёкший TTL).
//
// Он существует для того, чтобы неизвестный kid не стал усилителем нагрузки:
// поток подделок с произвольными kid иначе означал бы обращение к источнику
// ключей на каждый запрос. Интервал НАМЕРЕННО на порядки короче TTL кэша —
// смысл вынужденного перезапроса именно в том, чтобы не ждать TTL, — и это
// соотношение закреплено пробой предпосылки.
const forcedRefreshMinInterval = 3 * time.Second

// NewJWKSCache constructs a cache; first fetch is lazy on Get.
func NewJWKSCache(url string, ttl time.Duration, httpClient *http.Client) *JWKSCache {
	return &JWKSCache{url: url, ttl: ttl, httpClient: httpClient}
}

// FetchedAt returns the timestamp of the most recent successful fetch.
// Exported for tests / observability.
func (c *JWKSCache) FetchedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fetchedAt
}

// Resolve looks up a JWK by kid, refreshing the cache when stale or when the
// kid is unknown. Returns ErrKeyNotFound after a force-refresh if kid still
// unknown.
//
// Два повода перезапроса РАЗНЫЕ и обрабатываются разными ветками. «Снимок
// устарел по времени» — обычный перезапрос. «Подписант назвал ключ, которого в
// снимке нет» — ВЫНУЖДЕННЫЙ: снимок при этом может быть совершенно свежим и всё
// равно неполным, что и происходит при ротации подписного ключа у провайдера.
// Если бы вынужденный перезапрос проходил через предикат свежести, ротация
// оборачивалась бы отказом всех НОВОВЫДАННЫХ токенов до истечения TTL.
func (c *JWKSCache) Resolve(ctx context.Context, kid string) (*JWK, error) {
	c.mu.RLock()
	stale := c.set == nil || time.Since(c.fetchedAt) > c.ttl
	forced := false
	if !stale && c.set != nil {
		k, err := c.set.FindByKid(kid)
		c.mu.RUnlock()
		if err == nil {
			return k, nil
		}
		// Свежий снимок, но kid в нём отсутствует → вынужденный перезапрос.
		forced = true
	} else {
		c.mu.RUnlock()
	}

	if err := c.refresh(ctx, forced); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.set == nil {
		return nil, ErrJWKSFetchFailed
	}
	return c.set.FindByKid(kid)
}

// refresh перезапрашивает набор ключей. forced=true означает «повод — не время,
// а неизвестный kid»: предикат свежести такому вызывающему не применяется, но
// вместо него действует собственный минимальный интервал
// (forcedRefreshMinInterval), чтобы поток неизвестных kid не превратился в
// усилитель нагрузки на источник ключей.
func (c *JWKSCache) refresh(ctx context.Context, forced bool) error {
	// Serialize fetches on fetchMu (single-flight) but do NOT hold the RWMutex
	// across the network I/O below — mu is taken only for the short double-check
	// read and the final publish. This bounds the critical section on mu to a
	// map assignment so concurrent verifications keep resolving during a fetch.
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()
	// Double-check: another goroutine may have refreshed while we waited on fetchMu.
	c.mu.RLock()
	now := time.Now()
	fresh := c.set != nil && now.Sub(c.fetchedAt) < c.ttl
	forcedRecently := !c.forcedAt.IsZero() && now.Sub(c.forcedAt) < forcedRefreshMinInterval
	c.mu.RUnlock()
	if forced {
		// Уже сходили за набором только что (в том числе — соседняя горутина,
		// пока мы ждали на fetchMu): её результат уже опубликован, повторное
		// обращение ничего не добавит. Вызывающий перечитает снимок сам.
		if forcedRecently {
			return nil
		}
		c.mu.Lock()
		c.forcedAt = now
		c.mu.Unlock()
	} else if fresh {
		return nil
	}
	// c.url is the operator-configured JWKS endpoint (KACHO_HYDRA_JWKS_URL /
	// derived from KACHO_API_DOMAIN), never request-derived — not an SSRF sink.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil) // #nosec G704 -- JWKS URL is operator config, not user input
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrJWKSFetchFailed, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req) // #nosec G704 -- JWKS URL is operator config, not user input
	if err != nil {
		return fmt.Errorf("%w: %v", ErrJWKSUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: status=%d body=%q", ErrJWKSFetchFailed, resp.StatusCode, string(body))
	}
	// Cap body to prevent DoS via massive JWKS document.
	limited := io.LimitReader(resp.Body, 1<<20) // 1 MiB
	var set JWKSet
	if err := json.NewDecoder(limited).Decode(&set); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrJWKSFetchFailed, err)
	}
	if len(set.Keys) == 0 {
		return fmt.Errorf("%w: empty key set", ErrJWKSFetchFailed)
	}
	// Publish under the write lock — bounded to field assignment, no I/O.
	c.mu.Lock()
	c.set = &set
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return nil
}

// Verify parses and validates the access token. Returns sentinel-typed errors
// callers can map to `invalid_token` / `insufficient_user_authentication`
// WWW-Authenticate challenges.
func (v *JWTVerifier) Verify(ctx context.Context, token string) (*VerifiedToken, error) {
	if token == "" {
		return nil, errors.New("empty token")
	}

	// 1. Parse header without verifying signature — we need kid + alg to
	//    select the key.
	header, err := splitJWT(token)
	if err != nil {
		return nil, fmt.Errorf("invalid jwt structure: %w", err)
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if jerr := json.Unmarshal(header, &hdr); jerr != nil {
		return nil, fmt.Errorf("invalid jwt header: %w", jerr)
	}
	if _, ok := AllowedJWTAlgs[hdr.Alg]; !ok {
		return nil, fmt.Errorf("%w: alg=%q", ErrUnsupportedAlg, hdr.Alg)
	}
	// Hydra typically emits `typ=at+jwt` or no typ. Reject `typ=JWT` is NOT
	// done — common legacy. Reject typ=dpop+jwt (DPoP proof masquerading).
	if strings.EqualFold(hdr.Typ, "dpop+jwt") {
		return nil, fmt.Errorf("%w: typ=dpop+jwt is not a valid access token", ErrUnsupportedAlg)
	}

	// 2. Resolve key from JWKS.
	jwk, err := v.jwks.Resolve(ctx, hdr.Kid)
	if err != nil {
		return nil, fmt.Errorf("jwks resolve kid=%q: %w", hdr.Kid, err)
	}
	if expected := jwk.AlgForJWT(); expected != "" && expected != hdr.Alg {
		return nil, fmt.Errorf("%w: jwk_alg=%q jwt_alg=%q", ErrAlgMismatch, expected, hdr.Alg)
	}
	pubKey, err := jwk.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("jwk to public key: %w", err)
	}

	// 3. Parse + verify via golang-jwt; supply our pinned key.
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{hdr.Alg}),
		jwt.WithLeeway(v.clockSkew),
		jwt.WithIssuedAt(),
	)
	claims := jwt.MapClaims{}
	parsed, err := parser.ParseWithClaims(token, claims, func(_ *jwt.Token) (any, error) {
		return pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("jwt verify: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("jwt invalid")
	}

	// 4. Validate iss / aud / exp / nbf / iat (manual — golang-jwt already
	//    checks exp/nbf/iat with leeway, but we want iss/aud strict).
	iss, _ := claims["iss"].(string)
	if iss != v.expectedIssuer {
		return nil, fmt.Errorf("iss mismatch: got %q expected %q", iss, v.expectedIssuer)
	}
	auds, err := extractAudience(claims)
	if err != nil {
		return nil, err
	}
	if v.expectedAudience != "" {
		if !audienceContains(auds, v.expectedAudience) {
			if !v.allowMissingAudience {
				return nil, fmt.Errorf("aud does not contain %q (got %v)", v.expectedAudience, auds)
			}
		}
	}

	out := &VerifiedToken{
		Raw:      token,
		Kid:      hdr.Kid,
		Alg:      hdr.Alg,
		Issuer:   iss,
		Audience: auds,
		Claims:   claims,
	}

	if sub, ok := claims["sub"].(string); ok {
		out.Subject = sub
	}
	if jti, ok := claims["jti"].(string); ok {
		out.JTI = jti
	}
	if iat, ok := numericTime(claims["iat"]); ok {
		out.IssuedAt = iat
	}
	if exp, ok := numericTime(claims["exp"]); ok {
		out.ExpiresAt = exp
	}
	if nbf, ok := numericTime(claims["nbf"]); ok {
		out.NotBefore = nbf
	}
	if at, ok := numericTime(claims["auth_time"]); ok {
		out.AuthTime = at
	}
	if acr, ok := claims["acr"].(string); ok {
		out.ACR = acr
	}
	out.AMR = stringSlice(claims["amr"])
	if scope, ok := claims["scope"].(string); ok {
		out.Scope = scope
	}
	if ext, ok := claims["ext_claims"].(map[string]any); ok {
		out.ExtClaims = ext
	}

	// Cnf extraction.
	if cnfRaw, ok := claims["cnf"].(map[string]any); ok {
		if jkt, ok := cnfRaw["jkt"].(string); ok && jkt != "" {
			out.Cnf.Jkt = jkt
			out.Cnf.HasJkt = true
		}
		if x5t, ok := cnfRaw["x5t#S256"].(string); ok && x5t != "" {
			out.Cnf.X5tS256 = x5t
			out.Cnf.HasX5tS = true
		}
	}
	if !out.Cnf.HasJkt && !out.Cnf.HasX5tS {
		out.Cnf.IsBearer = true
	}

	return out, nil
}

// splitJWT decodes the header segment of a compact JWS and validates that the
// payload + signature segments are well-formed base64url. Only the header bytes
// are returned: callers need it to select the key (kid/alg), while the payload
// claims and signature are decoded+verified by the jwt library (JWT path) or the
// DPoP proof verifier. Returning parallel payload/sig copies here would only
// invite a decode/enforcement mismatch, so they are validated then dropped.
func splitJWT(token string) (header []byte, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("jwt must have 3 parts, got %d", len(parts))
	}
	if header, err = base64.RawURLEncoding.DecodeString(parts[0]); err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	if _, err = base64.RawURLEncoding.DecodeString(parts[1]); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	if _, err = base64.RawURLEncoding.DecodeString(parts[2]); err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	return header, nil
}

func extractAudience(claims jwt.MapClaims) ([]string, error) {
	v, ok := claims["aud"]
	if !ok {
		return nil, nil
	}
	switch t := v.(type) {
	case string:
		return []string{t}, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("aud entry is not string: %T", e)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("aud has unexpected type %T", v)
	}
}

func audienceContains(auds []string, want string) bool {
	for _, a := range auds {
		if a == want {
			return true
		}
	}
	return false
}

func numericTime(v any) (time.Time, bool) {
	switch n := v.(type) {
	case float64:
		return time.Unix(int64(n), 0), true
	case int64:
		return time.Unix(n, 0), true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return time.Unix(i, 0), true
		}
	}
	return time.Time{}, false
}

func stringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), t...)
	case string:
		return []string{t}
	}
	return nil
}
