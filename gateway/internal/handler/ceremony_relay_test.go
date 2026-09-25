// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ceremony_relay_test.go — краевая половина wiring-пробы разводки координаты
// `/iam/v1/authorize` (замысел LINE-A-1 §5.2, полоса L13; kacho#2817).
//
// Пробы идут через цепочку края в том порядке, в каком её собирает
// композиционный корень: полоса личности → полоса прав → мультиплексор, на
// котором объявление смонтировано `MountLoginLaneRoutes`, а под `/` стоит
// дублёр транскодера REST→gRPC. Слушателя два — формы и выдачи, — и у каждого
// свой дублёр: проба различает, КУДА ушёл запрос, а не только «ушёл ли».
package handler_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// transcoderStandIn — дублёр транскодера под `/`: считает, что дошло.
type transcoderStandIn struct{ served atomic.Int64 }

func (s *transcoderStandIn) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.served.Add(1)
	w.WriteHeader(http.StatusOK)
}

// edgeUnderOwn — край под `own`: два дублёра слушателей, по ретранслятору на
// цель, монтаж объявления, транскодер под `/`, полоса прав с ПУСТЫМ каталогом —
// всякий путь, не освобождённый объявлением, отвергается каталогом до
// следующего звена (промах мимо каталога есть отказ).
type edgeUnderOwn struct {
	chain      http.Handler
	form       *formListenerStub
	issuance   *formListenerStub
	transcoder *transcoderStandIn
	relays     map[middleware.RelayTarget]*handler.LoginLaneRelay
	mux        *http.ServeMux
}

func newEdgeUnderOwn(t *testing.T, issuance *formListenerStub, mount bool) *edgeUnderOwn {
	t.Helper()
	e := &edgeUnderOwn{
		form:       &formListenerStub{status: http.StatusOK, body: `{}`},
		issuance:   issuance,
		transcoder: &transcoderStandIn{},
		relays:     map[middleware.RelayTarget]*handler.LoginLaneRelay{},
	}
	formSrv := httptest.NewServer(e.form)
	t.Cleanup(formSrv.Close)
	issuanceSrv := httptest.NewServer(e.issuance)
	t.Cleanup(issuanceSrv.Close)
	urls := map[middleware.RelayTarget]string{middleware.RelayTargetForm: formSrv.URL, middleware.RelayTargetIssuance: issuanceSrv.URL}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var set []*handler.LoginLaneRelay
	for _, tg := range middleware.RelayTargets() {
		r, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
			Logger: logger, Serves: tg, Target: urls[tg],
			ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
		})
		require.NoError(t, err)
		e.relays[tg] = r
		set = append(set, r)
	}
	e.mux = http.NewServeMux()
	if mount {
		// Тот же обработчик, что стоит под `/`: им внутренний слушатель отвечает
		// на запись, которой на нём нет (корень передаёт ровно так же).
		_, err := handler.MountLoginLaneRoutes(e.mux, e.transcoder, set...)
		require.NoError(t, err)
	}
	e.mux.Handle("/", e.transcoder)

	authz, err := middleware.NewAuthzMiddleware(middleware.AuthzMiddlewareConfig{
		Enabled:         true,
		Catalog:         middleware.NewPermissionCatalog(),
		Subjects:        middleware.NewSubjectExtractor(true),
		Context:         middleware.NewContextExtractor(time.Now, true),
		Resources:       middleware.NewResourceExtractor(nil),
		Checker:         refusingChecker{},
		Logger:          logger,
		CacheTTL:        5 * time.Second,
		CacheMaxEntries: 100,
		PublicAllowlist: middleware.DefaultPublicAllowlist(),
		RestRouter:      middleware.NewRestRouter(),
	})
	require.NoError(t, err)
	a := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", nil, logger).
		WithHumanSession(&fakeOwn{found: false}).
		WithSessionCutoffCheck(&fakeCut{}, time.Hour)
	e.chain = a.HTTP(authz.HTTP(e.mux))
	return e
}

