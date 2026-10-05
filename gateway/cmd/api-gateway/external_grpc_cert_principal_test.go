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
// (2) Слушатель ПОТРЕБОВАЛ и ПРОВЕРИЛ лист: перехватчик перед полосой видит
// в состоянии соединения предъявленный лист и проверенную рукопожатием
// цепочку, кончающуюся корнем того якоря, которым лист выпущен. Тип учётных
// данных («tls») для этого мало: он одинаков и у соединения, где слушатель
// клиентского листа не спросил, и у соединения, где спросил и не проверил, —
// на обоих отказ полосы был бы отказом на пустом месте, и проба зеленела бы,
// не проверив ничего.

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
	authType string
	// presented — сколько листов клиент предъявил слушателю; verifiedRoot —
	// корень первой проверенной рукопожатием цепочки (nil — не проверено).
	presented    int
	verifiedRoot *x509.Certificate
	principal    string
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
	auth, err := withCertPrincipalLane(
		middleware.NewAuthInterceptor(middleware.AuthModeProductionStrict, "", certPrincipalNoLookup{}, logger),
		cfg, linktls.NewAnchor(links...), logger)
	require.NoError(t, err)

	seen := make(chan certPrincipalSeen, 4)
	before := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		got := certPrincipalSeen{authType: "<нет пира>"}
		if p, ok := peer.FromContext(ss.Context()); ok && p.AuthInfo != nil {
			got.authType = p.AuthInfo.AuthType()
		}
		// Состояние соединения — каким бы типом его ни положили учётные данные
		// сервера: близнец судит слушатель, а не тип (тип судит предмет).
		st := linktls.PeerState(ss.Context())
		if p, ok := peer.FromContext(ss.Context()); st == nil && ok {
			if ti, isTLS := p.AuthInfo.(credentials.TLSInfo); isTLS {
				st = &ti.State
			}
		}
		if st != nil {
			got.presented = len(st.PeerCertificates)
			if len(st.VerifiedChains) > 0 && len(st.VerifiedChains[0]) > 0 {
				chain := st.VerifiedChains[0]
				got.verifiedRoot = chain[len(chain)-1]
			}
		}
		err := h(srv, ss)
		select {
		case seen <- got:
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
				got.presented = s.presented
				got.verifiedRoot = s.verifiedRoot
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
		issuer    *testCA
		leaf      *tls.Certificate
	}{
		{"лист установки с SPIFFE-именем", "33", instCA,
			leafOf(instCA, "spiffe://kacho.cloud/ns/tenant-ns/sa/kacho-vpc")},
		{"лист звена фронта", "30", linkCA,
			leafOf(linkCA, "spiffe://kacho.cloud/ns/kacho/sa/console-front")},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, seen := edge.call(t, c.leaf)
			// Близнец (2): слушатель потребовал лист и проверил его цепочку до
			// корня выпустившего якоря.
			if seen.authType != "tls" || seen.presented == 0 {
				t.Fatalf("строка %s: до полосы дошло соединение без предъявленного листа "+
					"(учётные данные %q, листов %d) — слушатель клиентского листа не спросил, "+
					"проба не проверила бы ничего", c.row, seen.authType, seen.presented)
			}
			if seen.verifiedRoot == nil || !seen.verifiedRoot.Equal(c.issuer.cert) {
				t.Fatalf("строка %s: лист дошёл до полосы без цепочки, проверенной до корня %q "+
					"(корень проверенной цепочки: %v) — слушатель лист не проверил, "+
					"проба не проверила бы ничего", c.row, c.issuer.cert.Subject.CommonName, rootName(seen.verifiedRoot))
			}
			if code != codes.Unauthenticated || seen.principal != "" {
				t.Errorf("строка %s: %s без токена прошёл полосу личности как %q (код %v) — "+
					"клиентский сертификат на внешнем gRPC края стал личностью", c.row, c.name, seen.principal, code)
			}
		})
	}
}

