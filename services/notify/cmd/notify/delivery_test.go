// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// delivery_test.go — цикл доставки в композиционном корне (полоса A2 задачи
// #2915; замысел З21–З26, §9 «Рёбра»): строка ленты источника доходит до
// ретранслятора письмом с подписью DKIM, и её исход записан у источника.
//
// Что держат пробы — провязку, а не клетки: каждая клетка исхода строки уже
// держится пробами пакета deliver на портах фикстуры. Здесь порты — НАСТОЯЩИЕ
// реализации, собранные из загруженной и проверенной конфигурации:
//
//   - сборка — встроенный каталог `bundle.New` (шаблон `notify-probe/probe-hello`);
//   - право — `grant.New` над клиентом kaname `ResolveSend` по mTLS с точным
//     URI SAN листа kaname (`notify.kaname.addr`, `notify.kaname.san`);
//   - рендер — `render.New` (origin, имя и адрес отправителя установки);
//   - подпись — `dkim.NewSigner(cfg.FromDomain(), <источник пары>)`;
//   - отправитель — `newRelaySender` с якорем `notify.smtp.trustAnchorFile`;
//   - циклы источников — `source.Start` с удостоверением notify из файлов пира.
//
// Поддельные только внешние узлы: сервер ленты источника (Subscribe, Claim,
// Ack), kaname (ResolveSend) и ретранслятор (smtptest), все — на петле с TLS
// одного УЦ пробы (ретранслятор — своего), и сетка на адресата (порт
// deliver.Limiter; её держат integration-пробы пакета limits).
//
// Контракт испытуемого (шов корня; имена — этой пробы):
//
//	type deliveryDeps struct {
//		Pairs    dkim.PairSource       // страж DNS установки (*dnscheck.Guard)
//		Limiter  deliver.Limiter       // сетка на адресата (*limits.Limiter)
//		Registry prometheus.Registerer // реестр процесса
//		Log      *slog.Logger
//	}
//	func startDelivery(ctx context.Context, cfg config.Config, d deliveryDeps) (*delivery, error)
//	func (d *delivery) Wait() // после отмены ctx: циклы, строки в полёте, соединения

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
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkimkey"
	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
)

const (
	probeModule    = "notify-probe"
	probeTemplate  = "probe-hello"
	probeRecipient = "user@example.invalid"
	notifyLeafSAN  = "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify"
	probeSAN       = "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify-probe"
	kanameSAN      = "spiffe://kacho.cloud/ns/kacho/sa/kaname"
)

// ── УЦ пробы ─────────────────────────────────────────────────────────────────

type meshCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
	ser  atomic.Int64
}

func newMeshCA(t *testing.T) *meshCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "notify delivery probe CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	ca := &meshCA{cert: cert, key: key, pool: pool}
	ca.ser.Store(1)
	return ca
}

// leaf — лист с одним URI-SAN; серверный несёт ещё IP петли: имя узла
// проходит при любом SAN, и серверы различает ровно URI-SAN.
func (ca *meshCA) leaf(t *testing.T, server bool, uri string) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(ca.ser.Add(1)), Subject: pkix.Name{CommonName: uri},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, URIs: []*url.URL{u},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

// serve поднимает сервер gRPC на петле с листом uri; клиент обязан
// предъявить лист этого УЦ.
func (ca *meshCA) serve(t *testing.T, uri string, register func(*grpc.Server)) string {
	t.Helper()
	der, key := ca.leaf(t, true, uri)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		ClientCAs:    ca.pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12,
	})))
	register(srv)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return l.Addr().String()
}

// peerFiles — удостоверение notify (клиентский лист и УЦ) файлами, как их
// монтирует чарт.
func (ca *meshCA) peerFiles(t *testing.T, env map[string]string) {
	t.Helper()
	dir := t.TempDir()
	der, key := ca.leaf(t, false, notifyLeafSAN)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, typ string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	env["KACHO_NOTIFY_PEER_TLS_CA_FILE"] = write("ca.crt", "CERTIFICATE", ca.cert.Raw)
	env["KACHO_NOTIFY_PEER_TLS_CERT_FILE"] = write("tls.crt", "CERTIFICATE", der)
	env["KACHO_NOTIFY_PEER_TLS_KEY_FILE"] = write("tls.key", "EC PRIVATE KEY", keyDER)
}