func (e *edgeUnderOwn) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.chain.ServeHTTP(rec, req)
	return rec
}

// refusingChecker — модель прав, отказывающая всем: до неё доходит лишь то, у
// чего нашлась запись каталога, а каталог пуст.
type refusingChecker struct{}

func (refusingChecker) Check(context.Context, middleware.AuthzCheckInput) (middleware.AuthzCheckResult, error) {
	return middleware.AuthzCheckResult{Allowed: false, CheckedAt: time.Now()}, nil
}

// consoleCallback — адрес возврата с кодом, который пишет церемония: ответ
// обязан уйти клиенту КАК ЕСТЬ (З8/З9 — код перенаправления и его цель пишет
// служба, край их не переписывает).
const consoleCallback = "https://console.kacho.local/callback?code=ac-SECRET-0001&state=st-0123456789abcdef"

func authorizeNavigation() *http.Request {
	req := httptest.NewRequest(http.MethodGet, middleware.CeremonyPathAuthorize+"?"+ceremonyQuery, nil)
	req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "s-expired"})
	req.RemoteAddr = "10.0.0.1:4242"
	return req
}

// §5.2, сторона края: бэрый GET резолвится в ретранслятор на слушатель выдачи,
// а не в AuthorizeService; ответ церемонии — 302 с `Location` — уходит как есть.
// Обмен кода уходит в ТОТ ЖЕ ретранслятор.
func TestCeremonyRelay_L13_BarePathReachesTheIssuanceListenerAndTheAnswerPassesAsIs(t *testing.T) {
	issuance := &formListenerStub{status: http.StatusFound, respHdr: http.Header{"Location": {consoleCallback}}}
	e := newEdgeUnderOwn(t, issuance, true)

	rec := e.serve(authorizeNavigation())
	require.Equal(t, http.StatusFound, rec.Code, "навигация обязана получить ответ церемонии: %s", rec.Body.String())
	require.Equal(t, consoleCallback, rec.Result().Header.Get("Location"), "Location церемонии переписан краем")
	require.Empty(t, rec.Result().Header["Set-Cookie"], "край добавил Set-Cookie к ответу церемонии")
	require.Equal(t, 1, issuance.count(), "навигация не дошла до слушателя выдачи")
	got := issuance.last()
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, middleware.CeremonyPathAuthorize, got.path)
	require.Equal(t, ceremonyQuery, got.query, "строка запроса церемонии не доехала как есть")
	require.Contains(t, got.header.Get("Cookie"), middleware.OurSessionCarrierName+"=s-expired",
		"носитель сессии обязан доехать: вопрос о сессии решает церемония, а не край")

	const tokenBody = `{"access_token":"t","token_type":"Bearer"}`
	issuance.answer(http.StatusOK, nil, tokenBody)
	tok := httptest.NewRequest(http.MethodPost, middleware.CeremonyPathToken,
		strings.NewReader("grant_type=authorization_code&code=ac-1&code_verifier=v&client_id=console"))
	tok.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = e.serve(tok)
	require.Equal(t, http.StatusOK, rec.Code, "обмен кода обязан дойти до слушателя выдачи: %s", rec.Body.String())
	require.Equal(t, tokenBody, rec.Body.String())
	require.Equal(t, 2, issuance.count())
	require.Equal(t, middleware.CeremonyPathToken, issuance.last().path)

	require.Zero(t, e.form.count(), "координата церемонии ушла на слушатель формы")
	require.Zero(t, e.transcoder.served.Load(), "координата церемонии ушла в транскодер")
	stats := e.relays[middleware.RelayTargetIssuance].Stats()
	require.Equal(t, uint64(1), stats.Relayed["authorize"])
	require.Equal(t, uint64(1), stats.Relayed["token"])
}

