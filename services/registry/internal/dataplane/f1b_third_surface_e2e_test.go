// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_third_surface_e2e_test.go — Ф1б-13: ТРЕТЬЯ поверхность предъявления —
// плоскость данных реестра — судится тем же способом, что две поверхности края.
//
// # Почему это отдельная проба, хотя проверяющий тот же
//
// Пробы пакета `jwks` утверждают о ПРОВЕРЯЮЩЕМ: они зовут `Verify` напрямую.
// Здесь предмет другой — ПОВЕРХНОСТЬ: настоящий обработчик плоскости данных за
// настоящим http-сервером, настоящий заголовок с удостоверением, настоящий
// ответ. Ограничения живут в разборе заголовка и в транспорте, и проба над
// собранным в памяти входом о них не узнает.
//
// # Утверждение фазы, которое здесь замыкается
//
// «Ни одна из трёх поверхностей не принимает токен нашего издателя, не имея
// объявленной записи его набора». Две поверхности края закрыты своей пробой;
// эта закрывает третью тем же составом утверждений: приём нашего · отказ по
// издателю без записи · положительный контроль прежним издателем.
package dataplane

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/registry/internal/clients/jwks"
)

const (
	f1bOurIssuer    = "https://kaname.kacho.local"
	f1bLegacyIssuer = "https://hydra.api.kacho.test"
	f1bServiceAud   = "registry.kacho.local"
	f1bSubject      = "sva-ci"
)

// f1bKeySetOf поднимает НАСТОЯЩИЙ источник набора ключей одного издателя.
func f1bKeySetOf(t *testing.T, kid string) (url string, priv *ecdsa.PrivateKey) {
	t.Helper()
	var err error
	priv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "EC", "kid": kid, "alg": "ES256", "use": "sig", "crv": "P-256",
			"x": base64.RawURLEncoding.EncodeToString(priv.X.FillBytes(make([]byte, 32))),
			"y": base64.RawURLEncoding.EncodeToString(priv.Y.FillBytes(make([]byte, 32))),
		}}})
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/jwks.json", priv
}

// f1bMint чеканит токен указанного издателя.
func f1bMint(t *testing.T, priv *ecdsa.PrivateKey, kid, issuer, typ string) string {
	t.Helper()
	now := time.Now().Unix()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": issuer, "sub": f1bSubject, "aud": []any{f1bServiceAud},
		"iat": now, "nbf": now, "exp": now + 300, "jti": "tok-" + kid,
	})
	tok.Header["kid"] = kid
	// ПУСТОЙ заголовок типа и ОТСУТСТВУЮЩИЙ — разные входы, и различает их только
	// эта ветка. Проба «наш токен без типа» обязана подавать второе: первое
	// проверяющий увидит как значение, а требование сценария — про отсутствие.
	if typ != "" {
		tok.Header["typ"] = typ
	} else {
		delete(tok.Header, "typ")
	}
	raw, err := tok.SignedString(priv)
	require.NoError(t, err)
	return raw
}

// TestF1b13_RegistryDataPlaneAcceptsOurIssuerThroughARealConnection.
func TestF1b13_RegistryDataPlaneAcceptsOurIssuerThroughARealConnection(t *testing.T) {
	ourURL, ourPriv := f1bKeySetOf(t, "ours-1")
	legacyURL, legacyPriv := f1bKeySetOf(t, "legacy-1")

	verifier, err := jwks.New([]jwks.KeySetSource{
		{Issuer: f1bOurIssuer, URL: ourURL, TokenType: "at+jwt"},
		{Issuer: f1bLegacyIssuer, URL: legacyURL, TokenType: "JWT", TolerateAbsentTokenType: true},
	}, f1bServiceAud)
	require.NoError(t, err)

	az := &fakeAuthz{allow: map[string]bool{
		"v_get registry_repository:reg-A/app": true,
	}}
	be := &fakeBackend{
		exists: map[string]bool{"reg-A/app": true},
		blobs:  map[string]bool{"reg-A/app|sha256:own": true},
	}
	// НАСТОЯЩИЙ обработчик плоскости данных с НАСТОЯЩИМ проверяющим.
	h := New(verifier, az, be, presenceFor(be), &fakeForwarder{status: 200}, &fakeRepoReg{},
		nil, nil, nil, "https://api.kacho.local/iam/token", f1bServiceAud, nil)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	get := func(token string) int {
		req, rerr := http.NewRequest(http.MethodGet, srv.URL+"/v2/reg-A/app/blobs/sha256:own", nil)
		require.NoError(t, rerr)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, derr := http.DefaultClient.Do(req)
		require.NoError(t, derr)
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	// (1) Токен НАШЕГО издателя принимается.
	if got := get(f1bMint(t, ourPriv, "ours-1", f1bOurIssuer, "at+jwt")); got != http.StatusOK {
		t.Fatalf("плоскость данных отвергла токен НАШЕГО издателя: %d", got)
	}

	// (2) Токен ПРЕЖНЕГО издателя продолжает проходить — положительный контроль
	// обратимости: переход аддитивен на всех трёх поверхностях одинаково.
	if got := get(f1bMint(t, legacyPriv, "legacy-1", f1bLegacyIssuer, "JWT")); got != http.StatusOK {
		t.Fatalf("плоскость данных отвергла токен ПРЕЖНЕГО издателя: %d", got)
	}

	// (3) Издатель без объявленной записи источника — ОТКАЗ, и запись не
	// подбирается перебором: подписан НАШИМ ключом, объявляет чужого.
	stranger := f1bMint(t, ourPriv, "ours-1", "https://issuer.example.invalid", "at+jwt")
	if got := get(stranger); got == http.StatusOK {
		t.Fatalf("принят токен издателя, для которого записи источника нет")
	}

	// (4) Развязка «издатель ↔ набор ключей» на этой поверхности тоже: подписан
	// ключом прежнего издателя, объявляет НАШЕГО.
	crossed := f1bMint(t, legacyPriv, "legacy-1", f1bOurIssuer, "at+jwt")
	if got := get(crossed); got == http.StatusOK {
		t.Fatalf("ключ прежнего издателя проверил токен, объявляющий НАШЕГО")
	}

	// (5) НАШ токен без типа — отказ; на нашей полосе послабления нет.
	noTyp := f1bMint(t, ourPriv, "ours-1", f1bOurIssuer, "")
	if got := get(noTyp); got == http.StatusOK {
		t.Fatalf("принят наш токен без объявленного типа")
	}
}