// ── источник ленты ───────────────────────────────────────────────────────────

type ackRecord struct {
	id, token string
	kind      notifyv1.OutcomeKind
	reason    notifyv1.OutcomeReason
}

// probeFeed — сервер ленты пробного модуля: одна строка в аренде до записи
// её исхода; поток подписки открыт.
type probeFeed struct {
	notifyv1.UnimplementedInternalNotificationFeedServiceServer
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer

	mu     sync.Mutex
	row    *notifyv1.ClaimedNotification
	leased bool
	acks   []ackRecord
}

func newProbeFeed() *probeFeed {
	return &probeFeed{row: &notifyv1.ClaimedNotification{
		Id:             "ntf-" + uuid.NewString(),
		LeaseToken:     uuid.NewString(),
		LeaseRemaining: durationpb.New(5 * time.Minute),
		Template:       probeTemplate,
		SchemaRev:      1,
		Class:          notifyv1.NotificationClass_NOTICE,
		Recipient:      &notifyv1.ClaimedNotification_Address{Address: probeRecipient},
		Attrs:          map[string]string{"target": "/"},
		EnqueuedAt:     timestamppb.New(time.Now().Add(-time.Second)),
		ExpiresIn:      durationpb.New(time.Hour),
	}}
}

func (f *probeFeed) Claim(_ context.Context, _ *notifyv1.ClaimRequest) (*notifyv1.ClaimResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leased || len(f.acks) > 0 {
		return &notifyv1.ClaimResponse{}, nil
	}
	f.leased = true
	return &notifyv1.ClaimResponse{Notifications: []*notifyv1.ClaimedNotification{f.row}}, nil
}

func (f *probeFeed) Ack(_ context.Context, req *notifyv1.AckRequest) (*notifyv1.AckResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acks = append(f.acks, ackRecord{
		id: req.GetId(), token: req.GetLeaseToken(),
		kind: req.GetOutcome().GetKind(), reason: req.GetOutcome().GetReason(),
	})
	return &notifyv1.AckResponse{}, nil
}

func (f *probeFeed) Subscribe(_ *subscriptionv1.SubscriptionRequest,
	stream grpc.ServerStreamingServer[subscriptionv1.SubscriptionMessage]) error {
	if err := stream.Send(&subscriptionv1.SubscriptionMessage{Message: &subscriptionv1.SubscriptionMessage_Opened{
		Opened: &subscriptionv1.SubscriptionOpened{Position: "0", CaughtUp: true},
	}}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

func (f *probeFeed) recorded() []ackRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ackRecord(nil), f.acks...)
}

// ── kaname ───────────────────────────────────────────────────────────────────

type fakeKaname struct {
	iamv1.UnimplementedInternalNotificationGrantServiceServer
	decision iamv1.SendDecision

	mu   sync.Mutex
	reqs []*iamv1.ResolveSendRequest
}

func (k *fakeKaname) ResolveSend(_ context.Context, req *iamv1.ResolveSendRequest) (*iamv1.ResolveSendResponse, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.reqs = append(k.reqs, req)
	return &iamv1.ResolveSendResponse{Decision: k.decision}, nil
}

func (k *fakeKaname) calls() []*iamv1.ResolveSendRequest {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]*iamv1.ResolveSendRequest(nil), k.reqs...)
}

// ── сетка и пара ─────────────────────────────────────────────────────────────

type openLimiter struct{}

func (openLimiter) Reserve(context.Context, limits.Row) (*limits.Reservation, error) {
	return &limits.Reservation{}, nil
}
func (openLimiter) Release(context.Context, *limits.Reservation) error { return nil }

type filePair struct{ p dkimkey.Pair }

func (f filePair) Pair() dkimkey.Pair { return f.p }

// ── стенд пробы ──────────────────────────────────────────────────────────────

type deliveryRig struct {
	feed   *probeFeed
	kaname *fakeKaname
	relay  *smtptest.Relay
	stop   func()
}

