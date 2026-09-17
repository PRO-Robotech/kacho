// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_relay_test.go — ретрансляция четырёх глаголов формы (приёмка Ф3
// Р2, Р7): Ф3-17 (недоступность через край), Ф3-51 (половина края — состав
// ретранслированного запроса).
//
// Пробы идут ЧЕРЕЗ ЦЕПОЧКУ `AuthInterceptor.HTTP(mux)` с ретранслятором,
// зарегистрированным на четырёх путях: полоса личности стоит ПЕРЕД
// ретрансляцией, выставляет личность по проверенному носителю — и ретранслятор
// обязан её снять. Дублёр слушателя формы читает заголовки каждого запроса и
// считает их; второй дублёр не принимает соединений вовсе.
package handler_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// fakeOwn — дублёр `Resolve` на один вопрос.
type fakeOwn struct {
	sess  middleware.HumanSession
	found bool
	err   error
}

func (f *fakeOwn) ResolveHumanSession(context.Context, string) (middleware.HumanSession, bool, error) {
	return f.sess, f.found, f.err
}

type fakeCut struct {
	cutoff time.Time
	found  bool
	err    error
}

func (f *fakeCut) SessionCutoffOf(context.Context, string) (time.Time, bool, error) {
	return f.cutoff, f.found, f.err
}

var authAt = time.Date(2026, 9, 16, 12, 0, 0, 123456000, time.UTC)

func liveSession() middleware.HumanSession {
	return middleware.HumanSession{UserID: "usr-1", Email: "a@example.com", DisplayName: "A",
		AuthenticatedAt: authAt, ExpiresAt: authAt.Add(24 * time.Hour), AssuranceLevel: "1"}
}

// seen — один запрос, дошедший до дублёра слушателя формы.
type seen struct {
	method, path, query string
	header              http.Header
	body                string
}

// formListenerStub — дублёр слушателя формы службы: читает всё, отвечает тем,
// что задано.
type formListenerStub struct {
	mu       sync.Mutex
	requests []seen
	status   int
	body     string
	respHdr  http.Header
}

func (s *formListenerStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, seen{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: string(b)})
	s.mu.Unlock()
	for k, vs := range s.respHdr {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s.status)
	_, _ = io.WriteString(w, s.body)
}

func (s *formListenerStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *formListenerStub) last() seen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[len(s.requests)-1]
}

// chainWithRelay — край под `own`: полоса личности + ретранслятор на четырёх
// путях, цель — `target`.
func chainWithRelay(t *testing.T, own *fakeOwn, cut *fakeCut, target string) (http.Handler, *handler.LoginLaneRelay) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
		Logger: logger,
		Target: target,
		// Оператор чтения цепочки — тот же, что у решения о доступе: один
		// доверенный прыжок, адрес берётся справа.
		ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("ретранслятор не собрался: %v", err)
	}
	mux := http.NewServeMux()
	for _, rt := range middleware.LoginLaneRoutes() {
		mux.Handle(rt.Path, relay)
	}
	a := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", nil, logger).
		WithHumanSession(own).
		WithSessionCutoffCheck(cut, time.Hour)
	return a.HTTP(mux), relay
}

func formRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer must-not-cross")
	req.Header.Set("X-Kacho-Admin", "true") // присланное клиентом — полоса снимает на входе
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	req.Header.Set("X-Real-IP", "10.0.0.1")
	req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "s2-live"})
	req.AddCookie(&http.Cookie{Name: "kaname_form", Value: "ctx"})
	req.RemoteAddr = "10.0.0.1:4242"
	return req
}

