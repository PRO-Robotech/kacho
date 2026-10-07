// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// password_enroll_relay_test.go — заведение первого пароля из живой сессии
// СКВОЗЬ КРАЙ (kaname#213, приёмка службы first-password-from-a-live-session,
// FP-01; kacho#3056): человек без пароля с живой сессией заводит пароль через
// внешний слушатель края и входит им.
//
// Цепочка — та же, что в композиционном корне: полоса личности под `own`,
// монтаж объявления `handler.MountLoginLaneRoutes`, ретранслятор цели формы.
// Дублёр слушателя формы держит состояние двух людей и отвечает ФОРМАМИ службы
// (`loginlanehttp.writeRefusal`, `method`): проба судит, что край довёз
// запрос и вернул ответ службы как есть, а не что-то своё.
//
// Близнецы: у человека, у которого пароль уже есть, — отказ СЛУЖБЫ (тело
// службы, запрос до неё дошёл), а не края; неверный метод — общая форма
// отказа службы, побайтно та же, что у соседнего глагола смены пароля.
package handler_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

// enrollPathWant — путь глагола по объявлению службы
// (`loginlanehttp.PathPasswordEnroll`), выписан дословно.
const enrollPathWant = "/iam/v1/auth/password/enroll"

// Формы отказа службы (`loginlanehttp.writeRefusal`): «пароль уже есть» —
// 409, код 6 (A7 Р3); неверный метод — 405, код 3, `Allow`.
const (
	passwordAlreadySetBody = `{"code":6,"message":"password is already set; change it with the current password","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"PASSWORD_ALREADY_SET","domain":"iam.kaname.cloud"}]}`
	methodNotAllowedBody   = `{"code":3,"message":"method not allowed","details":[]}`
	loginFailedBody        = `{"code":16,"message":"authentication failed","details":[]}`
)

// passwordService — дублёр слушателя формы на два глагола: заведение пароля
// по носителю сессии и вход паролем. Носитель → адрес; адрес → пароль.
type passwordService struct {
	mu        sync.Mutex
	sessions  map[string]string
	passwords map[string]string
	reached   map[string]int
}

func (s *passwordService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reached[r.URL.Path]++
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, methodNotAllowedBody)
		return
	}
	var form map[string]string
	_ = json.NewDecoder(r.Body).Decode(&form)
	switch r.URL.Path {
	case enrollPathWant:
		c, err := r.Cookie(middleware.OurSessionCarrierName)
		email, live := "", false
		if err == nil {
			email, live = s.sessions[c.Value]
		}
		switch {
		case !live:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":16,"message":"request not performed","details":[]}`)
		case s.passwords[email] != "":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, passwordAlreadySetBody)
		default:
			s.passwords[email] = form["newPassword"]
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"session":{"email":"`+email+`"}}`)
		}
	case middleware.LoginLanePathLogin:
		if p := s.passwords[form["email"]]; p == "" || p != form["password"] {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, loginFailedBody)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: middleware.OurSessionCarrierName, Value: "s-new", Path: "/", HttpOnly: true, Secure: true})
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"session":{"email":"`+form["email"]+`"}}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *passwordService) hits(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reached[path]
}

func enrollRequest(method, path, carrier, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if carrier != "" {
		req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: carrier})
	}
	req.AddCookie(&http.Cookie{Name: "kaname_form", Value: "ctx"})
	req.RemoteAddr = relayedClientIP + ":4242"
	return onExternalListener(req)
}

func TestPasswordEnroll_3056_FP01_PasswordlessHumanEnrollsThroughTheEdgeAndSignsInWithIt(t *testing.T) {
	svc := &passwordService{
		sessions:  map[string]string{"s-passwordless": "a@example.com", "s-has-password": "b@example.com"},
		passwords: map[string]string{"b@example.com": "old-secret-1"},
		reached:   map[string]int{},
	}
	srv := privateloopback.NewServer(t, svc)
	t.Cleanup(srv.Close)
	chain, relays := chainWithRelays(t, &fakeOwn{found: true, sess: liveSession()}, &fakeCut{}, srv.URL)

	serve := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		return rec
	}

	// FP-01: заведение — 200 от службы, без Set-Cookie (носитель не перевыпускается).
	rec := serve(enrollRequest(http.MethodPost, enrollPathWant, "s-passwordless", `{"newPassword":"first-secret-1","csrfToken":"x"}`))
	if rec.Code != http.StatusOK || svc.hits(enrollPathWant) != 1 {
		t.Fatalf("заведение первого пароля через край: %d %q, дошло до службы %d", rec.Code, rec.Body.String(), svc.hits(enrollPathWant))
	}
	if sc := rec.Result().Header["Set-Cookie"]; len(sc) != 0 {
		t.Errorf("заведение не перевыпускает носитель, а край вернул Set-Cookie %v", sc)
	}
	// …и входит им: вход паролем через край — 200 и носитель службы.
	rec = serve(enrollRequest(http.MethodPost, middleware.LoginLanePathLogin, "", `{"email":"a@example.com","password":"first-secret-1","csrfToken":"x"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("вход заведённым паролем: %d %q", rec.Code, rec.Body.String())
	}
	if sc := rec.Result().Header.Get("Set-Cookie"); !strings.HasPrefix(sc, middleware.OurSessionCarrierName+"=s-new") {
		t.Errorf("вход не вернул носитель службы: %q", sc)
	}

	// Близнец: у человека с паролем — отказ СЛУЖБЫ, побайтно её тело.
	before := svc.hits(enrollPathWant)
	rec = serve(enrollRequest(http.MethodPost, enrollPathWant, "s-has-password", `{"newPassword":"another-1","csrfToken":"x"}`))
	if rec.Code != http.StatusConflict || rec.Body.String() != passwordAlreadySetBody || svc.hits(enrollPathWant) != before+1 {
		t.Errorf("человек с паролем: ожидался отказ службы 409 %s; получено %d %q, дошло до службы %d", passwordAlreadySetBody, rec.Code, rec.Body.String(), svc.hits(enrollPathWant)-before)
	}

	// Неверный метод — общая форма отказа службы, та же, что у смены пароля.
	rec = serve(enrollRequest(http.MethodGet, enrollPathWant, "s-passwordless", ""))
	neighbour := serve(enrollRequest(http.MethodGet, middleware.LoginLanePathPassword, "s-passwordless", ""))
	if rec.Code != http.StatusMethodNotAllowed || rec.Body.String() != methodNotAllowedBody || rec.Header().Get("Allow") != http.MethodPost {
		t.Errorf("неверный метод: ожидался 405 службы %s с Allow: POST; получено %d %q Allow=%q", methodNotAllowedBody, rec.Code, rec.Body.String(), rec.Header().Get("Allow"))
	}
	if rec.Code != neighbour.Code || rec.Body.String() != neighbour.Body.String() {
		t.Errorf("форма отказа на неверный метод разошлась с соседним глаголом: %d %q против %d %q", rec.Code, rec.Body.String(), neighbour.Code, neighbour.Body.String())
	}

	if got := relays[middleware.RelayTargetForm].Stats().Relayed["password-enroll"]; got != 3 {
		t.Errorf("клетка ретрансляции password-enroll = %d, ожидалось 3", got)
	}
	t.Logf("перепись: до службы на глаголе дошло %d · на входе %d", svc.hits(enrollPathWant), svc.hits(middleware.LoginLanePathLogin))
}