// Метаданные обнаружения (RFC 8414 §3; приёмка LINE-A-1-22, kacho#2721) —
// публичное чтение: запрос БЕЗ носителя и без удостоверения проходит край с
// пустым каталогом и доходит до слушателя выдачи — туда же, куда навигация и
// обмен, а не на слушатель формы и не в транскодер. Документ уходит клиенту как
// есть. Отрицательный близнец в одном факте — соседний документ `/.well-known/`
// вне объявления: его полоса прав отвергает до всякого слушателя.
func TestCeremonyRelay_Discovery_AnonymousReadReachesTheIssuanceListenerAndPassesAsIs(t *testing.T) {
	const document = `{"issuer":"https://kaname.kacho.local","authorization_endpoint":"https://kaname.kacho.local/iam/v1/authorize"}`
	issuance := &formListenerStub{status: http.StatusOK, body: document}
	e := newEdgeUnderOwn(t, issuance, true)

	rec := e.serve(httptest.NewRequest(http.MethodGet, middleware.CeremonyPathDiscovery, nil))
	require.Equal(t, http.StatusOK, rec.Code, "чтение метаданных обнаружения обязано дойти до службы: %s", rec.Body.String())
	require.Equal(t, document, rec.Body.String(), "документ обнаружения переписан краем")
	require.Empty(t, rec.Result().Header["Set-Cookie"], "край добавил Set-Cookie к публичному документу")
	require.Equal(t, 1, issuance.count(), "чтение метаданных не дошло до слушателя выдачи")
	got := issuance.last()
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, middleware.CeremonyPathDiscovery, got.path)
	require.Zero(t, e.form.count(), "метаданные обнаружения ушли на слушатель формы")
	require.Zero(t, e.transcoder.served.Load(), "метаданные обнаружения ушли в транскодер")
	require.Equal(t, uint64(1), e.relays[middleware.RelayTargetIssuance].Stats().Relayed["discovery"])

	for _, neighbour := range []string{"/.well-known/openid-configuration", middleware.CeremonyPathDiscovery + "/iam"} {
		rec = e.serve(httptest.NewRequest(http.MethodGet, neighbour, nil))
		require.NotEqual(t, http.StatusOK, rec.Code, "%s прошёл полосу прав с пустым каталогом — освобождение протекло на соседа", neighbour)
	}
	require.Equal(t, 1, issuance.count(), "соседний документ /.well-known/ дошёл до слушателя выдачи")
	require.Zero(t, e.transcoder.served.Load(), "соседний документ /.well-known/ дошёл до транскодера мимо каталога")
}

// §5.2 ось 1: сосед `:check` НЕ перехвачен — ретранслятор его не видит, до
// слушателей он не доходит, а освобождение церемонии на него не протекает:
// с пустым каталогом полоса прав его отвергает (запись каталога у него есть
// в боевом дереве, и судит его каталог, а не объявление церемонии).
func TestCeremonyRelay_L13_TheCheckNeighbourIsNeitherRelayedNorExempt(t *testing.T) {
	issuance := &formListenerStub{status: http.StatusFound, respHdr: http.Header{"Location": {consoleCallback}}}
	e := newEdgeUnderOwn(t, issuance, true)
	for path := range map[string]bool{"/iam/v1/authorize:check": true, "/iam/v1/authorize:batchCheck": true} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := e.serve(req)
		require.NotEqual(t, http.StatusOK, rec.Code, "%s прошёл полосу прав с пустым каталогом — освобождение протекло на соседа", path)
		require.Zero(t, e.transcoder.served.Load(), "%s дошёл до транскодера мимо каталога", path)
	}
	require.Zero(t, issuance.count(), "сосед :verb ретранслирован на слушатель выдачи")
	require.Zero(t, e.form.count())

	// Положительный близнец: без полосы прав тот же `:check` уходит в
	// транскодер, а не в ретранслятор, — адресат соседа прежний.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/iam/v1/authorize:check", strings.NewReader(`{}`))
	e.mux.ServeHTTP(rec, req)
	require.Equal(t, int64(1), e.transcoder.served.Load(), "`:check` не дошёл до транскодера")
	require.Zero(t, issuance.count())
}

