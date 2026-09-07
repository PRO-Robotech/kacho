// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_revocation_mtls_test.go — ребро «реестр → авторитет отзыва» предъявляет
// СВОЙ клиентский сертификат.
//
// Маршрут набора ключей открыт задокументированным исключением, обоснованным
// тем, что на проводе только публичный материал. На маршрут отзыва ПРИСЫЛАЮТ
// предъявленный токен, поэтому обоснование туда не распространяется, а молчаливое
// его распространение было бы запрещённым допущением «внутренний периметр
// доверенный». Авторитет отвергает вызывающего, чью цепочку транспорт не
// проверил.
package jwks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testPKI — минимальный внутренний удостоверяющий центр пробы: якорь, пара
// сервера и пара клиента, разложенные файлами (настройка ребра называет ФАЙЛЫ).
type testPKI struct {
	caFile     string
	serverCert tls.Certificate
	clientCert string
	clientKey  string
	pool       *x509.CertPool
}

func issueTestPKI(t *testing.T) testPKI {
	t.Helper()
	dir := t.TempDir()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kacho-internal-ca (probe)"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	caFile := filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600))

	leaf := func(cn string, serial int64, server bool) (string, string, tls.Certificate) {
		key, kerr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, kerr)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: cn},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
		}
		if server {
			tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			tmpl.DNSNames = []string{cn}
			tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		} else {
			tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
			tmpl.DNSNames = []string{cn}
		}
		der, cerr := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		require.NoError(t, cerr)
		keyDER, merr := x509.MarshalECPrivateKey(key)
		require.NoError(t, merr)

		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
		certFile := filepath.Join(dir, cn+".crt")
		keyFile := filepath.Join(dir, cn+".key")
		require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
		require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))

		pair, perr := tls.X509KeyPair(certPEM, keyPEM)
		require.NoError(t, perr)
		return certFile, keyFile, pair
	}

	_, _, serverPair := leaf("kaname-internal", 2, true)
	clientCert, clientKey, _ := leaf("kacho-registry", 3, false)

	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	return testPKI{caFile: caFile, serverCert: serverPair, clientCert: clientCert, clientKey: clientKey, pool: pool}
}

// mutualAuthority — авторитет отзыва на TLS, который ЗАПРАШИВАЕТ клиентский
// сертификат, но не требует его на рукопожатии (набор ключей на том же
// слушателе обязан оставаться доступным без сертификата). Отказ выносит сам
// обработчик — вызывающему без проверенной цепочки отвечает 401.
type mutualAuthority struct {
	srv           *httptest.Server
	asked         atomic.Int32
	withoutClient atomic.Int32
}

func newMutualAuthority(t *testing.T, pki testPKI) *mutualAuthority {
	t.Helper()
	a := &mutualAuthority{}
	a.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.asked.Add(1)
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			a.withoutClient.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"client_certificate_required"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true})
	}))
	a.srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{pki.serverCert},
		ClientCAs:    pki.pool,
		// Необязательно-взаимный режим: сертификат запрашивается, но его
		// отсутствие рукопожатие не роняет — иначе маршрут набора ключей на том
		// же слушателе перестал бы быть origin-agnostic.
		ClientAuth: tls.VerifyClientCertIfGiven,
		MinVersion: tls.VersionTLS12,
	}
	a.srv.StartTLS()
	t.Cleanup(a.srv.Close)
	return a
}

// TestF1_25_RevocationEdgePresentsItsClientCertificate — несущая половина:
// объявленные учётные данные ребра доводят обращение до утвердительного ответа.
//
// Без неё «всё отвергается» верно и для реализации, которая сертификата не
// предъявляет, — то есть проба не отличает исправное ребро от мёртвого контроля.
func TestF1_25_RevocationEdgePresentsItsClientCertificate(t *testing.T) {
	pki := issueTestPKI(t)
	a := newMutualAuthority(t, pki)

	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	at := time.Now()

	reader, err := NewIntrospectionReader(a.srv.URL, RevocationTransport{
		Enable:     true,
		CAFiles:    []string{pki.caFile},
		CertFile:   pki.clientCert,
		KeyFile:    pki.clientKey,
		ServerName: "kaname-internal",
	}, WithIntrospectionClock(func() time.Time { return at }))
	require.NoError(t, err)

	v := newVerifierWith(t, reader, ourPair(ks))
	sub, err := v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", at, 30*time.Minute)))
	require.NoError(t, err, "ребро с объявленными учётными данными обязано получать ответ авторитета")
	require.Equal(t, "sva-1", sub)
	require.Positive(t, a.asked.Load())
	require.Zero(t, a.withoutClient.Load(),
		"обращение обязано нести клиентский сертификат: авторитет отвергает вызывающего, чью цепочку транспорт не проверил")
}

