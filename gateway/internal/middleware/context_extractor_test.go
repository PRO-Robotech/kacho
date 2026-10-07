// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestContextExtractor_BuildHTTP_AlwaysHasCurrentTime(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	e := mustExtractor(t, fixedNow(now), "1")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})
	assert.Equal(t, now.Unix(), ctx["current_time"])
}

func TestContextExtractor_BuildHTTP_ExtractsFromVerifiedToken(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	mfaAt := now.Add(-2 * time.Minute)

	e := mustExtractor(t, fixedNow(now), "1")
	tok := &middleware.VerifiedToken{
		ACR:      "3",
		AMR:      []string{"webauthn", "pwd"},
		AuthTime: now.Add(-1 * time.Minute),
		JTI:      "jti-1",
		Cnf:      middleware.TokenConfirmation{Jkt: "abc", HasJkt: true},
		ExtClaims: map[string]any{
			"kaname_mfa_at":            float64(mfaAt.Unix()),
			"kaname_device_compliance": "tpm-attested",
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.1.2.3:1234"

	subj := middleware.ResolvedSubject{FGA: "user:usr_x", Kind: middleware.SubjectKindUser}
	ctx := e.BuildHTTP(tok, r, subj)

	assert.Equal(t, "3", ctx["acr_value"])
	assert.Equal(t, []string{"webauthn", "pwd"}, ctx["amr_claims"])
	assert.Equal(t, mfaAt.Unix(), ctx["mfa_at"])
	assert.Equal(t, "tpm-attested", ctx["device_attestation"])
	// `passkey_aaguid` и `device_id` здесь утверждались как отдельные ключи
	// условия. Оба сняты вместе со своим предметом: ни одно из двух клейм не
	// чеканит ни один путь выпуска, а закрытый словарь условий модели прав их
	// не читает ни одним предикатом. Приходящее клеймо этих имён теперь едет
	// насквозь под своим именем — как всякое незнакомое (см.
	// TestContextExtractor_PreservesUnknownKanameClaims).
	assert.Equal(t, "abc", ctx["dpop_jkt"])
	assert.Equal(t, "jti-1", ctx["jti"])
	assert.Equal(t, "user", ctx["subject_kind"])
	assert.Equal(t, "10.1.2.3", ctx["client_ip"])
}

func TestContextExtractor_HonoursXForwardedFor_WhenTrusted(t *testing.T) {
	// 1 trusted proxy hop → the client IP is the entry the trusted
	// ingress recorded, i.e. the RIGHTMOST XFF entry (parts[len-1]), never the
	// client-forgeable leftmost.
	e := mustExtractor(t, time.Now, "1")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	r.RemoteAddr = "10.0.0.1:443"
	ctx := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})
	assert.Equal(t, "10.0.0.1", ctx["client_ip"])
}

func TestContextExtractor_IgnoresXForwardedFor_WhenUntrusted(t *testing.T) {
	e := mustExtractor(t, time.Now, "0")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.5")
	r.RemoteAddr = "10.0.0.1:443"
	ctx := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})
	assert.Equal(t, "10.0.0.1", ctx["client_ip"])
}

func TestContextExtractor_PrefersXFFIndexOverXRealIP(t *testing.T) {
	// With a trusted proxy present, the hop-indexed XFF entry is the most
	// robust source and wins over a bare X-Real-IP.
	e := mustExtractor(t, time.Now, "1")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "198.51.100.42")
	r.Header.Set("X-Forwarded-For", "10.0.0.99")
	ctx := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})
	assert.Equal(t, "10.0.0.99", ctx["client_ip"])
}

func TestContextExtractor_XFF_SpoofedLeftmostIgnored(t *testing.T) {
	// Attacker prepends a forged entry; the trusted ingress appends the real
	// peer as the rightmost entry. With 1 trusted hop, the forged leftmost is
	// never selected — CWE-348 / source_ip spoofing is defeated.
	e := mustExtractor(t, time.Now, "1")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "10.0.0.5, 203.0.113.7")
	r.RemoteAddr = "192.0.2.10:443"
	ctx := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})
	assert.Equal(t, "203.0.113.7", ctx["client_ip"])
}

func TestContextExtractor_XFF_MultiHop(t *testing.T) {
	// Two trusted hops: real client is parts[len-2]; a forged leftmost is still
	// ignored.
	e := mustExtractor(t, time.Now, "2")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "9.9.9.9, 203.0.113.7, 10.0.0.2")
	r.RemoteAddr = "192.0.2.10:443"
	ctx := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})
	assert.Equal(t, "203.0.113.7", ctx["client_ip"])
}

func TestContextExtractor_XFF_ZeroHopsIgnoresHeaders(t *testing.T) {
	// 0 trusted hops → forwarded headers are ignored entirely; the TCP peer is
	// authoritative. There is no separate "honour forwarded headers" switch:
	// 0 is how the knob says it.
	e := mustExtractor(t, time.Now, "0")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "10.0.0.5")
	r.Header.Set("X-Real-IP", "10.0.0.6")
	r.RemoteAddr = "192.0.2.10:443"
	ctx := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})
	assert.Equal(t, "192.0.2.10", ctx["client_ip"])
}

