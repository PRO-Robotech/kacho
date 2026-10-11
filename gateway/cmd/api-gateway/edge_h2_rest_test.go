// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/authnrefusal"
	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// kacho#3125. Внешние слушатели края делят один порт между нативным gRPC и
// REST мультиплексором. Клиент, выбравший по ALPN (или заранее, без TLS) HTTP/2,
// с REST-запросом без учётных данных обязан получить 401 с телом отказа
// целиком — а не обрыв соединения. Тот же порт обязан по-прежнему обслуживать
// gRPC.
//
// Сборка — та же, что у main.go, теми же функциями, а не копией: edgeTLSConfig,
// newEdgeCmux с боевым бюджетом, newEdgeHTTPServer и serveEdgeMux (разделение
// порта, обёртка происхождения, подъём обоих серверов); перед REST — настоящий
// слой аутентификации в боевой посадке. Своего у пробы — только обработчик за
// аутентификацией и сервер gRPC со службой здоровья. Перепись
// TestEdgeH2REST_EdgeAssemblyHasASingleHome держит, что корень не собирает
// сервер, не делит порт и не зовёт Serve мимо этих функций, а вызовов
// serveEdgeMux у него ровно столько, сколько здесь вариантов слушателя.
//
// Метки происхождения пробы берут готовыми из пакета-владельца listenerorigin.
// Что держат его пробы: поведение установщиков и читателей метки —
// listenerorigin_test.go; что экспортированных установщиков метки «внутренний»
// ровно столько, сколько в его ведомости, — перепись
// TestListenerOriginMarkSettersMatchTheLedger (mark_setter_census_test.go).
// Гейты сборки края (edge_assembly_*_test.go) запрещают звать установщики вне
// корня; новый установщик в самом пакете ловит перепись владельца, а не они.

// edgeListenerKind — вариант внешнего слушателя корня.
type edgeListenerKind string

const (
	edgeListenerTLS   edgeListenerKind = "tls"   // cfg.TLSListenAddr: tls.Listen → cmux
	edgeListenerPlain edgeListenerKind = "plain" // cfg.ListenAddr: net.Listen → cmux
)

var probedEdgeListeners = []edgeListenerKind{edgeListenerTLS, edgeListenerPlain}

type edgeUnderTest struct {
	addr string
	pool *x509.CertPool // nil у открытого слушателя
}

func serveEdgeListener(t *testing.T, kind edgeListenerKind) edgeUnderTest {
	t.Helper()
	var (
		l    net.Listener
		pool *x509.CertPool
		err  error
	)
	switch kind {
	case edgeListenerTLS:
		ca := newTestCA(t, "kacho-edge-test-ca")
		certFile, keyFile := ca.issueLeaf(t, leafOpts{
			commonName:  "api-gateway",
			ipAddresses: []net.IP{net.ParseIP("127.0.0.1")},
			isServer:    true,
		})
		cert, loadErr := tls.LoadX509KeyPair(certFile, keyFile)
		require.NoError(t, loadErr)
		l, err = tls.Listen("tcp", "127.0.0.1:0", edgeTLSConfig(cert))
		pool = x509.NewCertPool()
		pool.AddCert(ca.cert)
	case edgeListenerPlain:
		l, err = net.Listen("tcp", "127.0.0.1:0")
	default:
		t.Fatalf("неизвестный вариант слушателя %q", kind)
	}
	require.NoError(t, err)

	m := newEdgeCmux(l, edgeFirstByteBudget)

	grpcSrv := grpc.NewServer(grpc.Creds(linktls.ServerCredentials()))
	healthgrpc.RegisterHealthServer(grpcSrv, health.NewServer())

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := middleware.NewAuthInterceptor(middleware.AuthModeProduction, "", nil, logger)
	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Публичный путь: отвечает, помечено ли соединение внешним — метка
		// обязана доезжать и по HTTP/2, иначе церемонии на нём отказывают.
		if listenerorigin.OnExternalListener(r.Context()) {
			_, _ = io.WriteString(w, "external "+r.Proto)
			return
		}
		_, _ = io.WriteString(w, "unmarked "+r.Proto)
	})
	httpSrv := newEdgeHTTPServer(auth.HTTP(public))

	go func() {
		// Исход пробы судится по ответам клиенту: отказ сервера виден как
		// оборванный запрос, а журнал после конца теста писать некуда.
		_ = serveEdgeMux(m, grpcSrv, httpSrv, func(string, error) {})
	}()
	t.Cleanup(func() {
		_ = httpSrv.Close()
		grpcSrv.Stop()
		m.Close()
		_ = l.Close()
	})
	return edgeUnderTest{addr: l.Addr().String(), pool: pool}
}

