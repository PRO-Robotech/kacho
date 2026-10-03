// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package ka1budget_test — пробы стадии S2 приёмки KA1 (kacho#2713, kacho#2738,
// Р4), чьё «Дано» передаёт сборке ВЕЛИЧИНУ бюджета: KA1-22, 23, 24, 26.
//
// # Почему отдельная единица сборки
//
// В Go не собирающийся `_test.go` роняет весь пакет. На базе стадии (голова S1)
// параметров бюджета у сборок нет, и эти пробы красны ОТСУТСТВИЕМ испытуемого —
// компилятор называет недостающий параметр. Лежи они рядом с пробами S1, исход
// тех стал бы «не выполнилось», а не «зелёный» или «красный». Поэтому —
// отдельный пакет, а харнесс тот же (`ka1stand`), не копия.
//
// Все задержки — управляемые задержки ДУБЛЁРОВ; сна в пробе нет.
package ka1budget_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/e2e/ka1stand"
	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/restmux"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

// ─── KA1-22: зависший сосед на пути аутентификации ──────────────────────────

// TestKA1_22_IdentityQuestionIsBoundedByTheKnob — величину предела вопросов
// службе доступа задаёт ручка (`1500ms`): ответ за `1200ms` проходит на каждой
// строке (прежние константы `1s` отказали бы на (в) и (г)), молчание — ответ Р1
// раньше `2500ms`. Близнец столбца (2) — столбец (1) той же строки.
func TestKA1_22_IdentityQuestionIsBoundedByTheKnob(t *testing.T) {
	type row struct {
		name     string
		question func(*ka1stand.Stand) *ka1stand.Question
		present  func(*testing.T, *ka1stand.Stand) ka1stand.Presented
		native   func(*testing.T, *ka1stand.Stand) string
	}
	rows := []row{
		{"(а) Resolve", func(s *ka1stand.Stand) *ka1stand.Question { return s.Ident.SessionQ },
			func(*testing.T, *ka1stand.Stand) ka1stand.Presented {
				return ka1stand.SessionCarrier(ka1stand.SessionLive)
			}, nil},
		{"(б) SessionCutoffOf", func(s *ka1stand.Stand) *ka1stand.Question { return s.Ident.CutoffQ },
			func(*testing.T, *ka1stand.Stand) ka1stand.Presented {
				return ka1stand.SessionCarrier(ka1stand.SessionLive)
			}, nil},
		{"(в) IsRevoked второго издателя", func(s *ka1stand.Stand) *ka1stand.Question { return s.Ident.RevokedQ },
			func(t *testing.T, s *ka1stand.Stand) ka1stand.Presented {
				return ka1stand.Bearer(s.LegacyToken(t, ka1stand.JTILive))
			},
			func(t *testing.T, s *ka1stand.Stand) string { return s.LegacyToken(t, ka1stand.JTILive) }},
		{"(г) годность базового", func(s *ka1stand.Stand) *ka1stand.Question { return s.Ident.BasicQ },
			func(_ *testing.T, s *ka1stand.Stand) ka1stand.Presented { return ka1stand.Bearer(s.BasicGood) },
			func(_ *testing.T, s *ka1stand.Stand) string { return s.BasicGood }},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			// Столбец (1): отвечает с задержкой 1200ms — в пределах ручки 1500ms.
			st := ka1stand.New(t, ka1stand.Options{IdentityCallBudget: 1500 * time.Millisecond})
			r.question(st).Delay(1200 * time.Millisecond)
			if got := st.REST(t, http.MethodGet, ka1stand.ListRoute, r.present(t, st)); got.Status != http.StatusOK {
				t.Errorf("KA1-22 %s (1): ответ за 1200ms при бюджете 1500ms обязан пройти\n  получено: %s", r.name, got)
			}
			if r.native != nil {
				if got := st.GRPC(t, ka1stand.PingMethod, r.native(t, st), nil); got.St.Code() != codes.OK {
					t.Errorf("KA1-22 %s (1) нативная: ожидался OK\n  получено: %s", r.name, got)
				}
			}

			// Столбец (2): молчит.
			st2 := ka1stand.New(t, ka1stand.Options{IdentityCallBudget: 1500 * time.Millisecond})
			r.question(st2).Set(ka1stand.Silent)
			got := st2.REST(t, http.MethodGet, ka1stand.ListRoute, r.present(t, st2))
			ka1stand.RequireUnavailable(t, "KA1-22 "+r.name+" (2)", got)
			if got.TimedOut || got.Elapsed >= 2500*time.Millisecond {
				t.Errorf("KA1-22 %s (2): ответ не пришёл раньше 2500ms (%s)", r.name, got)
			}
			if r.native != nil {
				n := st2.GRPC(t, ka1stand.PingMethod, r.native(t, st2), nil)
				ka1stand.RequireNativeUnavailable(t, "KA1-22 "+r.name+" (2) нативная", n)
				if n.Elapsed >= 2500*time.Millisecond {
					t.Errorf("KA1-22 %s (2) нативная: ответ не пришёл раньше 2500ms (%s)", r.name, n.Elapsed)
				}
			}
		})
	}
}

