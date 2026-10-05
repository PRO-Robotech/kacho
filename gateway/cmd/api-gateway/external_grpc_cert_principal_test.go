// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// external_grpc_cert_principal_test.go — ЧЕЙ СЕРТИФИКАТ НА ВНЕШНЕМ gRPC КРАЯ
// СТАНОВИТСЯ ЛИЧНОСТЬЮ (kacho#3028, перепись каналов, строки 30 и 33).
//
// Проба стоит на проводке процесса, а не на перехватчике: полоса личности —
// withCertPrincipalLane, политика слушателя — Config.ExternalListenerClientAuth,
// мультиплексор — newEdgeCmux, сервер — proxy.NewServer с учётными данными
// linktls.ServerCredentials, ровно как в main() (что сервер корня несёт именно
// их, держит TestListenerOriginWiring_GRPCServerCarriesTheLinkCredentials).
// Рукопожатие настоящее, клиентский сертификат предъявляется по сети.
//
// Предмет. Лист с SPIFFE-именем установки, предъявленный без токена,
// служебной учёткой не становится: такой лист выпускает кластерный
// выпускающий всякому, кто заводит запрос на сертификат в любом пространстве
// имён (строка 27), и полоса по нему открыла бы службу без удостоверения
// снаружи (строка 33). Лист звена фронта — тем более (строка 30): звено
// ретранслирует запросы всех своих клиентов.
//
// Близнецы. (1) Тот же путь без сертификата — тот же отказ: проба не может
// покраснеть оттого, что отказывает всё подряд, ведь сертификатные случаи
// сравниваются с ним, а исход «прошёл» виден в перехватчике за полосой.
// (2) Сертификат ДОШЁЛ до сервера: перехватчик перед полосой видит TLS
// соединения — отказ не от того, что рукопожатие не донесло лист.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/operations"
	"github.com/soheilhy/cmux"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

type certPrincipalNoLookup struct{}

func (certPrincipalNoLookup) LookupByExternalID(context.Context, string) (middleware.Subject, error) {
	return middleware.Subject{}, status.Error(codes.Unauthenticated, "no lookup in this probe")
}

// certPrincipalEdge — внешний gRPC края, собранный функциями корня.
type certPrincipalEdge struct {
	addr     string
	serverCA *testCA
	// seen — что увидели перехватчики: тип учётных данных пира ДО полосы и
	// личность ПОСЛЕ неё (пусто — до обработчика запрос не дошёл).
	seen chan certPrincipalSeen
}

type certPrincipalSeen struct {
	authType  string
	principal string
}

func startCertPrincipalEdge(t *testing.T, instCA, linkCA *testCA) certPrincipalEdge {
	t.Helper()
	cfg := config.Config{
		AuthNTrustDomain:        "kacho.cloud",
		HybridMTLSExternal:      true,
		MTLSCAFile:              instCA.caFile(t),
		AuthZTrustedProxyCAFile: linkCA.caFile(t),
	}
	serverCA := newTestCA(t, "edge-server-ca")
	certFile, keyFile := serverCA.issueLeaf(t, leafOpts{
		commonName: "edge", ipAddresses: []net.IP{net.ParseIP("127.0.0.1")}, isServer: true,
	})
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	tlsCfg, err := cfg.ExternalListenerClientAuth(&tls.Config{
		Certificates: []tls.Certificate{cert}, NextProtos: []string{"h2", "http/1.1"}, MinVersion: tls.VersionTLS12,
	})
	require.NoError(t, err)
	links, err := cfg.TrustedProxyLinkAnchor()
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := withCertPrincipalLane(
		middleware.NewAuthInterceptor(middleware.AuthModeProductionStrict, "", certPrincipalNoLookup{}, logger),
		cfg, linktls.NewAnchor(links...), logger)

	seen := make(chan certPrincipalSeen, 4)
	before := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		authType := "<нет пира>"
		if p, ok := peer.FromContext(ss.Context()); ok && p.AuthInfo != nil {
			authType = p.AuthInfo.AuthType()
		}
		err := h(srv, ss)
		select {
		case seen <- certPrincipalSeen{authType: authType}:
		default:
		}
		return err
	}
	after := func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, _ grpc.StreamHandler) error {
		p := operations.PrincipalFromContext(ss.Context())
		select {
		case seen <- certPrincipalSeen{principal: p.Type + "/" + p.ID}:
		default:
		}
		// Дальше прокси не идёт: бэкендов в пробе нет, и исход «полоса
		// пропустила» читается по личности, а не по ответу бэкенда.
		return status.Error(codes.FailedPrecondition, "probe: passed authentication")
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	require.NoError(t, err)
	m := newEdgeCmux(ln, edgeFirstByteBudget)
	grpcL := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	srv := proxy.NewServer(proxy.Resolver(proxy.Backends{}),
		grpc.Creds(linktls.ServerCredentials()),
		grpc.ChainStreamInterceptor(before, auth.Stream(), after))
	go func() { _ = srv.Serve(grpcL) }()
	go func() { _ = m.Serve() }()
	t.Cleanup(func() { srv.Stop(); _ = ln.Close() })
	return certPrincipalEdge{addr: ln.Addr().String(), serverCA: serverCA, seen: seen}
}

