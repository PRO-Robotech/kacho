// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package source

// fixture_test.go — фикстурный источник ленты на живом сетевом слушателе с
// mTLS тестового УЦ: сервер ленты (`Claim`) и сервер подписки (`Subscribe`)
// по контракту corelib, управляемые пробой.
//
// # Почему не настоящий сервер ленты на базе
//
// Предмет полосы N2 — цикл notify на стороне КЛИЕНТА: перечень источников,
// клиент с точным SAN, подписка, такт `Claim`, срок вызова. Аренда строки в
// базе источника — предмет corelib (`notify/feed`, NTF1-B30, B31) и в этих
// пробах не утверждается. Пробам нужен источник, который по команде пробы
// зависает на `Claim`, отвечает позже срока после коммита аренды, не шлёт
// `SubscriptionOpened` или держит поток недоступным, — настоящий сервер
// ленты этого не умеет, а сетевой путь (TLS, SAN, сроки gRPC) здесь настоящий.
//
// # Фикстура не снисходительнее продукта
//
// Границы входа `Claim` — те же, что у сервера ленты corelib (`claimClasses`):
// `max` в [1..feed.MaxClaim], `classes` непуст и из перечня — иначе
// INVALID_ARGUMENT с именем поля. Аренда — как у оператора аренды: строка,
// отданная вызовом, занята до конца аренды, даже если вызов отменён после
// коммита. Положительный контроль самой фикстуры — fixture_selfcheck_test.go:
// он зовёт её сырым клиентом gRPC и не зависит от испытуемого.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

const (
	// notifySAN — удостоверение notify, которым цикл ходит к источникам.
	notifySAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify"

	// feedKind — вид ленты на проводе подписки (тип объекта модели прав).
	feedKind = "notification_feed"

	// wantCallTimeout — срок вызова `Claim` и установления `Subscribe`
	// (§8 замысла, `sourceCallTimeout`): константа 10 с, не ручка.
	wantCallTimeout = 10 * time.Second

	// fixtureRecipient — адресат строк фикстуры; отличим от настоящего.
	fixtureRecipient = "n2-fixture@example.invalid"
)

// sanOf — точный URI SAN сервера ленты модуля (форма декларации
// `global.kacho.spiffe.<служба>`).
func sanOf(module string) string { return "spiffe://kacho.cloud/ns/kacho/sa/kacho-" + module }

// ── тестовый УЦ ──────────────────────────────────────────────────────────────

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

var serial struct {
	mu sync.Mutex
	n  int64
}

func nextSerial() *big.Int {
	serial.mu.Lock()
	defer serial.mu.Unlock()
	serial.n++
	return big.NewInt(serial.n + 1)
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "notify source fixture CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
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
	return &testCA{cert: cert, key: key, pool: pool}
}