// ─── П6: дублёр бэкенда домена за мостом ────────────────────────────────────

type backend struct {
	vpcpb.UnimplementedNetworkServiceServer
	delay atomic.Int64
	addr  string
}

func (b *backend) Get(ctx context.Context, in *vpcpb.GetNetworkRequest) (*vpcpb.Network, error) {
	if d := time.Duration(b.delay.Load()); d > 0 {
		tm := time.NewTimer(d)
		defer tm.Stop()
		select {
		case <-tm.C:
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	}
	return &vpcpb.Network{Id: in.GetNetworkId(), Name: "ka1"}, nil
}

func newBackend(t *testing.T) *backend {
	t.Helper()
	b := &backend{}
	lis := privateloopback.Listen(t)
	srv := grpc.NewServer()
	vpcpb.RegisterNetworkServiceServer(srv, b)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	b.addr = lis.Addr().String()
	return b
}

// ─── П9: владелец журнала за потоком подписки ───────────────────────────────

type journalOwner struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer
	conn subscriptionstream.OwnerConn
}

// Subscribe держит поток открытым и шлёт событие раз в 500ms.
func (o *journalOwner) Subscribe(_ *subscriptionv1.SubscriptionRequest, s subscriptionv1.InternalSubscriptionService_SubscribeServer) error {
	if err := s.Send(&subscriptionv1.SubscriptionMessage{Message: &subscriptionv1.SubscriptionMessage_Opened{
		Opened: &subscriptionv1.SubscriptionOpened{Position: "p0", CaughtUp: true, HonoredFilters: []string{"project_id"}, RetainsEverything: true},
	}}); err != nil {
		return err
	}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for i := 1; ; i++ {
		select {
		case <-s.Context().Done():
			return nil
		case <-tick.C:
			if err := s.Send(&subscriptionv1.SubscriptionMessage{Message: &subscriptionv1.SubscriptionMessage_Event{
				Event: &subscriptionv1.SubscriptionEvent{
					Position: "p" + strconv.Itoa(i), Kind: "vpc.network", ResourceId: "net-ka1", ProjectId: "prj-ka1",
					Change: subscriptionv1.SubscriptionEvent_CREATED,
				},
			}}); err != nil {
				return err
			}
		}
	}
}

func newJournalOwner(t *testing.T) *journalOwner {
	t.Helper()
	o := &journalOwner{}
	lis := privateloopback.Listen(t)
	srv := grpc.NewServer()
	subscriptionv1.RegisterInternalSubscriptionServiceServer(srv, o)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	o.conn = subscriptionv1.NewInternalSubscriptionServiceClient(conn)
	return o
}

