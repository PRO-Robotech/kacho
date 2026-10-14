// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trusted_proxy_link_anchor_test.go — ИМЯ ЗВЕНА ПРОВЕРЯЕТСЯ ОТДЕЛЬНЫМ ЯКОРЕМ,
// НЕ ЯКОРЕМ УСТАНОВКИ (kacho#3028, круг 5).
//
// Канал «кто выпускает лист»: якорь установки выпускает листы кластерным
// выпускающим, и лист с именем звена получает всякий, кто заводит запрос на
// сертификат в любом пространстве имён. Имена звеньев поэтому объявляются
// только вместе с якорем звеньев, а якорь звеньев, совпадающий с якорем
// установки хоть одним корнем, — отказ старта: он возвращает ровно тот канал.
// Якорь звеньев доходит до рукопожатия: слушатель принимает его листы.
package config_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// anchored — край за звеном со всем объявленным: круг, звенья, имена,
// слушатель с якорем установки и отдельный якорь звеньев.
func anchored(t *testing.T) config.Config {
	t.Helper()
	c := linked(linkSAN)
	c.MTLSCAFile = writeTestCAFile(t)
	c.AuthZTrustedProxyCAFile = writeTestCAFile(t)
	return c
}

func TestTrustedProxyLinkAnchor_ReadsTheDeclaredAnchor(t *testing.T) {
	roots, err := anchored(t).TrustedProxyLinkAnchor()
	if err != nil || len(roots) != 1 {
		t.Fatalf("законный якорь звеньев: %d корней, %v", len(roots), err)
	}
}

// Имена звеньев без якоря звеньев — отказ, называющий ручку якоря.
func TestTrustedProxyLinkSANs_NamesWithoutTheLinkAnchorAreRefused(t *testing.T) {
	c := anchored(t)
	c.AuthZTrustedProxyCAFile = ""
	if _, err := c.TrustedProxyLinkSANs(); err == nil || !strings.Contains(err.Error(), config.TrustedProxyCAFileKnob) {
		t.Fatalf("имена звеньев без якоря звеньев приняты либо отказ не называет %s: %v", config.TrustedProxyCAFileKnob, err)
	}
	if _, err := anchored(t).TrustedProxyLinkSANs(); err != nil {
		t.Fatalf("близнец с якорем звеньев отвергнут: %v", err)
	}
}

// Якорь звеньев = якорь установки — отказ: лист с именем звена выдал бы себе
// кто угодно. Близнец — разные удостоверяющие центры.
func TestTrustedProxyLinkAnchor_SharingARootWithTheInstallationIsRefused(t *testing.T) {
	c := anchored(t)
	c.AuthZTrustedProxyCAFile = c.MTLSCAFile
	if _, err := c.TrustedProxyLinkAnchor(); err == nil || !strings.Contains(err.Error(), config.TrustedProxyCAFileKnob) {
		t.Fatalf("якорь звеньев, совпадающий с якорем установки, принят: %v", err)
	}
	// Тот же корень внутри связки из двух — тоже отказ.
	both := filepath.Join(t.TempDir(), "bundle.crt")
	a, _ := os.ReadFile(writeTestCAFile(t))
	b, _ := os.ReadFile(c.MTLSCAFile)
	if err := os.WriteFile(both, append(a, b...), 0o600); err != nil {
		t.Fatal(err)
	}
	c.AuthZTrustedProxyCAFile = both
	if _, err := c.TrustedProxyLinkAnchor(); err == nil {
		t.Fatal("связка якоря звеньев с корнем установки внутри принята")
	}
}

// Неразборный якорь — отказ при любом флаге доверия.
func TestTrustedProxyLinkAnchor_UnreadableAnchorIsRefused(t *testing.T) {
	c := anchored(t)
	junk := filepath.Join(t.TempDir(), "junk.crt")
	if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.AuthZTrustedProxyCAFile = junk
	c.AuthZTrustedXForwardedFor = false
	if _, err := c.TrustedProxyLinkAnchor(); err == nil {
		t.Fatal("неразборный якорь звеньев принят")
	}
}

// Лист НЕ удостоверяющего центра в роли якоря — отказ: корнем цепочки он не
// бывает, и якорь не признал бы ни одного звена молча.
func TestTrustedProxyLinkAnchor_NonCAIsRefused(t *testing.T) {
	c := anchored(t)
	leaf := filepath.Join(t.TempDir(), "leaf.crt")
	ca := newPKI(t)
	der := ca.issue(t, "x.front-link.kacho.internal")
	if err := os.WriteFile(leaf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	c.AuthZTrustedProxyCAFile = leaf
	if _, err := c.TrustedProxyLinkAnchor(); err == nil {
		t.Fatal("лист вместо удостоверяющего центра принят якорем звеньев")
	}
}

// Якорь звеньев доходит до рукопожатия: лист звена принимается слушателем, и
// проверенная цепочка кончается корнем якоря звеньев. Близнец — без якоря
// звеньев тот же лист рукопожатие не проходит.
func TestExternalListenerClientAuth_AdmitsTheLinkAnchor(t *testing.T) {
	link := newPKI(t)
	c := anchored(t)
	c.AuthZTrustedProxyCAFile = link.writeCA(t)

	srvCfg, err := c.ExternalListenerClientAuth(&tls.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if chains := handshake(t, srvCfg, link, "api-gateway-front-console.front-link.kacho.internal"); len(chains) == 0 ||
		!chains[0][len(chains[0])-1].Equal(link.ca) {
		t.Fatalf("лист якоря звеньев: цепочка %v не кончается корнем якоря звеньев", chains)
	}

	c.AuthZTrustedProxyCAFile = ""
	srvCfg, err = c.ExternalListenerClientAuth(&tls.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if chains := handshake(t, srvCfg, link, "api-gateway-front-console.front-link.kacho.internal"); chains != nil {
		t.Fatal("близнец: без якоря звеньев лист звена прошёл рукопожатие")
	}
}

// handshake — рукопожатие по памяти: клиент с листом pki, сервер по srvCfg.
// Отдаёт проверенные цепочки либо nil при отказе рукопожатия.
func handshake(t *testing.T, srvCfg *tls.Config, pki testPKI, dns string) [][]*x509.Certificate {
	t.Helper()
	srv := newPKI(t)
	srvCfg = srvCfg.Clone()
	srvCfg.Certificates = []tls.Certificate{srv.pair(t, "edge.test", x509.ExtKeyUsageServerAuth)}
	roots := x509.NewCertPool()
	roots.AddCert(srv.ca)
	cliCfg := &tls.Config{RootCAs: roots, ServerName: "edge.test", MinVersion: tls.VersionTLS12,
		Certificates: []tls.Certificate{pki.pair(t, dns, x509.ExtKeyUsageClientAuth)}}
	cc, sc := pipe()
	defer cc.Close()
	defer sc.Close()
	type res struct {
		st  tls.ConnectionState
		err error
	}
	done := make(chan res, 1)
	go func() {
		s := tls.Server(sc, srvCfg)
		err := s.Handshake()
		done <- res{s.ConnectionState(), err}
	}()
	_ = tls.Client(cc, cliCfg).Handshake()
	r := <-done
	if r.err != nil {
		return nil
	}
	return r.st.VerifiedChains
}
