// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// linktls_test.go — СОСТОЯНИЕ TLS ЗВЕНА ДОХОДИТ ДО ЧИТАТЕЛЯ АДРЕСА (kacho#3028, C4).
//
// Предмет — проверенная цепочка клиентского сертификата соединения, принятого
// TLS-слушателем края за мультиплексором протоколов и меткой происхождения.
// Сервер HTTP заполняет r.TLS только для голого *tls.Conn, а сервер gRPC без
// своих учётных данных вовсе не знает о TLS: за мультиплексором оба видят
// обёртку и состояния не видят. Звено фронта узнаётся по имени в сертификате,
// и до этого пакета имени этого не видел ни один читатель.
//
// Пробы — на настоящем рукопожатии: сервер с якорем и необязательным
// клиентским сертификатом (та же политика, что у внешнего слушателя края),
// клиент со звеньевым сертификатом. Близнец каждого положительного исхода —
// тот же путь без сертификата либо без TLS.
package linktls_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/peer"

	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

const linkSAN = "spiffe://kacho.test/ns/kacho/sa/console-front"

type pki struct {
	pool   *x509.CertPool
	server tls.Certificate
	link   tls.Certificate
}

func newPKI(t *testing.T) pki {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "probe-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	issue := func(serial int64, tmpl *x509.Certificate) tls.Certificate {
		k, kErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if kErr != nil {
			t.Fatal(kErr)
		}
		tmpl.SerialNumber = big.NewInt(serial)
		tmpl.NotBefore, tmpl.NotAfter = time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
		der, cErr := x509.CreateCertificate(rand.Reader, tmpl, ca, &k.PublicKey, caKey)
		if cErr != nil {
			t.Fatal(cErr)
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}
	}
	u, _ := url.Parse(linkSAN)
	p := pki{pool: x509.NewCertPool()}
	p.pool.AddCert(ca)
	p.server = issue(2, &x509.Certificate{
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	p.link = issue(3, &x509.Certificate{
		URIs: []*url.URL{u}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	return p
}

// serverConfig — политика внешнего слушателя края: сертификат клиента
// необязателен, а предъявленный проверяется якорем.
func (p pki) serverConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{p.server}, ClientCAs: p.pool,
		ClientAuth: tls.VerifyClientCertIfGiven, MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	}
}

func (p pki) clientConfig(withLink bool) *tls.Config {
	c := &tls.Config{RootCAs: p.pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
	if withLink {
		c.Certificates = []tls.Certificate{p.link}
	}
	return c
}

func leafURIs(st *tls.ConnectionState) []string {
	if st == nil {
		return nil
	}
	var out []string
	for _, chain := range st.VerifiedChains {
		if len(chain) > 0 {
			for _, u := range chain[0].URIs {
				out = append(out, u.String())
			}
		}
	}
	return out
}

// oneConn — слушатель, отдающий ровно одно заранее принятое соединение.
type oneConn struct {
	net.Listener
	c    net.Conn
	done bool
}

func (l *oneConn) Accept() (net.Conn, error) {
	if l.done {
		return nil, io.EOF
	}
	l.done = true
	return l.c, nil
}

// handshaked — серверная сторона завершённого рукопожатия по настоящему TCP.
func handshaked(t *testing.T, p pki, withLink bool) *tls.Conn {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", p.serverConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, dErr := tls.Dial("tcp", ln.Addr().String(), p.clientConfig(withLink))
		if dErr == nil {
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	raw, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	tc := raw.(*tls.Conn)
	if err := tc.HandshakeContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	return tc
}

// Состояние видно сквозь обе обёртки края: мультиплексор протоколов и метку
// происхождения. Близнец — то же соединение без клиентского сертификата:
// состояние есть, проверенной цепочки нет.
func TestConnState_SeesTheLinkCertificateThroughTheEdgeWrappers(t *testing.T) {
	p := newPKI(t)
	wrapped, err := listenerorigin.ExternalListener(&oneConn{c: &cmux.MuxConn{Conn: handshaked(t, p, true)}}).Accept()
	if err != nil {
		t.Fatal(err)
	}
	if got := leafURIs(linktls.ConnState(wrapped)); len(got) != 1 || got[0] != linkSAN {
		t.Fatalf("имя звена сквозь обёртки края: %v; ожидалось [%s]", got, linkSAN)
	}
	bare, _ := listenerorigin.ExternalListener(&oneConn{c: &cmux.MuxConn{Conn: handshaked(t, p, false)}}).Accept()
	st := linktls.ConnState(bare)
	if st == nil {
		t.Fatal("близнец без сертификата: состояние TLS потеряно")
	}
	if len(st.VerifiedChains) != 0 {
		t.Fatalf("близнец без сертификата: проверенная цепочка %v", leafURIs(st))
	}
}

// Соединение без TLS — состояния нет; рукопожатие не завершено — состояния нет
// (цепочка ещё не проверена, и выдать её авансом значило бы доверять ей).
func TestConnState_NoStateWithoutACompletedHandshake(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	if st := linktls.ConnState(&cmux.MuxConn{Conn: a}); st != nil {
		t.Fatalf("соединение без TLS дало состояние %+v", st)
	}
	if st := linktls.ConnState(tls.Server(a, newPKI(t).serverConfig())); st != nil {
		t.Fatalf("рукопожатие не завершено, а состояние выдано: %+v", st)
	}
}

// gRPC: сервер с учётными данными пакета за мультиплексором отдаёт обработчику
// проверенную цепочку звена через PeerState — и НЕ как credentials.TLSInfo:
// TLSInfo читают как личность клиента (полоса личности по сертификату), и
// выдай его пакет, лист установки стал бы служебной учёткой снаружи
// (linktls.AuthInfo). Близнец — клиент без сертификата: TLS есть, цепочки нет.
func TestServerCredentials_GRPCHandlerSeesTheLinkChain(t *testing.T) {
	p := newPKI(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", p.serverConfig())
	if err != nil {
		t.Fatal(err)
	}
	m := cmux.New(ln)
	grpcL := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	seen := make(chan []string, 4)
	srv := grpc.NewServer(grpc.Creds(linktls.ServerCredentials()),
		grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			pr, _ := peer.FromContext(ctx)
			switch st := linktls.PeerState(ctx); {
			case pr == nil:
				seen <- []string{"<нет пира>"}
			case isTLSInfo(pr.AuthInfo):
				seen <- []string{"<credentials.TLSInfo: состояние видно полосе личности>"}
			case st == nil:
				seen <- []string{"<состояния нет>"}
			default:
				seen <- leafURIs(st)
			}
			return h(ctx, req)
		}))
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(grpcL) }()
	go func() { _ = m.Serve() }()
	t.Cleanup(func() { srv.Stop(); _ = ln.Close() })

	for _, c := range []struct {
		name     string
		withLink bool
		want     []string
	}{
		{"звено с сертификатом", true, []string{linkSAN}},
		{"близнец: без сертификата", false, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			conn, cErr := grpc.NewClient(ln.Addr().String(),
				grpc.WithTransportCredentials(credentials.NewTLS(p.clientConfig(c.withLink))))
			if cErr != nil {
				t.Fatal(cErr)
			}
			defer conn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, cErr = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}); cErr != nil {
				t.Fatal(cErr)
			}
			got := <-seen
			if len(got) != len(c.want) || (len(got) == 1 && got[0] != c.want[0]) {
				t.Fatalf("обработчик увидел цепочку %v; ожидалось %v", got, c.want)
			}
		})
	}
}

