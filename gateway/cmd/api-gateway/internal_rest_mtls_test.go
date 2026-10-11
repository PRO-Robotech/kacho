// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// Пробы транспорта внутреннего REST-слушателя края (kacho#3131).
//
// Слушатель отдаёт Internal* REST (метка «внутренний»), поэтому его транспорт —
// mTLS: клиентский лист обязателен, цепочка — к УЦ установки, имя листа — в
// круге KACHO_API_GATEWAY_INTERNAL_REST_CLIENT_SANS. Сборка — ТЕ ЖЕ функции,
// что зовёт корень: validateProductionInternalListener (страж старта и сборщик
// транспорта) и serveInternalREST (дом слушателя), а не своя копия.
//
// Оракул отказа — «запрос не получил ответа обработчика»: в TLS 1.3 сервер
// судит клиентский лист ПОСЛЕ того, как клиент счёл рукопожатие завершённым,
// поэтому ошибка у клиента всплывает на первом чтении, и текст её зависит от
// вида отказа (certificate_required на пустом листе, bad_certificate на
// чужом). Равенство текстов здесь не утверждается: различать их — дело журнала
// сервера, а не клиента.

const (
	irOperatorSAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-internal-rest-operator"
	irVPCSAN      = "spiffe://kacho.cloud/ns/kacho/sa/kacho-vpc"
	irEdgeSAN     = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"
)

type irPKI struct {
	dir      string
	caCert   *x509.Certificate
	caKey    *ecdsa.PrivateKey
	caFile   string
	certFile string // серверный лист слушателя
	keyFile  string
}

func irKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return k
}

func irSerial(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	require.NoError(t, err)
	return n
}

func irNewCA(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	k := irKey(t)
	tpl := &x509.Certificate{
		SerialNumber:          irSerial(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	require.NoError(t, err)
	c, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return c, k
}

func irLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, usage x509.ExtKeyUsage, uri string, ips []net.IP) tls.Certificate {
	t.Helper()
	k := irKey(t)
	tpl := &x509.Certificate{
		SerialNumber: irSerial(t),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  ips,
	}
	if uri != "" {
		u, err := url.Parse(uri)
		require.NoError(t, err)
		tpl.URIs = []*url.URL{u}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &k.PublicKey, caKey)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}
}

func irWritePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600))
}

func newIRPKI(t *testing.T) *irPKI {
	t.Helper()
	dir := t.TempDir()
	ca, caKey := irNewCA(t, "kacho-internal-ca")
	p := &irPKI{dir: dir, caCert: ca, caKey: caKey,
		caFile: filepath.Join(dir, "ca.crt"), certFile: filepath.Join(dir, "tls.crt"), keyFile: filepath.Join(dir, "tls.key")}
	irWritePEM(t, p.caFile, "CERTIFICATE", ca.Raw)
	srv := irLeaf(t, ca, caKey, x509.ExtKeyUsageServerAuth, "", []net.IP{net.ParseIP("127.0.0.1")})
	irWritePEM(t, p.certFile, "CERTIFICATE", srv.Certificate[0])
	kd, err := x509.MarshalECPrivateKey(srv.PrivateKey.(*ecdsa.PrivateKey))
	require.NoError(t, err)
	irWritePEM(t, p.keyFile, "EC PRIVATE KEY", kd)
	return p
}

func (p *irPKI) cfg(addr string) config.Config {
	return config.Config{
		InternalRESTAddr:       addr,
		InternalRESTCertFile:   p.certFile,
		InternalRESTKeyFile:    p.keyFile,
		MTLSCAFile:             p.caFile,
		InternalRESTClientSANs: irOperatorSAN,
	}
}

