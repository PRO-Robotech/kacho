// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_relay_composition_test.go — состав ретранслированного запроса на
// КАЖДОМ пути объявления полосы формы: приёмка Ф12-38 («запрос несёт `Cookie` и
// один `X-Forwarded-For`, ноль заголовков пространства `x-kacho-` в обеих формах
// написания и без `Authorization`») в форме Ф3 Р2, на которую она ссылается.
//
// Прежние пробы утверждали состав на выходе и признаке формы; пути, дописанные
// в объявление позже (Ф4, Ф5, Ф12), им не покрывались. Здесь перечень путей
// берётся из объявления, а не выписывается: глагол, дописанный завтра, попадает
// под ту же пробу без правки.
//
// Носителей два, и каждый стережёт свою половину «ноль x-kacho-»:
//
//   - `A`, живая сессия: полоса личности САМА пишет заголовки принципала перед
//     продолжением, и снимать их обязан ретранслятор. Чужого субъекта полоса на
//     этом носителе перезаписывает своим — здесь подделка не видна;
//   - `C`, сессии нет: полоса не пишет ничего, и присланное клиентом стерегут
//     только вычистки — на входе полосы и в ретрансляторе.
//
// Клиент на обоих присылает чужого субъекта в обеих формах написания,
// удостоверение и свою цепочку адресов.
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

const (
	// forgedSubject — чужой субъект, которого клиент называет заголовками
	// пространства `x-kacho-`.
	forgedSubject = "usr-forged-9"
	// relayedClientIP — адрес, который оператор цепочки выводит СПРАВА по одному
	// доверенному прыжку из `X-Forwarded-For: 203.0.113.9, 10.0.0.1`
	// (`chainWithRelay`: `WithTrustedProxyHops(1)`).
	relayedClientIP = "10.0.0.1"
)

// forgedFormRequest — запрос клиента на глагол формы: наши печенья, чужая
// цепочка адресов, удостоверение и заголовки принципала чужого субъекта в
// голой и мостовой формах.
func forgedFormRequest(rt middleware.LoginLaneRoute) (*http.Request, string) {
	method, target, body := http.MethodPost, rt.Path, `{"csrfToken":"x"}`
	switch rt.Path {
	case middleware.LoginLanePathCSRF:
		method, target, body = http.MethodGet, rt.Path+"?form=login", ""
	case middleware.LoginLanePathSecondFactor:
		method, body = http.MethodGet, ""
	}
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer must-not-cross")
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	for _, name := range []string{principalmeta.HeaderPrincipalType, principalmeta.HeaderPrincipalID, principalmeta.HeaderPrincipalDisplay} {
		value := forgedSubject
		if name == principalmeta.HeaderPrincipalType {
			value = "user"
		}
		req.Header.Set(name, value)
		req.Header.Set(principalmeta.BridgePrefix+name, value)
	}
	req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "s2-live"})
	req.AddCookie(&http.Cookie{Name: "kaname_form", Value: "ctx"})
	req.RemoteAddr = relayedClientIP + ":4242"
	return req, body
}

func TestLoginLaneRelay_F12_38_EveryDeclaredVerbCarriesTheNamedCompositionOnly(t *testing.T) {
	routes := middleware.LoginLaneRoutes()
	if len(routes) == 0 {
		t.Fatal("объявление полосы формы пусто — проверять нечего, это не зелёный")
	}
	for _, carrier := range []struct {
		name string
		own  *fakeOwn
	}{
		{"A (живая сессия)", &fakeOwn{found: true, sess: liveSession()}},
		{"C (сессии нет)", &fakeOwn{found: false}},
	} {
		relayComposition(t, carrier.name, carrier.own, routes)
	}
}

func relayComposition(t *testing.T, carrier string, own *fakeOwn, routes []middleware.LoginLaneRoute) {
	t.Helper()
	stub := &formListenerStub{status: http.StatusOK, body: `{}`}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	chain, relay := chainWithRelay(t, own, &fakeCut{}, srv.URL)

	var withCookie, oneForwardedFor, kachoLeft, credentialLeft, forgedLeft int
	for _, rt := range routes {
		before := stub.count()
		req, body := forgedFormRequest(rt)
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || stub.count() != before+1 {
			t.Errorf("%s · %s: глагол обязан ретранслироваться; получено %d, дошло до службы %d", carrier, rt.Verb, rec.Code, stub.count()-before)
			continue
		}
		got := stub.last()

		// Метод, путь с параметрами и тело — как есть (Ф3 Р2).
		if got.method != req.Method || got.path != rt.Path || got.body != body {
			t.Errorf("%s · %s: метод/путь/тело не переданы как есть: %s %s %q", carrier, rt.Verb, got.method, got.path, got.body)
		}
		if rt.Path == middleware.LoginLanePathCSRF && got.query != "form=login" {
			t.Errorf("%s · %s: параметры пути не доехали: %q", carrier, rt.Verb, got.query)
		}
		if got.header.Get("Content-Type") != "application/json" {
			t.Errorf("%s · %s: Content-Type: %q", carrier, rt.Verb, got.header.Get("Content-Type"))
		}

		// Cookie — есть, оба наших.
		c := got.header.Get("Cookie")
		if strings.Contains(c, middleware.OurSessionCarrierName+"=s2-live") && strings.Contains(c, "kaname_form=ctx") {
			withCookie++
		} else {
			t.Errorf("%s · %s: печенья не доехали: %q", carrier, rt.Verb, c)
		}

		// X-Forwarded-For — ровно один заголовок одним значением: адрес,
		// выведенный оператором цепочки, а не присланная клиентом цепочка.
		if xff := got.header.Values("X-Forwarded-For"); len(xff) == 1 && xff[0] == relayedClientIP {
			oneForwardedFor++
		} else {
			t.Errorf("%s · %s: X-Forwarded-For обязан быть ровно одним значением %q, получено %q", carrier, rt.Verb, relayedClientIP, xff)
		}

		// Ноль заголовков пространства x-kacho- в обеих формах написания — ни
		// поставленных полосой, ни присланных клиентом.
		if left := kachoHeaders(got.header); len(left) != 0 {
			sort.Strings(left)
			kachoLeft++
			t.Errorf("%s · %s: на слушатель формы уехали заголовки пространства x-kacho-: %v", carrier, rt.Verb, left)
		}

		// Удостоверения нет.
		if got.header.Get("Authorization") != "" {
			credentialLeft++
			t.Errorf("%s · %s: удостоверение уехало на слушатель формы", carrier, rt.Verb)
		}

		// Чужой субъект не доезжает ни под каким именем.
		for name, values := range got.header {
			for _, v := range values {
				if strings.Contains(v, forgedSubject) {
					forgedLeft++
					t.Errorf("%s · %s: заголовок %s несёт чужого субъекта: %q", carrier, rt.Verb, name, v)
				}
			}
		}
	}

	stats := relay.Stats()
	for _, rt := range routes {
		if stats.Relayed[rt.Verb] != 1 {
			t.Errorf("%s: клетка ретрансляции %q = %d, ожидалось 1", carrier, rt.Verb, stats.Relayed[rt.Verb])
		}
	}
	t.Logf("перепись: носитель %s — путей объявления %d · дошло до службы %d · с печеньями %d · с одним X-Forwarded-For %d · с x-kacho- %d · с Authorization %d · с чужим субъектом %d",
		carrier, len(routes), stub.count(), withCookie, oneForwardedFor, kachoLeft, credentialLeft, forgedLeft)
}
