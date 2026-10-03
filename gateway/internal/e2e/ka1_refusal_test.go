// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ka1_refusal_test.go — приёмка KA1, Предмет 2 (kacho#2958, Р2, Р3): удостоверение
// не принято → ОДИН отказ `401`, побайтово одинаковый для всех причин на своей
// поверхности; указание повысить уровень остаётся различимым.
//
// Сценарии KA1-10, 11, 12, 14 (строки слоя аутентификации) и KA1-15. Строки
// KA1-13 — уровни обработчиков: `handler` (выход) и `subscriptionstream` (поток).
//
// Проба стоит рядом с пробой пути отказа 403 (f6b_address_gate_e2e_test.go):
// неразличимость причин держится сравнением ответов, а не чтением писателей.
package e2e_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/gateway/internal/e2e/ka1stand"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

const ka1Machine = "sva-00000000000000ka1"

// machine — машинный токен нашего издателя.
func machine(c jwt.MapClaims) {
	c["kaname_principal_type"] = "service_account"
	c["kaname_principal_id"] = ka1Machine
	c["sub"] = ka1Machine
}

func expired(c jwt.MapClaims) {
	past := time.Now().Add(-2 * time.Hour).Unix()
	c["iat"], c["nbf"], c["exp"] = past-60, past-60, past
}

// withoutPrincipalClaims — токен, у которого личность берётся только из `sub`
// (запасная ветвь резолва). Строка (е) и её близнец оба без утверждений
// принципала: отличие между ними — один факт, наличие `sub`.
func withoutPrincipalClaims(c jwt.MapClaims) {
	delete(c, "kaname_principal_type")
	delete(c, "kaname_principal_id")
}

// dpopKey — ключ доказательства и его отпечаток для `cnf.jkt`.
func dpopKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	jwk := middleware.JWK{
		Kty: "EC", Crv: "P-256",
		X: base64.RawURLEncoding.EncodeToString(k.X.FillBytes(make([]byte, 32))),
		Y: base64.RawURLEncoding.EncodeToString(k.Y.FillBytes(make([]byte, 32))),
	}
	thumb, err := jwk.Thumbprint()
	require.NoError(t, err)
	return k, thumb
}

func dpopScheme(tok string) ka1stand.Presented {
	return func(r *http.Request) { r.Header.Set("Authorization", "DPoP "+tok) }
}

type ka1RefusalRow struct {
	name    string
	stand   *ka1stand.Stand
	method  string
	route   string
	present ka1stand.Presented
}

// ka1RefusalRows — закрытый перечень причин KA1-10 (а)–(л).
func ka1RefusalRows(t *testing.T) (rows []ka1RefusalRow, plain, binding, dpop *ka1stand.Stand) {
	t.Helper()
	plain = ka1stand.New(t, ka1stand.Options{})
	binding = ka1stand.New(t, ka1stand.Options{RequireBinding: true})
	dpop = ka1stand.New(t, ka1stand.Options{DPoP: true})
	_, jkt := dpopKey(t)
	get := http.MethodGet
	rows = []ka1RefusalRow{
		{"(а) удостоверения нет", plain, get, ka1stand.ListRoute, ka1stand.Nothing},
		{"(б) изменён байт подписи", plain, get, ka1stand.ListRoute, ka1stand.Bearer(ka1stand.Tamper(plain.OurToken(t, ka1stand.JTILive, nil)))},
		{"(в) издатель не объявлен", plain, get, ka1stand.ListRoute, ka1stand.Bearer(plain.Undeclared.Mint(t, middleware.PlatformTokenType, ka1stand.JTILive, nil))},
		{"(г) срок истёк", plain, get, ka1stand.ListRoute, ka1stand.Bearer(plain.OurToken(t, ka1stand.JTILive, expired))},
		{"(д) отозван", plain, get, ka1stand.ListRoute, ka1stand.Bearer(plain.OurToken(t, ka1stand.JTIRevoked, nil))},
		{"(е) без sub", plain, get, ka1stand.ListRoute, ka1stand.Bearer(plain.OurToken(t, ka1stand.JTILive, func(c jwt.MapClaims) {
			withoutPrincipalClaims(c)
			delete(c, "sub")
		}))},
		{"(ж) машина без привязки", binding, get, ka1stand.ListRoute, ka1stand.Bearer(binding.OurToken(t, ka1stand.JTILive, machine))},
		{"(з) неизвестный идентификатор", plain, get, ka1stand.ListRoute, ka1stand.Bearer(plain.BasicUnknownID)},
		{"(и) неверный секрет", plain, get, ka1stand.ListRoute, ka1stand.Bearer(plain.BasicWrongSecret)},
		{"(к) базовое на глаголе с полом 2", plain, http.MethodPost, ka1stand.FloorRoute, ka1stand.Bearer(plain.BasicGood)},
		{"(л) DPoP без доказательства", dpop, get, ka1stand.ListRoute, dpopScheme(dpop.OurToken(t, ka1stand.JTILive, func(c jwt.MapClaims) {
			c["cnf"] = map[string]any{"jkt": jkt}
		}))},
	}
	return rows, plain, binding, dpop
}

