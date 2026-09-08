// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Непрерывный fuzzing — верификатор access-токена.
//
// Токен приходит от клиента в заголовке `Authorization`, поэтому его содержимое
// — вход противника целиком: от структуры до алгоритма подписи. Цель гоняет
// НАСТОЯЩИЙ верификатор (`middleware.JWTVerifier.Verify`) — тот самый, через
// который проходит каждый запрос к шлюзу.
//
// Ключ верификации выдаётся из набора в памяти (подставленный `http.Client`
// вместо похода в сеть), поэтому прогон герметичен и повторяем: разбор
// заголовка, выбор ключа по `kid`, закрепление алгоритма за ключом, проверка
// подписи и разбор claim'ов идут ровно те же, что в бою.
//
// Утверждается три вещи, и все три — про отказ, а не про отсутствие паники:
//
//   - отказ обязан быть отказом: при ошибке наружу не уезжает разобранный токен;
//   - принять можно только своим ключом и только разрешённым алгоритмом —
//     принятый токен обязан нести алгоритм из списка и ожидаемого издателя;
//   - `alg=none` и `alg=HS*` на структурно корректном токене обязаны быть
//     отвергнуты ИМЕННО как неразрешённый алгоритм (`ErrUnsupportedAlg`), до
//     обращения к ключу. Это подмена алгоритма (RFC 8725 §2.1): симметричная
//     подпись, проверяемая ПУБЛИЧНЫМ ключом, превращает открытый ключ в
//     секрет подписи, а `none` отменяет подпись вовсе.
//
// Раньше эта цель жила в дереве iam и звала `verifyJWTStub`, который на оба
// посева `alg=none` и `alg=HS256` отвечал «годен», хотя рядом стояла пометка
// «должны быть отвергнуты». Проверка была ровно обратной тому, что обещала.
package fuzz_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	mw "github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

const (
	fuzzJWTIssuer   = "https://hydra.kacho.local/"
	fuzzJWTAudience = "kacho-api-gateway"
	fuzzJWTKid      = "fuzz-key-1"
)

func FuzzJWTVerify(f *testing.F) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatalf("сгенерировать ключ подписи: %v", err)
	}
	verifier, err := mw.NewJWTVerifier(mw.JWTVerifierConfig{Issuers: []mw.IssuerKeySet{{Issuer: fuzzJWTIssuer, KeySetURL: "https://kaname.kacho.local/.well-known/jwks.json", TokenTypes: []string{mw.LegacyTokenType, mw.PlatformTokenType}, TolerateAbsentTokenType: true}},

		JWKSCacheTTL: time.Hour,
		HTTPClient:   &http.Client{Transport: staticJWKS(f, &key.PublicKey)},

		ExpectedAudience: fuzzJWTAudience,
	})
	if err != nil {
		f.Fatalf("собрать верификатор: %v", err)
	}

	// Годный токен — не для того, чтобы «проверить успех», а чтобы доказать, что
	// стенд живой. Цель, у которой ВСЁ отваливается на первом же шаге, ничем не
	// лучше заглушки: она зелена, потому что до предмета не доходит.
	good := signedJWT(f, key, jwt.MapClaims{
		"iss": fuzzJWTIssuer,
		"aud": []string{fuzzJWTAudience},
		"sub": "usr-fuzz",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Add(-time.Minute).Unix(),
	}, jwt.SigningMethodES256, fuzzJWTKid)
	if _, verr := verifier.Verify(context.Background(), good); verr != nil {
		f.Fatalf("стенд не верифицирует заведомо годный токен (%v) — цель не дойдёт "+
			"до предмета ни на одном входе", verr)
	}

	seeds := []string{
		good,
		// Отмена подписи и подмена на симметричный алгоритм — те самые два
		// посева, ради которых цель заводилась.
		unsignedJWT(f, `{"alg":"none","kid":"`+fuzzJWTKid+`"}`, `{"iss":"`+fuzzJWTIssuer+`","sub":"usr-attacker"}`),
		unsignedJWT(f, `{"alg":"HS256","kid":"`+fuzzJWTKid+`"}`, `{"iss":"`+fuzzJWTIssuer+`","sub":"usr-attacker"}`),
		// Доказательство подписи, выданной НЕ нашим ключом.
		signedJWT(f, mustKey(f), jwt.MapClaims{
			"iss": fuzzJWTIssuer,
			"aud": []string{fuzzJWTAudience},
			"exp": time.Now().Add(time.Hour).Unix(),
		}, jwt.SigningMethodES256, fuzzJWTKid),
		// Проба выдачи проверки владения ключом за access-токен.
		unsignedJWT(f, `{"alg":"ES256","typ":"dpop+jwt","kid":"`+fuzzJWTKid+`"}`, `{"jti":"j"}`),
		// Структурный мусор.
		"",
		"abc",
		"abc.def",
		"abc..def",
		"...",
		"eyJh\x00\x00.eyJ\x00.\x00",
		strings.Repeat("a", 10000) + "." + strings.Repeat("b", 10000) + "." + strings.Repeat("c", 10000),
		// Ключ, которого в наборе нет.
		unsignedJWT(f, `{"alg":"ES256","kid":"unknown"}`, `{"sub":"x"}`),
		// Инъекция в claim'ах — не должна доехать до успеха и не должна ронять разбор.
		unsignedJWT(f, `{"alg":"RS256","kid":"`+fuzzJWTKid+`"}`, `{"sub":"'; DROP TABLE users; --"}`),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, token string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ПАНИКА на токене %q (len=%d): %v", token, len(token), r)
			}
		}()

		got, err := verifier.Verify(context.Background(), token)

		if err != nil {
			if got != nil {
				t.Fatalf("верификатор вернул И ошибку (%v), И разобранный токен — "+
					"вызывающий, проверяющий только одно из двух, получит доступ по "+
					"отклонённому токену (вход %q)", err, token)
			}
		} else {
			if got == nil {
				t.Fatalf("верификатор принял токен %q, но не вернул ничего", token)
			}
			if _, ok := mw.AllowedJWTAlgs[got.Alg]; !ok {
				t.Fatalf("принят токен с алгоритмом %q вне списка разрешённых (вход %q)",
					got.Alg, token)
			}
			if got.Issuer != fuzzJWTIssuer {
				t.Fatalf("принят токен чужого издателя %q (вход %q)", got.Issuer, token)
			}
		}

		// Подмена алгоритма: на структурно корректном токене решение обязано
		// приниматься по алгоритму и до обращения к ключу.
		if alg, ok := jwtHeaderAlg(token); ok && (alg == "none" || strings.HasPrefix(alg, "HS")) {
			if err == nil {
				t.Fatalf("токен с alg=%q ПРИНЯТ — подпись либо отменена, либо проверяется "+
					"симметрично публичным ключом (вход %q)", alg, token)
			}
			if !errors.Is(err, mw.ErrUnsupportedAlg) {
				t.Fatalf("токен с alg=%q отвергнут не как неразрешённый алгоритм, а как %v — "+
					"значит решение принималось позже, уже после работы с ключом (вход %q)",
					alg, err, token)
			}
		}
	})
}