// startInternalREST поднимает внутренний слушатель ТЕМИ ЖЕ функциями, что корень.
func startInternalREST(t *testing.T, p *irPKI) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	tlsCfg, err := validateProductionInternalListener(p.cfg(l.Addr().String()))
	require.NoError(t, err)
	httpSrv := newEdgeHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !listenerorigin.IsExternal(r.Context()) {
			// r.TLS виден обработчику только при TLS внешней обёрткой (C1).
			if r.TLS == nil {
				_, _ = io.WriteString(w, "internal-without-tls-state")
				return
			}
			_, _ = io.WriteString(w, "internal "+r.Proto)
			return
		}
		_, _ = io.WriteString(w, "external")
	}))
	go func() { _ = serveInternalREST(httpSrv, l, tlsCfg) }()
	t.Cleanup(func() { _ = httpSrv.Close(); _ = l.Close() })
	return l.Addr().String()
}

func (p *irPKI) client(certs ...tls.Certificate) *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(p.caCert)
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, Certificates: certs, MinVersion: tls.VersionTLS12},
	}}
}

// irGet возвращает тело ответа обработчика либо ошибку транспорта.
func irGet(c *http.Client, u string) (string, error) {
	resp, err := c.Get(u)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return resp.Status + " " + string(b), nil
}

// P1: запрос без клиентского листа ответа обработчика не получает — ни по
// TLS, ни открытым текстом.
func TestInternalREST_RequestWithoutClientCertificateIsRefused(t *testing.T) {
	p := newIRPKI(t)
	addr := startInternalREST(t, p)

	got, err := irGet(p.client(), "https://"+addr+"/vpc/v1/addressPools")
	require.Errorf(t, err, "внутренний REST-слушатель обслужил TLS-запрос БЕЗ клиентского сертификата: %q", got)

	plain := &http.Client{Timeout: 5 * time.Second}
	got, err = irGet(plain, "http://"+addr+"/vpc/v1/addressPools")
	if err == nil {
		require.NotContainsf(t, got, "internal", "внутренний REST-слушатель обслужил запрос открытым текстом: %q", got)
	}
}

// Законный близнец P1–P3: лист УЦ установки с именем из круга получает ответ
// обработчика с меткой «внутренний». Без него отказ P1–P3 читался бы и как
// «слушатель не поднялся».
func TestInternalREST_OperatorLeafInTheCircleIsServedInternal(t *testing.T) {
	p := newIRPKI(t)
	addr := startInternalREST(t, p)
	op := irLeaf(t, p.caCert, p.caKey, x509.ExtKeyUsageClientAuth, irOperatorSAN, nil)

	got, err := irGet(p.client(op), "https://"+addr+"/vpc/v1/addressPools")
	require.NoError(t, err)
	require.Equal(t, "200 OK internal HTTP/1.1", got)

	// HTTP/2 по ALPN — тот же исход: REST края обслуживает h2 и здесь.
	h2 := p.client(op)
	h2.Transport.(*http.Transport).ForceAttemptHTTP2 = true
	got, err = irGet(h2, "https://"+addr+"/vpc/v1/addressPools")
	require.NoError(t, err)
	require.Equal(t, "200 OK internal HTTP/2.0", got)
}

// P3: лист того же УЦ с именем ВНЕ круга — лист службы и лист самого края —
// отказ. Меняется ровно один факт против близнеца: имя в листе.
func TestInternalREST_LeafOutsideTheCircleIsRefused(t *testing.T) {
	p := newIRPKI(t)
	addr := startInternalREST(t, p)
	for _, san := range []string{irVPCSAN, irEdgeSAN} {
		leaf := irLeaf(t, p.caCert, p.caKey, x509.ExtKeyUsageClientAuth, san, nil)
		got, err := irGet(p.client(leaf), "https://"+addr+"/vpc/v1/addressPools")
		require.Errorf(t, err, "лист %s вне круга получил ответ обработчика: %q", san, got)
	}
}

