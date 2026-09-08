// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_harness_test.go — общая оснастка проб под-фазы F1 (платформа чеканит свои
// токены): источник набора ключей с полным набором ручек (тип содержимого, тело,
// срок годности, счётчик обращений) и чеканка токена с УПРАВЛЯЕМЫМ заголовком.
//
// Отдельный источник, а не расширение прежнего: прежний описывает ЗЕРКАЛО чужого
// издателя и обязан продолжать вести себя ровно так же (полоса прежнего издателя
// вне области F1). Ручки, которых требуют сценарии F1-27 (тип содержимого, потолок
// тела) и F1-13 (тип токена), прежнему не нужны, и заводить их там значило бы
// менять оснастку полосы, которую менять нельзя.
package jwks

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// testPlatformIss — издатель, под которым чеканит НАША платформа.
	testPlatformIss = "https://kaname.kacho.local"
	// testLegacyIss — прежний издатель; его запись — зеркало, живёт до F4.
	testLegacyIss = "https://hydra.api.kacho.cloud"
	// typAccessJWT — тип токена доступа нашей чеканки (RFC 9068).
	typAccessJWT = "at+jwt"
	// typJWT — тип токена прежнего издателя.
	typJWT = "JWT"
)

// keySet — источник набора проверочных ключей ОДНОГО издателя со всеми ручками,
// которых требуют сценарии F1.
type keySet struct {
	srv     *httptest.Server
	fetches atomic.Int32

	rsaKeys map[string]*rsa.PrivateKey
	ecKeys  map[string]*ecdsa.PrivateKey
	edKeys  map[string]ed25519.PrivateKey

	// extra — записи, которые потребитель разобрать НЕ может (F1-26).
	extra []map[string]any

	// contentType — тип содержимого ответа; пусто → application/json.
	// Значение "-" означает «заголовка нет вовсе»: обычное пустое значение
	// такого состояния не даёт — библиотека проставила бы тип сама, угадав его
	// по телу, и проба «типа нет» проверяла бы не то.
	contentType string
	// cacheControl — срок годности снимка; пусто → max-age=300.
	cacheControl string
	// rawBody — тело целиком в обход сборки из ключей (потолок тела, мусор).
	rawBody []byte
}

func newKeySet(t *testing.T) *keySet {
	t.Helper()
	ks := &keySet{
		rsaKeys: map[string]*rsa.PrivateKey{},
		ecKeys:  map[string]*ecdsa.PrivateKey{},
		edKeys:  map[string]ed25519.PrivateKey{},
	}
	ks.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ks.fetches.Add(1)
		ct := ks.contentType
		if ct == "" {
			ct = "application/json"
		}
		cc := ks.cacheControl
		if cc == "" {
			cc = "max-age=300"
		}
		if ct == "-" {
			// nil, а не пустая строка: пустая строка не отключает угадывание типа
			// по телу, а nil — отключает.
			w.Header()["Content-Type"] = nil
		} else {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Cache-Control", cc)
		if ks.rawBody != nil {
			_, _ = w.Write(ks.rawBody)
			return
		}
		_ = json.NewEncoder(w).Encode(ks.doc())
	}))
	t.Cleanup(ks.srv.Close)
	return ks
}

func (ks *keySet) url() string { return ks.srv.URL }

// addRSA/addEC/addEd заводят ключ соответствующего вида под идентификатором kid.
func (ks *keySet) addRSA(t *testing.T, kid string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	ks.rsaKeys[kid] = key
}

func (ks *keySet) addEC(t *testing.T, kid string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ks.ecKeys[kid] = key
}

func (ks *keySet) addEd(t *testing.T, kid string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ks.edKeys[kid] = priv
}