// jwtHeaderAlg повторяет структурные требования верификатора и возвращает `alg`
// ТОЛЬКО когда токен доходит до проверки алгоритма: три сегмента, все три —
// корректный base64url, заголовок — разбираемый JSON. На всём остальном
// верификатор отвечает раньше и другой ошибкой, и требовать от него
// ErrUnsupportedAlg было бы неверно.
func jwtHeaderAlg(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	for _, p := range parts[1:] {
		if _, err := base64.RawURLEncoding.DecodeString(p); err != nil {
			return "", false
		}
	}
	var hdr struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(header, &hdr); err != nil {
		return "", false
	}
	return hdr.Alg, true
}

// staticJWKS отдаёт набор ключей из памяти вместо похода в сеть: прогон
// герметичен, а путь выбора ключа по `kid` остаётся настоящим.
func staticJWKS(f *testing.F, pub *ecdsa.PublicKey) http.RoundTripper {
	f.Helper()
	body, err := json.Marshal(mw.JWKSet{Keys: []mw.JWK{{
		Kty: "EC",
		Kid: fuzzJWTKid,
		Alg: mw.AlgES256,
		Use: "sig",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(pub.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(pub.Y.Bytes()),
	}}})
	if err != nil {
		f.Fatalf("собрать набор ключей: %v", err)
	}
	return roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustKey(f *testing.F) *ecdsa.PrivateKey {
	f.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatalf("сгенерировать ключ: %v", err)
	}
	return k
}

func signedJWT(f *testing.F, key *ecdsa.PrivateKey, claims jwt.MapClaims, m jwt.SigningMethod, kid string) string {
	f.Helper()
	tok := jwt.NewWithClaims(m, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		f.Fatalf("подписать токен: %v", err)
	}
	return s
}

// unsignedJWT собирает компактную форму из готовых заголовка и полезной
// нагрузки с заведомо негодной подписью: для проверок алгоритма подпись
// значения не имеет — решение обязано приниматься до неё.
func unsignedJWT(f *testing.F, header, payload string) string {
	f.Helper()
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(header)) + "." + enc([]byte(payload)) + "." + enc([]byte("not-a-signature"))
}