// Условие ПАРНОЕ (§5.2, §5.1а): каждая координата и освобождена, и
// ретранслируема. Близнецы на обе половины — обе дерево уже переживало
// (kacho#2699, kacho#2701).
func TestCeremonyRelay_L13_BothHalvesOfThePairAreLoadBearing(t *testing.T) {
	// (1) Освобождена, но не смонтирована: запрос проходит полосу прав и уходит
	// под `/` — в боевом крае это «не найдено» транскодера, до службы 0.
	issuance := &formListenerStub{status: http.StatusFound, respHdr: http.Header{"Location": {consoleCallback}}}
	e := newEdgeUnderOwn(t, issuance, false)
	rec := e.serve(authorizeNavigation())
	require.Zero(t, issuance.count(), "без монтажа навигация всё равно дошла до слушателя выдачи — проба не различает половину пары")
	require.Equal(t, int64(1), e.transcoder.served.Load(), "без монтажа навигация обязана уйти под `/`, получено %d", rec.Code)

	// (2) Ретранслируема, но не освобождена: путь вне объявления, прикреплённый
	// к ретранслятору, полоса прав отвергает ДО него — каталог записи не несёт.
	issuance2 := &formListenerStub{status: http.StatusFound, respHdr: http.Header{"Location": {consoleCallback}}}
	e2 := newEdgeUnderOwn(t, issuance2, true)
	e2.mux.Handle("/iam/v1/authorizex", e2.relays[middleware.RelayTargetIssuance])
	rec = e2.serve(httptest.NewRequest(http.MethodGet, "/iam/v1/authorizex?"+ceremonyQuery, nil))
	require.NotEqual(t, http.StatusFound, rec.Code)
	require.Zero(t, issuance2.count(), "не освобождённый путь дошёл до слушателя выдачи мимо каталога")

	// Положительный контроль того же края: объявленная координата проходит.
	rec = e2.serve(authorizeNavigation())
	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, 1, issuance2.count())
}

// Ретранслятор цели служит ТОЛЬКО записям своей цели: путь чужой цели на нём —
// ошибка провязки, а не запрос, который стоит ретранслировать.
func TestCeremonyRelay_L13_ARelayServesOnlyItsOwnTargetsRecords(t *testing.T) {
	issuance := &formListenerStub{status: http.StatusFound}
	e := newEdgeUnderOwn(t, issuance, true)
	rec := httptest.NewRecorder()
	e.relays[middleware.RelayTargetForm].ServeHTTP(rec, authorizeNavigation())
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = httptest.NewRecorder()
	e.relays[middleware.RelayTargetIssuance].ServeHTTP(rec, httptest.NewRequest(http.MethodPost, middleware.LoginLanePathLogin, strings.NewReader(`{}`)))
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Zero(t, issuance.count())
	require.Zero(t, e.form.count())
	// Клетки — по записям своей цели и только им.
	for _, rt := range middleware.LoginLaneRoutes() {
		_, own := e.relays[rt.Target].Stats().Relayed[rt.Verb]
		require.True(t, own, "у ретранслятора цели %q нет клетки записи %q", rt.Target, rt.Verb)
		for tg, r := range e.relays {
			if tg == rt.Target {
				continue
			}
			_, foreign := r.Stats().Relayed[rt.Verb]
			require.False(t, foreign, "у ретранслятора цели %q есть клетка чужой записи %q", tg, rt.Verb)
		}
	}
}

