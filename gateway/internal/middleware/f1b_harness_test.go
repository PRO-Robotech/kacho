// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_harness_test.go — оснастка проб Ф1б: НАСТОЯЩИЙ источник набора ключей за
// http-сервером и чеканка токена с произвольными издателем, адресатом, типом,
// идентификатором ключа и составом утверждений.
//
// Оснастка живёт здесь, а не переиспользуется у первой конфигурации: та лежит в
// пакетно-приватных пробах другого пакета и из этого пакета недостижима. Копия
// осознанная и названная — общей делается ПОЛИТИКА (`pkg/tokenpolicy`), а не
// функция (приёмка Ф1 §2.6).
//
// Ключи здесь порождаются на КАЖДЫЙ вызов и живут только в памяти пробы: ни
// одного ключа в дереве.
package middleware

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// f1bKeySet — источник набора проверочных ключей одного издателя.
type f1bKeySet struct {
	t      *testing.T
	srv    *httptest.Server
	keys   []map[string]any
	signer map[string]any // kid → приватная половина
	alg    map[string]string
	// Requests — сколько раз источник ответил. Нужен пробе развязки: снимок
	// одной записи не вправе наполняться обращением к другой.
	Requests int
	// Status — код, которым источник отвечает. 0 → 200.
	Status int
	// Body — тело, подменяющее набор целиком (для проб потолка и типа).
	Body []byte
	// ContentType — подмена типа содержимого.
	ContentType string
}

func newF1bKeySet(t *testing.T) *f1bKeySet {
	t.Helper()
	ks := &f1bKeySet{t: t, signer: map[string]any{}, alg: map[string]string{}}
	ks.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ks.Requests++
		ct := ks.ContentType
		if ct == "" {
			ct = "application/json"
		}
		w.Header().Set("Content-Type", ct)
		if ks.Status != 0 {
			w.WriteHeader(ks.Status)
		}
		if ks.Body != nil {
			_, _ = w.Write(ks.Body)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": ks.keys})
	}))
	t.Cleanup(ks.srv.Close)
	return ks
}

func (k *f1bKeySet) URL() string { return k.srv.URL }

// addRSA заводит ключ RS256.
func (k *f1bKeySet) addRSA(kid string) {
	k.t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		k.t.Fatalf("порождение ключа RSA: %v", err)
	}
	k.signer[kid] = priv
	k.alg[kid] = "RS256"
	k.keys = append(k.keys, map[string]any{
		"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
	})
}

// addEC заводит ключ ES256.
func (k *f1bKeySet) addEC(kid string) {
	k.t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		k.t.Fatalf("порождение ключа EC: %v", err)
	}
	k.signer[kid] = priv
	k.alg[kid] = "ES256"
	k.keys = append(k.keys, map[string]any{
		"kty": "EC", "kid": kid, "alg": "ES256", "use": "sig", "crv": "P-256",
		"x": base64.RawURLEncoding.EncodeToString(priv.X.FillBytes(make([]byte, 32))),
		"y": base64.RawURLEncoding.EncodeToString(priv.Y.FillBytes(make([]byte, 32))),
	})
}

// addEd заводит ключ EdDSA.
func (k *f1bKeySet) addEd(kid string) {
	k.t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		k.t.Fatalf("порождение ключа Ed25519: %v", err)
	}
	k.signer[kid] = priv
	k.alg[kid] = "EdDSA"
	k.keys = append(k.keys, map[string]any{
		"kty": "OKP", "kid": kid, "alg": "EdDSA", "use": "sig", "crv": "Ed25519",
		"x": base64.RawURLEncoding.EncodeToString(pub),
	})
}

// mint чеканит токен ключом kid этого набора.
//
// Тип заголовка и состав утверждений задаёт вызывающий целиком: проба обязана
// уметь выпустить в том числе токен, объявляющий ЧУЖОГО издателя — иначе
// развязку «издатель ↔ набор ключей» нечем проверить.
func (k *f1bKeySet) mint(kid, typ string, claims jwt.MapClaims) string {
	k.t.Helper()
	priv, ok := k.signer[kid]
	if !ok {
		k.t.Fatalf("в наборе нет ключа %q", kid)
	}
	var method jwt.SigningMethod
	switch k.alg[kid] {
	case "RS256":
		method = jwt.SigningMethodRS256
	case "ES256":
		method = jwt.SigningMethodES256
	case "EdDSA":
		method = jwt.SigningMethodEdDSA
	default:
		k.t.Fatalf("неизвестный алгоритм ключа %q", kid)
	}
	tok := jwt.NewWithClaims(method, claims)
	tok.Header["kid"] = kid
	if typ != "" {
		tok.Header["typ"] = typ
	} else {
		delete(tok.Header, "typ")
	}
	raw, err := tok.SignedString(priv)
	if err != nil {
		k.t.Fatalf("подпись токена: %v", err)
	}
	return raw
}

// f1bClaims — законный набор утверждений для указанных издателя и адресата.
func f1bClaims(issuer, audience, subject string, now time.Time, ttl time.Duration) jwt.MapClaims {
	return jwt.MapClaims{
		"iss": issuer,
		"sub": subject,
		"aud": []string{audience},
		"iat": now.Unix(),
		"nbf": now.Unix(),
		"exp": now.Add(ttl).Unix(),
		"jti": "tok-" + subject + "-1",
	}
}

const (
	f1bPlatformIssuer = "https://iam.kacho.test"
	f1bLegacyIssuer   = "https://hydra.api.kacho.test"
	f1bAudience       = "https://api.kacho.test"
)

// newTwoIssuerVerifierForDeclaration — проверяющий с двумя записями, у нашей
// объявлено чтение отзыва.
func newTwoIssuerVerifierForDeclaration(t *testing.T) *JWTVerifier {
	t.Helper()
	ours, legacy := newF1bKeySet(t), newF1bKeySet(t)
	ours.addEC("ours-1")
	legacy.addRSA("legacy-1")
	v, err := NewJWTVerifier(JWTVerifierConfig{
		Issuers: []IssuerKeySet{
			{Issuer: f1bPlatformIssuer, KeySetURL: ours.URL(), TokenTypes: []string{PlatformTokenType}, ReadRevocation: true},
			{Issuer: f1bLegacyIssuer, KeySetURL: legacy.URL(), TokenTypes: []string{LegacyTokenType}, TolerateAbsentTokenType: true},
		},
		ExpectedAudience: f1bAudience,
	})
	if err != nil {
		t.Fatalf("построение проверяющего: %v", err)
	}
	return v
}

// newLegacyOnlyVerifierForDeclaration — проверяющий с одной записью, отзыв не
// читает ни одна.
func newLegacyOnlyVerifierForDeclaration(t *testing.T) *JWTVerifier {
	t.Helper()
	legacy := newF1bKeySet(t)
	legacy.addRSA("legacy-1")
	v, err := NewJWTVerifier(JWTVerifierConfig{
		Issuers: []IssuerKeySet{
			{Issuer: f1bLegacyIssuer, KeySetURL: legacy.URL(), TokenTypes: []string{LegacyTokenType}, TolerateAbsentTokenType: true},
		},
		ExpectedAudience: f1bAudience,
	})
	if err != nil {
		t.Fatalf("построение проверяющего: %v", err)
	}
	return v
}