// leaf выпускает лист с одним URI-SAN. Серверный лист несёт ещё DNS
// `localhost` и IP петли: проверка имени узла проходит при любом SAN, и
// различает серверы ровно URI-SAN — один факт.
func (ca *testCA) leaf(t *testing.T, cn string, server bool, uri string) tls.Certificate {
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
		SerialNumber: nextSerial(),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		URIs:         []*url.URL{u},
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// peerTLS — удостоверение notify для mTLS к источникам: клиентский лист с
// SAN notify и УЦ, которым проверяется сервер. Точный SAN сервера цикл
// сверяет сам по записи перечня.
func (ca *testCA) peerTLS(t *testing.T) *tls.Config {
	t.Helper()
	return &tls.Config{
		Certificates: []tls.Certificate{ca.leaf(t, "notify", false, notifySAN)},
		RootCAs:      ca.pool,
		MinVersion:   tls.VersionTLS12,
	}
}

// ── фикстурный источник ──────────────────────────────────────────────────────

type subMode int

const (
	// subOpen — поток открывается и шлёт `SubscriptionOpened` первым кадром.
	subOpen subMode = iota
	// subSilent — поток принят, но `SubscriptionOpened` не приходит никогда.
	subSilent
	// subUnavailable — сервер подписки остановлен: UNAVAILABLE.
	subUnavailable
)

// claimCall — один вызов `Claim`, как его увидел сервер.
type claimCall struct {
	max     uint32
	classes []notifyv1.NotificationClass
	start   time.Time
	end     time.Time
	ended   bool
	err     error // контекст вызова закончился раньше ответа
	leased  int   // строк, чья аренда закоммичена этим вызовом
}

// streamSeen — один поток `Subscribe`, как его увидел сервер.
type streamSeen struct {
	kinds []string
	start time.Time
	end   time.Time
	ended bool
	err   error
}

type fakeRow struct {
	id         string
	leaseUntil time.Time
	done       bool
}

type sourceOpts struct {
	// serverSAN — URI-SAN сертификата сервера; пусто — sanOf(module).
	serverSAN string
	// leaseTTL — длительность аренды, которую выставляет `Claim`.
	leaseTTL time.Duration
	// mode — поведение сервера подписки.
	mode subMode
}

type fakeSource struct {
	module   string
	addr     string
	leaseTTL time.Duration

	mu      sync.Mutex
	rows    []*fakeRow
	seq     int
	calls   []*claimCall
	frozen  bool
	delays  []time.Duration
	mode    subMode
	streams []*streamSeen
	live    map[*streamSeen]chan struct{}
}

type feedSide struct {
	notifyv1.UnimplementedInternalNotificationFeedServiceServer
	s *fakeSource
}

type subSide struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer
	s *fakeSource
}

// newFakeSource поднимает источник модуля на петле с mTLS: клиент обязан
// предъявить лист этого УЦ.
func newFakeSource(t *testing.T, ca *testCA, module string, o sourceOpts) *fakeSource {
	t.Helper()
	if o.serverSAN == "" {
		o.serverSAN = sanOf(module)
	}
	if o.leaseTTL == 0 {
		o.leaseTTL = time.Hour
	}
	s := &fakeSource{module: module, leaseTTL: o.leaseTTL, mode: o.mode, live: map[*streamSeen]chan struct{}{}}
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{ca.leaf(t, "source-"+module, true, o.serverSAN)},
		ClientCAs:    ca.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	})
	srv := grpc.NewServer(grpc.Creds(creds))
	notifyv1.RegisterInternalNotificationFeedServiceServer(srv, feedSide{s: s})
	subscriptionv1.RegisterInternalSubscriptionServiceServer(srv, subSide{s: s})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.addr = l.Addr().String()
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return s
}

// record — запись перечня источников для этого источника.
func (s *fakeSource) record(auth config.Authorization) config.Source {
	return config.Source{
		Module:         s.module,
		FeedAddr:       s.addr,
		SAN:            sanOf(s.module),
		Classes:        []feed.Class{feed.ClassSecurity, feed.ClassNotice},
		RecipientForms: []config.RecipientForm{config.RecipientAddress},
		Authorization:  auth,
	}
}

// put ставит n строк; signal — разбудить открытые потоки событием ленты
// (как коммит постановки пишет строку журнала).
func (s *fakeSource) put(n int, signal bool) []string {
	s.mu.Lock()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		s.seq++
		id := fmt.Sprintf("%s-row-%d", s.module, s.seq)
		s.rows = append(s.rows, &fakeRow{id: id})
		ids = append(ids, id)
	}
	var wake []chan struct{}
	if signal {
		for _, ch := range s.live {
			wake = append(wake, ch)
		}
	}
	s.mu.Unlock()
	for _, ch := range wake {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return ids
}

// complete — исход строки записан: строка больше не выдаётся.
func (s *fakeSource) complete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.id == id {
			r.done = true
		}
	}
}

func (s *fakeSource) setFrozen(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frozen = v
}