func (ks *keySet) doc() map[string]any {
	keys := make([]map[string]any, 0, len(ks.rsaKeys)+len(ks.ecKeys)+len(ks.edKeys)+len(ks.extra))
	for kid, key := range ks.rsaKeys {
		keys = append(keys, map[string]any{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
			"n": b64u(key.N.Bytes()), "e": b64u(big.NewInt(int64(key.E)).Bytes()),
		})
	}
	for kid, key := range ks.ecKeys {
		x, y := ecXY(key)
		keys = append(keys, map[string]any{
			"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": kid,
			"x": b64u(x), "y": b64u(y),
		})
	}
	for kid, key := range ks.edKeys {
		pub, _ := key.Public().(ed25519.PublicKey)
		keys = append(keys, map[string]any{
			"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": kid,
			"x": b64u(pub),
		})
	}
	keys = append(keys, ks.extra...)
	return map[string]any{"keys": keys}
}

// signingInput собирает первые два сегмента компактного JWS с УПРАВЛЯЕМЫМ
// заголовком: сценариям F1 нужны и отсутствующий тип, и чужой алгоритм.
func signingInput(alg, kid, typ string, claims map[string]any) string {
	hdr := map[string]any{"alg": alg}
	if kid != "" {
		hdr["kid"] = kid
	}
	if typ != "" {
		hdr["typ"] = typ
	}
	hb, _ := json.Marshal(hdr)
	cb, _ := json.Marshal(claims)
	return b64u(hb) + "." + b64u(cb)
}

func (ks *keySet) mintRS(t *testing.T, kid, typ string, claims map[string]any) string {
	t.Helper()
	key, ok := ks.rsaKeys[kid]
	require.Truef(t, ok, "в наборе нет ключа RSA %q", kid)
	si := signingInput("RS256", kid, typ, claims)
	sum := sha256.Sum256([]byte(si))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	require.NoError(t, err)
	return si + "." + b64u(sig)
}

func (ks *keySet) mintES(t *testing.T, kid, typ string, claims map[string]any) string {
	t.Helper()
	key, ok := ks.ecKeys[kid]
	require.Truef(t, ok, "в наборе нет ключа EC %q", kid)
	si := signingInput("ES256", kid, typ, claims)
	sum := sha256.Sum256([]byte(si))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	require.NoError(t, err)
	return si + "." + b64u(append(padTo(r.Bytes(), 32), padTo(s.Bytes(), 32)...))
}

func (ks *keySet) mintEd(t *testing.T, kid, typ string, claims map[string]any) string {
	t.Helper()
	key, ok := ks.edKeys[kid]
	require.Truef(t, ok, "в наборе нет ключа Ed25519 %q", kid)
	si := signingInput("EdDSA", kid, typ, claims)
	return si + "." + b64u(ed25519.Sign(key, []byte(si)))
}

// unsigned собирает токен с подписью-заглушкой: он обязан быть отвергнут ДО того,
// как дело дойдёт до разрешения ключа, поэтому подпись нерелевантна.
func unsigned(alg, kid, typ string, claims map[string]any) string {
	return signingInput(alg, kid, typ, claims) + "." + b64u([]byte("stub"))
}

// platformClaims — тело токена НАШЕЙ чеканки: адресат поверхности, наш издатель,
// объявленные iat/nbf/exp и идентификатор выпуска.
func platformClaims(sub string, now time.Time, ttl time.Duration) map[string]any {
	return map[string]any{
		"sub": sub,
		"aud": testAud,
		"iss": testPlatformIss,
		"iat": now.Unix(),
		"nbf": now.Unix(),
		"exp": now.Add(ttl).Unix(),
		"jti": fmt.Sprintf("jti-%d", now.UnixNano()),
	}
}

// issuerPair — объявленная запись «издатель → адрес набора» для проб.
type issuerPair struct {
	issuer string
	url    string
	typ    string
}

// atClock подменяет источник времени проверяющего. Часы — ВХОД, а не системное
// время: без этого пробы допуска на расхождение часов недетерминированы.
func atClock(v *Verifier, at *time.Time) {
	v.now = func() time.Time { return *at }
}

func ourPair(ks *keySet) issuerPair {
	return issuerPair{issuer: testPlatformIss, url: ks.url(), typ: typAccessJWT}
}

func legacyPair(ks *keySet) issuerPair {
	return issuerPair{issuer: testLegacyIss, url: ks.url(), typ: typJWT}
}