// KA1-10 — REST: отказ одинаков для всех причин.
func TestKA1_10_RESTRefusalIsTheSameForEveryCause(t *testing.T) {
	rows, _, _, _ := ka1RefusalRows(t)
	shots := make([]ka1stand.Shot, len(rows))
	for i, r := range rows {
		shots[i] = r.stand.REST(t, r.method, r.route, r.present)
		ka1stand.RequireRefusal(t, "KA1-10 "+r.name, shots[i], false)
	}
	for i := 1; i < len(shots); i++ {
		if !shots[0].Same(shots[i]) {
			t.Errorf("KA1-10: отказы различимы\n  %s: %s\n  %s: %s", rows[0].name, shots[0], rows[i].name, shots[i])
		}
	}
}

// KA1-11 — нативная поверхность: отказ одинаков для всех причин.
func TestKA1_11_NativeRefusalIsTheSameForEveryCause(t *testing.T) {
	plain := ka1stand.New(t, ka1stand.Options{})
	binding := ka1stand.New(t, ka1stand.Options{RequireBinding: true})
	dpop := ka1stand.New(t, ka1stand.Options{DPoP: true})
	_, jkt := dpopKey(t)
	type row struct {
		name   string
		stand  *ka1stand.Stand
		method string
		tok    string
		cert   *ka1stand.Cert
	}
	rows := []row{
		{"(а) удостоверения нет", plain, ka1stand.PingMethod, "", nil},
		{"(б) изменён байт подписи", plain, ka1stand.PingMethod, ka1stand.Tamper(plain.OurToken(t, ka1stand.JTILive, nil)), nil},
		{"(в) издатель не объявлен", plain, ka1stand.PingMethod, plain.Undeclared.Mint(t, middleware.PlatformTokenType, ka1stand.JTILive, nil), nil},
		{"(г) срок истёк", plain, ka1stand.PingMethod, plain.OurToken(t, ka1stand.JTILive, expired), nil},
		{"(д) отозван", plain, ka1stand.PingMethod, plain.OurToken(t, ka1stand.JTIRevoked, nil), nil},
		{"(е) без sub", plain, ka1stand.PingMethod, plain.OurToken(t, ka1stand.JTILive, func(c jwt.MapClaims) {
			withoutPrincipalClaims(c)
			delete(c, "sub")
		}), nil},
		{"(ж) машина без привязки", binding, ka1stand.PingMethod, binding.OurToken(t, ka1stand.JTILive, machine), nil},
		{"(з) неизвестный идентификатор", plain, ka1stand.PingMethod, plain.BasicUnknownID, nil},
		{"(и) неверный секрет", plain, ka1stand.PingMethod, plain.BasicWrongSecret, nil},
		{"(к) базовое на глаголе с полом 2", plain, ka1stand.FloorMethod, plain.BasicGood, nil},
		{"(м) привязка к A по соединению с B", dpop, ka1stand.PingMethod, dpop.OurToken(t, ka1stand.JTILive, func(c jwt.MapClaims) {
			c["cnf"] = map[string]any{"x5t#S256": dpop.CertA.Thumb}
		}), &dpop.CertB},
		{"(н) DPoP-привязанный на нативной", dpop, ka1stand.PingMethod, dpop.OurToken(t, ka1stand.JTILive, func(c jwt.MapClaims) {
			c["cnf"] = map[string]any{"jkt": jkt}
		}), nil},
	}
	var first []byte
	for i, r := range rows {
		got := r.stand.GRPC(t, r.method, r.tok, r.cert)
		ka1stand.RequireNativeRefusal(t, "KA1-11 "+r.name, got)
		b := ka1stand.MarshalStatus(t, got.St)
		if i == 0 {
			first = b
		} else if string(b) != string(first) {
			t.Errorf("KA1-11: статус %s после сериализации отличается от %s\n  получено: %s", r.name, rows[0].name, got)
		}
	}
}

