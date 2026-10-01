// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// testClock — управляемые часы пробы: звено, вызов и хранилище судят их.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock { return &testClock{t: time.Unix(1_900_000_000, 0).UTC()} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeLane — фиктивная полоса формы: считает пришедшие запросы и запоминает
// `X-Forwarded-For`, который ей отдал край.
type fakeLane struct {
	calls atomic.Int64
	mu    sync.Mutex
	xff   []string
}

func (f *fakeLane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	f.mu.Lock()
	f.xff = append(f.xff, r.Header.Get("X-Forwarded-For"))
	f.mu.Unlock()
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

// rig — звено над хранилищем, фиктивной полосой и управляемыми часами.
type rig struct {
	t      testing.TB
	clock  *testClock
	limits config.AnonMailLimits
	store  Store
	pow    *PoW
	gate   *Gate
	lane   *fakeLane
	h      http.Handler
}

func hops(t testing.TB, n int) config.TrustedHops {
	t.Helper()
	h, err := config.ParseTrustedHops(strconv.Itoa(n))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func newRig(t testing.TB, l config.AnonMailLimits, mk func(clock *testClock) Store, trustedHops int) *rig {
	t.Helper()
	clock := newTestClock()
	ce, err := middleware.NewContextExtractor(clock.Now, hops(t, trustedHops))
	if err != nil {
		t.Fatal(err)
	}
	pow, err := NewPoW(testPoWKey, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	store := mk(clock)
	t.Cleanup(func() { _ = store.Close() })
	g, err := NewGate(GateConfig{
		Store:    store,
		PoW:      pow,
		Limits:   l,
		ClientIP: ce.ClientIP,
		Now:      clock.Now,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	lane := &fakeLane{}
	return &rig{t: t, clock: clock, limits: l, store: store, pow: pow, gate: g, lane: lane, h: g.Wrap(lane)}
}

func memoryRig(t testing.TB, l config.AnonMailLimits) *rig {
	return newRig(t, l, func(c *testClock) Store { return NewMemoryStore(l, c.Now) }, 0)
}

// send — один запрос к пути с TCP-пиром ip; proof — значение `X-Kacho-Proof`
// (пусто — без заголовка); body — тело формы.
func (r *rig) send(path, ip, proof, body string, xff ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = netJoin(ip, "40000")
	req.Header.Set("Content-Type", "application/json")
	if proof != "" {
		req.Header.Set(ProofHeader, proof)
	}
	if len(xff) > 0 {
		req.Header.Set("X-Forwarded-For", strings.Join(xff, ", "))
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	return rec
}

func netJoin(ip, port string) string {
	if strings.Contains(ip, ":") {
		return "[" + ip + "]:" + port
	}
	return ip + ":" + port
}

// statusBody — разобранное тело отказа края.
type statusBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details []struct {
		Type     string            `json:"@type"`
		Reason   string            `json:"reason"`
		Domain   string            `json:"domain"`
		Metadata map[string]string `json:"metadata"`
	} `json:"details"`
}

func parseStatus(t testing.TB, rec *httptest.ResponseRecorder) statusBody {
	t.Helper()
	var b statusBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("тело %q не разбирается: %v", rec.Body.String(), err)
	}
	return b
}

// challengeOf — вызов из ответа 429 PROOF_OF_WORK_REQUIRED; иначе провал.
func challengeOf(t testing.TB, rec *httptest.ResponseRecorder) (token string, bits int) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("ожидался вызов 429, получено %d %s", rec.Code, rec.Body.String())
	}
	b := parseStatus(t, rec)
	if b.Code != 8 || b.Message != "proof of work required" || len(b.Details) != 1 ||
		b.Details[0].Reason != "PROOF_OF_WORK_REQUIRED" {
		t.Fatalf("тело вызова не той формы: %s", rec.Body.String())
	}
	md := b.Details[0].Metadata
	n, err := strconv.Atoi(md["difficultyBits"])
	if err != nil || md["challenge"] == "" || md["expiresAt"] == "" {
		t.Fatalf("metadata вызова неполна: %v", md)
	}
	return md["challenge"], n
}

func isRateLimited(t testing.TB, rec *httptest.ResponseRecorder) bool {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		return false
	}
	b := parseStatus(t, rec)
	return b.Code == 8 && b.Message == "too many requests" && len(b.Details) == 1 && b.Details[0].Reason == "RATE_LIMITED"
}

// pass — запрос без доказательства; получивший вызов повторяется с верным
// доказательством. Возвращает биты вызова (0 — прошёл без вызова) и итоговый
// ответ.
func (r *rig) pass(path, ip string) (int, *httptest.ResponseRecorder) {
	r.t.Helper()
	rec := r.send(path, ip, "", `{"email":"z@example.test"}`)
	if rec.Code == http.StatusOK {
		return 0, rec
	}
	if rec.Code != http.StatusTooManyRequests || isRateLimited(r.t, rec) {
		return -1, rec
	}
	tok, bits := challengeOf(r.t, rec)
	again := r.send(path, ip, tok+":"+solve(r.t, tok, bits), `{"email":"z@example.test"}`)
	return bits, again
}