// ─── П10: сборка края — слой аутентификации, мост, ручка потока ─────────────

type p10 struct {
	url     string
	st      *ka1stand.Stand
	backend *backend
}

func newP10(t *testing.T, identity, bridge time.Duration) *p10 {
	t.Helper()
	st := ka1stand.New(t, ka1stand.Options{IdentityCallBudget: identity})
	be := newBackend(t)
	owner := newJournalOwner(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bridgeMux, err := restmux.NewMux(ctx, map[string]string{"vpc": be.addr}, nil, nil, bridge)
	require.NoError(t, err)

	stream, err := subscriptionstream.NewHandler(subscriptionstream.Config{
		Owners:       subscriptionstream.Owners{"probe": owner.conn},
		StreamBudget: 5 * time.Second, Heartbeat: 2 * time.Second,
		MaxStreams: 4, MaxStreamsPerSubject: 4,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(subscriptionstream.Path, stream)
	mux.Handle("/", bridgeMux)
	srv := privateloopback.NewServer(t, st.Auth.HTTP(mux))
	t.Cleanup(srv.Close)
	return &p10{url: srv.URL, st: st, backend: be}
}

// unaryGet — унарный `Get` через мост с годным токеном; срок пробы 3s.
func (p *p10) unaryGet(t *testing.T) (code int, body string, elapsed time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), ka1stand.ProbeDeadline)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url+"/vpc/v1/networks/net-ka1", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+p.st.OurToken(t, ka1stand.JTILive, nil))
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "ответа нет за срок пробы: " + err.Error(), time.Since(start)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), time.Since(start)
}

// KA1-23 — зависший бэкенд за мостом: `504`, тело несёт `code=4`, раньше `2s`.
// Близнец — задержка бэкенда 0: `200` и тело ресурса.
func TestKA1_23_HungBackendIsAnsweredWithDeadlineExceeded(t *testing.T) {
	p := newP10(t, time.Second, 300*time.Millisecond)
	p.backend.delay.Store(int64(5 * time.Second))
	code, body, elapsed := p.unaryGet(t)
	var parsed struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal([]byte(body), &parsed)
	if code != http.StatusGatewayTimeout || parsed.Code != int(codes.DeadlineExceeded) || elapsed >= 2*time.Second {
		t.Errorf("KA1-23: ожидался 504 с code=4 раньше 2s; получено %d за %s: %s", code, elapsed, body)
	}

	p.backend.delay.Store(0)
	if code, body, _ := p.unaryGet(t); code != http.StatusOK || !strings.Contains(body, `"net-ka1"`) {
		t.Errorf("KA1-23 близнец: ожидался 200 с телом ресурса; получено %d: %s", code, body)
	}
}

// KA1-24 — бюджеты края не трогают поток подписки: поток получает события и
// после отметки `1s` (позже обоих бюджетов) и закрывается не раньше `5s` — по
// своему сроку. Близнец — унарный `Get` через тот же мост: `504` раньше `2s`.
func TestKA1_24_BudgetsDoNotCutTheSubscriptionStream(t *testing.T) {
	p := newP10(t, 200*time.Millisecond, 300*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := url.Values{"owner": {"probe"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url+subscriptionstream.Path+"?"+q.Encode(), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+p.st.OurToken(t, ka1stand.JTILive, nil))
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "поток не открыт")

	var lastEvent time.Duration
	events := 0
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "event: ") && !strings.Contains(sc.Text(), "opened") {
			events++
			lastEvent = time.Since(start)
		}
	}
	closedAt := time.Since(start)
	if events == 0 || lastEvent <= time.Second {
		t.Errorf("KA1-24: поток не получал событий после отметки 1s (событий %d, последнее на %s)", events, lastEvent)
	}
	if closedAt < 5*time.Second {
		t.Errorf("KA1-24: поток закрылся на %s — раньше своего срока 5s", closedAt)
	}

	p.backend.delay.Store(int64(5 * time.Second))
	if code, body, elapsed := p.unaryGet(t); code != http.StatusGatewayTimeout || elapsed >= 2*time.Second {
		t.Errorf("KA1-24 близнец: унарный вызов через мост обязан дать 504 раньше 2s; получено %d за %s: %s", code, elapsed, body)
	}
}