// KA1-12 — наша сессия: отказ одинаков и заканчивает носитель.
func TestKA1_12_SessionRefusalIsTheSameAndEndsTheCarrier(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{})
	unknown := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.SessionCarrier(ka1stand.SessionUnknown))

	st.Ident.CutoffAtT0.Store(true)
	cut := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.SessionCarrier(ka1stand.SessionLive))

	st.Ident.NoAuthInstant.Store(true)
	noInstant := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.SessionCarrier(ka1stand.SessionLive))

	rows := []struct {
		name string
		shot ka1stand.Shot
	}{{"(а) сессии нет", unknown}, {"(б) отсечка T0", cut}, {"(в) без момента аутентификации", noInstant}}
	for _, r := range rows {
		ka1stand.RequireRefusal(t, "KA1-12 "+r.name, r.shot, true)
	}
	for i := 1; i < len(rows); i++ {
		if !rows[0].shot.Same(rows[i].shot) {
			t.Errorf("KA1-12: отказы полосы сессии различимы\n  %s: %s\n  %s: %s",
				rows[0].name, rows[0].shot, rows[i].name, rows[i].shot)
		}
	}
}

// KA1-14 — положительные близнецы строк слоя аутентификации KA1-10…12: каждый
// отличается от своей отрицательной строки ровно одним фактом.
func TestKA1_14_TwinsAreServed(t *testing.T) {
	plain := ka1stand.New(t, ka1stand.Options{})
	binding := ka1stand.New(t, ka1stand.Options{RequireBinding: true})
	dpop := ka1stand.New(t, ka1stand.Options{DPoP: true})
	priv, jkt := dpopKey(t)
	jktToken := dpop.OurToken(t, ka1stand.JTILive, func(c jwt.MapClaims) { c["cnf"] = map[string]any{"jkt": jkt} })
	withSub := plain.OurToken(t, ka1stand.JTILive, withoutPrincipalClaims)

	rest := []struct {
		name    string
		stand   *ka1stand.Stand
		present ka1stand.Presented
	}{
		{"(б)(в)(г)(д) подпись цела, издатель объявлен, срок не истёк, jti жив", plain, ka1stand.Bearer(plain.OurToken(t, ka1stand.JTILive, nil))},
		{"(е) sub есть", plain, ka1stand.Bearer(withSub)},
		{"(ж) машина с привязкой", binding, ka1stand.Bearer(binding.OurToken(t, ka1stand.JTILive, func(c jwt.MapClaims) {
			machine(c)
			c["cnf"] = map[string]any{"x5t#S256": binding.CertA.Thumb}
		}))},
		{"(з)(и)(к) годное базовое на глаголе без пола", plain, ka1stand.Bearer(plain.BasicGood)},
		{"(л) DPoP с годным доказательством", dpop, func(r *http.Request) {
			dpopScheme(jktToken)(r)
			htu := "http://" + r.URL.Host + r.URL.Path
			r.Header.Set("DPoP", signDPoPHeader(t, priv, r.Method, htu, "ka1-proof-1", time.Now(), ""))
		}},
		{"сессия жива, отсечки нет, адрес подтверждён", plain, ka1stand.SessionCarrier(ka1stand.SessionLive)},
	}
	for _, r := range rest {
		if got := r.stand.REST(t, http.MethodGet, ka1stand.ListRoute, r.present); got.Status != http.StatusOK {
			t.Errorf("KA1-14 REST %s: ожидался 200\n  получено: %s", r.name, got)
		}
	}

	native := []struct {
		name  string
		stand *ka1stand.Stand
		tok   string
		cert  *ka1stand.Cert
	}{
		{"(б)(в)(г)(д) годный токен", plain, plain.OurToken(t, ka1stand.JTILive, nil), nil},
		{"(е) sub есть", plain, withSub, nil},
		{"(з)(и)(к) годное базовое на глаголе без пола", plain, plain.BasicGood, nil},
		{"(м) привязка к A по соединению с A", dpop, dpop.OurToken(t, ka1stand.JTILive, func(c jwt.MapClaims) {
			c["cnf"] = map[string]any{"x5t#S256": dpop.CertA.Thumb}
		}), &dpop.CertA},
		{"(н) тот же токен без cnf.jkt", dpop, dpop.OurToken(t, ka1stand.JTILive, nil), nil},
	}
	for _, r := range native {
		if got := r.stand.GRPC(t, ka1stand.PingMethod, r.tok, r.cert); got.St.Code() != codes.OK {
			t.Errorf("KA1-14 нативная %s: ожидался OK\n  получено: %s", r.name, got)
		}
	}
}