func kachoHeaders(h http.Header) []string {
	var out []string
	for name := range h {
		if _, ok := principalmeta.KachoNamespaceKey(name); ok {
			out = append(out, name)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф3-51 — состав ретранслированного запроса.

func TestLoginLaneRelay_F3_51_RelayedRequestCarriesCookiesAndOneForwardedForAndNoIdentity(t *testing.T) {
	stub := &formListenerStub{status: http.StatusOK, body: `{}`,
		respHdr: http.Header{"Set-Cookie": {"kaname_session=; Max-Age=0; Path=/; HttpOnly; Secure; SameSite=Lax"}}}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	// Живая сессия: полоса ВЫСТАВИТ личность перед ретрансляцией — и её обязан
	// снять ретранслятор (§1.10: шесть заголовков принципала).
	chain, relay := chainWithRelay(t, &fakeOwn{found: true, sess: liveSession()}, &fakeCut{}, srv.URL)

	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, formRequest(http.MethodPost, middleware.LoginLanePathLogout, `{"csrfToken":"x"}`))
	if rec.Code != http.StatusOK || stub.count() != 1 {
		t.Fatalf("выход с живой сессией: %d, ретранслировано %d", rec.Code, stub.count())
	}
	got := stub.last()
	if got.method != http.MethodPost || got.path != middleware.LoginLanePathLogout || got.body != `{"csrfToken":"x"}` {
		t.Fatalf("метод/путь/тело не переданы как есть: %s %s %q", got.method, got.path, got.body)
	}
	if left := kachoHeaders(got.header); len(left) != 0 {
		t.Fatalf("на слушатель формы уехали заголовки пространства x-kacho-: %v", left)
	}
	if got.header.Get("Authorization") != "" {
		t.Fatal("удостоверение уехало на слушатель формы")
	}
	if c := got.header.Get("Cookie"); !strings.Contains(c, middleware.OurSessionCarrierName+"=s2-live") || !strings.Contains(c, "kaname_form=ctx") {
		t.Fatalf("печенья не доехали: %q", c)
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type: %q", got.header.Get("Content-Type"))
	}
	xff := got.header.Values("X-Forwarded-For")
	if len(xff) != 1 || xff[0] != "10.0.0.1" {
		t.Fatalf("X-Forwarded-For обязан быть РОВНО ОДНИМ адресом, выведенным оператором цепочки (справа по числу прыжков): %v", xff)
	}
	// Ответ службы уходит клиенту как есть — включая Set-Cookie.
	if sc := rec.Result().Header["Set-Cookie"]; len(sc) != 1 || !strings.HasPrefix(sc[0], "kaname_session=; Max-Age=0") {
		t.Fatalf("Set-Cookie службы не доехал до клиента: %v", sc)
	}
	if rec.Body.String() != `{}` {
		t.Fatalf("тело ответа изменено: %q", rec.Body.String())
	}

	// Признак формы — GET с параметрами, параметры доезжают.
	rec = httptest.NewRecorder()
	chain.ServeHTTP(rec, formRequest(http.MethodGet, middleware.LoginLanePathCSRF+"?form=login", ""))
	if got := stub.last(); got.method != http.MethodGet || got.query != "form=login" {
		t.Fatalf("csrf: %s %s?%s", got.method, got.path, got.query)
	}

	stats := relay.Stats()
	if stats.Relayed["logout"] != 1 || stats.Relayed["csrf"] != 1 || stats.Relayed["login"] != 0 || stats.Relayed["password"] != 0 {
		t.Fatalf("клетки ретрансляции по глаголу: %v", stats.Relayed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф3-17 — недоступность через край.

func TestLoginLaneRelay_F3_17_ServiceRefusalIsRelayedAsIsAndUnreachableServiceIs503(t *testing.T) {
	stub := &formListenerStub{status: http.StatusServiceUnavailable,
		body: `{"code":14,"message":"logout not performed; try again later"}`}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	// Под `own` вопросы края и слушатель формы бьют в одно хранилище: Resolve
	// отвечает UNAVAILABLE, слушатель — исходом Ф1-58.
	own := &fakeOwn{err: context.DeadlineExceeded}
	chain, relay := chainWithRelay(t, own, &fakeCut{}, srv.URL)

	for _, p := range []string{middleware.LoginLanePathLogout, middleware.LoginLanePathLogin, middleware.LoginLanePathCSRF} {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, formRequest(http.MethodPost, p, `{}`))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: ответ службы обязан уйти как есть (503), получено %d %s", p, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "logout not performed; try again later") {
			t.Fatalf("%s: тело службы изменено: %s", p, rec.Body.String())
		}
		if len(rec.Result().Header["Set-Cookie"]) != 0 {
			t.Fatalf("%s: Set-Cookie на недоступности: %v", p, rec.Result().Header["Set-Cookie"])
		}
	}
	if stub.count() != 3 {
		t.Fatalf("ретранслировано %d, ожидалось 3", stub.count())
	}
	// Тот же дублёр на смене пароля → F4d-23 на крае, ретранслировано 0 сверх.
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, formRequest(http.MethodPost, middleware.LoginLanePathPassword, `{}`))
	if rec.Code != http.StatusUnauthorized || stub.count() != 3 {
		t.Fatalf("смена пароля при недоступном Resolve: %d, ретранслировано %d", rec.Code, stub.count())
	}

	// Служба недостижима САМОЙ ретрансляцией: дублёр не принимает соединений.
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	chain2, relay2 := chainWithRelay(t, own, &fakeCut{}, closedURL)
	for _, p := range []string{middleware.LoginLanePathLogout, middleware.LoginLanePathLogin, middleware.LoginLanePathCSRF} {
		rec := httptest.NewRecorder()
		chain2.ServeHTTP(rec, formRequest(http.MethodPost, p, `{}`))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: недостижимая служба обязана давать 503 текстом края, получено %d", p, rec.Code)
		}
		var body struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != 14 || body.Message != "service unavailable; try again later" {
			t.Fatalf("%s: тело отказа края: %s", p, rec.Body.String())
		}
		if len(rec.Result().Header["Set-Cookie"]) != 0 {
			t.Fatalf("%s: Set-Cookie на недостижимой службе", p)
		}
	}
	if relay2.Stats().Unreachable != 3 {
		t.Fatalf("клетка «служба недостижима» = %d, ожидалось 3", relay2.Stats().Unreachable)
	}
	if s := relay.Stats(); s.Relayed["logout"] != 1 || s.Relayed["login"] != 1 || s.Relayed["csrf"] != 1 || s.Relayed["password"] != 0 || s.Unreachable != 0 {
		t.Fatalf("клетки первого ретранслятора: %+v", s)
	}
}

// Ответ службы уходит клиенту как есть — статус, заголовки, тело (положительный
// контроль на успешном входе).
func TestLoginLaneRelay_F3_51_ServiceResponsePassesThroughUnchanged(t *testing.T) {
	stub := &formListenerStub{status: http.StatusOK, body: `{"session":{"expiresAt":"2026-09-17T12:00:00Z"}}`,
		respHdr: http.Header{"Set-Cookie": {"kaname_session=new; Max-Age=86400; Path=/; HttpOnly; Secure; SameSite=Lax"}, "X-Service": {"kaname"}}}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	chain, _ := chainWithRelay(t, &fakeOwn{found: false}, &fakeCut{}, srv.URL)
	rec := httptest.NewRecorder()
	req := formRequest(http.MethodPost, middleware.LoginLanePathLogin, `{"email":"a@example.com","password":"p"}`)
	chain.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != stub.body {
		t.Fatalf("вход: %d %s", rec.Code, rec.Body.String())
	}
	if sc := rec.Result().Header.Get("Set-Cookie"); !strings.HasPrefix(sc, "kaname_session=new;") {
		t.Fatalf("Set-Cookie входа: %q", sc)
	}
	if rec.Result().Header.Get("X-Service") != "kaname" {
		t.Fatal("заголовки ответа службы не доехали")
	}
	// Личность на «сессии нет» полоса не выставляла — и в запросе её нет.
	if left := kachoHeaders(stub.last().header); len(left) != 0 {
		t.Fatalf("заголовки пространства на входе: %v", left)
	}
}
