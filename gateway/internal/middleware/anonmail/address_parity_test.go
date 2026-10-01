// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestGate_CX2_12_TheAddressTheEdgeCountsIsTheAddressTheServiceGets — проба
// «адрес, которым считает край, равен адресу, пришедшему в службу» (CX2-12):
// настоящий ретранслятор полосы формы со звеном перед ним и фиктивная полоса
// формы, печатающая полученный X-Forwarded-For. Цепочка пересылки задана
// пробой целиком; при одном доверенном прыжке оба читателя берут адрес на
// доверенной глубине — одним оператором ClientIP.
func TestGate_CX2_12_TheAddressTheEdgeCountsIsTheAddressTheServiceGets(t *testing.T) {
	var (
		mu  sync.Mutex
		got []string
	)
	lane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Header.Get("X-Forwarded-For"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer lane.Close()
	l := testLimits()
	var mem *MemoryStore
	r := newRig(t, l, func(c *testClock) Store { mem = mustMemoryStore(t, l, c.Now); return mem }, 1)
	ce, err := middleware.NewContextExtractor(r.clock.Now, hops(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Serves: middleware.RelayTargetForm,
		Target: lane.URL, ClientIP: ce.ClientIP, AnonMailGate: r.gate.Wrap,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, pathRecovery, strings.NewReader(`{"email":"z@example.test"}`))
	req.RemoteAddr = "192.0.2.254:40000"
	req.Header.Set("X-Forwarded-For", "10.66.0.1, 203.0.113.9")
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ретрансляция: %d %s", rec.Code, rec.Body.String())
	}
	k, _ := KeysFor("203.0.113.9")
	mem.momentsMu.Lock()
	counted := len(mem.moments[k.Source])
	keys := len(mem.moments)
	mem.momentsMu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	t.Logf("край считал ключом %s (моментов %d, ключей %d) · служба получила X-Forwarded-For %v", k.Source, counted, keys, got)
	if len(got) != 1 || got[0] != "203.0.113.9" {
		t.Fatalf("служба получила %v, ожидался один адрес 203.0.113.9", got)
	}
	if counted != 1 {
		t.Fatalf("край не посчитал запрос ключом адреса, отданного службе")
	}
}
