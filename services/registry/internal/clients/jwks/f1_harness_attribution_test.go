// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_harness_attribution_test.go — свойство оснастки, на котором держится каждое
// утверждение вида «обращений к источнику ноль» (F1-22, F1-46 и соседние).
package jwks

import (
	"net"
	"net/http"
	"net/url"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestKeySetCountsEveryArrivalAtItsAddressAndNoStrangerReachesIt — счёт источника
// полон и чист одновременно.
//
// Полон: каждое прибытие на адрес источника считается, какой бы путь оно ни
// спрашивало. Обращение продукта к хосту источника по адресу, выведенному из
// записи (стандартный путь набора) или из издателя, — тот самый дефект, который
// утверждения «обращений ноль» обязаны видеть; счёт, отбирающий прибытия по пути,
// вычитал бы его вместе с посторонними.
//
// Чист: посторонний до источника не доходит. Порт петли — ресурс машины: закрытый
// сервер соседней пробы освобождает его, ядро отдаёт его следующему, и клиент,
// переживший свой сервер, спрашивает 127.0.0.1:<порт>. Источник слушает
// собственный адрес петли, поэтому такое обращение к нему не приходит — и в счёт
// не попадает не потому, что его отбросили, а потому, что его нет.
func TestKeySetCountsEveryArrivalAtItsAddressAndNoStrangerReachesIt(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	origin, err := url.Parse(ks.url())
	require.NoError(t, err)
	if runtime.GOOS == "linux" {
		require.NotEqual(t, "127.0.0.1", origin.Hostname(),
			"предпосылка: источник слушает собственный адрес петли, а не общий")
	}

	stranger := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	for _, path := range []string{"/", "/.well-known/jwks.json"} {
		target := "http://" + net.JoinHostPort("127.0.0.1", origin.Port()) + path
		req, rerr := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		require.NoError(t, rerr)
		// Отказ соединения — законный исход: посторонний и должен не доходить.
		if resp, derr := stranger.Do(req); derr == nil {
			_ = resp.Body.Close()
		}
	}
	require.Zero(t, ks.fetches.Load(),
		"посторонний, знающий только порт источника, не доходит до источника")

	for _, path := range []string{"/.well-known/jwks.json", "/kaname.kacho.local"} {
		req, rerr := http.NewRequestWithContext(t.Context(), http.MethodGet, origin.Scheme+"://"+origin.Host+path, nil)
		require.NoError(t, rerr)
		resp, derr := ks.srv.Client().Do(req)
		require.NoError(t, derr)
		_ = resp.Body.Close()
	}
	require.Equal(t, int32(2), ks.fetches.Load(),
		"прибытие на адрес источника по выведенному пути считается: иначе обращение продукта не туда выглядело бы как отсутствие обращения")

	v := newVerifier(t, ourPair(ks))
	sub, err := v.Verify(t.Context(),
		ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute)))
	require.NoError(t, err)
	require.Equal(t, "sva-1", sub)
	require.Equal(t, int32(3), ks.fetches.Load(),
		"обращение проверяющего по объявленному адресу считается")
}
