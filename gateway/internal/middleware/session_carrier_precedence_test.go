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
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// ─────────────────────────────────────────────────────────────────────────────
// НАШ НОСИТЕЛЬ НЕ УЕЗЖАЕТ ЧУЖОЙ СТОРОНЕ.
//
// Значение нашего носителя предъявительское: кто его держит, тот и предъявляет
// сессию. До состояния «оба» два печенья не могли жить в одном браузере, и
// передача заголовка `Cookie` целиком ничего не выносила. Теперь это
// объявленное состояние, и второй путь того же класса — откат «оба» → «только
// чужой»: наш носитель остаётся в браузерах при снятом читателе, и тогда его
// несёт каждый запрос.
//
// Дублёр ЗАПИСЫВАЕТ полученный заголовок: свойство читается из наблюдения, а
// не из кода.

// recordingProviderStub — чужая сторона, запоминающая КАЖДЫЙ полученный `Cookie`.
func recordingProviderStub(t *testing.T, got *[]string, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*got = append(*got, r.Header.Get("Cookie"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"active":true,"authenticated_at":"`+
			ownAuthAt.UTC().Format(time.RFC3339Nano)+
			`","identity":{"id":"kid-foreign","traits":{"email":"foreign@example.com"}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const ourBearerValue = "OURS-LIVE-BEARER"

func TestForeignLane_NeverCarriesOurBearerToTheForeignSide(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	provider := recordingProviderStub(t, &seen, &mu)

	// Состояние отката: наш читатель СНЯТ, наш носитель в браузере остался.
	// Здесь чужая полоса обязана сработать — и обязана уйти без нашего значения.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	foreignSubject := cutoffLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}
	a := NewAuthInterceptor(AuthModeDev, "", foreignSubject, logger).
		WithKratos(NewKratosClient(provider.URL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour)
	who := NewSessionIdentityHandler(logger).
		WithKratos(NewKratosClient(provider.URL), foreignSubject).
		WithSessionCutoff(&fakeCutoff{})
	mux := http.NewServeMux()
	who.Register(mux)
	mux.Handle("/", &countingNext{})
	chain := a.HTTP(mux)

	paths := []string{platformPath, "/iam/v1/auth/me"}
	for _, path := range paths {
		serve(chain, withForeignCarrier(
			withOurCarrier(httptest.NewRequest(http.MethodGet, path, nil), ourBearerValue),
			"foreign-live"))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatalf("чужая сторона не спрошена ни разу на %d путях — проба судила бы о непроисходившем",
			len(paths))
	}
	leaked := 0
	for _, hdr := range seen {
		if strings.Contains(hdr, ourBearerValue) || strings.Contains(hdr, OurSessionCarrierName) {
			leaked++
			t.Errorf("чужой стороне ушёл наш носитель в заголовке %q: значение предъявительское, "+
				"и державший его предъявляет нашу сессию", hdr)
		}
		if !strings.Contains(hdr, providerSessionCarrierName) {
			t.Errorf("чужой стороне ушёл заголовок БЕЗ её печенья (%q) — она не смогла бы ответить", hdr)
		}
	}
	t.Logf("перепись: обращений к чужой стороне %d · несущих значение нашего носителя %d · путей %d",
		len(seen), leaked, len(paths))
}

// Граница имени. Предикат присутствия чужого носителя искал ПОДСТРОКУ в
// заголовке: имя, оказавшееся ЧАСТЬЮ чужого имени печенья, считалось
// предъявлением. Половины парные — и сужение обязано не съесть законное.
func TestProviderCarrierPredicate_MatchesTheNameAndNotItsSubstring(t *testing.T) {
	cases := []struct {
		name    string
		cookies []*http.Cookie
		want    bool
	}{
		{"своё имя — предъявлено", []*http.Cookie{{Name: providerSessionCarrierName, Value: "v"}}, true},
		{"рядом с нашим — предъявлено", []*http.Cookie{
			{Name: OurSessionCarrierName, Value: "o"},
			{Name: providerSessionCarrierName, Value: "v"},
		}, true},
		{"имя как ПРИСТАВКА чужого печенья", []*http.Cookie{
			{Name: providerSessionCarrierName + "_debug", Value: "v"}}, false},
		{"имя как ОКОНЧАНИЕ чужого печенья", []*http.Cookie{
			{Name: "x_" + providerSessionCarrierName, Value: "v"}}, false},
		{"имя в ЗНАЧЕНИИ чужого печенья", []*http.Cookie{
			{Name: "note", Value: providerSessionCarrierName}}, false},
		{"печенья нет вовсе", nil, false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, platformPath, nil)
		for _, c := range tc.cookies {
			req.AddCookie(c)
		}
		if got := providerSessionCarrierPresented(req); got != tc.want {
			t.Errorf("%s: предикат ответил %v, ожидалось %v (заголовок %q)",
				tc.name, got, tc.want, req.Header.Get("Cookie"))
		}
	}
	t.Logf("перепись: форм заголовка проверено %d · положительных 2 · отрицательных 4", len(cases))
}

// Решение, ставшее наблюдаемым вместе с разбором по имени: значение, которого
// браузер провести НЕ МОЖЕТ, носителем не является — ни нашим, ни чужим.
//
// Выбор fail-closed и назван вслух: принять неразбираемое значение значило бы
// вынести соседу то, чего мы сами не прочитали, а отказ здесь неотличим для
// человека от «сессии нет» — состояния, в котором он и находится, раз его
// печенье до нас не доехало целым.
func TestCarrierPredicates_AValueNoBrowserCanFrameIsNotACarrier(t *testing.T) {
	// Октеты вне RFC 6265 для значения печенья: разбор их отвергает, а клиент
	// Go при отправке печатает «dropping invalid bytes».
	const unframeable = "знач;ение"
	req := httptest.NewRequest(http.MethodGet, platformPath, nil)
	req.Header.Set("Cookie",
		OurSessionCarrierName+"="+unframeable+"; "+providerSessionCarrierName+"="+unframeable)

	if _, ours := ourSessionCarrierOf(req); ours {
		t.Error("наш носитель признан предъявленным на значении, которого браузер провести не может")
	}
	if providerSessionCarrierPresented(req) {
		t.Error("чужой носитель признан предъявленным на значении, которого браузер провести не может")
	}
	if got := ProviderSessionCarrierHeader(req); got != "" {
		t.Errorf("чужой стороне собран заголовок %q из непрочитанного значения", got)
	}

	// Положительная половина: то же имя с ПРОВОДИМЫМ значением — носитель.
	// Без неё проба зеленела бы и на предикатах, отвергающих всё подряд.
	ok := httptest.NewRequest(http.MethodGet, platformPath, nil)
	ok.AddCookie(&http.Cookie{Name: OurSessionCarrierName, Value: "v-own"})
	ok.AddCookie(&http.Cookie{Name: providerSessionCarrierName, Value: "v-foreign"})
	if _, ours := ourSessionCarrierOf(ok); !ours {
		t.Fatal("наш носитель с проводимым значением не признан — предикат отвергает законный вход")
	}
	if !providerSessionCarrierPresented(ok) {
		t.Fatal("чужой носитель с проводимым значением не признан — предикат отвергает законный вход")
	}
	t.Logf("перепись: сторон проверено 2 · непроводимых значений отвергнуто 2 · проводимых принято 2")
}

// ─────────────────────────────────────────────────────────────────────────────
// ПОЛ ВТОРОГО ФАКТОРА НЕ ПОВЫШАЕТСЯ СНЯТИЕМ НАШЕГО НОСИТЕЛЯ.
//
// Свойство названо ровно тем, чем оно является, — и не шире. Две сессии одного
// человека суть две РАЗНЫЕ аутентификации, и требовать, чтобы уровень вовсе не
// зависел от того, какая из них предъявлена, значило бы требовать, чтобы
// аутентификации не различались. Требуется другое, и оно о полномочии:
// СНЯТИЕ нашего носителя не может ПОДНЯТЬ уровень, доехавший до замка.
//
// Иначе пол второго фактора выбирал бы предъявитель: удалив у себя одно
// печенье, человек проходил бы замок, которого не проходил, — и переходное
// состояние стало бы способом обойти второй фактор, а не способом не потерять
// вход.
//
// Механизм: в состоянии «оба» чужая полоса уезжает с ПУСТЫМ уровнем. Пустой
// ранжируется нулём — положительного пола он не удовлетворяет, нулевого не
// касается, — то есть обычный доступ чужой сессии сохраняется, а НОВОЕ
// полномочие берётся только через нашу чеканку. Это та же форма, которой наша
// полоса отвечает на уровень вне оси, и второго механизма не заводится.

// stepUpFloorPath — глагол с ПОЛОЖИТЕЛЬНЫМ полом в каталоге прав.
const stepUpFloorPath = "/iam/v1/users/usr-abc/tokens"

// mfaProviderStub — чужая сторона, называющая уровень `aal2`.
func mfaProviderStub(t *testing.T, asked *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"active":true,"authenticated_at":"`+
			ownAuthAt.UTC().Format(time.RFC3339Nano)+
			`","authenticator_assurance_level":"aal2",`+
			`"identity":{"id":"kid-foreign","traits":{"email":"foreign@example.com"}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBothCarriers_DroppingOurCarrierCannotRaiseTheAuthenticationFloor(t *testing.T) {
	var asked atomic.Int64
	provider := mfaProviderStub(t, &asked)
	// Наша сессия уровня «1», чужая — `aal2`: тот самый перекос, на котором
	// снятие печенья становилось повышением.
	sess := liveOwnSession()
	sess.AssuranceLevel = "1"
	reader := &fakeHumanSession{found: true, sess: sess}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	foreignSubject := cutoffLookup{subj: Subject{Type: "user", ID: "usr-own-1", DisplayName: "A"}}
	both := NewAuthInterceptor(AuthModeDev, "", foreignSubject, logger).
		WithHumanSession(reader).
		WithKratos(NewKratosClient(provider.URL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour).
		WithTransitionalCarrierWindow(ownAuthAt.Add(time.Hour))

	withOurs := serveForACR(t, both, withForeignCarrier(
		withOurCarrier(httptest.NewRequest(http.MethodGet, stepUpFloorPath, nil), "ours-live"),
		"foreign-live"))
	withoutOurs := serveForACR(t, both, withForeignCarrier(
		httptest.NewRequest(http.MethodGet, stepUpFloorPath, nil), "foreign-live"))

	if rank(withoutOurs) > rank(withOurs) {
		t.Fatalf("снятие нашего носителя ПОДНЯЛО уровень: с нашим %q, без него %q — пол второго "+
			"фактора выбирает предъявитель", withOurs, withoutOurs)
	}
	t.Logf("перепись: состояние «оба» · уровень с нашим носителем %q · без него %q · "+
		"повышение снятием невозможно", withOurs, withoutOurs)
}

// rank — порядок уровней оси каталога; пустой ранжируется нулём.
func rank(acr string) int {
	switch acr {
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	}
	return 0
}

// serveForACR прогоняет запрос и возвращает уровень, доехавший до следующего
// звена. Отказ замка — тоже исход: уровень при нём пуст либо недостаточен.
func serveForACR(t *testing.T, a *AuthInterceptor, req *http.Request) string {
	t.Helper()
	got := ""
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(principalmeta.HeaderTokenACR)
	})
	serve(a.HTTP(next), req)
	return got
}

// Законный близнец: в состоянии «ТОЛЬКО ЧУЖОЙ» уровень чужой сессии доезжает
// до замка как прежде. Без этой половины зелёное выше достигалось бы снятием
// второго фактора у всех, кто сегодня живёт на чужой посадке, — и выглядело
// бы это как «стало строже».
func TestForeignOnlyState_KeepsTheForeignSecondFactor(t *testing.T) {
	var asked atomic.Int64
	provider := mfaProviderStub(t, &asked)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	foreignOnly := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}, logger).
		WithKratos(NewKratosClient(provider.URL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour)

	got := serveForACR(t, foreignOnly, withForeignCarrier(
		httptest.NewRequest(http.MethodGet, stepUpFloorPath, nil), "foreign-live"))
	if got != "2" {
		t.Fatalf("в состоянии «только чужой» уровень чужой сессии доехал как %q, ожидалось \"2\" — "+
			"второй фактор отнят у тех, кому его нечем заменить", got)
	}
	if n := foreignOnly.SessionLane().Snapshot().TransitionalFloorWithheld; n != 0 {
		t.Fatalf("клетка удержания пола = %d вне переходного состояния, ожидалось 0", n)
	}
	t.Logf("перепись: состояние «только чужой» · уровень, доехавший до замка %q · удержаний пола %d",
		got, foreignOnly.SessionLane().Snapshot().TransitionalFloorWithheld)
}

// ─────────────────────────────────────────────────────────────────────────────
// В ОКНЕ ДОЧИТЫВАЮТСЯ ЖИВЫЕ ЧУЖИЕ СЕССИИ, НОВЫЕ НЕ ЗАВОДЯТСЯ.
//
// Отсечка отзыва на чужой полосе сравнивает НАШ отзыв с моментом
// аутентификации, который называет ЧУЖАЯ сторона. Пока её форма входа
// достижима, отозванный проходит вход заново, получает момент новее нашей
// отсечки — и отзыв снят. Переходное состояние тогда не переходное, а
// постоянная вторая дверь.
//
// Окно поэтому несёт МОМЕНТ СВОЕГО ОТКРЫТИЯ, и чужая сессия принимается, только
// если она СТАРШЕ его. Механизм замкнут краем и не опирается на обещание
// профиля закрыть чужую форму: годная сессия обязана попасть в промежуток
// «новее нашей отсечки, старше открытия окна», и вход заново из него выпадает
// с той стороны, с которой его не подделать — момент называет чужая сторона по
// факту входа.

func windowedChain(t *testing.T, providerURL string, openedAt time.Time, cut SessionCutoffReader) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}, logger).
		WithHumanSession(&fakeHumanSession{found: false}).
		WithKratos(NewKratosClient(providerURL)).
		WithSessionCutoffCheck(cut, time.Hour).
		WithTransitionalCarrierWindow(openedAt)
	return a.HTTP(&countingNext{})
}

// providerStubAt — чужая сторона, называющая ЗАДАННЫЙ момент аутентификации.
func providerStubAt(t *testing.T, at time.Time) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"active":true,"authenticated_at":"`+
			at.UTC().Format(time.RFC3339Nano)+
			`","authenticator_assurance_level":"aal1",`+
			`"identity":{"id":"kid-foreign","traits":{"email":"foreign@example.com"}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTransitionalWindow_AForeignSignInAfterTheWindowOpenedIsNotAdmitted(t *testing.T) {
	windowOpened := ownAuthAt.Add(time.Hour)
	cases := []struct {
		name   string
		authAt time.Time
		admit  bool
	}{
		{"живая сессия СТАРШЕ открытия окна — дочитывается", windowOpened.Add(-time.Minute), true},
		{"вход заново ПОСЛЕ открытия окна — не заводится", windowOpened.Add(time.Minute), false},
		{"ровно в момент открытия — не заводится (граница закрыта)", windowOpened, false},
		{"момента аутентификации нет вовсе — доказать старшинство нечем", time.Time{}, false},
	}
	admitted := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := providerStubAt(t, tc.authAt)
			chain := windowedChain(t, provider.URL, windowOpened, &fakeCutoff{})
			rec := serve(chain, withForeignCarrier(
				httptest.NewRequest(http.MethodGet, platformPath, nil), "foreign"))
			got := rec.Code == http.StatusOK
			if got != tc.admit {
				t.Fatalf("код %d (впущен=%v), ожидалось впущен=%v", rec.Code, got, tc.admit)
			}
			if got {
				admitted++
			}
		})
	}
	t.Logf("перепись: моментов аутентификации проверено %d · впущено %d · отвергнуто %d",
		len(cases), admitted, len(cases)-admitted)
}

// Законный близнец: ОКНО ЗАКРЫТО (состояние «только чужой») — момент
// аутентификации ничем не ограничен, и вход заново работает как прежде. Без
// этой половины зелёное выше добывалось бы запретом входа на чужой посадке,
// где его нечем заменить.
func TestTransitionalWindow_WhenClosedTheForeignSideAdmitsANewSignIn(t *testing.T) {
	fresh := ownAuthAt.Add(48 * time.Hour)
	provider := providerStubAt(t, fresh)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}, logger).
		WithKratos(NewKratosClient(provider.URL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour).
		WithTransitionalCarrierWindow(time.Time{}) // окно закрыто
	rec := serve(a.HTTP(&countingNext{}), withForeignCarrier(
		httptest.NewRequest(http.MethodGet, platformPath, nil), "foreign"))
	if rec.Code != http.StatusOK {
		t.Fatalf("при закрытом окне свежая чужая сессия отвергнута кодом %d — вход отнят там, "+
			"где его нечем заменить", rec.Code)
	}
	t.Logf("перепись: окно закрыто · момент аутентификации на 48ч новее · код %d", rec.Code)
}

// ─────────────────────────────────────────────────────────────────────────────
// ОТКАЗ ОКНА НЕ ЗАПИРАЕТ ЧЕЛОВЕКА СНАРУЖИ.
//
// Граница окна ввелась как отказ, не оканчивающий носитель, и полоса
// исполняется на КАЖДОМ пути. Человек с чужой сессией вне окна получал 401 и
// на глаголе выхода, и на глаголе входа: ни сдать носитель, ни получить новый.
// Состояние держится до ручной чистки браузера, а снять его можно только
// откатом профиля — то есть снятием самого контроля.
//
// ОБА ДОВОДА УЖЕ ЗАПИСАНЫ В ЭТОМ ПАКЕТЕ, и новая граница введена без них:
//
//   - отсечка отвечает отказом ВМЕСТЕ с окончанием носителя: «порознь первое
//     даёт СТОЯЩИЙ отказ» — сессия жива, момент её прежний, и повторной
//     аутентификации ничто не запросит;
//   - у пред-аутентификационного перечня (`isPublicHTTPPath`) сказано прямо:
//     человек, чью сессию отозвали, обязан СОХРАНИТЬ возможность завершить
//     выход.
//
// Знаменатель называется: проверяются ВСЕ пути браузерной сессии, а не путь
// платформы, на котором свойство «отказ» и так очевидно.

// windowRefusedChain — край в окне, чужая сессия заведена ПОСЛЕ его открытия.
func windowRefusedChain(t *testing.T) (http.Handler, *countingNext) {
	t.Helper()
	windowOpened := ownAuthAt
	provider := providerStubAt(t, windowOpened.Add(time.Hour)) // вход заново
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}, logger).
		WithHumanSession(&fakeHumanSession{found: false}).
		WithKratos(NewKratosClient(provider.URL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour).
		WithTransitionalCarrierWindow(windowOpened)
	next := &countingNext{}
	return a.HTTP(next), next
}

// endedBothCarriers — погашены ли ОБА имени ответом.
func endedBothCarriers(res *http.Response) bool {
	ended := map[string]bool{}
	for _, c := range res.Cookies() {
		if c.MaxAge < 0 {
			ended[c.Name] = true
		}
	}
	for _, n := range SessionCarrierNames() {
		if !ended[n] {
			return false
		}
	}
	return true
}

// Половина первая: отказ по границе ОКАНЧИВАЕТ носитель — на каждом пути.
// Иначе браузер предъявляет отвергнутое печенье вечно.
func TestTransitionalWindow_ARefusalEndsTheCarrierOnEveryPath(t *testing.T) {
	paths := allBrowserPaths()
	chain, _ := windowRefusedChain(t)
	kept := 0
	for _, path := range paths {
		rec := serve(chain, withForeignCarrier(
			httptest.NewRequest(http.MethodPost, path, nil), "foreign-after-window"))
		if !endedBothCarriers(rec.Result()) {
			kept++
			t.Errorf("%s: отказ по границе окна не погасил носитель (Set-Cookie: %v) — браузер "+
				"предъявляет отвергнутое печенье на каждом следующем запросе, и состояние держится "+
				"до ручной чистки", path, rec.Result().Header["Set-Cookie"])
		}
	}
	t.Logf("перепись: путей браузерной сессии %d · пройдено %d · оставивших носитель %d",
		len(paths), len(paths), kept)
}

// Половина вторая: на пред-аутентификационных путях отказа НЕТ — человек
// доходит до глагола выхода и до глагола входа.
func TestTransitionalWindow_ARefusalLeavesSignOutAndSignInReachable(t *testing.T) {
	escapes := []string{
		LoginLanePathLogout, // завершить выход
		LoginLanePathLogin,  // начать вход заново
		LoginLanePathCSRF,   // признак формы, без которого вход не начать
		"/oauth/logout",     // выход края
		"/iam/v1/auth/me",   // консоль обязана узнать, что она анонимна
	}
	chain, next := windowRefusedChain(t)
	blocked := 0
	for _, path := range escapes {
		before := next.served
		rec := serve(chain, withForeignCarrier(
			httptest.NewRequest(http.MethodPost, path, nil), "foreign-after-window"))
		if rec.Code == http.StatusUnauthorized || next.served == before {
			blocked++
			t.Errorf("%s: путь выхода закрыт отказом окна (код %d, дошло до звена %v) — человек "+
				"не может ни сдать носитель, ни получить новый, и снять состояние можно только "+
				"откатом профиля, то есть снятием самого контроля",
				path, rec.Code, next.served != before)
		}
	}
	t.Logf("перепись: путей спасения проверено %d · закрытых %d", len(escapes), blocked)
}

// Половина третья, наблюдаемая: браузер, исполнивший гашение, СЛЕДУЮЩИМ
// запросом анонимен и проходит. Без неё первые две можно было бы удовлетворить,
// не дав человеку выйти из состояния.
func TestTransitionalWindow_AfterTheRefusalTheNextRequestIsAnonymousAndPasses(t *testing.T) {
	chain, next := windowRefusedChain(t)
	first := serve(chain, withForeignCarrier(
		httptest.NewRequest(http.MethodGet, platformPath, nil), "foreign-after-window"))
	if !endedBothCarriers(first.Result()) {
		t.Fatal("первый ответ не погасил носитель — второму запросу неоткуда стать анонимным")
	}
	// Браузер исполнил Set-Cookie: печенья больше нет.
	before := next.served
	second := serve(chain, httptest.NewRequest(http.MethodGet, platformPath, nil))
	if second.Code != http.StatusOK || next.served != before+1 {
		t.Fatalf("после гашения запрос без носителя дал %d (дошло до звена %v) — выхода из "+
			"состояния нет", second.Code, next.served != before)
	}
	t.Logf("перепись: запросов 2 · первый отказан и погасил носитель · второй анонимен и прошёл")
}

// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНА, КОТОРУЮ МЫ РЕШИЛИ БОЛЬШЕ НЕ ЗАВОДИТЬ, НЕ ЗАВОДИТ У НАС ЗАПИСЕЙ.
//
// Граница окна стояла ПОСЛЕ резолва субъекта, а резолв на этой полосе умеет
// заводить зеркало лениво. Сессия, которую мы отвергаем, успевала создать у нас
// запись — тем самым действием, которое мы отвергаем. Смысл окна «новых не
// заводим» нарушался буквально, в единственном числе, каким его вообще можно
// нарушить.

// upsertingLookup — резолвер с ленивым заведением зеркала, считающий заведения.
type upsertingLookup struct {
	subj     Subject
	upserted int
	looked   int
}

func (u *upsertingLookup) LookupByExternalID(context.Context, string) (Subject, error) {
	u.looked++
	return u.subj, nil
}

func (u *upsertingLookup) LookupOrUpsertFromKratos(_ context.Context, _, _, _ string) (Subject, error) {
	u.upserted++
	return u.subj, nil
}

func TestTransitionalWindow_ARefusedForeignSessionCreatesNoMirror(t *testing.T) {
	windowOpened := ownAuthAt
	provider := providerStubAt(t, windowOpened.Add(time.Hour)) // вход заново — отвергается
	lookup := &upsertingLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewAuthInterceptor(AuthModeDev, "", lookup, logger).
		WithKratos(NewKratosClient(provider.URL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour).
		WithTransitionalCarrierWindow(windowOpened)

	rec := serve(a.HTTP(&countingNext{}), withForeignCarrier(
		httptest.NewRequest(http.MethodGet, platformPath, nil), "foreign-after-window"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("сессия вне окна не отвергнута (код %d) — проба судила бы не тот исход", rec.Code)
	}
	if lookup.upserted != 0 || lookup.looked != 0 {
		t.Fatalf("отвергнутая сессия обратилась к резолверу: заведений зеркала %d, поисков %d — "+
			"сторона, которую мы решили больше не заводить, продолжает заводить у нас записи "+
			"ТЕМ САМЫМ действием, которое мы отвергаем", lookup.upserted, lookup.looked)
	}
	t.Logf("перепись: заведений зеркала %d · поисков субъекта %d · код %d",
		lookup.upserted, lookup.looked, rec.Code)
}

// Законный близнец: сессия ВНУТРИ окна зеркало заводит, как прежде. Без него
// зелёное выше достигалось бы снятием ленивого заведения вовсе.
func TestTransitionalWindow_AnAdmittedForeignSessionStillResolvesItsSubject(t *testing.T) {
	windowOpened := ownAuthAt.Add(time.Hour)
	provider := providerStubAt(t, ownAuthAt) // старше окна — дочитывается
	lookup := &upsertingLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewAuthInterceptor(AuthModeDev, "", lookup, logger).
		WithKratos(NewKratosClient(provider.URL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour).
		WithTransitionalCarrierWindow(windowOpened)

	next := &countingNext{}
	rec := serve(a.HTTP(next), withForeignCarrier(
		httptest.NewRequest(http.MethodGet, platformPath, nil), "foreign-in-window"))
	if rec.Code != http.StatusOK || next.served != 1 {
		t.Fatalf("живая чужая сессия внутри окна не прошла: код %d", rec.Code)
	}
	if lookup.upserted+lookup.looked == 0 {
		t.Fatal("впущенная сессия не резолвила субъекта — личность взялась бы из ниоткуда")
	}
	t.Logf("перепись: заведений зеркала %d · поисков субъекта %d · код %d",
		lookup.upserted, lookup.looked, rec.Code)
}