// delayNext — следующие вызовы `Claim` (по порядку) отвечают через d ПОСЛЕ
// коммита аренды.
func (s *fakeSource) delayNext(d ...time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delays = append(s.delays, d...)
}

func (s *fakeSource) claimCalls() []claimCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]claimCall, len(s.calls))
	for i, c := range s.calls {
		out[i] = *c
	}
	return out
}

func (s *fakeSource) streamsSeen() []streamSeen {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]streamSeen, len(s.streams))
	for i, st := range s.streams {
		out[i] = *st
	}
	return out
}

func (s *fakeSource) endCall(c *claimCall, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.end, c.ended, c.err = time.Now(), true, err
}

// Claim — границы входа сервера ленты corelib, затем аренда пачки.
func (f feedSide) Claim(ctx context.Context, req *notifyv1.ClaimRequest) (*notifyv1.ClaimResponse, error) {
	s := f.s
	call := &claimCall{max: req.GetMax(), classes: slices.Clone(req.GetClasses()), start: time.Now()}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	frozen := s.frozen
	s.mu.Unlock()

	switch m := req.GetMax(); {
	case m == 0:
		s.endCall(call, nil)
		return nil, status.Error(codes.InvalidArgument, "max: required")
	case m > feed.MaxClaim:
		s.endCall(call, nil)
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("max: must be ≤ %d", feed.MaxClaim))
	}
	if len(req.GetClasses()) == 0 {
		s.endCall(call, nil)
		return nil, status.Error(codes.InvalidArgument, "classes: required")
	}
	wantNotice := false
	for _, c := range req.GetClasses() {
		switch c {
		case notifyv1.NotificationClass_NOTICE:
			wantNotice = true
		case notifyv1.NotificationClass_SECURITY:
		default:
			s.endCall(call, nil)
			return nil, status.Error(codes.InvalidArgument, "classes: "+c.String()+" is not a class")
		}
	}

	if frozen {
		// Зависший источник: вызов, пришедший во время заморозки, не
		// отвечает НИКОГДА — до конца своего контекста. Разморозка
		// отвечает только новым вызовам.
		<-ctx.Done()
		s.endCall(call, ctx.Err())
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	// Коммит аренды — до ответа: строка занята, даже если ответ не дойдёт.
	s.mu.Lock()
	now := time.Now()
	type leasedRow struct {
		id    string
		until time.Time
	}
	var got []leasedRow
	if wantNotice {
		for _, r := range s.rows {
			if len(got) >= int(req.GetMax()) {
				break
			}
			if r.done || now.Before(r.leaseUntil) {
				continue
			}
			r.leaseUntil = now.Add(s.leaseTTL)
			got = append(got, leasedRow{id: r.id, until: r.leaseUntil})
		}
	}
	call.leased = len(got)
	var delay time.Duration
	if len(s.delays) > 0 {
		delay, s.delays = s.delays[0], s.delays[1:]
	}
	s.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			s.endCall(call, ctx.Err())
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	}
	out := &notifyv1.ClaimResponse{}
	sent := time.Now()
	for _, r := range got {
		out.Notifications = append(out.Notifications, &notifyv1.ClaimedNotification{
			Id:             r.id,
			LeaseToken:     "lease-" + r.id,
			LeaseRemaining: durationpb.New(max(r.until.Sub(sent), 0)),
			Template:       f.s.module + "/hello",
			SchemaRev:      1,
			Class:          notifyv1.NotificationClass_NOTICE,
			Recipient:      &notifyv1.ClaimedNotification_Address{Address: fixtureRecipient},
			EnqueuedAt:     timestamppb.New(now),
			ExpiresIn:      durationpb.New(time.Hour),
		})
	}
	s.endCall(call, nil)
	return out, nil
}

