// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_relay_test.go — ретрансляция глаголов формы (приёмка Ф3
// Р2, Р7): Ф3-17 (недоступность через край), Ф3-51 (половина края — состав
// ретранслированного запроса).
//
// Пробы идут ЧЕРЕЗ ЦЕПОЧКУ `AuthInterceptor.HTTP(mux)` с ретранслятором,
// зарегистрированным на путях перечня: полоса личности стоит ПЕРЕД
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
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
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

func (f *fakeCut) SessionCutoffOf(context.Context, middleware.CutoffSubject) (time.Time, bool, error) {
	return f.cutoff, f.found, f.err
}

var authAt = time.Date(2026, 9, 16, 12, 0, 0, 123456000, time.UTC)

// Адрес подтверждён: проба о другом предмете (ретрансляция глаголов формы);
// рубеж адреса держит own_session_address_gate_test.go (приёмка F6b, DoD п. 2).
func liveSession() middleware.HumanSession {
	return middleware.HumanSession{UserID: "usr-1", Email: "a@example.com", DisplayName: "A",
		AuthenticatedAt: authAt, ExpiresAt: authAt.Add(24 * time.Hour), AssuranceLevel: "1", EmailVerified: true}
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
	status, respHdr, body := s.status, s.respHdr, s.body
	s.mu.Unlock()
	for k, vs := range respHdr {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// answer — сменить ответ дублёра между запросами.
func (s *formListenerStub) answer(status int, respHdr http.Header, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.respHdr, s.body = status, respHdr, body
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

// chainWithRelay — край под `own`: полоса личности + ретрансляторы на путях
// объявления, цель ОБОИХ — `target`; возвращает ретранслятор полосы формы.
func chainWithRelay(t *testing.T, own *fakeOwn, cut *fakeCut, target string) (http.Handler, *handler.LoginLaneRelay) {
	t.Helper()
	chain, relays := chainWithRelays(t, own, cut, target)
	return chain, relays[middleware.RelayTargetForm]
}

// chainWithRelays — край под `own`: по ретранслятору на КАЖДУЮ цель закрытого
// перечня, все — на один дублёр `target`, смонтированы ТЕМ ЖЕ монтажом, что в
// композиционном корне (`handler.MountLoginLaneRoutes`).
func chainWithRelays(t *testing.T, own *fakeOwn, cut *fakeCut, target string) (http.Handler, map[middleware.RelayTarget]*handler.LoginLaneRelay) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	relays := map[middleware.RelayTarget]*handler.LoginLaneRelay{}
	var set []*handler.LoginLaneRelay
	for _, tg := range middleware.RelayTargets() {
		relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
			Logger: logger,
			Serves: tg,
			Target: target,
			// Оператор чтения цепочки — тот же, что у решения о доступе: один
			// доверенный прыжок, адрес берётся справа.
			ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
			Timeout:  2 * time.Second,
		})
		if err != nil {
			t.Fatalf("ретранслятор цели %q не собрался: %v", tg, err)
		}
		relays[tg] = relay
		set = append(set, relay)
	}
	mux := http.NewServeMux()
	// Под `/` здесь ничего нет — мультиплексор отвечает на чужой путь
	// `http.NotFoundHandler()`; им же отвечает внутренний слушатель на запись,
	// которой на нём нет.
	if _, err := handler.MountLoginLaneRoutes(mux, http.NotFoundHandler(), set...); err != nil {
		t.Fatalf("монтаж объявления: %v", err)
	}
	a := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", nil, logger).
		WithHumanSession(own).
		WithSessionCutoffCheck(cut, time.Hour)
	return a.HTTP(mux), relays
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
	srv := privateloopback.NewServer(t, stub)
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
	srv := privateloopback.NewServer(t, stub)
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
	// Тот же дублёр на смене пароля → ответ Р1 приёмки KA1 на крае (503, носитель
	// цел; заменил F4d-23), ретранслировано 0 сверх.
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, formRequest(http.MethodPost, middleware.LoginLanePathPassword, `{}`))
	if rec.Code != http.StatusServiceUnavailable || stub.count() != 3 ||
		!strings.Contains(rec.Body.String(), "credential state could not be established") ||
		len(rec.Result().Header["Set-Cookie"]) != 0 {
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
	if relay2.Stats().Unreachable != 3 || relay2.Stats().Target != middleware.RelayTargetForm {
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
	srv := privateloopback.NewServer(t, stub)
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

// ─────────────────────────────────────────────────────────────────────────────
// F8-S5-09 — три глагола своих сессий (приёмка F8-S5 Р1, kacho#3061)
// ретранслируются на слушатель формы с описанием клиента.

// ownSessionsPaths — три пути Р1 приёмки F8-S5, выписанные ДОСЛОВНО по её
// таблице, а не взятые из объявления края: проба сверяет край с решением.
var ownSessionsPaths = map[string]string{
	"sessions":            "/iam/v1/auth/sessions",
	"sessions-end":        "/iam/v1/auth/sessions/end",
	"sessions-end-others": "/iam/v1/auth/sessions/end-others",
}

// ownSessionsRequest — запрос консоли на глагол своих сессий: носитель сессии,
// описание клиента и всё, что ретранслятор обязан снять, — удостоверение в
// обеих формах имени и заголовки пространства `x-kacho-*`, включая
// подсемейство `x-kacho-token-` (номер записи сессии, идентификатор
// предъявленного).
func ownSessionsRequest(method, path, userAgent string) *http.Request {
	body := ""
	if method == http.MethodPost {
		body = `{"id":"s-other","csrfToken":"x"}`
	}
	req := formRequest(method, path, body)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Del("User-Agent")
	}
	req.Header.Set(principalmeta.BridgePrefix+"authorization", "Bearer must-not-cross-either")
	req.Header.Set(principalmeta.HeaderTokenSessionID, "sess-record-7")
	req.Header.Set(principalmeta.HeaderTokenJti, "jti-forged")
	return req
}

func TestLoginLaneRelay_F8_S5_09_OwnSessionVerbsReachTheFormTargetWithTheClientDescription(t *testing.T) {
	stub := &formListenerStub{status: http.StatusOK, body: `{}`}
	srv := privateloopback.NewServer(t, stub)
	t.Cleanup(srv.Close)
	chain, relays := chainWithRelays(t, &fakeOwn{found: true, sess: liveSession()}, &fakeCut{}, srv.URL)
	form := relays[middleware.RelayTargetForm]

	serveOne := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		return rec
	}

	// (а) перечень: GET с описанием клиента и носителем сессии.
	rec := serveOne(ownSessionsRequest(http.MethodGet, ownSessionsPaths["sessions"], "f8-s5-09"))
	if rec.Code != http.StatusOK || stub.count() != 1 {
		t.Fatalf("F8-S5-09 (а): %d %q, принято целью %d — путь обязан ретранслироваться на слушатель формы", rec.Code, rec.Body.String(), stub.count())
	}
	got := stub.last()
	if got.method != http.MethodGet || got.path != ownSessionsPaths["sessions"] {
		t.Fatalf("F8-S5-09 (а): цель приняла %s %s", got.method, got.path)
	}
	if ua := got.header.Values("User-Agent"); len(ua) != 1 || ua[0] != "f8-s5-09" {
		t.Errorf("F8-S5-09 (а): описание клиента не дошло дословно — цель получила User-Agent %q", ua)
	}
	if c := got.header.Get("Cookie"); !strings.Contains(c, middleware.OurSessionCarrierName+"=s2-live") {
		t.Errorf("F8-S5-09 (а): носитель сессии не дошёл до цели: Cookie %q", c)
	}
	for name := range got.header {
		if l := strings.ToLower(name); l == "authorization" || l == strings.ToLower(principalmeta.BridgePrefix+"authorization") {
			t.Errorf("F8-S5-09 (а): удостоверение уехало на слушатель формы под именем %q", name)
		}
	}
	if left := kachoHeaders(got.header); len(left) != 0 {
		t.Errorf("F8-S5-09 (а): на слушатель формы уехали заголовки пространства x-kacho-: %v", left)
	}

	// (б), (в) снятие одной и снятие прочих — POST, приняты целью формы.
	for _, verb := range []string{"sessions-end", "sessions-end-others"} {
		before := stub.count()
		rec := serveOne(ownSessionsRequest(http.MethodPost, ownSessionsPaths[verb], "f8-s5-09"))
		if rec.Code != http.StatusOK || stub.count() != before+1 {
			t.Errorf("F8-S5-09 %s: %d %q, принято целью %d (ожидался 1)", verb, rec.Code, rec.Body.String(), stub.count()-before)
			continue
		}
		if got := stub.last(); got.method != http.MethodPost || got.path != ownSessionsPaths[verb] || got.body != `{"id":"s-other","csrfToken":"x"}` {
			t.Errorf("F8-S5-09 %s: цель приняла %s %s %q — метод, путь и тело обязаны доехать как есть", verb, got.method, got.path, got.body)
		}
	}
	stats := form.Stats()
	for verb := range ownSessionsPaths {
		if stats.Relayed[verb] != 1 {
			t.Errorf("F8-S5-09: клетка ретрансляции %q = %d, ожидался 1 (%v)", verb, stats.Relayed[verb], stats.Relayed)
		}
	}

	// (г) путь вне перечня — цель не вызвана, край отвечает как на неизвестный
	// путь. Близнец — (а): различие одно, путь. Точность держит мультиплексор,
	// собранный монтажом объявления; ретранслятор — второй рубеж.
	before := stub.count()
	rec = serveOne(ownSessionsRequest(http.MethodGet, "/iam/v1/auth/sessions/other", "f8-s5-09"))
	unknown := serveOne(ownSessionsRequest(http.MethodGet, "/iam/v1/auth/no-such-verb", "f8-s5-09"))
	if stub.count() != before {
		t.Fatalf("F8-S5-09 (г): путь вне перечня дошёл до цели формы (%d обращений)", stub.count()-before)
	}
	if rec.Code != http.StatusNotFound || rec.Code != unknown.Code || rec.Body.String() != unknown.Body.String() {
		t.Errorf("F8-S5-09 (г): ответ %d %q, на неизвестный путь край отвечает %d %q — обязаны совпасть",
			rec.Code, rec.Body.String(), unknown.Code, unknown.Body.String())
	}
	direct := httptest.NewRecorder()
	form.ServeHTTP(direct, ownSessionsRequest(http.MethodGet, "/iam/v1/auth/sessions/other", "f8-s5-09"))
	if direct.Code != http.StatusNotFound || stub.count() != before {
		t.Errorf("F8-S5-09 (г): ретранслятор формы напрямую ответил %d и довёз до цели %d — второй рубеж обязан отказать", direct.Code, stub.count()-before)
	}

	// Близнец без описания клиента: край не подставляет своё (имя HTTP-библиотеки
	// края служба записала бы описанием клиента сессии).
	rec = serveOne(ownSessionsRequest(http.MethodGet, ownSessionsPaths["sessions"], ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("F8-S5-09 близнец без User-Agent: %d %q", rec.Code, rec.Body.String())
	}
	if ua := stub.last().header.Get("User-Agent"); ua != "" {
		t.Errorf("F8-S5-09 близнец без User-Agent: цель получила описание клиента %q, которого клиент не присылал", ua)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// F8S6-10 — два глагола смены адреса почты (приёмка F8-S6 Р1, kacho#3062)
// ретранслируются на слушатель формы; путь без записи отвечает краем 404.

// emailChangePaths — два пути Р1 приёмки F8-S6, выписанные ДОСЛОВНО по её
// решению, а не взятые из объявления края.
var emailChangePaths = map[string]string{
	"email-change":         "/iam/v1/auth/email-change",
	"email-change-confirm": "/iam/v1/auth/email-change/confirm",
}

func TestLoginLaneRelay_F8S6_10_EmailChangeVerbsReachTheFormTargetPathWithoutRecordIsNot(t *testing.T) {
	stub := &formListenerStub{status: http.StatusOK, body: `{}`}
	srv := privateloopback.NewServer(t, stub)
	t.Cleanup(srv.Close)
	chain, relays := chainWithRelays(t, &fakeOwn{found: true, sess: liveSession()}, &fakeCut{}, srv.URL)
	form := relays[middleware.RelayTargetForm]

	serveOne := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		return rec
	}
	bodies := map[string]string{
		"email-change":         `{"newEmail":"new@example.test","csrfToken":"x"}`,
		"email-change-confirm": `{"code":"123456","csrfToken":"x"}`,
	}

	for verb, path := range emailChangePaths {
		before := stub.count()
		req := ownSessionsRequest(http.MethodPost, path, "f8s6-10")
		req.Body = io.NopCloser(strings.NewReader(bodies[verb]))
		req.ContentLength = int64(len(bodies[verb]))
		rec := serveOne(req)
		if rec.Code != http.StatusOK || stub.count() != before+1 {
			t.Errorf("F8S6-10 %s: %d %q, принято целью %d (ожидался 1) — путь обязан ретранслироваться на слушатель формы",
				verb, rec.Code, rec.Body.String(), stub.count()-before)
			continue
		}
		got := stub.last()
		if got.method != http.MethodPost || got.path != path || got.body != bodies[verb] {
			t.Errorf("F8S6-10 %s: цель приняла %s %s %q — метод, путь и тело обязаны доехать как есть", verb, got.method, got.path, got.body)
		}
		if c := got.header.Get("Cookie"); !strings.Contains(c, middleware.OurSessionCarrierName+"=s2-live") {
			t.Errorf("F8S6-10 %s: носитель сессии не дошёл до цели: Cookie %q", verb, c)
		}
		for name := range got.header {
			if l := strings.ToLower(name); l == "authorization" || l == strings.ToLower(principalmeta.BridgePrefix+"authorization") {
				t.Errorf("F8S6-10 %s: удостоверение уехало на слушатель формы под именем %q", verb, name)
			}
		}
		if left := kachoHeaders(got.header); len(left) != 0 {
			t.Errorf("F8S6-10 %s: на слушатель формы уехали заголовки пространства x-kacho-: %v", verb, left)
		}
	}
	stats := form.Stats()
	for verb := range emailChangePaths {
		if stats.Relayed[verb] != 1 {
			t.Errorf("F8S6-10: клетка ретрансляции %q = %d, ожидался 1 (%v)", verb, stats.Relayed[verb], stats.Relayed)
		}
	}

	// Путь без записи — цель не вызвана, край отвечает как на неизвестный путь.
	// Близнец — записи выше: различие одно, путь.
	before := stub.count()
	unknown := serveOne(ownSessionsRequest(http.MethodPost, "/iam/v1/auth/no-such-verb", "f8s6-10"))
	for _, p := range []string{"/iam/v1/auth/email-change/other", "/iam/v1/auth/email-change/confirm/", "/iam/v1/auth/email-changex"} {
		rec := serveOne(ownSessionsRequest(http.MethodPost, p, "f8s6-10"))
		if rec.Code != http.StatusNotFound || rec.Code != unknown.Code || rec.Body.String() != unknown.Body.String() {
			t.Errorf("F8S6-10 %s: ответ %d %q, на неизвестный путь край отвечает %d %q — обязаны совпасть",
				p, rec.Code, rec.Body.String(), unknown.Code, unknown.Body.String())
		}
		direct := httptest.NewRecorder()
		form.ServeHTTP(direct, ownSessionsRequest(http.MethodPost, p, "f8s6-10"))
		if direct.Code != http.StatusNotFound {
			t.Errorf("F8S6-10 %s: ретранслятор формы напрямую ответил %d — второй рубеж обязан отказать", p, direct.Code)
		}
	}
	if stub.count() != before {
		t.Fatalf("F8S6-10: путь без записи дошёл до цели формы (%d обращений)", stub.count()-before)
	}
}
