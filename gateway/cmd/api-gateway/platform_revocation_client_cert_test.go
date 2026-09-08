// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// platform_revocation_client_cert_test.go — хоп к НАШЕМУ авторитету отзыва
// обязан ПРЕДЪЯВЛЯТЬ клиентский сертификат.
//
// ПОЧЕМУ ЭТО СВОЙСТВО, А НЕ УКРАШЕНИЕ. Авторитет отзыва (iam) выставлен на
// внутреннем слушателе, который сертификат ЗАПРАШИВАЕТ, и отвечает
// `401 client_certificate_required` тому, кто его не предъявил. Край при этом
// классифицирует такой ответ как НАСТРОЙКУ, а не сбой (и правильно делает), то
// есть отказывает fail-closed КАЖДОМУ предъявителю нашей чеканки. Пока у хопа
// нет клиентской пары, вторая полоса края неисполнима НИ ПРИ КАКОМ входе:
// возможность объявлена, задокументирована, покрыта типами — и не работает.
//
// ПОЧЕМУ УТВЕРЖДАЕТСЯ ИСХОД, А НЕ ОБЪЯВЛЕНИЕ. Проверять, что в транспорте лежит
// пара, значило бы утверждать намерение: клиент, который пару держит и в
// рукопожатии её не отдаёт, остался бы зелёным. Поэтому здесь настоящий
// сервер, требующий проверенную цепочку, и предмет утверждения — то, что он
// увидел.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// authorityRequiringClientCert поднимает подобие авторитета отзыва: TLS-сервер,
// который ТРЕБУЕТ проверенный клиентский сертификат и, не получив его, отвечает
// ровно тем же опознавательным словом, что и настоящий.
func authorityRequiringClientCert(t *testing.T, ca *testCA) *httptest.Server {
	t.Helper()

	srvCert, srvKey := ca.issueLeaf(t, leafOpts{
		commonName:  "kaname-internal",
		dnsNames:    []string{"localhost"},
		ipAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		isServer:    true,
	})
	pair, err := tls.LoadX509KeyPair(srvCert, srvKey)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(ca.certPEM))

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"client_certificate_required"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"active":true}`))
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{pair},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// Предъявленная пара доходит до авторитета: он видит ПРОВЕРЕННУЮ цепочку.
func TestPlatformRevocationHop_PresentsClientCertificate(t *testing.T) {
	ca := newTestCA(t, "kacho-internal-ca")
	srv := authorityRequiringClientCert(t, ca)
	clientCert, clientKey := ca.issueLeaf(t, leafOpts{commonName: "api-gateway"})

	c, err := newPlatformRevocationHopClient(ca.caFile(t), clientCert, clientKey, 5*time.Second)
	require.NoError(t, err)

	resp, err := c.Post(srv.URL, "application/x-www-form-urlencoded", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"авторитет не увидел проверенной цепочки — хоп сертификата не предъявил")
}

// ЗАКОННЫЙ БЛИЗНЕЦ И ОДНОВРЕМЕННО ДОКАЗАТЕЛЬСТВО, ЧТО ПРОБА ВЫШЕ УМЕЕТ УПАСТЬ:
// без пары тот же хоп получает от того же авторитета ровно тот отказ, который
// наблюдался на стенде. Без этой половины первая проба была бы зелена на
// сервере, который сертификата не спрашивает вовсе.
func TestPlatformRevocationHop_WithoutClientCertificate_IsRefusedByTheAuthority(t *testing.T) {
	ca := newTestCA(t, "kacho-internal-ca")
	srv := authorityRequiringClientCert(t, ca)

	c, err := newPlatformRevocationHopClient(ca.caFile(t), "", "", 5*time.Second)
	require.NoError(t, err)

	resp, err := c.Post(srv.URL, "application/x-www-form-urlencoded", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"авторитет обязан отказать пиру без сертификата — иначе проба выше ничего не измеряет")
}

// Настроенная НАПОЛОВИНУ пара — отказ в СТАРТЕ, а не тихий проход без
// сертификата: половина пары означает намерение оператора, которое молча не
// исполнилось бы, и хоп ушёл бы в тот же вечный fail-closed.
func TestPlatformRevocationHop_HalfConfiguredPair_RefusesToStart(t *testing.T) {
	ca := newTestCA(t, "kacho-internal-ca")
	clientCert, clientKey := ca.issueLeaf(t, leafOpts{commonName: "api-gateway"})

	_, err := newPlatformRevocationHopClient(ca.caFile(t), clientCert, "", 5*time.Second)
	require.Error(t, err, "сертификат без ключа обязан ронять старт")
	require.Contains(t, err.Error(), "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_KEY_FILE")

	_, err = newPlatformRevocationHopClient(ca.caFile(t), "", clientKey, 5*time.Second)
	require.Error(t, err, "ключ без сертификата обязан ронять старт")
	require.Contains(t, err.Error(), "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CERT_FILE")
}

// Нечитаемая пара — тоже отказ в старте, а не продолжение без сертификата.
func TestPlatformRevocationHop_UnreadablePair_RefusesToStart(t *testing.T) {
	ca := newTestCA(t, "kacho-internal-ca")
	clientCert, _ := ca.issueLeaf(t, leafOpts{commonName: "api-gateway"})

	_, err := newPlatformRevocationHopClient(
		ca.caFile(t), clientCert, t.TempDir()+"/absent.key", 5*time.Second)
	require.Error(t, err, "нечитаемый ключ обязан ронять старт")
}