// call — вызов метода маршрутизируемой формы с сертификатом (или без) и без
// токена. Возвращает код и что увидели перехватчики.
func (e certPrincipalEdge) call(t *testing.T, leaf *tls.Certificate) (codes.Code, certPrincipalSeen) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(e.serverCA.cert)
	cc := &tls.Config{ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12, RootCAs: roots}
	if leaf != nil {
		cc.Certificates = []tls.Certificate{*leaf}
	}
	conn, err := grpc.NewClient(e.addr, grpc.WithTransportCredentials(credentials.NewTLS(cc)))
	require.NoError(t, err)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	callErr := conn.Invoke(ctx, "/kacho.cloud.vpc.v1.NetworkService/Get", &emptypb.Empty{}, &emptypb.Empty{})
	var got certPrincipalSeen
	for i := 0; i < 2; i++ {
		select {
		case s := <-e.seen:
			if s.authType != "" {
				got.authType = s.authType
			}
			if s.principal != "" {
				got.principal = s.principal
			}
		case <-time.After(time.Second):
		}
	}
	return status.Code(callErr), got
}

func TestExternalGRPC_NoClientCertificateBecomesAPrincipalWithoutAToken(t *testing.T) {
	instCA := newTestCA(t, "kacho-internal-ca")
	linkCA := newTestCA(t, "api-gateway-front-link-ca")
	edge := startCertPrincipalEdge(t, instCA, linkCA)

	leafOf := func(ca *testCA, uri string) *tls.Certificate {
		certFile, keyFile := ca.issueLeaf(t, leafOpts{commonName: "probe", uriSANs: []string{uri}, dnsNames: []string{"console-front"}})
		c, err := tls.LoadX509KeyPair(certFile, keyFile)
		require.NoError(t, err)
		return &c
	}

	// Близнец (1): без сертификата — отказ полосы.
	code, seen := edge.call(t, nil)
	require.Equal(t, codes.Unauthenticated, code, "без сертификата и токена: увидено %+v", seen)

	for _, c := range []struct {
		name, row string
		leaf      *tls.Certificate
	}{
		{"лист установки с SPIFFE-именем", "33",
			leafOf(instCA, "spiffe://kacho.cloud/ns/tenant-ns/sa/kacho-vpc")},
		{"лист звена фронта", "30",
			leafOf(linkCA, "spiffe://kacho.cloud/ns/kacho/sa/console-front")},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, seen := edge.call(t, c.leaf)
			// Близнец (2): сертификат дошёл до сервера по TLS.
			if seen.authType != "tls" {
				t.Fatalf("строка %s: перехватчик перед полосой увидел учётные данные %q, ожидался tls — "+
					"проба не проверила бы ничего", c.row, seen.authType)
			}
			if code != codes.Unauthenticated || seen.principal != "" {
				t.Errorf("строка %s: %s без токена прошёл полосу личности как %q (код %v) — "+
					"клиентский сертификат на внешнем gRPC края стал личностью", c.row, c.name, seen.principal, code)
			}
		})
	}
}

// Проба выше держит СБОРЩИК полосы; что корень включает полосу именно им —
// держит эта (учётные данные сервера корня держит
// TestListenerOriginWiring_GRPCServerCarriesTheLinkCredentials). Полоса,
// включённая в main() мимо withCertPrincipalLane, вернула бы строку 30 при
// зелёной пробе сборщика. main() из пробы не исполним, поэтому провязка
// судится по разбору корня (как TestCompositionRoot_MountsTheAuthenticationFloor).
func TestCompositionRoot_CertLaneComesFromTheProbedBuilder(t *testing.T) {
	calls := calledFunctions(rootFile(t))
	t.Logf("корень: withCertPrincipalLane=%d WithMTLSPrincipal=%d NewAuthInterceptor=%d",
		calls["withCertPrincipalLane"], calls["WithMTLSPrincipal"], calls["NewAuthInterceptor"])
	// Предпосылка: корень по-прежнему строит перехватчик личности.
	if calls["NewAuthInterceptor"] == 0 {
		t.Fatal("main.go больше не строит перехватчик личности — проба потеряла предмет; " +
			"перенаправь её туда, где он теперь собирается, а не снимай")
	}
	if calls["withCertPrincipalLane"] != 1 {
		t.Errorf("корень зовёт withCertPrincipalLane %d раз, ожидался 1 — полоса личности по сертификату "+
			"собирается только сборщиком, который держит проба на рукопожатии", calls["withCertPrincipalLane"])
	}
	if calls["WithMTLSPrincipal"] != 0 {
		t.Errorf("корень зовёт WithMTLSPrincipal сам (%d) — полоса собрана мимо сборщика, и чей сертификат "+
			"становится личностью, проба больше не видит", calls["WithMTLSPrincipal"])
	}
}
