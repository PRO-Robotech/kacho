// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// client_address_link_identity_test.go — ЗВЕНО ФРОНТА УЗНАЁТСЯ ПО ИМЕНИ В
// СЕРТИФИКАТЕ, А gRPC ЧИТАЕТ ТОЛЬКО СВОЙ ЗАГОЛОВОК (kacho#3028, C3 и C4).
//
// Адрес пода — не личность: под с теми же метками получает адрес в той же
// сети и попадает в службу фронта, а адрес ушедшего пода выдаётся другому.
// Поэтому звено — пир, предъявивший проверенный якорем установки сертификат с
// именем из перечня звеньев, и только потом — адрес в круге и в перечне
// поимённо. Каждое отрицательное утверждение — в паре с близнецом, отличным в
// один факт.
//
// gRPC (C3): метаданные `grpcgateway-*` пишет только наш мост в процессе, а
// мост на нативный слушатель не ходит; на нативном пути их пишет клиент, и
// читать их нельзя никогда. Значения `x-forwarded-for` склеиваются по порядку
// до разбора справа: взятое первое значение отдавало выбор клиенту, который
// присылает два.
package middleware_test

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

const (
	frontSAN = "spiffe://kacho.test/ns/kacho/sa/console-front"
	otherSAN = "spiffe://kacho.test/ns/kacho/sa/someone-else"
)

// linkState — состояние TLS пира, чья цепочка ПРОВЕРЕНА до корня якоря
// звеньев проб (fixtureLinkCA) и чей лист несёт названное имя (URI либо DNS).
func linkState(san string) *tls.ConnectionState {
	return &tls.ConnectionState{HandshakeComplete: true, VerifiedChains: fixtureLinkCA().mustChain(san)}
}

// unverifiedState — тот же лист, предъявленный, но НЕ проверенный якорем
// (PeerCertificates без VerifiedChains): так выглядит сертификат на слушателе
// без якоря.
func unverifiedState(san string) *tls.ConnectionState {
	st := linkState(san)
	st.PeerCertificates = st.VerifiedChains[0]
	st.VerifiedChains = nil
	return st
}

// trustingTheLink — край за раздачей: прыжок, круг, звено поимённо и имя звена.
func trustingTheLink() *middleware.ContextExtractor {
	return middleware.NewContextExtractor(time.Now, true,
		middleware.WithTrustedProxyHops(1),
		middleware.WithTrustedProxies(netip.MustParsePrefix("10.244.0.0/16")),
		middleware.WithTrustedPeers(links{frontPod, podInCircle}),
		middleware.WithTrustedLinkSANs(frontSAN),
		middleware.WithTrustedLinkAnchor(fixtureLinkAnchor()))
}

func httpLink(peer, xff string, st *tls.ConnectionState) *http.Request {
	r := httpFrom(peer, xff)
	r.TLS = st
	return r
}

func grpcClientIP(e *middleware.ContextExtractor, peer string, st *tls.ConnectionState, md metadata.MD) any {
	addr := &net.TCPAddr{IP: net.ParseIP(peer), Port: 40000}
	return e.BuildPeerAddr(nil, addr, st, md, middleware.ResolvedSubject{})["client_ip"]
}

// C4. Под в круге и в перечне поимённо (тот же адрес, те же метки), но без
// сертификата звена, источник заголовком не сдвигает: адрес — не личность.
// Близнец — тот же пир с сертификатом звена.
func TestLinkIdentity_AddressWithoutTheLinkCertificateIsNotALink(t *testing.T) {
	e := trustingTheLink()
	for _, c := range []struct {
		name string
		st   *tls.ConnectionState
	}{
		{"без TLS", nil},
		{"TLS без сертификата", &tls.ConnectionState{HandshakeComplete: true}},
		{"сертификат с чужим именем", linkState(otherSAN)},
		{"имя звена в непроверенном сертификате", unverifiedState(frontSAN)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := e.ClientIP(httpLink(podInCircle, forgedSource, c.st)); got != podInCircle {
				t.Errorf("HTTP: пир %s без звеньевого сертификата сдвинул источник на %q", podInCircle, got)
			}
			md := metadata.Pairs("x-forwarded-for", forgedSource)
			if got := grpcClientIP(e, podInCircle, c.st, md); got != podInCircle {
				t.Errorf("gRPC: пир %s без звеньевого сертификата сдвинул источник на %v", podInCircle, got)
			}
		})
	}
	if got := e.ClientIP(httpLink(podInCircle, clientA, linkState(frontSAN))); got != clientA {
		t.Errorf("близнец HTTP: звено с сертификатом не принято: %q", got)
	}
	if got := grpcClientIP(e, podInCircle, linkState(frontSAN), metadata.Pairs("x-forwarded-for", clientA)); got != clientA {
		t.Errorf("близнец gRPC: звено с сертификатом не принято: %v", got)
	}
}

// Имя звена при адресе ВНЕ перечня поимённо — не звено: адрес остаётся
// сужением, сертификат его не заменяет.
func TestLinkIdentity_CertificateOutsideTheNamedAddressesIsNotALink(t *testing.T) {
	e := trustingTheLink()
	const strayPod = "10.244.9.9"
	if got := e.ClientIP(httpLink(strayPod, forgedSource, linkState(frontSAN))); got != strayPod {
		t.Fatalf("пир вне перечня поимённо с сертификатом звена сдвинул источник на %q", got)
	}
}