type deliveryOpts struct {
	decision  iamv1.SendDecision
	kanameSAN string // SAN листа kaname; пусто — kanameSAN
}

func startDeliveryRig(t *testing.T, o deliveryOpts) *deliveryRig {
	t.Helper()
	ca := newMeshCA(t)
	feed := newProbeFeed()
	feedAddr := ca.serve(t, probeSAN, func(s *grpc.Server) {
		notifyv1.RegisterInternalNotificationFeedServiceServer(s, feed)
		subscriptionv1.RegisterInternalSubscriptionServiceServer(s, feed)
	})
	if o.kanameSAN == "" {
		o.kanameSAN = kanameSAN
	}
	kn := &fakeKaname{decision: o.decision}
	kanameAddr := ca.serve(t, o.kanameSAN, func(s *grpc.Server) {
		iamv1.RegisterInternalNotificationGrantServiceServer(s, kn)
	})
	relayCA := smtptest.NewCA(t)
	relay := smtptest.Start(t, smtptest.Script{Issuer: relayCA})
	anchor := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(anchor, relayCA.PEM(), 0o600); err != nil {
		t.Fatal(err)
	}

	roster, err := json.Marshal([]map[string]any{{
		"module": probeModule, "feedAddr": feedAddr, "san": probeSAN,
		"classes": []string{"notice"}, "recipientForms": []string{"address"}, "authorization": "resolveSend",
	}})
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"KACHO_NOTIFY_SOURCES":                string(roster),
		"KACHO_NOTIFY_SOURCE_LIMITS":          `{"` + probeModule + `":{"rate":5,"burst":5,"paused":false}}`,
		"KACHO_NOTIFY_KANAME_ADDR":            kanameAddr,
		"KACHO_NOTIFY_KANAME_SAN":             kanameSAN,
		"KACHO_NOTIFY_SMTP_CONNECTION_URI":    relayURI("smtp", "", relay.Host, relay.Port),
		"KACHO_NOTIFY_SMTP_TRUST_ANCHOR_FILE": anchor,
		"KACHO_NOTIFY_CLAIM_INTERVAL":         "1s",
	}
	ca.peerFiles(t, env)
	cfg := loadConfig(t, env)

	pair, err := dkimkey.ReadPair(cfg.DKIMKeyFile, cfg.DKIMSelectorFile)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: пара DKIM фикстуры: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d, err := startDelivery(ctx, cfg, deliveryDeps{
		Pairs:    filePair{pair},
		Limiter:  openLimiter{},
		Registry: prometheus.NewRegistry(),
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		cancel()
		t.Fatalf("цикл доставки не собран из исправной конфигурации: %v", err)
	}
	r := &deliveryRig{feed: feed, kaname: kn, relay: relay}
	var once sync.Once
	r.stop = func() { once.Do(func() { cancel(); d.Wait() }) }
	t.Cleanup(r.stop)
	return r
}

// waitUntil ждёт исхода cond не дольше 20 с: ожидание кончается исходом, а
// срок — только верхняя граница.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !cond() {
		select {
		case <-deadline.C:
			if !cond() {
				t.Fatalf("не дождались за 20 с: %s", what)
			}
			return
		case <-tick.C:
		}
	}
}

