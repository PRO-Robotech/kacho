// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestLoginLaneRelay_NTF2_63_AnonMailRecordsPassTheLimiterAndOnlyThey —
// звено-ограничитель стоит перед ретрансляцией записей с признаком anonMail
// (recovery, register) и только их: пути предъявления кода, вход и прочие
// глаголы формы его не проходят (NTF2-63). Звено, ответившее отказом, держит
// запрос — до службы он не доходит.
func TestLoginLaneRelay_NTF2_63_AnonMailRecordsPassTheLimiterAndOnlyThey(t *testing.T) {
	var served atomic.Int64
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer svc.Close()
	seen := map[string]int{}
	refuse := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen[r.URL.Path]++
			w.WriteHeader(http.StatusTooManyRequests)
		})
	}
	r, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Serves: middleware.RelayTargetForm,
		Target: svc.URL, ClientIP: func(*http.Request) string { return "192.0.2.1" }, AnonMailGate: refuse,
	})
	require.NoError(t, err)
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Target != middleware.RelayTargetForm {
			continue
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, rt.Path, strings.NewReader(`{}`)))
		if rt.AnonMail() {
			require.Equal(t, http.StatusTooManyRequests, rec.Code, "%s: звено не стоит перед ретрансляцией", rt.Path)
			require.Equal(t, 1, seen[rt.Path], rt.Path)
		} else {
			require.Equal(t, http.StatusOK, rec.Code, "%s: путь без признака прошёл через звено", rt.Path)
			require.Zero(t, seen[rt.Path], rt.Path)
		}
	}
	require.Len(t, seen, 2, "звено судило не ровно два пути: %v", seen)
	snap := r.Stats()
	require.Zero(t, snap.Relayed["recovery"], "отвергнутый звеном запрос посчитан ретранслированным")
	t.Logf("звено судило %v · служба получила %d", seen, served.Load())
}

// TestLoginLaneRelay_AnonMailGateIsRequiredExactlyWhereThereIsSomethingToJudge —
// ретранслятор цели с записями anonMail без звена не строится (почта без
// лимита); звено у цели без таких записей — тоже ошибка сборки (судить нечего).
func TestLoginLaneRelay_AnonMailGateIsRequiredExactlyWhereThereIsSomethingToJudge(t *testing.T) {
	cfg := func(tg middleware.RelayTarget, gate func(http.Handler) http.Handler) handler.LoginLaneRelayConfig {
		return handler.LoginLaneRelayConfig{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Serves: tg,
			Target: "https://kaname.kacho.svc:9096", ClientIP: func(*http.Request) string { return "" }, AnonMailGate: gate}
	}
	pass := func(h http.Handler) http.Handler { return h }
	_, err := handler.NewLoginLaneRelay(cfg(middleware.RelayTargetForm, nil))
	require.ErrorContains(t, err, "anonMail")
	_, err = handler.NewLoginLaneRelay(cfg(middleware.RelayTargetIssuance, pass))
	require.ErrorContains(t, err, "judge nothing")
	_, err = handler.NewLoginLaneRelay(cfg(middleware.RelayTargetForm, pass))
	require.NoError(t, err)
	_, err = handler.NewLoginLaneRelay(cfg(middleware.RelayTargetIssuance, nil))
	require.NoError(t, err)
}
