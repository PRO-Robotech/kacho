// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientaddress_test.go — ОПЕРАТОР АДРЕСА КЛИЕНТА СОБИРАЕТСЯ ОДНОЙ ФУНКЦИЕЙ, И В
// БОЕВОМ ПРОФИЛЕ ОН НЕ ВПРАВЕ НЕ ДОВЕРЯТЬ НИКОМУ (kacho#3028, C6; пробы U8/U13;
// круг 5 — якорь звеньев).
//
// Корень и эта проба зовут одну и ту же функцию сборки, clientaddress.Start.
// Она разбирает круг, звенья поимённо, имена звеньев и якорь звеньев, собирает
// оператор, запускает обновление перечня звеньев и отказывает, если в боевом
// профиле заголовок адреса не принимается ни от кого.
//
// Инъекции, которые проба обязана ловить (снятие из функции сборки):
//   - опции WithTrustedPeers, WithTrustedLinkSANs, WithTrustedLinkAnchor —
//     оператор не доверяет никому → отказ;
//   - запуска Run — первой попытки разрешения нет, снимка нет → звено не
//     признано.
package clientaddress_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/clientaddress"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

const (
	probeLinkSAN  = "api-gateway-front-console.front-link.kacho.internal"
	probeFrontPod = "10.244.1.17"
	probeClient   = "198.51.100.23"
)

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newAuthority(t *testing.T) authority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "probe"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return authority{cert: c, key: key}
}

func (a authority) write(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (a authority) chain(t *testing.T, dns string) [][]*x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), DNSNames: []string{dns},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return [][]*x509.Certificate{{leaf, a.cert}}
}

func linkedConfig(t *testing.T, appEnv string, link authority) config.Config {
	return config.Config{
		AppEnv:                        appEnv,
		AuthZTrustedXForwardedFor:     true,
		AuthZTrustedProxyCount:        1,
		AuthZTrustedProxyCIDRs:        "10.244.0.0/16",
		AuthZTrustedProxyPeers:        "api-gateway-front-console",
		AuthZTrustedProxyPeersRefresh: time.Hour,
		AuthZTrustedProxySANs:         probeLinkSAN,
		AuthZTrustedProxyCAFile:       link.write(t),
		TLSListenAddr:                 ":8443", TLSCertFile: "/tls/tls.crt", TLSKeyFile: "/tls/tls.key",
		HybridMTLSExternal: true, MTLSCAFile: newAuthority(t).write(t),
	}
}

func resolve(_ context.Context, name string) ([]netip.Addr, error) {
	if name == "api-gateway-front-console" {
		return []netip.Addr{netip.MustParseAddr(probeFrontPod)}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func linkRequest(chains [][]*x509.Certificate) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/auth/login", nil)
	r.RemoteAddr = net.JoinHostPort(probeFrontPod, "40000")
	r.Header.Set("X-Forwarded-For", probeClient)
	r.TLS = &tls.ConnectionState{HandshakeComplete: true, VerifiedChains: chains}
	return r
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// Близнец: боевая сборка со всем объявленным — оператор доверяет звену, перечень
// обновляется (первая попытка Run отмечена), адрес клиента за звеном — свой, а
// якорь отдан наружу для полосы личности.
func TestStart_ProductionBuildTrustsTheLink(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	link := newAuthority(t)
	op, err := clientaddress.Start(ctx, linkedConfig(t, "production", link), resolve, quiet)
	if err != nil {
		t.Fatalf("боевая сборка со всем объявленным отвергнута: %v", err)
	}
	if op.Extractor.TrustsNobody() {
		t.Fatal("боевая сборка: TrustsNobody() = true")
	}
	if op.Links == nil {
		t.Fatal("перечень звеньев не собран")
	}
	if op.Anchor.Empty() {
		t.Fatal("якорь звеньев не отдан наружу — полоса личности не узнала бы лист звена")
	}
	select {
	case <-op.Links.Ran():
	case <-time.After(5 * time.Second):
		t.Fatal("снимка нет: обновление перечня звеньев не запущено (Run)")
	}
	if got := op.Extractor.ClientIP(linkRequest(link.chain(t, probeLinkSAN))); got != probeClient {
		t.Fatalf("адрес клиента за звеном %q; ожидался %s", got, probeClient)
	}
	// Тот же лист от якоря установки — не звено.
	if got := op.Extractor.ClientIP(linkRequest(newAuthority(t).chain(t, probeLinkSAN))); got != probeFrontPod {
		t.Fatalf("лист чужого якоря с именем звена сдвинул источник на %q", got)
	}
}

// В боевом профиле «никому» — отказ, и он называет ручки. Близнец — профиль
// разработки: стартует, оператор не доверяет никому.
func TestStart_ProductionRefusesToTrustNobody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, env := range []string{"production", ""} {
		_, err := clientaddress.Start(ctx, config.Config{AppEnv: env, AuthZTrustedXForwardedFor: true,
			AuthZTrustedProxyCount: 1}, resolve, quiet)
		if err == nil {
			t.Fatalf("AppEnv=%q: боевой профиль без звеньев принят", env)
		}
		for _, want := range []string{config.TrustedProxyCIDRsKnob, config.TrustedProxySANsKnob, config.TrustedProxyCAFileKnob} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("AppEnv=%q: отказ не называет %s: %v", env, want, err)
			}
		}
	}
	op, err := clientaddress.Start(ctx, config.Config{AppEnv: "dev", AuthZTrustedXForwardedFor: true,
		AuthZTrustedProxyCount: 1}, resolve, quiet)
	if err != nil || !op.Extractor.TrustsNobody() {
		t.Fatalf("близнец dev: %v", err)
	}
}

// Якорь звеньев не задан — отказ сборки (а не «никому» молча).
func TestStart_NamesWithoutTheLinkAnchorAreRefused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := linkedConfig(t, "production", newAuthority(t))
	c.AuthZTrustedProxyCAFile = ""
	if _, err := clientaddress.Start(ctx, c, resolve, quiet); err == nil ||
		!strings.Contains(err.Error(), config.TrustedProxyCAFileKnob) {
		t.Fatalf("имена звеньев без якоря звеньев собраны: %v", err)
	}
}
