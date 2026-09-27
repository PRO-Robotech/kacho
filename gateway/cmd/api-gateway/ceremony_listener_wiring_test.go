// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ceremony_listener_wiring_test.go — координаты церемонии авторизации на
// ГРАНИЧНОМ admin-REST слушателе края отвечают «не найдено», на внешнем —
// ретранслируются (sec-issuance-path-not-elsewhere; возврат безопасности круга 1
// по kacho#2817, находка M1).
//
// Топология — та же, что в `main()`: ОДИН `http.Server` с
// `listenerorigin.InternalConnContext` обслуживает и внешний слушатель, и
// обёрнутый `listenerorigin.InternalListener` внутренний. Под `/` — настоящий
// диспетчер REST (`restmux.NewMux`) за снятием удостоверения, как в корне;
// объявление смонтировано `handler.MountLoginLaneRoutes` с тем же обработчиком
// `/` вторым аргументом.
//
// «Не найдено» судится не кодом, а СОВПАДЕНИЕМ: ответ внутреннего слушателя на
// координату побайтно равен ответу того же слушателя края, на котором
// объявление не смонтировано вовсе (посадка external). Отдельный производитель
// 404 отличался бы телом и заголовками — и форма ответа выдавала бы, что путь
// здесь есть (тот же класс, что снят у Internal* на внешнем слушателе,
// restmux/mux.go, диспетчер).
package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/restmux"
)

const ceremonyCallback = "https://console.kacho.local/callback?code=ac-1&state=st-1"

// countingListener — дублёр слушателя службы: считает запросы, отвечает заданным.
type countingListener struct {
	hits     atomic.Int64
	status   int
	location string
}

func (s *countingListener) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.hits.Add(1)
	if s.location != "" {
		w.Header().Set("Location", s.location)
	}
	w.WriteHeader(s.status)
}

// edgeListeners — внешний и внутренний слушатели одного `http.Server`.
type edgeListeners struct{ external, internal string }

func serveEdge(t *testing.T, h http.Handler) edgeListeners {
	t.Helper()
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ConnContext: listenerorigin.InternalConnContext}
	extLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("внешний слушатель: %v", err)
	}
	intLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("внутренний слушатель: %v", err)
	}
	go func() { _ = srv.Serve(extLn) }()
	go func() { _ = srv.Serve(listenerorigin.InternalListener(intLn)) }()
	t.Cleanup(func() { _ = srv.Close() })
	return edgeListeners{external: "http://" + extLn.Addr().String(), internal: "http://" + intLn.Addr().String()}
}

// edgeAnswer — то, чем ответ различим снаружи: код, тело и заголовки без
// `Date` (время ответа — не форма).
type edgeAnswer struct {
	status int
	body   string
	header http.Header
}