func rootName(c *x509.Certificate) string {
	if c == nil {
		return "<нет>"
	}
	return c.Subject.CommonName
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

// Якорь звеньев полосы — тот же, что объявлен ручкой якоря (строка 30, второй
// слой). Корень передаёт сборщику якорь оператора адреса; передай он пустой
// или иной якорь — лист звена фронта прошёл бы в полосе как «лист установки»
// (Anchor.Foreign отдаёт всякий лист, чей корень не якоря), а проба на
// рукопожатии, строящая якорь сама, этого бы не увидела. Поэтому сборщик
// сверяет якорь с объявленным и отказывает; корень на отказе не стартует —
// это держит процессная проба TestTrustedProxyCircleIsJudgedAtStart
// (близнец «объявленные сеть, звенья и имена — старт»: посадка hybrid с
// якорем звеньев).
func TestCertPrincipalLane_AnchorIsTheDeclaredLinkAnchor(t *testing.T) {
	instCA := newTestCA(t, "kacho-internal-ca")
	linkCA := newTestCA(t, "api-gateway-front-link-ca")
	otherCA := newTestCA(t, "some-other-ca")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfgOf := func(hybrid bool, linkFile string) config.Config {
		return config.Config{
			AuthNTrustDomain: "kacho.cloud", HybridMTLSExternal: hybrid,
			MTLSCAFile: instCA.caFile(t), AuthZTrustedProxyCAFile: linkFile,
		}
	}
	build := func(cfg config.Config, links linktls.Anchor) error {
		_, err := withCertPrincipalLane(
			middleware.NewAuthInterceptor(middleware.AuthModeProductionStrict, "", certPrincipalNoLookup{}, logger),
			cfg, links, logger)
		return err
	}
	linkFile := linkCA.caFile(t)
	for _, c := range []struct {
		name    string
		cfg     config.Config
		links   linktls.Anchor
		refused bool
	}{
		{"пустой якорь при объявленном якоре звеньев", cfgOf(true, linkFile), linktls.Anchor{}, true},
		{"чужой якорь при объявленном якоре звеньев", cfgOf(true, linkFile), linktls.NewAnchor(otherCA.cert), true},
		{"якорь шире объявленного", cfgOf(true, linkFile), linktls.NewAnchor(linkCA.cert, otherCA.cert), true},
		{"якорь при необъявленном якоре звеньев", cfgOf(true, ""), linktls.NewAnchor(otherCA.cert), true},
		{"близнец: объявленный якорь", cfgOf(true, linkFile), linktls.NewAnchor(linkCA.cert), false},
		{"близнец: якорь не объявлен и не передан", cfgOf(true, ""), linktls.Anchor{}, false},
		{"близнец: полоса выключена", cfgOf(false, linkFile), linktls.Anchor{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := build(c.cfg, c.links)
			if !c.refused {
				require.NoError(t, err)
				return
			}
			if err == nil {
				t.Fatal("сборщик собрал полосу личности по сертификату с якорем звеньев, отличным от объявленного — " +
					"лист звена фронта прошёл бы в ней как лист установки (строка 30)")
			}
			require.Contains(t, err.Error(), config.TrustedProxyCAFileKnob, "отказ обязан назвать ручку якоря")
		})
	}
}

// Второй слой сборщика сам по себе: полоса, СОБРАННАЯ withCertPrincipalLane,
// не делает лист звена личностью даже тогда, когда видит состояние TLS
// (первый слой снят). Проба на рукопожатии второго слоя не видит — первый
// гасит полосу раньше, — поэтому якорь, переданный сборщиком полосе, держит
// эта проба: пустой якорь внутри сборщика краснит строку «лист звена».
func TestCertPrincipalLane_LinkLeafIsNotAPrincipalWhenTheLaneSeesTLS(t *testing.T) {
	instCA := newTestCA(t, "kacho-internal-ca")
	linkCA := newTestCA(t, "api-gateway-front-link-ca")
	cfg := config.Config{
		AuthNTrustDomain: "kacho.cloud", HybridMTLSExternal: true,
		MTLSCAFile: instCA.caFile(t), AuthZTrustedProxyCAFile: linkCA.caFile(t),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth, err := withCertPrincipalLane(
		middleware.NewAuthInterceptor(middleware.AuthModeProductionStrict, "", certPrincipalNoLookup{}, logger),
		cfg, linktls.NewAnchor(linkCA.cert), logger)
	require.NoError(t, err)

	chainOf := func(ca *testCA, uri string) [][]*x509.Certificate {
		certFile, keyFile := ca.issueLeaf(t, leafOpts{commonName: "probe", uriSANs: []string{uri}})
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		require.NoError(t, err)
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		require.NoError(t, err)
		return [][]*x509.Certificate{{leaf, ca.cert}}
	}
	call := func(chains [][]*x509.Certificate) (string, codes.Code) {
		ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{HandshakeComplete: true, VerifiedChains: chains}}})
		var got string
		_, err := auth.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/Get"},
			func(hctx context.Context, _ any) (any, error) {
				p := operations.PrincipalFromContext(hctx)
				got = p.Type + "/" + p.ID
				return nil, nil
			})
		return got, status.Code(err)
	}

	if got, code := call(chainOf(linkCA, "spiffe://kacho.cloud/ns/kacho/sa/console-front")); code != codes.Unauthenticated {
		t.Errorf("строка 30: лист звена фронта стал личностью %q (код %v) в полосе, собранной сборщиком корня — "+
			"сборщик не передал полосе якорь звеньев", got, code)
	}
	// Близнец: лист установки в той же полосе — личность (полоса живая, отказ
	// выше — от якоря, а не оттого, что полоса не признаёт ничего).
	if got, code := call(chainOf(instCA, "spiffe://kacho.cloud/ns/kacho/sa/kacho-vpc")); code != codes.OK || got == "/" {
		t.Errorf("близнец: лист установки в полосе, видящей TLS, не стал личностью (личность %q, код %v)", got, code)
	}
}
