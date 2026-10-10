// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"go/ast"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
// Сборка — та же, что у main.go: edgeTLSConfig, newEdgeCmux с боевым бюджетом,
// splitEdgeCmux, edgeHTTPProtocols, обёртка происхождения и ConnContext; перед
// REST — настоящий слой аутентификации в боевой посадке. Перепись в
// TestEdgeH2REST_EveryExternalRESTListenerIsProbed держит, что вариантов
// внешнего слушателя у корня ровно столько, сколько здесь проб.

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
	grpcL, restL := splitEdgeCmux(m)

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
	httpSrv := &http.Server{
		Handler:           auth.HTTP(public),
		ReadHeaderTimeout: 10 * time.Second,
		ConnContext:       linktls.WithConnState(listenerorigin.ConnContext),
		Protocols:         edgeHTTPProtocols(),
	}

	go func() { _ = grpcSrv.Serve(grpcL) }()
	go func() { _ = httpSrv.Serve(listenerorigin.ExternalListener(restL)) }()
	go func() { _ = m.Serve() }()
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

// Перепись: каждый внешний REST-слушатель корня получен из splitEdgeCmux, и
// вариантов таких слушателей столько, сколько вариантов покрыто пробами выше.
// Слушатель, обслуживаемый не через splitEdgeCmux, — находка: его REST по
// HTTP/2 пробой не покрыт.
func TestEdgeH2REST_EveryExternalRESTListenerIsProbed(t *testing.T) {
	fset, f := parseMain(t)
	fromSplit := map[string]bool{}
	var external int
	var unsplit []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) == 2 && len(n.Rhs) == 1 {
				if call, ok := n.Rhs[0].(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "splitEdgeCmux" {
						if rest, ok := n.Lhs[1].(*ast.Ident); ok {
							fromSplit[rest.Name] = true
						}
					}
				}
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Serve" || len(n.Args) != 1 {
				return true
			}
			if recv, ok := sel.X.(*ast.Ident); !ok || recv.Name != "httpSrv" {
				return true
			}
			wrap, ok := n.Args[0].(*ast.CallExpr)
			if !ok || originFunc(wrap.Fun) != "ExternalListener" || len(wrap.Args) != 1 {
				return true
			}
			external++
			arg, ok := wrap.Args[0].(*ast.Ident)
			if !ok || !fromSplit[arg.Name] {
				unsplit = append(unsplit, fset.Position(n.Pos()).String())
			}
		}
		return true
	})
	t.Logf("перепись main.go: внешних REST-слушателей %d · из splitEdgeCmux %d · вариантов под пробой %d",
		external, external-len(unsplit), len(probedEdgeListeners))
	if external == 0 {
		t.Fatal("в main.go не найдено ни одного внешнего REST-слушателя — перепись судит пустоту")
	}
	if len(unsplit) > 0 {
		t.Fatalf("внешний REST-слушатель обслуживается не из splitEdgeCmux — его HTTP/2 не покрыт пробой: %v", unsplit)
	}
	if external != len(probedEdgeListeners) {
		t.Fatalf("внешних REST-слушателей %d, вариантов под пробой %d — новый слушатель заведён без пробы",
			external, len(probedEdgeListeners))
	}
}