// restTransport — транспорт ровно одного протокола: HTTP/2 (по ALPN поверх TLS
// либо заранее известный без TLS) или HTTP/1.1. Транспорт HTTP/2 — тот, что
// входит в стандартную библиотеку (сборка golang.org/x/net/http2).
func restTransport(e edgeUnderTest, h2 bool) *http.Transport {
	var p http.Protocols
	switch {
	case h2 && e.pool != nil:
		p.SetHTTP2(true)
	case h2:
		p.SetUnencryptedHTTP2(true)
	default:
		p.SetHTTP1(true)
	}
	tr := &http.Transport{Protocols: &p}
	if e.pool != nil {
		tr.TLSClientConfig = &tls.Config{RootCAs: e.pool, MinVersion: tls.VersionTLS12}
	}
	return tr
}

// restAnswer — ответ края, прочитанный целиком.
type restAnswer struct {
	proto string
	code  int
	body  string
}

// h2RequestsPerConn — сколько запросов проба шлёт по ОДНОМУ соединению HTTP/2.
//
// Один запрос не судит соединение: сервер может ответить на HEADERS раньше,
// чем прочтёт следующий за ними кадр, и отказ соединению (GOAWAY) придёт уже
// после ответа — проба зеленела бы на сломанном соединении в доле прогонов,
// зависящей от планировщика. Клиент подтверждает каждый полученный SETTINGS,
// прочитав его, — до того, как прочтёт ответ на первый запрос, потому что
// SETTINGS идут в соединении раньше ответа. Значит, второй запрос уходит в
// соединение ПОСЛЕ всех подтверждений, и сервер читает его, только разобрав
// их: сломанное подтверждениями соединение не отвечает на второй запрос ни
// при каком порядке горутин. Третий и проверка состояния после — запас и
// утверждение, что соединение по-прежнему открыто.
const h2RequestsPerConn = 3

// h2Exchange шлёт h2RequestsPerConn запросов GET path по одному соединению
// HTTP/2 и утверждает, что соединение после них открыто и принимает запросы.
func h2Exchange(t *testing.T, e edgeUnderTest, path string) []restAnswer {
	t.Helper()
	scheme := "http"
	if e.pool != nil {
		scheme = "https"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cc, err := restTransport(e, true).NewClientConn(ctx, scheme, e.addr)
	require.NoError(t, err, "соединение HTTP/2 не открылось")
	defer func() { _ = cc.Close() }()
	answers := make([]restAnswer, 0, h2RequestsPerConn)
	for i := 1; i <= h2RequestsPerConn; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.url(path), nil)
		require.NoError(t, err)
		resp, err := cc.RoundTrip(req)
		require.NoError(t, err, "запрос %d из %d по одному соединению HTTP/2 оборван", i, h2RequestsPerConn)
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, err, "запрос %d из %d: тело ответа оборвано", i, h2RequestsPerConn)
		answers = append(answers, restAnswer{proto: resp.Proto, code: resp.StatusCode, body: string(body)})
	}
	require.NoError(t, cc.Err(), "после %d запросов соединение HTTP/2 закрыто", h2RequestsPerConn)
	require.Positive(t, cc.Available(), "после %d запросов соединение HTTP/2 не принимает новых (получен GOAWAY)", h2RequestsPerConn)
	return answers
}

