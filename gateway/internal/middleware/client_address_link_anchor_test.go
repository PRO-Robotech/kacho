// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// client_address_link_anchor_test.go — ИМЯ ЗВЕНА ЧИТАЕТСЯ ТОЛЬКО В ЛИСТЕ
// ЯКОРЯ ЗВЕНЬЕВ, А ЛИСТ ЗВЕНА НЕ СТАНОВИТСЯ ЛИЧНОСТЬЮ (kacho#3028, круг 5).
//
// Канал, пропущенный переписью четвёртого круга, — «кто выпускает лист».
// Якорь установки выпускает листы кластерным выпускающим: лист с именем звена
// получает всякий, кто заводит запрос на сертификат в любом пространстве имён.
// Поэтому звено — лист, чья проверенная цепочка кончается корнем ОТДЕЛЬНОГО
// якоря звеньев; тот же лист от якоря установки — не звено.
//
// Обратная сторона: звено ретранслирует запросы всех своих клиентов, и лист
// звена, ставший личностью на полосе личности по сертификату, сделал бы
// каждого анонимного клиента за звеном этой личностью.
//
// Цепочки — настоящие сертификаты (linkPKI): якорь узнаёт корень по байтам.
package middleware_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/netip"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/grpcsrv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// testAuthority — удостоверяющий центр пробы.
type testAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func mustAuthority(cn string) testAuthority {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	return testAuthority{cert: c, key: key}
}

func newTestAuthority(t *testing.T, cn string) testAuthority {
	t.Helper()
	return mustAuthority(cn)
}

// mustChain — проверенная цепочка [лист, корень] с именем san (URI либо DNS).
func (a testAuthority) mustChain(san string) [][]*x509.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if u, perr := url.Parse(san); perr == nil && u.Scheme != "" {
		tpl.URIs = []*url.URL{u}
	} else {
		tpl.DNSNames = []string{san}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		panic(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	return [][]*x509.Certificate{{leaf, a.cert}}
}

func (a testAuthority) chainFor(t *testing.T, san string) [][]*x509.Certificate {
	t.Helper()
	return a.mustChain(san)
}

func (a testAuthority) state(t *testing.T, san string) *tls.ConnectionState {
	return &tls.ConnectionState{HandshakeComplete: true, VerifiedChains: a.chainFor(t, san)}
}

// fixtureLinkCA — якорь звеньев проб этого пакета: им подписан лист
// linkState, и его объявляют сборки, доверяющие звену.
var fixtureLinkCA = sync.OnceValue(func() testAuthority { return mustAuthority("fixture-front-link-ca") })

// fixtureLinkAnchor — якорь звеньев проб пакета.
func fixtureLinkAnchor() linktls.Anchor { return linktls.NewAnchor(fixtureLinkCA().cert) }

// Лист якоря УСТАНОВКИ с именем звена — не звено; близнец — тот же пир, то
// же имя, лист якоря звеньев.
func TestLinkAnchor_InstallationIssuedLeafWithTheLinkNameIsNotALink(t *testing.T) {
	linkCA := newTestAuthority(t, "front-link-ca")
	instCA := newTestAuthority(t, "kacho-internal-ca")
	e := middleware.NewContextExtractor(time.Now, true,
		middleware.WithTrustedProxyHops(1),
		middleware.WithTrustedProxies(netip.MustParsePrefix("10.244.0.0/16")),
		middleware.WithTrustedPeers(links{frontPod}),
		middleware.WithTrustedLinkSANs(frontSAN),
		middleware.WithTrustedLinkAnchor(linktls.NewAnchor(linkCA.cert)))

	if got := e.ClientIP(httpLink(frontPod, forgedSource, instCA.state(t, frontSAN))); got != frontPod {
		t.Errorf("HTTP: лист якоря установки с именем звена сдвинул источник на %q", got)
	}
	if got := grpcClientIP(e, frontPod, instCA.state(t, frontSAN), metadata.Pairs("x-forwarded-for", forgedSource)); got != frontPod {
		t.Errorf("gRPC: лист якоря установки с именем звена сдвинул источник на %v", got)
	}
	if got := e.ClientIP(httpLink(frontPod, clientA, linkCA.state(t, frontSAN))); got != clientA {
		t.Errorf("близнец HTTP: лист якоря звеньев не принят: %q", got)
	}
	if got := grpcClientIP(e, frontPod, linkCA.state(t, frontSAN), metadata.Pairs("x-forwarded-for", clientA)); got != clientA {
		t.Errorf("близнец gRPC: лист якоря звеньев не принят: %v", got)
	}
}

// Без якоря звеньев заголовок не принимается ни от кого, и TrustsNobody() это
// говорит: имя звена без якоря — имя, которое выдаёт себе кто угодно.
func TestLinkAnchor_NoAnchorTrustsNobody(t *testing.T) {
	linkCA := newTestAuthority(t, "front-link-ca")
	without := middleware.NewContextExtractor(time.Now, true,
		middleware.WithTrustedProxyHops(1),
		middleware.WithTrustedProxies(netip.MustParsePrefix("10.244.0.0/16")),
		middleware.WithTrustedPeers(links{frontPod}),
		middleware.WithTrustedLinkSANs(frontSAN))
	if !without.TrustsNobody() {
		t.Error("якоря звеньев нет, а TrustsNobody() = false")
	}
	if got := without.ClientIP(httpLink(frontPod, forgedSource, linkCA.state(t, frontSAN))); got != frontPod {
		t.Errorf("якоря звеньев нет, а заголовок принят: %q", got)
	}
}

// Лист ЗВЕНА личностью не становится: полоса личности по сертификату его не
// видит, запрос идёт по полосе токена (здесь — без токена, отказ
// Unauthenticated). Близнец — лист якоря установки с тем же именем SPIFFE:
// личность служебной учётки.
func TestMTLSPrincipal_LinkLeafIsNeverAPrincipal(t *testing.T) {
	linkCA := newTestAuthority(t, "front-link-ca")
	instCA := newTestAuthority(t, "kacho-internal-ca")
	const sa = "spiffe://kacho.cloud/ns/kacho/sa/console-front"
	auth := middleware.NewAuthInterceptor(middleware.AuthModeProductionStrict, "", &countingLookup{}, authTestLogger()).
		WithMTLSPrincipal(grpcsrv.NewTrustDomain("kacho.cloud"), linktls.NewAnchor(linkCA.cert))

	call := func(chains [][]*x509.Certificate) (string, error) {
		ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{HandshakeComplete: true, VerifiedChains: chains}}})
		var got string
		_, err := auth.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test/Method"},
			func(hctx context.Context, _ any) (any, error) {
				md, _ := metadata.FromOutgoingContext(hctx)
				if v := md.Get("x-kacho-principal-id"); len(v) > 0 {
					got = v[0]
				}
				return nil, nil
			})
		return got, err
	}

	got, err := call(linkCA.chainFor(t, sa))
	if err == nil {
		t.Errorf("лист звена стал личностью %q: анонимный клиент за звеном прошёл как служебная учётка", got)
	} else if status.Code(err) != codes.Unauthenticated {
		t.Errorf("лист звена без токена: код %v, ожидался Unauthenticated", status.Code(err))
	}
	if got, err := call(instCA.chainFor(t, sa)); err != nil || got != "console-front" {
		t.Errorf("близнец: лист установки не стал личностью: id=%q err=%v", got, err)
	}
}
