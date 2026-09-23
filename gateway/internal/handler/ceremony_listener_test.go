// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ceremony_listener_test.go — координаты церемонии авторизации НЕ отвечают на
// граничном admin-REST слушателе края (sec-issuance-path-not-elsewhere; возврат
// безопасности круга 1 по kacho#2817, находка M1).
//
// Один `http.Server` края обслуживает все его HTTP-слушатели, и внутренний
// admin-REST слушатель отличается от внешних только меткой происхождения
// соединения (`listenerorigin`). Смонтированная без оглядки на неё запись
// ретранслировалась бы и оттуда: путь выдачи жил бы на втором слушателе.
//
// Пробы ПАРНЫЕ и меняют по одному факту:
//
//   - та же запись, тот же край — внутренний слушатель против внешнего:
//     внутренний отвечает тем, что стоит под `/` (ровно так, как ответил бы на
//     путь, которого у него нет), внешний — ретранслирует;
//   - тот же внутренний слушатель — запись другой цели: запись полосы формы
//     ретранслируется, то есть отказ держится решением О ЦЕЛИ, а не тем, что
//     внутренний слушатель в пробе сломан.
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// onInternalListener — запрос так, как его видит обработчик, когда соединение
// принял внутренний admin-REST слушатель (`listenerorigin.InternalConnContext`).
func onInternalListener(req *http.Request) *http.Request {
	return req.WithContext(listenerorigin.WithInternal(req.Context()))
}

// Решение о цели объявлено, и оно — то, которое судит проба ниже: записи
// слушателя выдачи отвечают только на внешних слушателях.
func TestRelayTarget_L13_TheIssuanceTargetIsExternalListenersOnly(t *testing.T) {
	require.True(t, middleware.RelayTargetIssuance.ExternalListenersOnly(),
		"путь выдачи не монтируется на внутреннем слушателе (sec-issuance-path-not-elsewhere)")
	require.False(t, middleware.RelayTarget("").ExternalListenersOnly(), "нулевое значение целью не является")
}

func TestCeremonyRelay_L13_TheInternalAdminListenerAnswersTheCeremonyAsAPathItDoesNotHave(t *testing.T) {
	issuance := &formListenerStub{status: http.StatusFound, respHdr: http.Header{"Location": {consoleCallback}}}
	mounted := newEdgeUnderOwn(t, issuance, true)
	bare := newEdgeUnderOwn(t, &formListenerStub{status: http.StatusFound}, false)

	var externalOnly, walked int
	for _, rt := range middleware.LoginLaneRoutes() {
		if !rt.Target.ExternalListenersOnly() {
			continue
		}
		externalOnly++

		// (1) Внутренний слушатель: запись уходит под `/`, до слушателя выдачи 0.
		beforeT, beforeI := mounted.transcoder.served.Load(), issuance.count()
		req, _, _ := forgedFormRequest(rt)
		got := mounted.serve(onInternalListener(req))
		require.Equal(t, beforeT+1, mounted.transcoder.served.Load(),
			"запись %q на внутреннем слушателе не ушла под `/` (получено %d)", rt.Verb, got.Code)
		require.Equal(t, beforeI, issuance.count(), "запись %q ретранслирована с внутреннего слушателя на слушатель выдачи", rt.Verb)

		// Ответ — тот, что тот же слушатель даёт, когда записи на нём нет вовсе.
		req, _, _ = forgedFormRequest(rt)
		want := bare.serve(onInternalListener(req))
		require.Equal(t, want.Code, got.Code, "запись %q: код внутреннего слушателя отличается от ответа на несмонтированный путь", rt.Verb)
		require.Equal(t, want.Body.String(), got.Body.String(), "запись %q: тело отличается от ответа на несмонтированный путь", rt.Verb)
		require.Equal(t, want.Result().Header, got.Result().Header, "запись %q: заголовки отличаются от ответа на несмонтированный путь", rt.Verb)

		// (2) Близнец — та же запись на ВНЕШНЕМ слушателе ретранслируется.
		beforeT = mounted.transcoder.served.Load()
		req, _, _ = forgedFormRequest(rt)
		ext := mounted.serve(req)
		require.Equal(t, beforeI+1, issuance.count(), "запись %q на внешнем слушателе не дошла до слушателя выдачи (получено %d)", rt.Verb, ext.Code)
		require.Equal(t, beforeT, mounted.transcoder.served.Load(), "запись %q на внешнем слушателе ушла под `/`", rt.Verb)
		require.Equal(t, rt.Path, issuance.last().path)
		walked++
	}
	require.Positive(t, externalOnly, "в объявлении нет записей, отвечающих только на внешних слушателях — проба судит пустоту")
	stats := mounted.relays[middleware.RelayTargetIssuance].Stats()
	for _, verb := range []string{"authorize", "token"} {
		require.Equal(t, uint64(1), stats.Relayed[verb], "клетка %q: ретранслировано ровно внешним близнецом", verb)
	}

	// (3) Близнец по цели — тот же внутренний слушатель, запись полосы формы:
	// ретранслируется. Отказ выше держится решением о цели записи.
	var formRelayed int
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Target.ExternalListenersOnly() {
			continue
		}
		before := mounted.form.count()
		req, _, _ := forgedFormRequest(rt)
		rec := mounted.serve(onInternalListener(req))
		if mounted.form.count() == before+1 {
			formRelayed++
			continue
		}
		t.Errorf("запись %q цели %q на внутреннем слушателе не ретранслирована (получено %d): внутренний слушатель пробы не отличает цель от поломки",
			rt.Verb, rt.Target, rec.Code)
	}
	require.Positive(t, formRelayed, "близнец по цели ничего не ретранслировал")
	t.Logf("перепись: записей только-внешних %d · судимых парой %d · записей прочих целей, ретранслированных с внутреннего слушателя, %d",
		externalOnly, walked, formRelayed)
}

// Сосед по посадке: без монтажа (посадка external) внутренний слушатель
// отвечает на координату ТЕМ ЖЕ обработчиком — под `/`. Это то, с чем проба
// выше сравнивает; здесь — что сравнение не пустое: обработчик под `/` в
// несмонтированном крае действительно получает запрос.
func TestCeremonyRelay_L13_TheUnmountedTwinReachesTheRootHandler(t *testing.T) {
	bare := newEdgeUnderOwn(t, &formListenerStub{status: http.StatusFound}, false)
	req := httptest.NewRequest(http.MethodGet, middleware.CeremonyPathAuthorize+"?"+ceremonyQuery, nil)
	bare.serve(onInternalListener(req))
	require.Equal(t, int64(1), bare.transcoder.served.Load())
}