func TestContextExtractor_InvalidIPFallsBackToRemoteAddr(t *testing.T) {
	e := mustExtractor(t, time.Now, "1")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "not-an-ip")
	r.RemoteAddr = "10.0.0.1:443"
	ctx := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})
	assert.Equal(t, "10.0.0.1", ctx["client_ip"])
}

func TestContextExtractor_IPv6_Canonicalised(t *testing.T) {
	e := mustExtractor(t, time.Now, "0")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[2001:db8::1]:443"
	ctx := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})
	assert.Equal(t, "2001:db8::1", ctx["client_ip"])
}

func TestContextExtractor_BuildPeerAddr(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	e := mustExtractor(t, fixedNow(now), "1")

	addr := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51000}
	ctx := e.BuildPeerAddr(nil, addr, "", middleware.ResolvedSubject{})
	assert.Equal(t, now.Unix(), ctx["current_time"])
	assert.Equal(t, "203.0.113.7", ctx["client_ip"])
}

func TestContextExtractor_BuildPeerAddr_WithXFFOverride(t *testing.T) {
	e := mustExtractor(t, time.Now, "1")
	addr := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 443}
	ctx := e.BuildPeerAddr(nil, addr, "203.0.113.10, 10.0.0.1", middleware.ResolvedSubject{})
	assert.Equal(t, "10.0.0.1", ctx["client_ip"])
}

func TestContextExtractor_PreservesUnknownKanameClaims(t *testing.T) {
	e := mustExtractor(t, time.Now, "0")
	tok := &middleware.VerifiedToken{
		ExtClaims: map[string]any{
			"kaname_future_thing": "hello",
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := e.BuildHTTP(tok, r, middleware.ResolvedSubject{})
	assert.Equal(t, "hello", ctx["kaname_future_thing"])
}

func TestContextExtractor_DropsResolvedKanameFields(t *testing.T) {
	e := mustExtractor(t, time.Now, "0")
	tok := &middleware.VerifiedToken{
		ExtClaims: map[string]any{
			"kaname_user_id":        "usr_x", // already resolved by SubjectExtractor; do not duplicate
			"kaname_principal_type": "user",
			"kaname_principal_id":   "usr_x",
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := e.BuildHTTP(tok, r, middleware.ResolvedSubject{})
	_, leak := ctx["kaname_user_id"]
	assert.False(t, leak)
	_, leak2 := ctx["kaname_principal_id"]
	assert.False(t, leak2)
}

func TestContextExtractor_AMRSliceCopied_NotShared(t *testing.T) {
	e := mustExtractor(t, time.Now, "0")
	src := []string{"webauthn"}
	tok := &middleware.VerifiedToken{AMR: src}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := e.BuildHTTP(tok, r, middleware.ResolvedSubject{})
	got := ctx["amr_claims"].([]string)
	src[0] = "tampered"
	assert.Equal(t, "webauthn", got[0])
}

func TestContextExtractor_NilRequest(t *testing.T) {
	e := mustExtractor(t, time.Now, "1")
	ctx := e.BuildHTTP(nil, nil, middleware.ResolvedSubject{})
	_, hasIP := ctx["client_ip"]
	assert.False(t, hasIP)
}

// TestNewContextExtractor_RefusesHopsNotBuiltByParsingTheKnob — число прыжков —
// обязательный параметр конструктора (замысел issue-2917, З8; CX2-12): значение,
// не построенное разбором ручки, — ошибка сборки корня, а не молчаливое «1».
func TestNewContextExtractor_RefusesHopsNotBuiltByParsingTheKnob(t *testing.T) {
	var unparsed config.TrustedHops
	e, err := middleware.NewContextExtractor(time.Now, unparsed)
	if err == nil {
		t.Fatalf("нулевое TrustedHops принято: оператор адреса собран без числа прыжков (%+v)", e)
	}
	if !strings.Contains(err.Error(), config.TrustedHopsKnob) {
		t.Fatalf("отказ сборки не называет ручку %s: %v", config.TrustedHopsKnob, err)
	}
	// Близнец: то же значение, построенное разбором, принимается.
	hops, perr := config.ParseTrustedHops("1")
	if perr != nil {
		t.Fatal(perr)
	}
	if _, err := middleware.NewContextExtractor(time.Now, hops); err != nil {
		t.Fatalf("построенное разбором число прыжков отвергнуто: %v", err)
	}
}

// mustExtractor — оператор клиентского адреса с числом прыжков, построенным
// разбором значения ручки, как у корня края.
func mustExtractor(t testing.TB, now func() time.Time, hops string) *middleware.ContextExtractor {
	t.Helper()
	parsed, err := config.ParseTrustedHops(hops)
	if err != nil {
		t.Fatalf("число прыжков %q: %v", hops, err)
	}
	e, err := middleware.NewContextExtractor(now, parsed)
	if err != nil {
		t.Fatalf("оператор клиентского адреса: %v", err)
	}
	return e
}