// challengeParam — значение параметра вызова, разобранное так же, как его
// разбирает консоль (`step-up.ts`: /(?:^|[\s,])error="([^"]*)"/).
func challengeParam(challenge, name string) (string, bool) {
	m := regexp.MustCompile(`(?:^|[\s,])` + regexp.QuoteMeta(name) + `="([^"]*)"`).FindStringSubmatch(challenge)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// KA1-15 — указание повысить уровень остаётся различимым (Р3).
func TestKA1_15_StepUpChallengeStaysDistinguishable(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{})
	low := st.OurToken(t, ka1stand.JTILive, func(c jwt.MapClaims) { c["acr"] = "1" })
	got := st.REST(t, http.MethodPost, ka1stand.FloorRoute, ka1stand.Bearer(low))

	ch := got.Header.Get("WWW-Authenticate")
	e, _ := challengeParam(ch, "error")
	acr, _ := challengeParam(ch, "acr_values")
	if got.Status != http.StatusUnauthorized || len(ch) < 7 || ch[:7] != "Bearer " ||
		e != "insufficient_user_authentication" || acr != "2" {
		t.Errorf("KA1-15: ожидался 401 с вызовом Bearer error=insufficient_user_authentication, acr_values=2\n  получено: %s", got)
	}
	if ch == ka1stand.RefusalChallenge || string(got.Body) == ka1stand.RefusalBody {
		t.Errorf("KA1-15: указание неотличимо от отказа Р2\n  получено: %s", got)
	}

	// Близнец: acr=2 на том же глаголе — 200; базовое удостоверение — отказ Р2 (KA1-10 (к)).
	if twin := st.REST(t, http.MethodPost, ka1stand.FloorRoute, ka1stand.Bearer(st.OurToken(t, ka1stand.JTILive, nil))); twin.Status != http.StatusOK {
		t.Errorf("KA1-15 близнец acr=2: ожидался 200\n  получено: %s", twin)
	}
	ka1stand.RequireRefusal(t, "KA1-15 близнец базовое", st.REST(t, http.MethodPost, ka1stand.FloorRoute, ka1stand.Bearer(st.BasicGood)), false)
}
