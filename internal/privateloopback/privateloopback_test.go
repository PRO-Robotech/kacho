// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package privateloopback

import (
	"crypto/rand"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// countingHandler — обработчик пробы: считает КАЖДОЕ прибытие, без разбора пути
// и звонящего. Ровно так считают серверы проб, ради которых пакет заведён.
func countingHandler(n *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
}

// sharedLoopbackTarget — адрес, который знает ПОСТОРОННИЙ звонящий: общий адрес
// петли и порт сервера. Так спрашивает клиент, переживший свой закрытый сервер,
// когда ядро отдало его порт следующему.
func sharedLoopbackTarget(t *testing.T, srv *httptest.Server, path string) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return u.Scheme + "://" + net.JoinHostPort("127.0.0.1", u.Port()) + path
}

// askAsAStranger — постороннее обращение; отказ соединения — законный исход
// (посторонний и должен не доходить), поэтому ошибка не утверждается.
func askAsAStranger(t *testing.T, target string) {
	t.Helper()
	// Посторонний не проверяет, к кому пришёл: ему нужен любой ответ по порту.
	c := &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
	}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	require.NoError(t, err)
	if resp, derr := c.Do(req); derr == nil {
		_ = resp.Body.Close()
	}
}

// TestStrangerKnowingOnlyThePortDoesNotReachTheServer — посторонний, знающий порт
// сервера, но не его адрес, до сервера не доходит; законный звонящий — доходит,
// и каждое его прибытие считается.
//
// Законный близнец формы постороннего — тот же вопрос к серверу httptest на общем
// адресе петли: он ДОХОДИТ. Без близнеца «не дошло» было бы верно и для
// посторонней формы, которая не доходит никуда, — и проба не отличала бы свойство
// пакета от собственной поломки. Близнец считает только прибытия со случайной
// меткой этой пробы: сам он слушает общий адрес, и чужие прибытия туда приходят
// законно — засчитать их близнецу значило бы подтвердить посылку постороннего,
// которой не было. «Не меньше одного», а не «ровно одно»: метку нельзя угадать, её
// можно только повторить, увидев, — то есть повтор возможен лишь после того, как
// наш вопрос дошёл, и посылку не подделывает.
func TestStrangerKnowingOnlyThePortDoesNotReachTheServer(t *testing.T) {
	mark := "/stranger-" + rand.Text()
	var marked atomic.Int32
	twin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == mark {
			marked.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(twin.Close)
	askAsAStranger(t, sharedLoopbackTarget(t, twin, mark))
	require.Positive(t, marked.Load(),
		"предпосылка: посторонняя форма доходит до сервера на общем адресе петли")

	for name, start := range map[string]func(http.Handler) *httptest.Server{
		"NewServer": func(h http.Handler) *httptest.Server { return NewServer(t, h) },
		"NewUnstartedServer+Start": func(h http.Handler) *httptest.Server {
			s := NewUnstartedServer(t, h)
			s.Start()
			return s
		},
		"NewUnstartedServer+StartTLS": func(h http.Handler) *httptest.Server {
			s := NewUnstartedServer(t, h)
			s.StartTLS()
			return s
		},
	} {
		t.Run(name, func(t *testing.T) {
			var n atomic.Int32
			srv := start(countingHandler(&n))
			t.Cleanup(srv.Close)

			askAsAStranger(t, sharedLoopbackTarget(t, srv, "/"))
			askAsAStranger(t, sharedLoopbackTarget(t, srv, mark))
			require.Zero(t, n.Load(),
				"посторонний, знающий только порт, не доходит до сервера на своём адресе петли")

			// Законный звонящий: адрес сервера целиком, любой путь.
			for _, path := range []string{"/", "/.well-known/jwks.json", "/any/derived/path"} {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
				require.NoError(t, err)
				resp, err := srv.Client().Do(req)
				require.NoError(t, err)
				_ = resp.Body.Close()
			}
			require.Equal(t, int32(3), n.Load(),
				"каждое прибытие по адресу сервера доходит до обработчика и считается")
		})
	}
}

// TestServerAddressIsALoopbackAddressOtherThanTheShared — адрес сервера лежит в
// петле, но не совпадает с общим 127.0.0.1. На Linux это обязательно: вся сеть
// 127.0.0.0/8 там принадлежит петле, и запасной путь на общий адрес был бы
// молчаливой потерей свойства.
func TestServerAddressIsALoopbackAddressOtherThanTheShared(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Logf("платформа %s: собственный адрес петли не гарантируется, свойство судится только на linux", runtime.GOOS)
	}
	srv := NewServer(t, http.NotFoundHandler())
	t.Cleanup(srv.Close)
	host, _, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	ip := net.ParseIP(host)
	require.NotNil(t, ip, "адрес слушателя — IP: %s", host)
	require.True(t, ip.IsLoopback(), "адрес слушателя лежит в петле: %s", ip)
	if runtime.GOOS == "linux" {
		require.False(t, ip.Equal(net.IPv4(127, 0, 0, 1)),
			"адрес слушателя — не общий адрес петли: %s", ip)
	}
}