// TestF1_25_CallWithoutAClientCertificateIsRefused — вызывающий без проверенной
// цепочки получает 401, и по правилу «отказ при сомнении» токен отвергается.
//
// Ровно поэтому проба, ждущая `{"active":true}` без клиентского сертификата,
// утверждала бы не то: 401 здесь — законный ответ авторитета, а не поломка.
func TestF1_25_CallWithoutAClientCertificateIsRefused(t *testing.T) {
	pki := issueTestPKI(t)
	a := newMutualAuthority(t, pki)

	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	at := time.Now()

	// Клиент доверяет якорю, но сертификата НЕ предъявляет — то самое состояние,
	// которое авторитет отвергает.
	bare := &http.Client{
		Timeout: introspectionTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:    pki.pool,
			ServerName: "kaname-internal",
			MinVersion: tls.VersionTLS12,
		}},
	}
	reader, err := NewIntrospectionReader(a.srv.URL, RevocationTransport{
		Enable:     true,
		CAFiles:    []string{pki.caFile},
		CertFile:   pki.clientCert,
		KeyFile:    pki.clientKey,
		ServerName: "kaname-internal",
	},
		WithIntrospectionClock(func() time.Time { return at }),
		WithIntrospectionHTTPClient(bare))
	require.NoError(t, err)

	v := newVerifierWith(t, reader, ourPair(ks))
	_, err = v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", at, 30*time.Minute)))
	require.ErrorIs(t, err, ErrInvalidToken,
		"401 от авторитета — такой же неопознанный исход, как любой другой: доступ закрывается")
	require.Positive(t, a.withoutClient.Load(), "предпосылка пробы: обращение действительно ушло без сертификата")
}

// TestF1_25_UnusableAnchorRefusesTheStart — якорь, объявленный и непригодный,
// роняет ПОСТРОЕНИЕ, а не откатывается на системные корни.
//
// Откат всегда «работает», поэтому ошибка в якоре стала бы ненаблюдаемой:
// контроль присутствовал бы, ходил бы не туда и не отказал бы ни разу.
func TestF1_25_UnusableAnchorRefusesTheStart(t *testing.T) {
	pki := issueTestPKI(t)
	dir := t.TempDir()
	emptyAnchor := filepath.Join(dir, "empty.crt")
	require.NoError(t, os.WriteFile(emptyAnchor, []byte("not a certificate\n"), 0o600))

	full := RevocationTransport{
		Enable:     true,
		CAFiles:    []string{pki.caFile},
		CertFile:   pki.clientCert,
		KeyFile:    pki.clientKey,
		ServerName: "kaname-internal",
	}
	const authorityURL = "https://kaname-internal:9097/internal/tokens/introspect"

	cases := map[string]func(tr *RevocationTransport){
		"якорь не объявлен":             func(tr *RevocationTransport) { tr.CAFiles = nil },
		"якоря нет на диске":            func(tr *RevocationTransport) { tr.CAFiles = []string{filepath.Join(dir, "absent.crt")} },
		"в якоре нет сертификата":       func(tr *RevocationTransport) { tr.CAFiles = []string{emptyAnchor} },
		"имя сервера не объявлено":      func(tr *RevocationTransport) { tr.ServerName = "" },
		"пара сертификата неполна":      func(tr *RevocationTransport) { tr.KeyFile = "" },
		"пары сертификата нет вовсе":    func(tr *RevocationTransport) { tr.CertFile, tr.KeyFile = "", "" },
		"ключа нет на диске":            func(tr *RevocationTransport) { tr.KeyFile = filepath.Join(dir, "absent.key") },
		"учётные данные не объявлены":   func(tr *RevocationTransport) { *tr = RevocationTransport{} },
		"учётные данные при выключении": func(tr *RevocationTransport) { tr.Enable = false },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			tr := full
			tr.CAFiles = append([]string(nil), full.CAFiles...)
			breakIt(&tr)
			_, err := NewIntrospectionReader(authorityURL, tr)
			require.Error(t, err, "непригодные учётные данные ребра обязаны ронять построение")
		})
	}

	t.Run("положительный контроль: полные учётные данные принимаются", func(t *testing.T) {
		r, err := NewIntrospectionReader(authorityURL, full)
		require.NoError(t, err)
		require.NotNil(t, r)
	})

	t.Run("открытый HTTP без учётных данных — локальная фикстура", func(t *testing.T) {
		r, err := NewIntrospectionReader("http://127.0.0.1:9097/internal/tokens/introspect", RevocationTransport{})
		require.NoError(t, err, "полоса локальных фикстур сохраняется: там TLS нет вовсе")
		require.NotNil(t, r)
	})

	t.Run("учётные данные объявлены, а адрес открытый", func(t *testing.T) {
		_, err := NewIntrospectionReader("http://127.0.0.1:9097/internal/tokens/introspect", full)
		require.Error(t, err, "клиентские учётные данные без TLS не значат ничего")
	})
}