// http1Get — один запрос по HTTP/1.1.
func http1Get(t *testing.T, e edgeUnderTest, path string) restAnswer {
	t.Helper()
	c := &http.Client{Transport: restTransport(e, false), Timeout: 10 * time.Second}
	resp, err := c.Get(e.url(path))
	require.NoError(t, err, "запрос по HTTP/1.1 оборван")
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "тело ответа оборвано")
	return restAnswer{proto: resp.Proto, code: resp.StatusCode, body: string(body)}
}

func (e edgeUnderTest) url(path string) string {
	if e.pool != nil {
		return "https://" + e.addr + path
	}
	return "http://" + e.addr + path
}

func expectedRefusal(t *testing.T) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	authnrefusal.WriteHTTP(rec)
	require.NotEmpty(t, rec.Body.String(), "производитель отказа не написал тела — сравнивать не с чем")
	return rec.Code, rec.Body.String()
}

// Предмет: анонимный REST-запрос по HTTP/2 получает отказ аутентификации
// целиком. Близнец по протоколу — тот же запрос по HTTP/1.1.
func TestEdgeH2REST_AnonymousRequestGetsTheWholeRefusal(t *testing.T) {
	wantCode, wantBody := expectedRefusal(t)
	for _, kind := range probedEdgeListeners {
		for _, h2 := range []bool{true, false} {
			name := string(kind) + "/http1"
			wantProto := "HTTP/1.1"
			if h2 {
				name = string(kind) + "/h2"
				wantProto = "HTTP/2.0"
			}
			t.Run(name, func(t *testing.T) {
				e := serveEdgeListener(t, kind)
				answers := []restAnswer{}
				if h2 {
					answers = h2Exchange(t, e, "/iam/v1/me")
				} else {
					answers = append(answers, http1Get(t, e, "/iam/v1/me"))
				}
				for i, a := range answers {
					require.Equal(t, wantProto, a.proto, "запрос %d: клиент не согласовал проверяемый протокол — проба судила бы не то", i+1)
					require.Equal(t, wantCode, a.code, "запрос %d по %s на слушателе %s", i+1, wantProto, kind)
					require.Equal(t, wantBody, a.body, "запрос %d по %s на слушателе %s", i+1, wantProto, kind)
				}
			})
		}
	}
}

// Метка внешнего слушателя доезжает до обработчика по HTTP/2 так же, как по
// HTTP/1.1: публичный путь проходит аутентификацию и видит метку.
func TestEdgeH2REST_ExternalMarkReachesTheHandlerOverH2(t *testing.T) {
	for _, kind := range probedEdgeListeners {
		t.Run(string(kind), func(t *testing.T) {
			e := serveEdgeListener(t, kind)
			for i, a := range h2Exchange(t, e, "/healthz") {
				require.Equal(t, http.StatusOK, a.code, "запрос %d", i+1)
				require.Equal(t, "external HTTP/2.0", a.body, "запрос %d", i+1)
			}
		})
	}
}

// Близнец: нативный gRPC на том же порту продолжает работать.
func TestEdgeH2REST_GRPCOnTheSamePortStillWorks(t *testing.T) {
	for _, kind := range probedEdgeListeners {
		t.Run(string(kind), func(t *testing.T) {
			e := serveEdgeListener(t, kind)
			creds := insecure.NewCredentials()
			if e.pool != nil {
				creds = credentials.NewTLS(&tls.Config{RootCAs: e.pool, MinVersion: tls.VersionTLS12})
			}
			conn, err := grpc.NewClient(e.addr, grpc.WithTransportCredentials(creds))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// Два вызова одним соединением: второй идёт уже после обмена
			// SETTINGS, то есть соединение пережило рукопожатие целиком.
			for i := 0; i < 2; i++ {
				resp, callErr := healthgrpc.NewHealthClient(conn).Check(ctx, &healthgrpc.HealthCheckRequest{})
				require.NoError(t, callErr)
				require.Equal(t, healthgrpc.HealthCheckResponse_SERVING, resp.GetStatus())
			}
		})
	}
}

