// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_harness_attribution_test.go — свойство оснастки, на котором держится каждое
// утверждение вида «обращений к источнику ноль» (F1-22, F1-46 и соседние).
package jwks

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestKeySetCountsOnlyArrivalsAtItsDeclaredAddress — счётчик обращений источника
// приписывает проверяющему только то, что пришло по ОБЪЯВЛЕННОМУ адресу записи.
//
// Порт петли — ресурс машины, а не пробы. Закрытый сервер освобождает порт, ядро
// отдаёт его следующему httptest.NewServer, и тот, кто продолжает спрашивать
// прежний адрес (проба «источник недоступен» в соседнем процессе, клиент с
// переподключением), попадает сюда. Счётчик, считающий всякое прибытие на порт,
// записал бы такое обращение проверяющему, и проба покраснела бы на продукте,
// который не ходил никуда.
//
// Посторонний знает порт, но не объявленный адрес: он спрашивает корень либо
// адрес СВОЕГО источника. Обе формы подаются ниже; законный близнец — обращение
// проверяющего по объявленному адресу — обязан считаться.
func TestKeySetCountsOnlyArrivalsAtItsDeclaredAddress(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	neighbour := newKeySet(t)
	neighbourURL, err := url.Parse(neighbour.url())
	require.NoError(t, err)

	foreign := map[string]string{
		"корень порта":                    ks.srv.URL + "/",
		"адрес чужого источника на порту": ks.srv.URL + neighbourURL.EscapedPath(),
	}
	for name, target := range foreign {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoErrorf(t, err, "предпосылка (%s): порт источника принимает соединения", name)
		_ = resp.Body.Close()
	}
	// Не меньше, а не ровно: прибытия мимо объявленного адреса открыты всей машине,
	// и в прогоне под нагрузкой к поданным здесь добавляются чужие.
	require.GreaterOrEqual(t, ks.foreign.Load(), int32(len(foreign)),
		"предпосылка: каждое постороннее прибытие дошло до этого источника")
	require.Zero(t, ks.fetches.Load(),
		"постороннее прибытие на порт источника не является обращением проверяющего")

	v := newVerifier(t, ourPair(ks))
	sub, err := v.Verify(context.Background(),
		ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute)))
	require.NoError(t, err)
	require.Equal(t, "sva-1", sub)
	require.Equal(t, int32(1), ks.fetches.Load(),
		"обращение проверяющего по объявленному адресу считается")
}