// §7 инв. 30, строка 4: предел одной ретрансляции наследуется у механизма —
// названная величина `LoginLaneRelayTimeout`, своей не заводится. Обход
// объявления: у КАЖДОЙ записи — ретранслятор с пределом. Неотвечающий слушатель
// выдачи даёт отказ края в пределах величины, а не висит; отвечающий в срок
// проходит насквозь.
func TestCeremonyRelay_L13_EveryRecordHasABoundedRelayAndAHangingListenerIsRefusedInTime(t *testing.T) {
	require.Equal(t, 10*time.Second, handler.LoginLaneRelayTimeout, "названная величина предела ретрансляции")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inherited := map[middleware.RelayTarget]*handler.LoginLaneRelay{}
	for _, tg := range middleware.RelayTargets() {
		r, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
			Logger: logger, Serves: tg, Target: "https://kaname.kacho.svc:9096", ClientIP: func(*http.Request) string { return "" },
		})
		require.NoError(t, err)
		inherited[tg] = r
	}
	walked := 0
	for _, rt := range middleware.LoginLaneRoutes() {
		walked++
		require.Equal(t, handler.LoginLaneRelayTimeout, inherited[rt.Target].Limit(),
			"запись %q: ретранслятор цели %q без наследованного предела", rt.Verb, rt.Target)
	}
	require.Positive(t, walked, "обход объявления пуст — судить нечего")
	t.Logf("перепись: записей объявления обойдено %d · с пределом %d (%s)", walked, walked, handler.LoginLaneRelayTimeout)

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	hanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(hanging.Close)
	bounded, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
		Logger: logger, Serves: middleware.RelayTargetIssuance, Target: hanging.URL,
		ClientIP: func(*http.Request) string { return "" }, Timeout: 200 * time.Millisecond,
	})
	require.NoError(t, err)
	start := time.Now()
	rec := httptest.NewRecorder()
	bounded.ServeHTTP(rec, authorizeNavigation())
	require.Less(t, time.Since(start), 2*time.Second, "ретрансляция на неотвечающий слушатель висела дольше предела")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.JSONEq(t, `{"code":14,"message":"`+handler.LoginLaneUnreachableMessage+`"}`, rec.Body.String())
	require.Empty(t, rec.Result().Header["Set-Cookie"], "отказ края по пределу не гасит носитель")
	require.Equal(t, uint64(1), bounded.Stats().Unreachable)

	// Положительный близнец: отвечающий в срок проходит насквозь с 302 и Location.
	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", consoleCallback)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(answering.Close)
	timely, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
		Logger: logger, Serves: middleware.RelayTargetIssuance, Target: answering.URL,
		ClientIP: func(*http.Request) string { return "" }, Timeout: 200 * time.Millisecond,
	})
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	timely.ServeHTTP(rec, authorizeNavigation())
	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, consoleCallback, rec.Result().Header.Get("Location"))
}

// §7 инв. 35: кода авторизации в журнале края нет — и это свойство СОХРАНЕНО.
// Ретранслятор пишет в журнал только путь: ни строки запроса навигации (в ней
// `state` и вызов PKCE), ни `Location` ответа с кодом.
func TestCeremonyRelay_L13_TheEdgeLogCarriesNeitherTheQueryNorTheCode(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", consoleCallback)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(answering.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	for _, target := range []string{answering.URL, closedURL} {
		r, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
			Logger: logger, Serves: middleware.RelayTargetIssuance, Target: target, ClientIP: func(*http.Request) string { return "" },
		})
		require.NoError(t, err)
		r.ServeHTTP(httptest.NewRecorder(), authorizeNavigation())
	}
	log := buf.String()
	// Положительный контроль: журнал не пуст — отказ по недостижимости записан,
	// и путь в нём есть.
	require.Contains(t, log, middleware.CeremonyPathAuthorize, "запись о недостижимости не несёт даже пути — проба ничего не видит")
	for _, secret := range []string{"st-0123456789abcdef", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", "ac-SECRET-0001", "code_challenge", "redirect_uri"} {
		require.NotContains(t, log, secret, "журнал края несёт часть строки запроса либо код авторизации")
	}
}