// Subscribe — поток ленты по режиму источника.
func (f subSide) Subscribe(req *subscriptionv1.SubscriptionRequest,
	stream grpc.ServerStreamingServer[subscriptionv1.SubscriptionMessage]) error {
	s := f.s
	ctx := stream.Context()
	seen := &streamSeen{kinds: slices.Clone(req.GetKinds()), start: time.Now()}
	wake := make(chan struct{}, 16)
	s.mu.Lock()
	s.streams = append(s.streams, seen)
	s.live[seen] = wake
	mode := s.mode
	s.mu.Unlock()
	end := func(err error) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.live, seen)
		seen.end, seen.ended, seen.err = time.Now(), true, err
		return err
	}

	switch mode {
	case subUnavailable:
		return end(status.Error(codes.Unavailable, "fixture: subscription server is stopped"))
	case subSilent:
		<-ctx.Done()
		return end(status.FromContextError(ctx.Err()).Err())
	}
	if err := stream.Send(&subscriptionv1.SubscriptionMessage{Message: &subscriptionv1.SubscriptionMessage_Opened{
		Opened: &subscriptionv1.SubscriptionOpened{Position: "0", CaughtUp: true},
	}}); err != nil {
		return end(err)
	}
	pos := 0
	for {
		select {
		case <-ctx.Done():
			return end(status.FromContextError(ctx.Err()).Err())
		case <-wake:
			pos++
			if err := stream.Send(&subscriptionv1.SubscriptionMessage{Message: &subscriptionv1.SubscriptionMessage_Event{
				Event: &subscriptionv1.SubscriptionEvent{
					Position:   fmt.Sprint(pos),
					Kind:       feedKind,
					ResourceId: s.module,
					Change:     subscriptionv1.SubscriptionEvent_UPDATED,
					Carrier: &subscriptionv1.SubscriptionEvent_StateUnavailable_{
						StateUnavailable: &subscriptionv1.SubscriptionEvent_StateUnavailable{
							Reason: subscriptionv1.SubscriptionEvent_StateUnavailable_NOT_PRODUCED,
						},
					},
				},
			}}); err != nil {
				return end(err)
			}
		}
	}
}

// ── фикстурный получатель пачек ──────────────────────────────────────────────

type delivery struct {
	module string
	id     string
	at     time.Time
}

// fakeDeliverer — получатель пачек цикла: свободных исполнителей столько,
// сколько задала проба; строку, которую получил, закрывает у источника
// (исход записан — следующий Claim её не выдаёт).
type fakeDeliverer struct {
	sources map[string]*fakeSource

	mu   sync.Mutex
	free int
	got  []delivery
}

func newDeliverer(free int, sources ...*fakeSource) *fakeDeliverer {
	d := &fakeDeliverer{free: free, sources: map[string]*fakeSource{}}
	for _, s := range sources {
		d.sources[s.module] = s
	}
	return d
}

func (d *fakeDeliverer) Free() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.free
}

func (d *fakeDeliverer) setFree(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.free = n
}

// deliveries — сколько раз строка дошла до получателя и когда впервые.
func (d *fakeDeliverer) deliveries(module, id string) (int, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n, first := 0, time.Time{}
	for _, g := range d.got {
		if g.module == module && g.id == id {
			if n == 0 {
				first = g.at
			}
			n++
		}
	}
	return n, first
}

// ── ожидание исхода ──────────────────────────────────────────────────────────

// waitFor ждёт исхода cond не дольше budget. Возвращает, наступил ли исход.
// Ожидание заканчивается ИСХОДОМ, а срок — только верхняя граница.
func waitFor(budget time.Duration, cond func() bool) bool {
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if cond() {
			return true
		}
		select {
		case <-deadline.C:
			return cond()
		case <-tick.C:
		}
	}
}

// counterValue — значение счётчика name с меткой source; ok=false — ряда нет.
func counterValue(t *testing.T, g prometheus.Gatherer, name, source string) (float64, bool) {
	t.Helper()
	mfs, err := g.Gather()
	if err != nil {
		t.Fatalf("сбор метрик: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "source" && lp.GetValue() == source {
					return m.GetCounter().GetValue(), true
				}
			}
		}
	}
	return 0, false
}
