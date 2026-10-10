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

// restClient — клиент ровно одного протокола: HTTP/2 (по ALPN поверх TLS либо
// заранее известный без TLS) или HTTP/1.1. Транспорт HTTP/2 — тот, что входит в
// стандартную библиотеку (сборка golang.org/x/net/http2).
func restClient(e edgeUnderTest, h2 bool) *http.Client {
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
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}
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
				resp, err := restClient(e, h2).Get(e.url("/iam/v1/me"))
				require.NoError(t, err, "REST-запрос без учётных данных по %s на слушателе %s оборван", wantProto, kind)
				defer func() { _ = resp.Body.Close() }()
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err, "тело отказа оборвано")
				require.Equal(t, wantProto, resp.Proto, "клиент не согласовал проверяемый протокол — проба судила бы не то")
				require.Equal(t, wantCode, resp.StatusCode)
				require.Equal(t, wantBody, string(body))
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
			resp, err := restClient(e, true).Get(e.url("/healthz"))
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, "external HTTP/2.0", string(body))
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
