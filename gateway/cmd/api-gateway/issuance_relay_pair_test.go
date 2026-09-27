// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// issuance_relay_pair_test.go — ретрансляция края к слушателю ВЫДАЧИ
// предъявляет клиентскую пару края (приёмка темпа службы доступа
// `PRO-Robotech/kaname` `ceremony-pace-is-named-by-number.md`, стадия S2 п. 1,
// сценарий KN-PACE-40; правило адреса источника Р7).
//
// Зачем: служба берёт адрес источника из `X-Forwarded-For` ТОЛЬКО у пира с
// проверенным сертификатом края; у любого другого пира — адрес соединения.
// Ретрансляция без пары приходит к слушателю адресом самого края, и все люди
// за краем делят один предел навигаций `authorize` и одно окно отказов обмена.
// Страж старта службы этого не видит — видит только пара на рукопожатии.
//
// Слушатель пробы — настоящий TLS-сервер в режиме службы `optional-mutual`:
// запрашивает клиентский сертификат, проверяет предъявленный, вызывающего без
// сертификата допускает. Судится композиционный корень целиком: страж и
// транспорт цели (`prepareRelayTarget`) плюс ретранслятор края.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/identityposture"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

const (
	edgeSAN         = "spiffe://kacho.cloud/ns/kacho-system/sa/kacho-api-gateway"
	issuanceSNI     = "kaname.kacho.svc"
	relayedClientIP = "198.51.100.7"
)

// issuanceSeen — что слушатель выдачи увидел в одном запросе.
type issuanceSeen struct {
	chains  [][]*x509.Certificate
	header  http.Header
	reached bool
}

// askingListener — слушатель выдачи в режиме `optional-mutual`.
type askingListener struct {
	srv  *httptest.Server
	mu   sync.Mutex
	last issuanceSeen
}

func (l *askingListener) seen() issuanceSeen {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.last
}

func newAskingListener(t *testing.T, ca *testCA) *askingListener {
	t.Helper()
	certFile, keyFile := ca.issueLeaf(t, leafOpts{
		commonName: "kaname", dnsNames: []string{issuanceSNI},
		ipAddresses: []net.IP{net.ParseIP("127.0.0.1")}, isServer: true,
	})
	leaf, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(ca.certPEM))

	l := &askingListener{}
	l.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		l.last = issuanceSeen{chains: r.TLS.VerifiedChains, header: r.Header.Clone(), reached: true}
		l.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	l.srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{leaf},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}
	l.srv.StartTLS()
	t.Cleanup(l.srv.Close)
	return l
}

// issuanceEdge — настройка края под `own` с парой края и якорем внутреннего УЦ.
type issuanceEdge struct {
	cfg      config.Config
	identity tls.Certificate
	pool     *x509.CertPool
}

func newIssuanceEdge(t *testing.T, ca *testCA, listenerURL string) issuanceEdge {
	t.Helper()
	certFile, keyFile := ca.issueLeaf(t, leafOpts{commonName: "api-gateway", uriSANs: []string{edgeSAN}})
	identity, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(ca.certPEM))
	return issuanceEdge{
		cfg: config.Config{
			IAMIssuanceURL:     listenerURL,
			MTLSClientCertFile: certFile,
			MTLSClientKeyFile:  keyFile,
			MTLSCAFile:         ca.caFile(t),
			MTLSIAMServerName:  issuanceSNI,
		},
		identity: identity,
		pool:     pool,
	}
}

// askDirect — прямой вызов слушателя, минуя край; identity=nil — без пары.
func askDirect(t *testing.T, e issuanceEdge, url string, identity *tls.Certificate) {
	t.Helper()
	tc := &tls.Config{RootCAs: e.pool, ServerName: issuanceSNI, MinVersion: tls.VersionTLS12}
	if identity != nil {
		tc.Certificates = []tls.Certificate{*identity}
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tc}}
	resp, err := client.Get(url + middleware.CeremonyPathAuthorize)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func leafHasURISAN(chains [][]*x509.Certificate, want string) bool {
	if len(chains) == 0 || len(chains[0]) == 0 {
		return false
	}
	for _, u := range chains[0][0].URIs {
		if u.String() == want {
			return true
		}
	}
	return false
}

// Близнецы фикстуры: слушатель ВИДИТ предъявленную пару края (иначе красное
// ниже было бы слепотой слушателя) и ДОПУСКАЕТ вызывающего без пары (иначе
// красное было бы отказом рукопожатия, а не отсутствием сертификата).
func TestIssuanceRelay_KN_PACE_40_Twin_TheAskingListenerSeesAPresentedPairAndAdmitsNone(t *testing.T) {
	ca := newTestCA(t, "kacho-test-ca")
	l := newAskingListener(t, ca)
	e := newIssuanceEdge(t, ca, l.srv.URL)

	askDirect(t, e, l.srv.URL, &e.identity)
	if got := l.seen(); !leafHasURISAN(got.chains, edgeSAN) {
		t.Fatalf("слушатель пробы не видит пару края, предъявленную напрямую: цепочек %d", len(got.chains))
	}
	askDirect(t, e, l.srv.URL, nil)
	if got := l.seen(); !got.reached || len(got.chains) != 0 {
		t.Fatalf("слушатель пробы обязан допускать вызывающего без пары и не видеть сертификата: дошёл=%v, цепочек %d",
			got.reached, len(got.chains))
	}
}

