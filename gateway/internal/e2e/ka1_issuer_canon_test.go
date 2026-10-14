// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ka1_issuer_canon_test.go — приёмка KA1, Предмет 4 (kacho#2758, Р5): издатель
// сравнивается в канонической форме, и канон один — для объявления и для `iss`
// предъявленного токена.
//
// Сценарии KA1-30, KA1-31 (харнесс слоя аутентификации с записью П7) и KA1-35
// (харнесс из РАЗОБРАННОЙ конфигурации: записи приёма — `config.Load` →
// `Config.TokenAcceptance` → `middleware.IssuerKeySetsFromAcceptance`, тот же код,
// что у процесса).
package e2e_test

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/e2e/ka1stand"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// p7Issuer — издатель записи П7 в точной форме объявления.
const p7Issuer = "https://issuer.example.test/realm/"

// p7Token — токен подписанта П7 с названным `iss`; `jti` известен записи
// отзыва как «жив» (П7, П3).
func p7Token(t *testing.T, st *ka1stand.Stand, iss string) string {
	t.Helper()
	return st.Extra.Mint(t, middleware.PlatformTokenType, ka1stand.JTILive, func(c jwt.MapClaims) {
		c["iss"] = iss
		c["sub"] = ka1stand.User
		c["kaname_principal_id"] = ka1stand.User
	})
}

// KA1-30 — предъявленный `iss` в другой форме того же издателя принят.
func TestKA1_30_SameIssuerInAnotherFormIsAccepted(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{ExtraIssuer: p7Issuer})
	rows := []struct{ name, iss string }{
		{"(а) без завершающей /", "https://issuer.example.test/realm"},
		{"(б) схема и хост в верхнем регистре", "HTTPS://ISSUER.EXAMPLE.TEST/realm/"},
		{"(в) порт по умолчанию", "https://issuer.example.test:443/realm/"},
		{"(г) завершающая точка хоста", "https://issuer.example.test./realm/"},
		{"(д) закодированный незарезервированный", "https://issuer.example.test/%72ealm/"},
		{"(е) точная форма объявления", p7Issuer},
	}
	for _, r := range rows {
		tok := p7Token(t, st, r.iss)
		if got := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(tok)); got.Status != http.StatusOK {
			t.Errorf("KA1-30 %s (%s): REST ожидал 200\n  получено: %s", r.name, r.iss, got)
		}
		if got := st.GRPC(t, ka1stand.PingMethod, tok, nil); got.St.Code() != codes.OK {
			t.Errorf("KA1-30 %s (%s): нативная ожидала OK\n  получено: %s", r.name, r.iss, got)
		}
	}
}

// KA1-31 — другой издатель не принят: отказ Р2 на каждой строке. Близнец каждой
// строки — строка (е) KA1-30.
func TestKA1_31_AnotherIssuerIsRefused(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{ExtraIssuer: p7Issuer})
	rows := []struct{ name, iss string }{
		{"(а) путь в другом регистре", "https://issuer.example.test/Realm/"},
		{"(б) другой путь", "https://issuer.example.test/realm2/"},
		{"(в) другая схема", "http://issuer.example.test/realm/"},
		{"(г) порт не по умолчанию", "https://issuer.example.test:8443/realm/"},
		{"(д) другой хост", "https://other.example.test/realm/"},
		{"(е) закодированный зарезервированный", "https://issuer.example.test/realm%2F"},
		{"(ж) не URL", "issuer.example.test/realm"},
	}
	for _, r := range rows {
		ka1stand.RequireRefusal(t, "KA1-31 "+r.name+" ("+r.iss+")",
			st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(p7Token(t, st, r.iss))), false)
	}
	if got := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(p7Token(t, st, p7Issuer))); got.Status != http.StatusOK {
		t.Errorf("KA1-31 близнец (точная форма): ожидался 200\n  получено: %s", got)
	}
}

// KA1-35 — разные формы объявления доходят до полосы нашей чеканки на пути
// запроса: токен проверен полосой, которая спрашивает НАШ авторитет (П11).
func TestKA1_35_DeclarationFormsReachOurMintingLane(t *testing.T) {
	signer, signerCert := ka1stand.NewTLSSigner(t, "https://issuer.example.test/realm", "ka1-p11")
	authority, authorityCert := ka1stand.NewTLSAuthority(t)
	pool := x509.NewCertPool()
	pool.AddCert(signerCert)
	pool.AddCert(authorityCert)
	hop := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}

	env := map[string]string{
		"KACHO_APP_ENV":                                   "production",
		"KACHO_API_GATEWAY_AUTHN_MODE":                    "production",
		"KACHO_API_GATEWAY_TOKEN_ISSUERS":                 "https://issuer.example.test/realm/",
		"KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS":          "HTTPS://issuer.example.test/realm=" + signer.KeySetURL(),
		"KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER":         "https://issuer.example.test:443/realm",
		"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL": authority.URL(),
		"KACHO_API_GATEWAY_TOKEN_AUDIENCE":                "https://api.kacho.test",
		"KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET":          "1s",
		"KACHO_API_GATEWAY_BACKEND_CALL_BUDGET":           "30s",
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	cfg, err := config.Load()
	require.NoError(t, err)
	acceptance, err := cfg.TokenAcceptance()
	if err != nil {
		t.Fatalf("KA1-35: записи приёма не построены из разобранной конфигурации: %v", err)
	}
	records, _, _ := middleware.IssuerKeySetsFromAcceptance(acceptance)

	mint := func() string {
		return signer.Mint(t, middleware.PlatformTokenType, ka1stand.JTILive, func(c jwt.MapClaims) {
			c["aud"] = []any{"https://api.kacho.test"}
			c["sub"] = ka1stand.User
			c["kaname_principal_id"] = ka1stand.User
		})
	}
	from := &ka1stand.ConfigAcceptance{
		Records: records, Audience: cfg.DeclaredTokenAudience(),
		AuthorityURL: cfg.PlatformTokenRevocationURL, HTTPClient: hop,
	}

	// Столбец (1): авторитет отвечает «жив».
	st := ka1stand.New(t, ka1stand.Options{FromConfig: from})
	tok := mint()
	if got := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(tok)); got.Status != http.StatusOK {
		t.Errorf("KA1-35 (1): REST ожидал 200\n  получено: %s", got)
	}
	if got := st.GRPC(t, ka1stand.PingMethod, tok, nil); got.St.Code() != codes.OK {
		t.Errorf("KA1-35 (1): нативная ожидала OK\n  получено: %s", got)
	}

	// Столбец (2): авторитет молчит — ответ Р1 на обеих поверхностях: токен
	// проверен полосой нашей чеканки, а не полосой записи стороннего издателя.
	// Сборка — новая, из той же разобранной конфигурации: у читателя отзыва
	// нашей чеканки своё окно кэша ответов (по токену), и столбец (1) иначе
	// отвечал бы за столбец (2) из этого окна.
	authority.Q.Set(ka1stand.Silent)
	st2 := ka1stand.New(t, ka1stand.Options{FromConfig: from})
	ka1stand.RequireUnavailable(t, "KA1-35 (2) REST", st2.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(tok)))
	ka1stand.RequireNativeUnavailable(t, "KA1-35 (2) нативная", st2.GRPC(t, ka1stand.PingMethod, tok, nil))
}