// TestDelivery_ProbeRowReachesTheRelaySignedAndIsAcked — строка пробного
// модуля (адресат — адрес) доходит до ретранслятора одним письмом с подписью
// DKIM домена отправителя; kaname спрошен о пространстве, шаблоне и моменте
// постановки строки; исход SENT записан у источника тем же токеном аренды.
func TestDelivery_ProbeRowReachesTheRelaySignedAndIsAcked(t *testing.T) {
	r := startDeliveryRig(t, deliveryOpts{decision: iamv1.SendDecision_ALLOW})
	waitUntil(t, "исход строки у источника", func() bool { return len(r.feed.recorded()) > 0 })
	r.stop()

	acks := r.feed.recorded()
	if len(acks) != 1 || acks[0].kind != notifyv1.OutcomeKind_SENT ||
		acks[0].id != r.feed.row.GetId() || acks[0].token != r.feed.row.GetLeaseToken() {
		t.Fatalf("исходы у источника %+v, ожидался один SENT строки %s", acks, r.feed.row.GetId())
	}
	calls := r.kaname.calls()
	if len(calls) != 1 {
		t.Fatalf("вызовов ResolveSend %d, ожидался 1", len(calls))
	}
	c := calls[0]
	if c.GetNamespace() != probeModule || c.GetTemplate() != probeTemplate ||
		!c.GetEnqueuedAt().AsTime().Equal(r.feed.row.GetEnqueuedAt().AsTime()) {
		t.Fatalf("ResolveSend(%q, %q, %v), ожидалось (%q, %q, %v)", c.GetNamespace(), c.GetTemplate(),
			c.GetEnqueuedAt().AsTime(), probeModule, probeTemplate, r.feed.row.GetEnqueuedAt().AsTime())
	}
	msgs := r.relay.Messages()
	if len(msgs) != 1 {
		t.Fatalf("писем у ретранслятора %d, ожидалось 1", len(msgs))
	}
	sess := r.relay.Sessions()[0]
	if !sess.TLS || len(sess.Rcpts) != 1 || sess.Rcpts[0] != probeRecipient {
		t.Fatalf("сессия TLS=%v RCPT=%v, ожидались TLS и [%s]", sess.TLS, sess.Rcpts, probeRecipient)
	}
	head, _, _ := strings.Cut(string(msgs[0]), "\n\n")
	if !strings.HasPrefix(head, "DKIM-Signature:") || !strings.Contains(head, "d=example.invalid") {
		t.Fatalf("письмо без подписи DKIM домена отправителя первым полем:\n%s", head)
	}
}

// TestDelivery_NotYetGrantedSendsNothing — близнец: тот же стенд, kaname
// отвечает NOT_YET_GRANTED — письма нет, исход DEFER(grant_skew).
func TestDelivery_NotYetGrantedSendsNothing(t *testing.T) {
	r := startDeliveryRig(t, deliveryOpts{decision: iamv1.SendDecision_NOT_YET_GRANTED})
	waitUntil(t, "исход строки у источника", func() bool { return len(r.feed.recorded()) > 0 })
	r.stop()
	acks := r.feed.recorded()
	if len(acks) != 1 || acks[0].kind != notifyv1.OutcomeKind_DEFER ||
		acks[0].reason != notifyv1.OutcomeReason_GRANT_SKEW {
		t.Fatalf("исходы %+v, ожидался один DEFER(grant_skew)", acks)
	}
	if n := len(r.relay.Sessions()); n != 0 {
		t.Fatalf("SMTP-сессий %d при NOT_YET_GRANTED", n)
	}
}

// TestDelivery_ForeignKanameLeafIsRefused — лист kaname с чужим URI SAN (тот
// же УЦ): решение от него не принимается — ResolveSend до сервера не доходит,
// письма нет, строка отложена platform_unavailable.
func TestDelivery_ForeignKanameLeafIsRefused(t *testing.T) {
	r := startDeliveryRig(t, deliveryOpts{
		decision:  iamv1.SendDecision_ALLOW,
		kanameSAN: "spiffe://kacho.cloud/ns/kacho/sa/impostor",
	})
	waitUntil(t, "исход строки у источника", func() bool { return len(r.feed.recorded()) > 0 })
	r.stop()
	if n := len(r.kaname.calls()); n != 0 {
		t.Fatalf("ResolveSend дошёл до листа с чужим SAN (%d вызовов)", n)
	}
	acks := r.feed.recorded()
	if len(acks) != 1 || acks[0].kind != notifyv1.OutcomeKind_DEFER ||
		acks[0].reason != notifyv1.OutcomeReason_PLATFORM_UNAVAILABLE {
		t.Fatalf("исходы %+v, ожидался один DEFER(platform_unavailable)", acks)
	}
	if n := len(r.relay.Sessions()); n != 0 {
		t.Fatalf("SMTP-сессий %d при отвергнутом листе kaname", n)
	}
}
