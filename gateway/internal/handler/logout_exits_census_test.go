// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// logout_exits_census_test.go — ВЫХОД ГАСИТ ОБА ИМЕНИ НА КАЖДОМ ВЫХОДЕ, КОТОРЫЙ
// ПРЕДЛАГАЕТ ПРОДУКТ, а не на одном обработчике.
//
// # Чем эта проба отличается от предыдущей, и почему предыдущей не хватало
//
// Соседняя проба (`logout_carrier_states_test.go`) измеряет ОДИН обработчик —
// `/oauth/logout` — и показывает «2 имени из 2». Утверждение верное и узкое:
// оно о том обработчике, а не о выходе. Единица счёта там — обработчик, а
// спрашивать надо по ДВУМ осям: КТО ГАСИТ и КТО ЭТОТ ВЫХОД ВЫЗЫВАЕТ. Выход,
// который никто не вызывает, гасит что угодно и ничего не решает; выход,
// который вызывают, гасит ровно то, что гасит.
//
// # Границы измерения названы, а не подразумеваются
//
// Отсюда видны выходы, достижимые ЧЕРЕЗ КРАЙ. Консоли живут вне этого дерева,
// и их собственный выход — переход на самообслуживание чужой стороны — отсюда
// не измеряется. Это сказано вслух: «ноль находок» здесь означает «ноль на
// осмотренном», и осмотренное названо переписью.
package handler_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// endedNames — имена, погашенные ответом.
func endedNames(res *http.Response) map[string]bool {
	out := map[string]bool{}
	for _, c := range res.Cookies() {
		if c.MaxAge < 0 {
			out[c.Name] = true
		}
	}
	return out
}

// TestLogoutExits_EveryExitReachableThroughTheEdgeEndsEveryName — перепись по
// двум осям.
func TestLogoutExits_EveryExitReachableThroughTheEdgeEndsEveryName(t *testing.T) {
	names := middleware.SessionCarrierNames()
	require.NotEmpty(t, names)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Ось «кто гасит» × ось «кто вызывает»: каждая запись — выход, который
	// продукт ПРЕДЛАГАЕТ человеку, и то, чем он достижим.
	type exit struct {
		name   string
		caller string
		serve  func(t *testing.T) *http.Response
	}
	exits := []exit{
		{
			name:   "обработчик выхода края",
			caller: "POST /oauth/logout",
			serve: func(t *testing.T) *http.Response {
				h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{Logger: logger})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/oauth/logout", nil))
				return rec.Result()
			},
		},
		{
			name:   "глагол выхода полосы формы",
			caller: "POST " + middleware.LoginLanePathLogout,
			serve: func(t *testing.T) *http.Response {
				// Служба гасит ТОЛЬКО своё имя: чужого она не знает и знать не
				// может — оно принадлежит стороне, которой она не управляет.
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{
						Name: middleware.OurSessionCarrierName, Value: "", MaxAge: -1, Path: "/",
					})
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(upstream.Close)

				relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
					Logger:   logger,
					Target:   upstream.URL,
					ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
					Timeout:  2 * time.Second,
				})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				relay.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, middleware.LoginLanePathLogout, nil))
				return rec.Result()
			},
		},
	}

	full := 0
	for _, e := range exits {
		t.Run(e.name, func(t *testing.T) {
			ended := endedNames(e.serve(t))
			missing := []string{}
			for _, n := range names {
				if !ended[n] {
					missing = append(missing, n)
				}
			}
			if len(missing) > 0 {
				t.Errorf("выход %q (вызывается: %s) гасит %d имени из %d; не погашены: %v. "+
					"Человек нажал «выйти» и по одному из имён остался вошедшим",
					e.name, e.caller, len(ended), len(names), missing)
				return
			}
			full++
		})
	}
	t.Logf("перепись: выходов, достижимых через край, осмотрено %d · гасят все имена %d · "+
		"имён в перечне %d. ВНЕ ОСМОТРА: собственный выход консолей — четыре консоли живут "+
		"вне этого дерева, и переход на самообслуживание чужой стороны отсюда не наблюдается",
		len(exits), full, len(names))
}

// Законный близнец: глагол, который выходом НЕ является, печений не гасит.
// Без него зелёное выше добывалось бы гашением на каждом ответе полосы формы —
// то есть выбрасыванием человека при любом обращении к ней.
func TestLoginLaneRelay_ANonLogoutVerbEndsNothing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
		Logger:   logger,
		Target:   upstream.URL,
		ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
		Timeout:  2 * time.Second,
	})
	require.NoError(t, err)

	checked := 0
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Path == middleware.LoginLanePathLogout {
			continue
		}
		checked++
		rec := httptest.NewRecorder()
		relay.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, rt.Path, nil))
		if n := len(endedNames(rec.Result())); n != 0 {
			t.Errorf("%s: глагол, выходом не являющийся, погасил %d имени", rt.Path, n)
		}
	}
	t.Logf("перепись: глаголов полосы формы осмотрено %d (из %d, выход исключён) · погасивших имена 0",
		checked, len(middleware.LoginLaneRoutes()))
}