// P5: лист ЧУЖОГО УЦ с именем из круга — отказ. Меняется ровно один факт
// против близнеца: выпускающий.
func TestInternalREST_LeafOfAForeignCAIsRefused(t *testing.T) {
	p := newIRPKI(t)
	addr := startInternalREST(t, p)
	foreign, foreignKey := irNewCA(t, "foreign-ca")
	leaf := irLeaf(t, foreign, foreignKey, x509.ExtKeyUsageClientAuth, irOperatorSAN, nil)
	got, err := irGet(p.client(leaf), "https://"+addr+"/vpc/v1/addressPools")
	require.Errorf(t, err, "лист чужого УЦ получил ответ обработчика: %q", got)
}

// Страж старта: слушатель объявлен, а материала нет — отказ, и текст отказа
// называет ручку. Ветки «отключить mTLS» у слушателя нет ни на какой метке
// KACHO_APP_ENV, поэтому метка в пробе не участвует.
func TestInternalREST_StartGuardRefusesAListenerWithoutItsMaterial(t *testing.T) {
	p := newIRPKI(t)
	cases := []struct {
		name string
		mut  func(*config.Config)
		knob string
	}{
		{"нет серверного листа", func(c *config.Config) { c.InternalRESTCertFile = "" }, config.InternalRESTCertKnob},
		{"нет ключа", func(c *config.Config) { c.InternalRESTKeyFile = "" }, config.InternalRESTKeyKnob},
		{"нет УЦ клиентов", func(c *config.Config) { c.MTLSCAFile = "" }, "KACHO_API_GATEWAY_MTLS_CA_FILE"},
		{"пустой круг", func(c *config.Config) { c.InternalRESTClientSANs = "" }, config.InternalRESTClientSANsKnob},
		{"круг из пробелов и запятых", func(c *config.Config) { c.InternalRESTClientSANs = " , ," }, config.InternalRESTClientSANsKnob},
		{"имя круга не spiffe", func(c *config.Config) { c.InternalRESTClientSANs = "kacho-internal-rest-operator" }, config.InternalRESTClientSANsKnob},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := p.cfg(":0")
			tc.mut(&c)
			tlsCfg, err := validateProductionInternalListener(c)
			require.Errorf(t, err, "страж пропустил слушатель: %s", tc.name)
			require.Nil(t, tlsCfg)
			require.Contains(t, err.Error(), tc.knob)
		})
	}

	// Законный близнец: полный материал — транспорт собран, клиентский лист
	// обязателен.
	tlsCfg, err := validateProductionInternalListener(p.cfg(":0"))
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)
	require.Equal(t, tls.RequireAndVerifyClientCert, tlsCfg.ClientAuth)
	circle, err := p.cfg(":0").InternalRESTClientCircle()
	require.NoError(t, err)
	require.Equal(t, []string{irOperatorSAN}, circle)

	// Слушатель не объявлен — ни транспорта, ни отказа.
	off := p.cfg("")
	off.InternalRESTCertFile, off.InternalRESTClientSANs = "", ""
	tlsCfg, err = validateProductionInternalListener(off)
	require.NoError(t, err)
	require.Nil(t, tlsCfg)
}

// Дом слушателя не поднимает его без mTLS: nil-транспорт и транспорт без
// обязательного клиентского листа — отказ, а не открытый текст.
func TestInternalREST_ServeRefusesWithoutMutualTLS(t *testing.T) {
	for name, cfg := range map[string]*tls.Config{
		"nil": nil,
		"без клиентского листа": {ClientAuth: tls.VerifyClientCertIfGiven},
	} {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		httpSrv := newEdgeHTTPServer(http.NotFoundHandler())
		done := make(chan error, 1)
		go func() { done <- serveInternalREST(httpSrv, l, cfg) }()
		select {
		case err := <-done:
			require.Errorf(t, err, "%s: serveInternalREST вернулся без отказа", name)
			require.True(t, strings.Contains(err.Error(), "mTLS"), "%s: %v", name, err)
		case <-time.After(3 * time.Second):
			_ = httpSrv.Close()
			t.Fatalf("%s: serveInternalREST поднял слушатель без mTLS", name)
		}
		_ = l.Close()
	}
}
