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
	"time"

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

	// Фабрика берёт t той подпробы, в которой зовётся: отказ подъёма обязан уронить
	// её, а не родителя из чужой горутины.
	for name, start := range map[string]func(testing.TB, http.Handler) *httptest.Server{
		"NewServer": NewServer,
		"NewUnstartedServer+Start": func(tb testing.TB, h http.Handler) *httptest.Server {
			s := NewUnstartedServer(tb, h)
			s.Start()
			return s
		},
		"NewUnstartedServer+StartTLS": func(tb testing.TB, h http.Handler) *httptest.Server {
			s := NewUnstartedServer(tb, h)
			s.StartTLS()
			return s
		},
	} {
		t.Run(name, func(t *testing.T) {
			var n atomic.Int32
			srv := start(t, countingHandler(&n))
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

// TestStrangerKnowingOnlyThePortIsNotAcceptedByListen — то же свойство для
// слушателя, на котором служит не httptest (сервер gRPC, свой http.Server):
// соединение постороннего, знающего только порт, слушатель не принимает, соединение
// по адресу слушателя целиком — принимает.
//
// Судится ПРИНЯТИЕ, а не исход набора номера: на общем адресе с тем же портом может
// законно слушать кто-то ещё, и «соединился» тогда не говорило бы о нашем слушателе
// ничего. Законный близнец формы постороннего — тот же набор номера к слушателю на
// общем адресе: его соединение принимается.
func TestStrangerKnowingOnlyThePortIsNotAcceptedByListen(t *testing.T) {
	twin, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = twin.Close() })
	dialAsAStranger(t, twin.Addr())
	require.True(t, acceptsWithin(t, twin, time.Second),
		"предпосылка: посторонняя форма доходит до слушателя на общем адресе петли")

	l := Listen(t)
	t.Cleanup(func() { _ = l.Close() })
	dialAsAStranger(t, l.Addr())
	require.False(t, acceptsWithin(t, l, 200*time.Millisecond),
		"посторонний, знающий только порт, не принят слушателем на своём адресе петли")

	c, err := net.DialTimeout("tcp4", l.Addr().String(), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.True(t, acceptsWithin(t, l, time.Second),
		"соединение по адресу слушателя целиком принимается")
}

// dialAsAStranger — набор номера по общему адресу петли и порту слушателя; отказ
// соединения — законный исход, поэтому он не утверждается.
func dialAsAStranger(t *testing.T, addr net.Addr) {
	t.Helper()
	_, port, err := net.SplitHostPort(addr.String())
	require.NoError(t, err)
	if c, derr := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", port), time.Second); derr == nil {
		t.Cleanup(func() { _ = c.Close() })
	}
}

// acceptsWithin — принял ли слушатель соединение за отведённое время.
func acceptsWithin(t *testing.T, l net.Listener, d time.Duration) bool {
	t.Helper()
	tl, ok := l.(*net.TCPListener)
	require.True(t, ok, "слушатель TCP: %T", l)
	require.NoError(t, tl.SetDeadline(time.Now().Add(d)))
	c, err := tl.Accept()
	if err != nil {
		var ne net.Error
		require.ErrorAs(t, err, &ne, "отказ приёма — только по сроку: %v", err)
		require.True(t, ne.Timeout(), "отказ приёма — только по сроку: %v", err)
		return false
	}
	_ = c.Close()
	return true
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