// Без перечня имён звеньев заголовок не принимается ни от кого, и
// TrustsNobody() это говорит.
func TestLinkIdentity_NoDeclaredNamesTrustsNobody(t *testing.T) {
	e := middleware.NewContextExtractor(time.Now, true,
		middleware.WithTrustedProxyHops(1),
		middleware.WithTrustedProxies(netip.MustParsePrefix("10.244.0.0/16")),
		middleware.WithTrustedPeers(links{frontPod}))
	if !e.TrustsNobody() {
		t.Fatal("имён звеньев нет, а TrustsNobody() = false")
	}
	if got := e.ClientIP(httpLink(frontPod, forgedSource, linkState(frontSAN))); got != frontPod {
		t.Fatalf("имён звеньев нет, а заголовок принят: %q", got)
	}
	if trustingTheLink().TrustsNobody() {
		t.Fatal("близнец: всё объявлено, а TrustsNobody() = true")
	}
}

// C3. На нативном gRPC `grpcgateway-*` — слова клиента. Звено с
// сертификатом: подделка в `grpcgateway-x-forwarded-for` и
// `grpcgateway-x-real-ip` источник не сдвигает; близнец — то же значение в
// `x-forwarded-for`, записанное звеном, — принимается.
func TestGRPC_BridgeMetadataIsNeverReadOnTheNativePath(t *testing.T) {
	e := trustingTheLink()
	st := linkState(frontSAN)
	for _, key := range []string{"grpcgateway-x-forwarded-for", "grpcgateway-x-real-ip"} {
		if got := grpcClientIP(e, frontPod, st, metadata.Pairs(key, forgedSource)); got == forgedSource {
			t.Errorf("метаданные %s сдвинули источник на подделку %s", key, forgedSource)
		}
		md := metadata.Pairs(key, forgedSource, "x-forwarded-for", clientA)
		if got := grpcClientIP(e, frontPod, st, md); got != clientA {
			t.Errorf("при %s рядом с x-forwarded-for источник %v; ожидалась запись звена %s", key, got, clientA)
		}
	}
	if got := grpcClientIP(e, frontPod, st, metadata.Pairs("x-forwarded-for", clientA)); got != clientA {
		t.Errorf("близнец: x-forwarded-for звена не принят: %v", got)
	}
}

// C3 (P2). Два значения `x-forwarded-for` склеиваются по порядку, и берётся
// крайнее правое склеенного: клиент кладёт подделку первым значением, звено
// дописывает своё последним.
func TestGRPC_ForwardedValuesAreJoinedBeforeReadingFromTheRight(t *testing.T) {
	e := trustingTheLink()
	md := metadata.MD{"x-forwarded-for": []string{forgedSource, clientA}}
	if got := grpcClientIP(e, frontPod, linkState(frontSAN), md); got != clientA {
		t.Fatalf("два значения x-forwarded-for: источник %v; ожидалось крайнее правое склеенного %s", got, clientA)
	}
	// Близнец: одно значение, цепочка внутри — тот же ответ.
	one := metadata.Pairs("x-forwarded-for", forgedSource+", "+clientA)
	if got := grpcClientIP(e, frontPod, linkState(frontSAN), one); got != clientA {
		t.Fatalf("близнец: одно значение с цепочкой: %v", got)
	}
}

// C2. HTTP: несколько строк X-Forwarded-For склеиваются так же; несколько
// X-Real-IP — неоднозначность, не читается.
func TestHTTP_ForwardedLinesAreJoinedAndAmbiguousRealIPIsIgnored(t *testing.T) {
	e := trustingTheLink()
	r := httpLink(frontPod, "", linkState(frontSAN))
	r.Header.Add("X-Forwarded-For", forgedSource)
	r.Header.Add("X-Forwarded-For", clientA)
	if got := e.ClientIP(r); got != clientA {
		t.Fatalf("две строки X-Forwarded-For: %q; ожидалось крайнее правое склеенного %s", got, clientA)
	}
	r2 := httpLink(frontPod, "", linkState(frontSAN))
	r2.Header.Add("X-Real-IP", forgedSource)
	r2.Header.Add("X-Real-IP", clientA)
	if got := e.ClientIP(r2); got != frontPod {
		t.Fatalf("две строки X-Real-IP приняты: %q; ожидался сам пир %s", got, frontPod)
	}
	r3 := httpLink(frontPod, "", linkState(frontSAN))
	r3.Header.Set("X-Real-IP", clientA)
	if got := e.ClientIP(r3); got != clientA {
		t.Fatalf("близнец: одна строка X-Real-IP звена не принята: %q", got)
	}
}

// C2. Один читатель: client_ip модели прав (HTTP и gRPC) и адрес ретрансляции
// полосы входа (ClientIP) — одно и то же значение на одном входе.
func TestOneReader_ConditionAndRelayAgree(t *testing.T) {
	e := trustingTheLink()
	for _, c := range []struct {
		peer string
		st   *tls.ConnectionState
	}{{frontPod, linkState(frontSAN)}, {frontPod, nil}, {podInCircle, linkState(otherSAN)}} {
		r := httpLink(c.peer, forgedSource+", "+clientA, c.st)
		relay := e.ClientIP(r)
		cond := e.BuildHTTP(nil, r, middleware.ResolvedSubject{})["client_ip"]
		grpcCond := grpcClientIP(e, c.peer, c.st, metadata.Pairs("x-forwarded-for", forgedSource+", "+clientA))
		if relay != cond || relay != grpcCond {
			t.Errorf("пир %s: ретрансляция %q, условие HTTP %v, условие gRPC %v — читатели разошлись", c.peer, relay, cond, grpcCond)
		}
	}
}
