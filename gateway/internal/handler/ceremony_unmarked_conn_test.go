// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ceremony_unmarked_conn_test.go — координата церемонии с соединения БЕЗ метки
// происхождения не ретранслируется: отказ по умолчанию (возврат go-style GS-1
// по kacho#2721).
//
// Запись цели, отвечающей только на внешних слушателях, ретранслируется лишь
// тогда, когда соединение принял слушатель, обёрнутый
// `listenerorigin.ExternalListener`. Соединение, которое не пометила ни одна
// обёртка, получает ответ `notHere` — тот же, что на внутреннем слушателе.
// Потерянная обёртка слушателя даёт отказ церемонии, а не её ретрансляцию.
package handler_test

import (
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// stubConn — принятое соединение пробы. `listenerorigin.ConnContext` читает
// только цепочку типов соединения, поэтому методы ему не нужны.
type stubConn struct{ net.Conn }

// oneConnListener — слушатель пробы: отдаёт заглушку соединения на Accept.
type oneConnListener struct{ net.Listener }

func (oneConnListener) Accept() (net.Conn, error) { return stubConn{}, nil }

// onExternalListener — запрос так, как его видит обработчик, когда соединение
// принял слушатель, обёрнутый `listenerorigin.ExternalListener` (так корень
// оборачивает оба внешних HTTP-слушателя). Метку ставят та же обёртка и тот же
// `listenerorigin.ConnContext`, что в корне: другого способа её поставить нет.
func onExternalListener(req *http.Request) *http.Request {
	conn, err := listenerorigin.ExternalListener(oneConnListener{}).Accept()
	if err != nil {
		panic(err)
	}
	return req.WithContext(listenerorigin.ConnContext(req.Context(), conn))
}

func TestCeremonyRelay_L13_AnUnmarkedConnectionIsNotRelayed(t *testing.T) {
	issuance := &formListenerStub{status: http.StatusFound, respHdr: http.Header{"Location": {consoleCallback}}}
	mounted := newEdgeUnderOwn(t, issuance, true)

	var walked int
	for _, rt := range middleware.LoginLaneRoutes() {
		if !rt.Target.ExternalListenersOnly() {
			continue
		}
		// Близнец — запрос с соединения, помеченного обёрткой внешнего
		// слушателя: ретранслируется. Различие с отказом ниже — одна метка.
		beforeT, beforeI := mounted.transcoder.served.Load(), issuance.count()
		req, _, _ := forgedFormRequest(rt)
		ext := mounted.serveUnmarked(onExternalListener(req))
		require.Equal(t, beforeI+1, issuance.count(),
			"запись %q с соединения внешнего слушателя не дошла до слушателя выдачи (код %d)", rt.Verb, ext.Code)
		require.Equal(t, beforeT, mounted.transcoder.served.Load(), "запись %q с соединения внешнего слушателя ушла под `/`", rt.Verb)

		// Соединение без метки: ответ обработчика под `/`, до слушателя выдачи 0.
		beforeT, beforeI = mounted.transcoder.served.Load(), issuance.count()
		req, _, _ = forgedFormRequest(rt)
		got := mounted.serveUnmarked(req)
		require.Equal(t, beforeI, issuance.count(),
			"запись %q с соединения без метки ретранслирована на слушатель выдачи (код %d)", rt.Verb, got.Code)
		require.Equal(t, beforeT+1, mounted.transcoder.served.Load(),
			"запись %q с соединения без метки не получила ответ обработчика под `/` (код %d)", rt.Verb, got.Code)
		walked++
	}
	require.Positive(t, walked, "в объявлении нет записей, отвечающих только на внешних слушателях — проба судит пустоту")
	t.Logf("перепись: записей только-внешних слушателей %d · ретранслировано близнецом с меткой %d",
		walked, issuance.count())
}