// ─── KA1-26: отзыв при выходе — в бюджете, исход выхода прежний ─────────────

// revocations — дублёр Revoke (П12): отвечает с управляемой задержкой либо
// молчит до освобождения пробой; считает принятые запросы.
type revocations struct {
	delay   time.Duration
	silent  bool
	release chan struct{}
	mu      sync.Mutex
	got     []*iamv1.RevokeRequest
}

func (r *revocations) Revoke(ctx context.Context, in *iamv1.RevokeRequest) error {
	r.mu.Lock()
	r.got = append(r.got, in)
	r.mu.Unlock()
	if r.silent {
		select {
		case <-r.release:
		case <-ctx.Done():
		}
		return errors.New("revoke: no answer")
	}
	tm := time.NewTimer(r.delay)
	defer tm.Stop()
	select {
	case <-tm.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type caller struct{}

func (caller) Verify(context.Context, string) (*handler.VerifiedCaller, error) {
	return &handler.VerifiedCaller{Subject: ka1stand.User, JTI: ka1stand.JTILive}, nil
}

func logout(t *testing.T, rev *revocations) (*httptest.ResponseRecorder, time.Duration) {
	t.Helper()
	h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Verifier:    caller{},
		Revocations: rev,
		CallBudget:  300 * time.Millisecond,
	})
	require.NoError(t, err)
	form := url.Values{"revoke_all": {"true"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer ka1-token")
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, req)
	return rec, time.Since(start)
}

func TestKA1_26_LogoutRevocationIsBoundedAndTheOutcomeStays(t *testing.T) {
	endings := ka1stand.CarrierEndingLines()

	// Столбец (2): Revoke молчит.
	silent := &revocations{silent: true, release: make(chan struct{})}
	t.Cleanup(func() { close(silent.release) })
	rec, elapsed := logout(t, silent)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	warnings, _ := out["warnings"].([]any)
	if rec.Code != http.StatusOK || out["ok"] != true || len(warnings) == 0 ||
		!sameSet(rec.Result().Header.Values("Set-Cookie"), endings) || elapsed >= 2*time.Second {
		t.Errorf("KA1-26 (2): ожидался 200, ok:true, непустой warnings, гашение носителя, раньше 2s; получено %d за %s: %s, Set-Cookie %v",
			rec.Code, elapsed, rec.Body.String(), rec.Result().Header.Values("Set-Cookie"))
	}

	// Столбец (1): Revoke отвечает за 100ms.
	answered := &revocations{delay: 100 * time.Millisecond}
	rec, _ = logout(t, answered)
	out = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	_, hasWarnings := out["warnings"]
	if rec.Code != http.StatusOK || out["ok"] != true || hasWarnings || !sameSet(rec.Result().Header.Values("Set-Cookie"), endings) {
		t.Errorf("KA1-26 (1): ожидался 200, ok:true без warnings и гашение носителя; получено %d: %s", rec.Code, rec.Body.String())
	}
	answered.mu.Lock()
	defer answered.mu.Unlock()
	if len(answered.got) != 1 || answered.got[0].GetTokenJti() != ka1stand.JTILive ||
		answered.got[0].GetUserId() != ka1stand.User || !answered.got[0].GetRevokeAllUserTokens() {
		t.Errorf("KA1-26 (1): дублёр принял %d запросов Revoke, ожидался ровно один с token_jti, user_id и revoke_all_user_tokens: %v",
			len(answered.got), answered.got)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		seen[x]--
	}
	for _, v := range seen {
		if v != 0 {
			return false
		}
	}
	return true
}
