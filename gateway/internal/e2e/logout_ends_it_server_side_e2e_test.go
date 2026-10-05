// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// logout_ends_it_server_side_e2e_test.go — ВЫХОД КРАЯ ГАСИТ УДОСТОВЕРЕНИЕ НА
// СЕРВЕРЕ, и прежнее предъявление после него отвергается на пути запроса
// (kacho#2959, kacho#2996; приёмка Ф3, Р4).
//
// Цепочка — та, что у стенда: всегда смонтированный слой аутентификации с
// проверкой отзыва по нашей записи, под ним — обработчик выхода и маршрут
// платформы. Запись отзыва одна на обе стороны: обработчик выхода пишет в неё
// через порт `SessionRevocationsClient`, слой аутентификации читает её на
// предъявлении. Так проба судит наблюдаемое — пустят ли прежний токен, — а не
// «был ли вызван Revoke».
//
// Близнец отличается одним фактом: запись отзыва не ответила. Тогда выход
// отвечает `503` «не выполнен», и прежний токен по-прежнему годен — сказано
// ровно то, что произошло.
package e2e_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

// sharedRecord — запись отзыва, общая для писателя (выход) и читателя (слой
// аутентификации, полоса записи через интроспекцию).
type sharedRecord struct {
	mu      sync.Mutex
	revoked map[string]bool
	down    bool
}

func (s *sharedRecord) Revoke(_ context.Context, in *iamv1.RevokeRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.down {
		return errors.New("rpc error: code = Unavailable desc = record did not answer")
	}
	s.revoked[in.GetTokenJti()] = true
	return nil
}

func (s *sharedRecord) introspection(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	jti := ""
	// Интроспекция получает токен; jti читается из его полезной нагрузки без
	// проверки подписи — подпись уже проверил слой аутентификации.
	if parts := strings.Split(r.Form.Get("token"), "."); len(parts) == 3 {
		var claims struct {
			JTI string `json:"jti"`
		}
		if raw, err := decodeSegment(parts[1]); err == nil {
			_ = json.Unmarshal(raw, &claims)
			jti = claims.JTI
		}
	}
	s.mu.Lock()
	active := jti != "" && !s.revoked[jti]
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"active": active})
}

type jwtCaller struct{ v *middleware.JWTVerifier }

func (c jwtCaller) Verify(ctx context.Context, tok string) (*handler.VerifiedCaller, error) {
	vt, err := c.v.Verify(ctx, tok)
	if err != nil {
		return nil, err
	}
	return &handler.VerifiedCaller{Subject: vt.Subject, JTI: vt.JTI}, nil
}

func newLogoutChain(t *testing.T, iss *issuerFixture, rec *sharedRecord) http.Handler {
	t.Helper()
	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: []middleware.IssuerKeySet{{Issuer: testIssuer, KeySetURL: iss.jwksURL,
			TokenTypes: []string{middleware.LegacyTokenType, middleware.PlatformTokenType}, TolerateAbsentTokenType: true}},
		ExpectedAudience: testAudience,
	})
	require.NoError(t, err)
	srv := privateloopback.NewServer(t, http.HandlerFunc(rec.introspection))
	t.Cleanup(srv.Close)
	introspection, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		IntrospectionURL: srv.URL, TTL: time.Nanosecond, Timeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth := middleware.NewAuthInterceptor(middleware.AuthModeProduction, "", nil, logger).
		WithVerifier(verifier).
		WithRevocationCheck(introspection, 0)
	logout, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{
		Logger: logger, Verifier: jwtCaller{v: verifier}, Revocations: rec, CallBudget: time.Second,
	})
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle("/oauth/logout", logout)
	mux.HandleFunc("/iam/v1/me", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return auth.HTTP(mux)
}

func presentOn(chain http.Handler, method, path, bearer string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "https://"+apiDomain+path, body)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)
	return rec
}

func TestE2E_Logout_EndsTheCredentialServerSideOnEveryAcceptedPresentation(t *testing.T) {
	iss := newIssuerFixture(t)
	defer iss.close()

	forms := map[string]func(tok string) (string, io.Reader){
		"Authorization: Bearer": func(tok string) (string, io.Reader) { return tok, nil },
		"форма token": func(tok string) (string, io.Reader) {
			return "", strings.NewReader(url.Values{"token": {tok}}.Encode())
		},
	}
	for name, form := range forms {
		t.Run(name, func(t *testing.T) {
			rec := &sharedRecord{revoked: map[string]bool{}}
			chain := newLogoutChain(t, iss, rec)
			tok := plainBearer(t, iss, "jti-logout-"+strings.ReplaceAll(name, " ", "-"))

			require.Equal(t, http.StatusOK, presentOn(chain, http.MethodGet, "/iam/v1/me", tok, nil).Code,
				"до выхода токен годен — иначе отказ после выхода ничего не доказывал бы")

			bearer, body := form(tok)
			out := presentOn(chain, http.MethodPost, "/oauth/logout", bearer, body)
			require.Equal(t, http.StatusOK, out.Code, out.Body.String())
			require.Equal(t, `{}`, out.Body.String())

			after := presentOn(chain, http.MethodGet, "/iam/v1/me", tok, nil)
			require.Equal(t, http.StatusUnauthorized, after.Code,
				"после выхода прежнее предъявление обязано получать отказ: выход погасил его на сервере")

			// Близнец: запись не ответила — выход «не выполнен», и это правда.
			twin := &sharedRecord{revoked: map[string]bool{}, down: true}
			chain = newLogoutChain(t, iss, twin)
			tok = plainBearer(t, iss, "jti-logout-twin-"+strings.ReplaceAll(name, " ", "-"))
			bearer, body = form(tok)
			out = presentOn(chain, http.MethodPost, "/oauth/logout", bearer, body)
			require.Equal(t, http.StatusServiceUnavailable, out.Code, out.Body.String())
			require.Equal(t, `{"code":14,"message":"logout not performed; try again later","details":[]}`, out.Body.String())
			require.Equal(t, http.StatusOK, presentOn(chain, http.MethodGet, "/iam/v1/me", tok, nil).Code,
				"выход не выполнен — токен годен, и ответ выхода это и сказал")
		})
	}
}

func decodeSegment(seg string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(seg) }