var noRedirects = &http.Client{
	Timeout:       5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func ask(t *testing.T, base, method, target, contentType, body string) edgeAnswer {
	t.Helper()
	req, err := http.NewRequest(method, base+target, strings.NewReader(body))
	if err != nil {
		t.Fatalf("запрос %s %s: %v", method, target, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := noRedirects.Do(req)
	if err != nil {
		t.Fatalf("%s %s%s: %v", method, base, target, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	h := resp.Header.Clone()
	h.Del("Date")
	return edgeAnswer{status: resp.StatusCode, body: string(b), header: h}
}

func TestCeremonyListenerWiring_L13_TheInternalAdminListener404sTheCeremonyAndTheExternalRelaysIt(t *testing.T) {
	dispatcher, err := restmux.NewMux(context.Background(), wiringMuxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	// Под `/` — ровно то, что ставит корень.
	rootHandler := principalmeta.StripCredentialBeforeForwarding(dispatcher)

	issuance := &countingListener{status: http.StatusFound, location: ceremonyCallback}
	form := &countingListener{status: http.StatusOK}
	stubs := map[middleware.RelayTarget]*countingListener{middleware.RelayTargetForm: form, middleware.RelayTargetIssuance: issuance}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var relays []*handler.LoginLaneRelay
	for _, tg := range middleware.RelayTargets() {
		stub := httptest.NewServer(stubs[tg])
		t.Cleanup(stub.Close)
		r, rErr := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
			Logger: logger, Serves: tg, Target: stub.URL, ClientIP: func(*http.Request) string { return "" },
		})
		if rErr != nil {
			t.Fatalf("ретранслятор цели %q: %v", tg, rErr)
		}
		relays = append(relays, r)
	}

	mountedMux := http.NewServeMux()
	if _, mErr := handler.MountLoginLaneRoutes(mountedMux, rootHandler, relays...); mErr != nil {
		t.Fatalf("монтаж объявления: %v", mErr)
	}
	mountedMux.Handle("/", rootHandler)
	own := serveEdge(t, mountedMux)

	// Близнец для сличения — край, на котором объявления нет (посадка external).
	bareMux := http.NewServeMux()
	bareMux.Handle("/", rootHandler)
	bare := serveEdge(t, bareMux)

	// Запрос пробы — на КАЖДУЮ запись объявления, отвечающую только на внешних
	// слушателях: перечень выводится из объявления, и запись, дописанная без
	// своего запроса, краснит пробу, а не остаётся непроверенной.
	requestOf := map[string]struct{ method, target, contentType, body string }{
		middleware.CeremonyPathAuthorize: {http.MethodGet, middleware.CeremonyPathAuthorize + "?response_type=code&client_id=console&state=st-1", "", ""},
		middleware.CeremonyPathToken:     {http.MethodPost, middleware.CeremonyPathToken, "application/x-www-form-urlencoded", "grant_type=authorization_code&code=ac-1&code_verifier=v&client_id=console"},
		middleware.CeremonyPathDiscovery: {http.MethodGet, middleware.CeremonyPathDiscovery, "", ""},
	}
	type coordinate struct{ verb, method, target, contentType, body string }
	var coordinates []coordinate
	for _, rt := range middleware.LoginLaneRoutes() {
		if !rt.Target.ExternalListenersOnly() {
			continue
		}
		r, ok := requestOf[rt.Path]
		if !ok {
			t.Fatalf("запись %q (%s) отвечает только на внешних слушателях, а запроса пробы у неё нет", rt.Verb, rt.Path)
		}
		coordinates = append(coordinates, coordinate{rt.Verb, r.method, r.target, r.contentType, r.body})
	}
	if len(coordinates) != len(requestOf) {
		t.Fatalf("записей только-внешних слушателей %d, запросов пробы %d — запрос без записи судит путь, которого край не объявляет", len(coordinates), len(requestOf))
	}
	for _, c := range coordinates {
		// (1) Внутренний слушатель: «не найдено» — тем же ответом, что на
		// слушателе, где пути нет вовсе. До слушателя выдачи — ничего.
		before := issuance.hits.Load()
		got := ask(t, own.internal, c.method, c.target, c.contentType, c.body)
		want := ask(t, bare.internal, c.method, c.target, c.contentType, c.body)
		if got.status != http.StatusNotFound {
			t.Errorf("%s на внутреннем слушателе: код %d, ожидался 404 (тело %q)", c.verb, got.status, got.body)
		}
		if got.status != want.status || got.body != want.body {
			t.Errorf("%s на внутреннем слушателе: ответ {%d %q} отличается от ответа на несмонтированный путь {%d %q}",
				c.verb, got.status, got.body, want.status, want.body)
		}
		for name := range mergeKeys(got.header, want.header) {
			if strings.Join(got.header.Values(name), ",") != strings.Join(want.header.Values(name), ",") {
				t.Errorf("%s на внутреннем слушателе: заголовок %s = %q, у несмонтированного пути %q",
					c.verb, name, got.header.Values(name), want.header.Values(name))
			}
		}
		if issuance.hits.Load() != before {
			t.Errorf("%s с внутреннего слушателя дошёл до слушателя выдачи", c.verb)
		}

		// (2) Близнец — тот же край, тот же запрос, ВНЕШНИЙ слушатель: ретрансляция.
		ext := ask(t, own.external, c.method, c.target, c.contentType, c.body)
		if issuance.hits.Load() != before+1 {
			t.Errorf("%s на внешнем слушателе не дошёл до слушателя выдачи (код %d)", c.verb, ext.status)
		}
		if ext.status != http.StatusFound || ext.header.Get("Location") != ceremonyCallback {
			t.Errorf("%s на внешнем слушателе: ответ церемонии не ушёл как есть: %d Location=%q", c.verb, ext.status, ext.header.Get("Location"))
		}
	}

	// (3) Близнец по цели — тот же внутренний слушатель, запись полосы формы:
	// ретранслируется. Отказ выше — решение о цели, а не поломка слушателя пробы.
	before := form.hits.Load()
	if got := ask(t, own.internal, http.MethodPost, middleware.LoginLanePathLogin, "application/json", `{}`); form.hits.Load() != before+1 {
		t.Errorf("запись формы с внутреннего слушателя не ретранслирована (код %d)", got.status)
	}
	t.Logf("перепись: координат церемонии %d · внутренний слушатель — «не найдено» побайтно, внешний — ретрансляция · слушатель выдачи получил %d · слушатель формы %d",
		len(coordinates), issuance.hits.Load(), form.hits.Load())
}

func mergeKeys(a, b http.Header) map[string]struct{} {
	out := map[string]struct{}{}
	for k := range a {
		out[k] = struct{}{}
	}
	for k := range b {
		out[k] = struct{}{}
	}
	return out
}
