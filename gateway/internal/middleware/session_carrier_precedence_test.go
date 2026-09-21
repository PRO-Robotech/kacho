// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_precedence_test.go — ЧЬЯ ЛИЧНОСТЬ ДЕЙСТВУЕТ, когда предъявлены
// ОБА носителя браузерной сессии.
//
// # Почему это отдельный предмет, а не следствие порядка проверок
//
// Переходное состояние («наш носитель читается, и чужой ЕЩЁ читается») делает
// представимым вход, которого до него не существовало: браузер с двумя живыми
// печеньями сразу. Это два пути аутентификации в одном запросе, и «побеждает
// тот, кого спросили первым» решением не является — оно лишь пересказ порядка
// строк в файле, который завтра переставят при рефакторинге, и личность
// действующего поменяется молча.
//
// РЕШЕНИЕ НАЗВАНО: действует НАША личность, и чужой читатель на таком запросе
// НЕ СПРАШИВАЕТСЯ ВОВСЕ. Три довода, и каждый — о полномочии, а не о вкусе:
//
//  1. отзыв. Отсечку НАШЕЙ сессии держит наш авторитет (`SessionCutoffOf`), и
//     на нашем носителе он исполняется. Чужую сессию мы отозвать не можем:
//     выбрав её при живом нашем носителе, край отдал бы решение о доступе
//     стороне, у которой мы его как раз забираем;
//  2. уровень уверенности. Пол второго фактора край читает с оси каталога по
//     НАШЕЙ сессии (Ф11 Р7). У чужой сессии этого поля нет в наших терминах, и
//     обращение с полом «2» прошло бы по чужому носителю мимо нашего замка;
//  3. направление переезда. Переход идёт К нашей чеканке: наш носитель — более
//     новый факт о том же человеке, а не равноправная альтернатива.
//
// # Второе решение, и оно не выводится из первого
//
// Наш носитель решает НА КАЖДОМ СВОЁМ ИСХОДЕ, включая отказ. Мёртвое печенье
// нашей сессии НЕ откатывает запрос на чужую полосу: иначе предъявитель,
// придя с протухшим `kaname_session` и живым чужим, выбирал бы полосу сам —
// то есть старшинство назначал бы он, а не мы. Чужой читатель спрашивается
// РОВНО тогда, когда нашего носителя в запросе НЕТ.
//
// # Что проба обязана увидеть, а не объявить
//
// «Не спрашивался» доказывается СЧЁТЧИКОМ на дублёре чужой стороны, а не
// чтением кода: дублёр считает обращения, и ноль — наблюдение.
package middleware

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// countingProviderStub — чужой поставщик, СЧИТАЮЩИЙ, сколько раз его спросили.
// Дублёр не снисходительнее настоящего: отвечает живой сессией, то есть в
// выигрыше отказать не может — ноль обращений здесь означает решение края, а не
// неудачу дублёра.
func countingProviderStub(t *testing.T, asked *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"active":true,"authenticated_at":"`+
			ownAuthAt.UTC().Format(time.RFC3339Nano)+
			`","identity":{"id":"kid-foreign","traits":{"email":"foreign@example.com"}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// bothCarriersChain — край в ПЕРЕХОДНОМ состоянии: живы ОБА читателя носителя.
// Возвращает цепочку боевой формы (полоса личности + маршрут «кто я» за ней) и
// счётчик обращений следующего звена.
func bothCarriersChain(
	t *testing.T, providerURL string, reader HumanSessionReader, cut SessionCutoffReader,
) (http.Handler, *countingNext) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	foreignSubject := cutoffLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}

	a := NewAuthInterceptor(AuthModeDev, "", foreignSubject, logger).
		WithHumanSession(reader).
		WithKratos(NewKratosClient(providerURL))
	if cut != nil {
		a = a.WithSessionCutoffCheck(cut, time.Hour)
	}

	who := NewSessionIdentityHandler(logger).
		WithHumanSession(reader).
		WithKratos(NewKratosClient(providerURL), foreignSubject).
		WithSessionCutoff(cut)

	next := &countingNext{}
	mux := http.NewServeMux()
	who.Register(mux)
	mux.Handle("/", next)
	return a.HTTP(mux), next
}

func withForeignCarrier(req *http.Request, value string) *http.Request {
	req.AddCookie(&http.Cookie{Name: providerSessionCarrierName, Value: value})
	return req
}

// meUserID — чью личность назвал маршрут «кто я»; "" означает `{"user":null}`.
func meUserID(t *testing.T, body []byte) string {
	t.Helper()
	var out struct {
		User *struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("ответ «кто я» не разбирается: %v (%s)", err, body)
	}
	if out.User == nil {
		return ""
	}
	return out.User.ID
}

// ─────────────────────────────────────────────────────────────────────────────
// Решение 1. Оба носителя предъявлены → действует НАША личность, чужой читатель
// не спрашивается ни разу — на ОБЕИХ полосах, читающих одну сессию.

func TestBothCarriersPresented_OurIdentityActsAndTheForeignReaderIsNotAsked(t *testing.T) {
	var foreignAsked atomic.Int64
	provider := countingProviderStub(t, &foreignAsked)
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	chain, next := bothCarriersChain(t, provider.URL, reader, &fakeCutoff{})

	req := withForeignCarrier(
		withOurCarrier(httptest.NewRequest(http.MethodGet, platformPath, nil), "ours-live"),
		"foreign-live")
	rec := serve(chain, req)
	if rec.Code != http.StatusOK || next.served != 1 {
		t.Fatalf("полоса личности: код %d, звеньев %d — ожидался проход", rec.Code, next.served)
	}
	if got := next.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "usr-own-1" {
		t.Fatalf("полоса личности выставила %q — действовать обязана НАША личность usr-own-1", got)
	}

	rec = serve(chain, withForeignCarrier(
		withOurCarrier(httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil), "ours-live"),
		"foreign-live"))
	if got := meUserID(t, rec.Body.Bytes()); got != "usr-own-1" {
		t.Fatalf("«кто я» назвал %q — обязан назвать НАШУ личность usr-own-1 (%s)", got, rec.Body.String())
	}

	if n := foreignAsked.Load(); n != 0 {
		t.Fatalf("чужой читатель спрошен %d раз при живом нашем носителе — старшинство обязано решаться "+
			"ДО обращения: спросив чужую сторону, край уже принял её ответ к рассмотрению", n)
	}
	t.Logf("перепись: полос, читающих сессию, 2 (полоса личности, «кто я») · обращений к чужому читателю %d · "+
		"действующая личность usr-own-1", foreignAsked.Load())
}

// ─────────────────────────────────────────────────────────────────────────────
// Решение 2. МЁРТВОЕ наше печенье при живом чужом — отказ, а не откат на чужую
// полосу: иначе полосу выбирал бы предъявитель.

func TestBothCarriersPresented_ADeadOwnCarrierDoesNotFallBackToTheForeignLane(t *testing.T) {
	var foreignAsked atomic.Int64
	provider := countingProviderStub(t, &foreignAsked)
	reader := &fakeHumanSession{found: false} // нашей сессии нет: снята выходом либо истекла
	chain, next := bothCarriersChain(t, provider.URL, reader, &fakeCutoff{})

	for _, path := range []string{platformPath, "/iam/v1/auth/me"} {
		rec := serve(chain, withForeignCarrier(
			withOurCarrier(httptest.NewRequest(http.MethodGet, path, nil), "ours-dead"),
			"foreign-live"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: мёртвый наш носитель при живом чужом дал %d %s — ожидался отказ F4d-22, "+
				"иначе предъявитель назначает полосу сам", path, rec.Code, rec.Body.String())
		}
		if !ourCarrierEnded(rec.Result()) || !carrierEnded(rec.Result()) {
			t.Fatalf("%s: гашение обязано идти по ОБОИМ именам, Set-Cookie: %v",
				path, rec.Result().Header["Set-Cookie"])
		}
	}
	if next.served != 0 {
		t.Fatalf("запрос с мёртвым нашим носителем дошёл до следующего звена %d раз", next.served)
	}
	if n := foreignAsked.Load(); n != 0 {
		t.Fatalf("чужой читатель спрошен %d раз на отказе нашей полосы — это и есть откат, "+
			"выбранный предъявителем", n)
	}
	t.Logf("перепись: путей проверено 2 · обращений к чужому читателю %d · звеньев за полосой %d",
		foreignAsked.Load(), next.served)
}

// ─────────────────────────────────────────────────────────────────────────────
// Переходное состояние обязано быть ПОЛЕЗНЫМ: человек с живой ЧУЖОЙ сессией и
// БЕЗ нашего носителя входа не теряет — и обе полосы говорят о нём одно и то же.

func TestBothCarriersWired_AForeignOnlyCarrierKeepsItsSignIn(t *testing.T) {
	var foreignAsked atomic.Int64
	provider := countingProviderStub(t, &foreignAsked)
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	chain, next := bothCarriersChain(t, provider.URL, reader, &fakeCutoff{})

	rec := serve(chain, withForeignCarrier(httptest.NewRequest(http.MethodGet, platformPath, nil), "foreign-live"))
	if rec.Code != http.StatusOK || next.served != 1 {
		t.Fatalf("полоса личности: код %d, звеньев %d — живая чужая сессия обязана проходить", rec.Code, next.served)
	}
	if got := next.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "usr-foreign" {
		t.Fatalf("полоса личности выставила %q, ожидалось usr-foreign", got)
	}

	rec = serve(chain, withForeignCarrier(httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil), "foreign-live"))
	if got := meUserID(t, rec.Body.Bytes()); got != "usr-foreign" {
		t.Fatalf("«кто я» назвал %q, ожидалось usr-foreign — полосы, читающие одну сессию, обязаны "+
			"сходиться: человек, чей запрос край принимает, не может видеть себя невошедшим (%s)",
			got, rec.Body.String())
	}
	if reader.asked != 0 {
		t.Fatalf("наш читатель спрошен %d раз без нашего носителя — вопрос задан о том, чего в запросе нет",
			reader.asked)
	}
	t.Logf("перепись: полос, читающих сессию, 2 · обращений к чужому читателю %d · обращений к нашему %d",
		foreignAsked.Load(), reader.asked)
}

// Законный близнец: носителя нет вовсе — анонимно дальше, ни один читатель не
// спрошен. Без этой половины проба падала бы на любом запросе.
func TestBothCarriersWired_NoCarrierAsksNeitherReader(t *testing.T) {
	var foreignAsked atomic.Int64
	provider := countingProviderStub(t, &foreignAsked)
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	chain, next := bothCarriersChain(t, provider.URL, reader, &fakeCutoff{})

	rec := serve(chain, httptest.NewRequest(http.MethodGet, platformPath, nil))
	if rec.Code != http.StatusOK || next.served != 1 {
		t.Fatalf("запрос без носителя: код %d, звеньев %d — ожидался анонимный проход", rec.Code, next.served)
	}
	if got := next.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "" {
		t.Fatalf("личность выставлена без носителя: %q", got)
	}
	if foreignAsked.Load() != 0 || reader.asked != 0 {
		t.Fatalf("читатели спрошены без носителя: чужой %d, наш %d", foreignAsked.Load(), reader.asked)
	}
	t.Logf("перепись: обращений к чужому читателю %d · к нашему %d", foreignAsked.Load(), reader.asked)
}

// ─────────────────────────────────────────────────────────────────────────────
// ЗНАМЕНАТЕЛЬ. Прежняя редакция этих проб брала два пути — путь платформы и
// «кто я», — и оба свойством обладали. Утверждение «наш носитель решает на
// каждом своём исходе» было доказано на подмножестве, которое его и так несёт:
// узкая проба, а не ложная. Пути формы входа ведут себя иначе по устройству —
// на них исход судит служба, — и именно там старшинство доставалось
// предъявителю.
//
// Перечень путей теперь ВЫВОДИТСЯ из объявления (`LoginLaneRoutes`), а не
// выписывается: глагол, дописанный к полосе формы, попадает под пробу сам.

// allBrowserPaths — ВСЕ пути, на которых край читает браузерную сессию:
// глаголы формы входа, путь платформы и «кто я».
func allBrowserPaths() []string {
	out := make([]string, 0, len(LoginLaneRoutes())+2)
	for _, rt := range LoginLaneRoutes() {
		out = append(out, rt.Path)
	}
	return append(out, platformPath, "/iam/v1/auth/me")
}

// На КАЖДОМ пути мёртвый наш носитель при живом чужом не отдаёт запрос чужой
// стороне: обращений к чужому читателю ноль, чужой личности не возникает.
func TestBothCarriersPresented_ADeadOwnCarrierYieldsNothingToTheForeignLaneOnEveryPath(t *testing.T) {
	paths := allBrowserPaths()
	if len(paths) == 0 {
		t.Fatal("перечень путей пуст — проба прошла бы по нулю путей и молчала")
	}
	var foreignAsked atomic.Int64
	provider := countingProviderStub(t, &foreignAsked)
	reader := &fakeHumanSession{found: false} // наша сессия снята выходом либо истекла
	chain, next := bothCarriersChain(t, provider.URL, reader, &fakeCutoff{})

	foreignPrincipals := 0
	for _, path := range paths {
		next.lastReq = nil
		before := foreignAsked.Load()
		serve(chain, withForeignCarrier(
			withOurCarrier(httptest.NewRequest(http.MethodGet, path, nil), "ours-dead"),
			"foreign-live"))
		if got := foreignAsked.Load() - before; got != 0 {
			t.Errorf("%s: чужой читатель спрошен %d раз при предъявленном нашем носителе — "+
				"полосу выбрал предъявитель, а не край", path, got)
		}
		if next.lastReq != nil &&
			next.lastReq.Header.Get(principalmeta.HeaderPrincipalID) == "usr-foreign" {
			foreignPrincipals++
			t.Errorf("%s: за нашим носителем действует ЧУЖАЯ личность usr-foreign", path)
		}
	}
	t.Logf("перепись: путей браузерной сессии в перечне %d (глаголов формы %d + путь платформы + «кто я») · "+
		"пройдено %d · обращений к чужому читателю %d · чужих личностей %d",
		len(paths), len(LoginLaneRoutes()), len(paths), foreignAsked.Load(), foreignPrincipals)
}

// Законный близнец того же знаменателя: БЕЗ нашего носителя чужая сессия
// работает на каждом пути, который её вообще читает. Без этой половины
// предыдущая проба зеленела бы и на крае, который чужую полосу снял совсем.
func TestBothCarriersWired_TheForeignLaneStillWorksOnEveryPathWithoutOurCarrier(t *testing.T) {
	var foreignAsked atomic.Int64
	provider := countingProviderStub(t, &foreignAsked)
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	chain, _ := bothCarriersChain(t, provider.URL, reader, &fakeCutoff{})

	paths := allBrowserPaths()
	for _, path := range paths {
		serve(chain, withForeignCarrier(httptest.NewRequest(http.MethodGet, path, nil), "foreign-live"))
	}
	if foreignAsked.Load() == 0 {
		t.Fatalf("чужой читатель не спрошен НИ РАЗУ на %d путях без нашего носителя — переходное "+
			"состояние перестало быть переходным", len(paths))
	}
	t.Logf("перепись: путей пройдено %d · обращений к чужому читателю %d", len(paths), foreignAsked.Load())
}