// KN-PACE-40: край ретранслирует навигацию `authorize` — слушатель видит
// проверенный сертификат с SAN края; запрос несёт ровно один `X-Forwarded-For`
// и не несёт `Authorization` и заголовков пространства `x-kacho-`.
func TestIssuanceRelay_KN_PACE_40_TheIssuanceListenerSeesTheEdgesVerifiedCertificate(t *testing.T) {
	ca := newTestCA(t, "kacho-test-ca")
	l := newAskingListener(t, ca)
	e := newIssuanceEdge(t, ca, l.srv.URL)

	tr, _, err := prepareRelayTarget(identityposture.Own, e.cfg, middleware.RelayTargetIssuance, e.cfg.IAMIssuanceURL)
	require.NoError(t, err, "страж и транспорт цели выдачи под own с парой края")
	relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Serves:    middleware.RelayTargetIssuance,
		Target:    e.cfg.IAMIssuanceURL,
		Transport: tr,
		ClientIP:  func(*http.Request) string { return relayedClientIP },
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, middleware.CeremonyPathAuthorize+"?response_type=code&state=st-1", nil)
	req.Header.Set("Authorization", "Bearer person-token")
	req.Header.Set("X-Kacho-Principal-Id", "usr-1")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)

	got := l.seen()
	require.True(t, got.reached, "ретрансляция не дошла до слушателя выдачи: ответ края %d %q", rec.Code, rec.Body.String())
	require.Equal(t, http.StatusOK, rec.Code)
	if !leafHasURISAN(got.chains, edgeSAN) {
		t.Fatalf("слушатель выдачи не видит проверенного сертификата края (цепочек %d): ретрансляция без пары "+
			"получает у службы ключ адреса самого края, и все люди за краем делят один предел", len(got.chains))
	}
	if xff := got.header.Values("X-Forwarded-For"); len(xff) != 1 || xff[0] != relayedClientIP {
		t.Fatalf("X-Forwarded-For на прыжке к выдаче: %q — ожидался ровно один адрес %q", xff, relayedClientIP)
	}
	if v := got.header.Get("Authorization"); v != "" {
		t.Fatalf("Authorization доехал до слушателя выдачи: %q", v)
	}
	for k := range got.header {
		if strings.HasPrefix(strings.ToLower(k), "x-kacho-") {
			t.Fatalf("заголовок пространства x-kacho- доехал до слушателя выдачи: %q", k)
		}
	}
}

// Страж края судит ось пары у КАЖДОЙ цели ретрансляции (S2 п. 1): половина пары
// и её отсутствие — отказ старта с именем ручки пары, у цели выдачи тоже.
// Близнец — та же цель с полной парой — стартует.
func TestRelayGuard_KN_PACE_40_TheEdgePairIsJudgedAtEveryRelayTarget(t *testing.T) {
	wired := map[middleware.RelayTarget]LoginLaneConfig{
		middleware.RelayTargetForm:     loginLaneWired(),
		middleware.RelayTargetIssuance: issuanceWired(),
	}
	cases := map[string]func(*LoginLaneConfig){
		"пары нет":          func(c *LoginLaneConfig) { c.ClientCertFile, c.ClientKeyFile = "", "" },
		"только сертификат": func(c *LoginLaneConfig) { c.ClientKeyFile = "" },
		"только ключ":       func(c *LoginLaneConfig) { c.ClientCertFile = "" },
	}
	judged := 0
	for _, tg := range middleware.RelayTargets() {
		twin, ok := wired[tg]
		if !ok {
			t.Fatalf("цель %q без положительного близнеца в пробе", tg)
		}
		if err := validateLoginLaneConfig(identityposture.Own, twin); err != nil {
			t.Fatalf("цель %q с полной парой обязана стартовать: %v", tg, err)
		}
		for name, mutate := range cases {
			cfg := twin
			mutate(&cfg)
			err := validateLoginLaneConfig(identityposture.Own, cfg)
			if err == nil {
				t.Errorf("цель %q, %s: обязан отвергать старт — хоп без пары края служба не узнаёт краем", tg, name)
				continue
			}
			if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_MTLS_CLIENT_") {
				t.Errorf("цель %q, %s: отказ обязан называть ручку пары: %q", tg, name, err.Error())
			}
			judged++
		}
	}
	issuance := issuanceWired()
	issuance.ClientCertFile, issuance.ClientKeyFile = "", ""
	if err := validateLoginLaneConfig(identityposture.Own, issuance); err == nil ||
		!strings.Contains(err.Error(), config.IssuanceURLKnob) || !strings.Contains(err.Error(), "optional-mutual") {
		t.Fatalf("отказ цели выдачи без пары обязан называть её ручку %s и режим optional-mutual: %v", config.IssuanceURLKnob, err)
	}
	t.Logf("перепись: целей %d · отказов по оси пары %d из %d", len(middleware.RelayTargets()), judged, len(middleware.RelayTargets())*len(cases))
}

// Режим слушателя выдачи — `optional-mutual` у службы (Р7 п. 1, п. 5): край
// объявляет цель тем же словом, из которого выводит предъявление пары.
func TestRelayTargetDecls_KN_PACE_40_TheIssuanceTargetIsOptionalMutual(t *testing.T) {
	d, ok := relayTargetDeclFor(middleware.RelayTargetIssuance)
	require.True(t, ok)
	if string(d.Mode) != "optional-mutual" {
		t.Fatalf("режим цели выдачи %q — ожидался optional-mutual (слушатель выдачи службы, Р7 п. 5)", d.Mode)
	}
}
