// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// client_address_operator_start_test.go — ОПЕРАТОР АДРЕСА КЛИЕНТА СОБИРАЕТСЯ
// ОДНОЙ ФУНКЦИЕЙ, И В БОЕВОМ ПРОФИЛЕ ОН НЕ ВПРАВЕ НЕ ДОВЕРЯТЬ НИКОМУ
// (kacho#3028, C6; пробы U8/U13).
//
// Корень и эта проба зовут одну и ту же функцию сборки, startClientAddressOperator.
// Она разбирает круг, звенья поимённо и имена звеньев, собирает оператор,
// запускает обновление перечня звеньев и отказывает, если в боевом профиле
// заголовок адреса не принимается ни от кого: за звеном фронта такой край
// видел бы всех клиентов одним адресом, и ограничение частоты «на источник»
// стало бы общим для всех. Предупреждение в журнале этого не останавливает —
// отказ в старте останавливает.
//
// Инъекции, которые проба обязана ловить (снятие из функции сборки):
//   - опции WithTrustedPeers — оператор не доверяет никому → отказ;
//   - опции WithTrustedLinkSANs — то же;
//   - запуска Run — первой попытки разрешения нет, снимка нет → звено не
//     признано.

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"crypto/tls"
	"crypto/x509"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

const (
	probeLinkSAN  = "spiffe://kacho.test/ns/kacho/sa/console-front"
	probeFrontPod = "10.244.1.17"
	probeClient   = "198.51.100.23"
)

func probeLinkedConfig(appEnv string) config.Config {
	return config.Config{
		AppEnv:                        appEnv,
		AuthZTrustedXForwardedFor:     true,
		AuthZTrustedProxyCount:        1,
		AuthZTrustedProxyCIDRs:        "10.244.0.0/16",
		AuthZTrustedProxyPeers:        "api-gateway-front-console",
		AuthZTrustedProxyPeersRefresh: time.Hour,
		AuthZTrustedProxySANs:         probeLinkSAN,
		TLSListenAddr:                 ":8443", TLSCertFile: "/tls/tls.crt", TLSKeyFile: "/tls/tls.key",
		HybridMTLSExternal: true, MTLSCAFile: "/mtls/ca.crt",
	}
}

func probeResolve(_ context.Context, name string) ([]netip.Addr, error) {
	if name == "api-gateway-front-console" {
		return []netip.Addr{netip.MustParseAddr(probeFrontPod)}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func probeLinkRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/auth/login", nil)
	r.RemoteAddr = net.JoinHostPort(probeFrontPod, "40000")
	r.Header.Set("X-Forwarded-For", probeClient)
	u, _ := url.Parse(probeLinkSAN)
	r.TLS = &tls.ConnectionState{HandshakeComplete: true,
		VerifiedChains: [][]*x509.Certificate{{{URIs: []*url.URL{u}}}}}
	return r
}

var quietLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// Близнец: боевая сборка со всем объявленным — оператор доверяет звену, перечень
// обновляется (первая попытка Run отмечена), адрес клиента за звеном — свой.
func TestClientAddressOperator_ProductionBuildTrustsTheLink(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op, links, err := startClientAddressOperator(ctx, probeLinkedConfig("production"), probeResolve, quietLogger)
	if err != nil {
		t.Fatalf("боевая сборка со всем объявленным отвергнута: %v", err)
	}
	if op.TrustsNobody() {
		t.Fatal("боевая сборка: TrustsNobody() = true")
	}
	if links == nil {
		t.Fatal("перечень звеньев не собран")
	}
	select {
	case <-links.Ran():
	case <-time.After(5 * time.Second):
		t.Fatal("снимка нет: обновление перечня звеньев не запущено (Run)")
	}
	if got := op.ClientIP(probeLinkRequest()); got != probeClient {
		t.Fatalf("адрес клиента за звеном %q; ожидался %s", got, probeClient)
	}
}

// В боевом профиле «никому» — отказ, и он называет ручку. Близнец — тот же
// профиль разработки: стартует, оператор не доверяет никому.
func TestClientAddressOperator_ProductionRefusesToTrustNobody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, env := range []string{"production", ""} {
		_, _, err := startClientAddressOperator(ctx, config.Config{AppEnv: env, AuthZTrustedXForwardedFor: true,
			AuthZTrustedProxyCount: 1}, probeResolve, quietLogger)
		if err == nil {
			t.Fatalf("AppEnv=%q: боевой профиль без звеньев принят", env)
		}
		for _, want := range []string{config.TrustedProxyCIDRsKnob, config.TrustedProxySANsKnob} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("AppEnv=%q: отказ не называет %s: %v", env, want, err)
			}
		}
	}
	op, _, err := startClientAddressOperator(ctx, config.Config{AppEnv: "dev", AuthZTrustedXForwardedFor: true,
		AuthZTrustedProxyCount: 1}, probeResolve, quietLogger)
	if err != nil || !op.TrustsNobody() {
		t.Fatalf("близнец dev: %v, TrustsNobody=%v", err, op != nil && op.TrustsNobody())
	}
}