// gRPC на слушателе без TLS: учётные данные пакета не роняют соединение и не
// выдают TLSInfo.
func TestServerCredentials_PlaintextListenerIsNotTLS(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	_, info, err := linktls.ServerCredentials().ServerHandshake(&cmux.MuxConn{Conn: a})
	if err != nil {
		t.Fatalf("рукопожатие без TLS отвергнуто: %v", err)
	}
	if _, isTLS := info.(linktls.AuthInfo); isTLS || isTLSInfo(info) {
		t.Fatal("соединение без TLS выдано за TLS")
	}
}

func isTLSInfo(info credentials.AuthInfo) bool {
	_, ok := info.(credentials.TLSInfo)
	return ok
}

// HTTP: сервер с ConnContext пакета отдаёт обработчику состояние звена через
// FromRequest. Близнец — запрос без сертификата.
func TestConnContext_HTTPHandlerSeesTheLinkChain(t *testing.T) {
	p := newPKI(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", p.serverConfig())
	if err != nil {
		t.Fatal(err)
	}
	m := cmux.New(ln)
	httpL := listenerorigin.ExternalListener(m.Match(cmux.Any()))
	seen := make(chan []string, 4)
	hs := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		// Та же композиция, что в корне края: метка происхождения, затем
		// состояние TLS.
		ConnContext: linktls.WithConnState(listenerorigin.ConnContext),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS != nil {
				t.Errorf("r.TLS заполнен за мультиплексором — предпосылка пакета изменилась")
			}
			if !listenerorigin.OnExternalListener(r.Context()) {
				t.Errorf("метка внешнего слушателя потеряна композицией ConnContext")
			}
			seen <- leafURIs(linktls.FromRequest(r))
		}),
	}
	go func() { _ = hs.Serve(httpL) }()
	go func() { _ = m.Serve() }()
	t.Cleanup(func() { _ = hs.Close(); _ = ln.Close() })

	for _, c := range []struct {
		name     string
		withLink bool
		want     int
	}{{"звено с сертификатом", true, 1}, {"близнец: без сертификата", false, 0}} {
		t.Run(c.name, func(t *testing.T) {
			cl := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: p.clientConfig(c.withLink)}}
			resp, gErr := cl.Get("https://" + ln.Addr().String() + "/")
			if gErr != nil {
				t.Fatal(gErr)
			}
			_ = resp.Body.Close()
			got := <-seen
			if len(got) != c.want || (c.want == 1 && got[0] != linkSAN) {
				t.Fatalf("обработчик увидел %v; ожидалось %d имя(ён) %s", got, c.want, linkSAN)
			}
		})
	}
}

// r.TLS, когда он заполнен (голый TLS-слушатель), предпочитается контексту.
func TestFromRequest_PrefersRequestTLS(t *testing.T) {
	st := &tls.ConnectionState{HandshakeComplete: true}
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.TLS = st
	if linktls.FromRequest(r) != st {
		t.Fatal("r.TLS не предпочтён")
	}
	if linktls.FromRequest(nil) != nil {
		t.Fatal("nil-запрос дал состояние")
	}
}