// dialEdgeRaw открывает соединение HTTP/2 без клиента HTTP: поверх TLS с ALPN
// h2 либо открытое.
func dialEdgeRaw(t *testing.T, e edgeUnderTest) net.Conn {
	t.Helper()
	if e.pool != nil {
		c, err := tls.Dial("tcp", e.addr, &tls.Config{RootCAs: e.pool, NextProtos: []string{"h2"}, MinVersion: tls.VersionTLS12})
		require.NoError(t, err)
		return c
	}
	c, err := net.Dial("tcp", e.addr)
	require.NoError(t, err)
	return c
}

// Предмет (kacho#3125, круг 2): соединение, заявившее до первого HEADERS кадр
// длиннее наибольшего допустимого, закрывается с FRAME_SIZE_ERROR, а не держит
// буфер по заявленной длине до таймаута. Таких соединений много, и все они
// анонимны: аутентификация до HEADERS не наступает. Наблюдаемое — прирост кучи,
// пока соединения открыты, и GOAWAY с кодом отказа на каждом.
func TestEdgeH2REST_OversizedFrameBeforeHeadersIsRefusedWithoutBuffering(t *testing.T) {
	const (
		conns = 50
		// Потолок прироста кучи на соединение: TLS-соединение, горутины
		// сервера и буферы разбора — единицы КиБ; буфер по заявленной длине —
		// 1 МиБ.
		perConnCeiling = 128 << 10
	)
	announced := 1<<20 - 1
	hdr := []byte{byte(announced >> 16), byte(announced >> 8), byte(announced), 0xfa, 0, 0, 0, 0, 0}
	for _, kind := range probedEdgeListeners {
		t.Run(string(kind), func(t *testing.T) {
			e := serveEdgeListener(t, kind)
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			cs := make([]net.Conn, 0, conns)
			for i := 0; i < conns; i++ {
				c := dialEdgeRaw(t, e)
				t.Cleanup(func() { _ = c.Close() })
				_, err := c.Write([]byte(http2.ClientPreface))
				require.NoError(t, err)
				fr := http2.NewFramer(c, nil)
				require.NoError(t, fr.WriteSettings())
				_, err = c.Write(hdr)
				require.NoError(t, err)
				cs = append(cs, c)
			}
			// Время серверу дочитать заголовки кадров: и матчеру, и серверу
			// HTTP/2, если соединение до него дошло.
			time.Sleep(300 * time.Millisecond)
			runtime.GC()
			runtime.ReadMemStats(&after)
			growth := int64(after.HeapInuse) - int64(before.HeapInuse)
			t.Logf("соединений %d с заявленным кадром %d Б: прирост кучи %d КиБ (потолок %d КиБ)",
				conns, announced, growth/1024, conns*perConnCeiling/1024)
			require.LessOrEqual(t, growth, int64(conns*perConnCeiling),
				"анонимное соединение с заявленным длинным кадром держит буфер по его длине")

			for i, c := range cs {
				require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
				fr := http2.NewFramer(io.Discard, c)
				var goAway *http2.GoAwayFrame
				for goAway == nil {
					f, err := fr.ReadFrame()
					require.NoError(t, err, "соединение %d: сервер не отказал кадру до таймаута чтения", i)
					goAway, _ = f.(*http2.GoAwayFrame)
				}
				require.Equal(t, http2.ErrCodeFrameSize, goAway.ErrCode, "соединение %d: код отказа", i)
			}
		})
	}
}
