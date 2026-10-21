// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bridge_readiness_connection_test.go — мост ходит к службе ТЕМ соединением,
// которое опрашивает `/readyz` края (kacho#3284).
//
// ПРИЗНАК. Проба консоли Ф12-35 (в) после переката службы доступа: `/readyz`
// края — «готов», право решено, а перечень ключей
// (`GET /iam/v1/users/{id}/accessKeys`) восемь раз подряд получил 503
// «connection error … connection refused» за 0 мс (console-e2e, run 39101759562).
//
// МЕХАНИЗМ. `Register<X>ServiceHandlerFromEndpoint` заводит СВОЁ соединение на
// каждый вызов: по одному на службу контракта и на каждый из трёх
// мультиплексоров — к службе доступа их было 51 сверх того, которое опрашивает
// `/readyz`. После разрыва каждое соединение восстанавливается по своему
// экспоненциальному таймеру; вызов на соединении в отказе отвечает сразу, не
// дожидаясь. «Готов» говорил о восстановлении одного соединения, а перечень
// ключей шёл другим — тем, чей таймер ещё не истёк.
//
// ПОЧЕМУ ПОРЯДОК ЗДЕСЬ ДЕТЕРМИНИРОВАН. На стенде, какое соединение восстановится
// первым, решает разброс таймеров. Здесь он закреплён: мост бьётся о
// неподнятую службу первым, и его таймер к подъёму службы ушёл на третий шаг
// (умолчание grpc: 1 с × 1,6ⁿ, разброс ±20 % — шаг 2,56 с не короче 2,05 с);
// соединение готовности впервые спрошено ПОСЛЕ подъёма. Если мост ходит своим
// соединением, «готов» приходит раньше, чем мост может ответить, — красное без
// жребия. Если тем же — «готов» не приходит, пока соединение не восстановлено.
package restmux

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	iampb "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	edgehealth "github.com/PRO-Robotech/kacho/gateway/internal/health"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

const (
	bridgeProbeUser    = "usr00000000000003284"
	bridgeProbeAccount = "acc00000000000003284"
)

type bridgeProbeKeys struct {
	iampb.UnimplementedAccessKeyServiceServer
}

func (bridgeProbeKeys) List(context.Context, *iampb.ListAccessKeysRequest) (*iampb.ListAccessKeysResponse, error) {
	return &iampb.ListAccessKeysResponse{}, nil
}

type bridgeProbeAccounts struct {
	iampb.UnimplementedAccountServiceServer
}

func (bridgeProbeAccounts) Get(_ context.Context, in *iampb.GetAccountRequest) (*iampb.Account, error) {
	return &iampb.Account{Id: in.GetAccountId()}, nil
}

// countingListener считает принятые транспортные соединения: единица счёта —
// TCP-соединение края со службой.
type countingListener struct {
	net.Listener
	accepted *atomic.Int64
}

func (l countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return c, err
}

// iamBackend — служба доступа на постоянном адресе, которая сначала лежит, а
// потом поднимается: тот же адрес, что у пода за сервисом кластера после
// переката. Адрес удержан (kacho#3331): пока служба лежит, подключение
// отвергается и занять адрес не может никто, а подъём — слушание на том же
// сокете, без окна, в которое адрес займёт посторонний.
type iamBackend struct {
	addr     string
	held     *privateloopback.Held
	accepted atomic.Int64
	gs       *grpc.Server
}

func newIAMBackend(t *testing.T) *iamBackend {
	t.Helper()
	h := privateloopback.Hold(t)
	b := &iamBackend{addr: h.Addr(), held: h}
	t.Cleanup(b.stop)
	return b
}

func (b *iamBackend) start(t *testing.T) {
	t.Helper()
	lis := b.held.Listen(t)
	gs := grpc.NewServer()
	iampb.RegisterAccessKeyServiceServer(gs, bridgeProbeKeys{})
	iampb.RegisterAccountServiceServer(gs, bridgeProbeAccounts{})
	healthpb.RegisterHealthServer(gs, health.NewServer())
	b.gs = gs
	go func() { _ = gs.Serve(countingListener{Listener: lis, accepted: &b.accepted}) }()
}

func (b *iamBackend) stop() {
	if b.gs != nil {
		b.gs.Stop()
		b.gs = nil
	}
}

// bridgePaths — два глагола двух разных служб контракта за одним адресом: прежде
// каждый шёл своим соединением.
var bridgePaths = []string{
	"/iam/v1/users/" + bridgeProbeUser + "/accessKeys",
	"/iam/v1/accounts/" + bridgeProbeAccount,
}

func serveGet(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// edgeOf — мост и `/readyz` края над ОДНОЙ картой соединений, как их собирает
// корень композиции (main.go: dialBackends → NewMux и readinessTargets).
func edgeOf(t *testing.T, addr string) (bridge http.Handler, readyz http.Handler) {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	backends := proxy.Backends{"iam": conn}
	m, err := NewMux(t.Context(), map[string]string{"iam": addr}, backends, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	return m, edgehealth.HTTPReadyz(backends, map[string]bool{"iam": true}, silentTestLogger())
}

// TestBridgeOpensNoConnectionBesideTheOneReadinessAsks — механизм, без таймеров:
// после вопроса готовности и глаголов двух служб служба приняла ОДНО соединение.
func TestBridgeOpensNoConnectionBesideTheOneReadinessAsks(t *testing.T) {
	b := newIAMBackend(t)
	b.start(t)
	bridge, readyz := edgeOf(t, b.addr)

	if w := serveGet(readyz, "/readyz"); w.Code != http.StatusOK {
		t.Fatalf("/readyz при поднятой службе: %d %s", w.Code, w.Body.String())
	}
	for _, p := range bridgePaths {
		if w := serveGet(bridge, p); w.Code != http.StatusOK {
			t.Fatalf("GET %s при поднятой службе: %d %s", p, w.Code, w.Body.String())
		}
	}
	if got := b.accepted.Load(); got != 1 {
		t.Fatalf("служба приняла соединений края: %d, ожидалось 1 — мост ходит не тем соединением, "+
			"которое опрашивает /readyz, и «готов» о его состоянии не говорит", got)
	}
}

// TestBridgeOwnDialIsOnePerBackend — законный близнец: соединений не передано
// (сборка моста вне корня), мост заводит ОДНО своё на адрес, а не по одному на
// службу контракта и мультиплексор.
func TestBridgeOwnDialIsOnePerBackend(t *testing.T) {
	b := newIAMBackend(t)
	b.start(t)
	m, err := NewMux(t.Context(), map[string]string{"iam": b.addr}, nil, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	for _, p := range bridgePaths {
		if w := serveGet(m, p); w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", p, w.Code, w.Body.String())
		}
	}
	if got := b.accepted.Load(); got != 1 {
		t.Fatalf("служба приняла соединений моста: %d, ожидалось 1 на адрес", got)
	}
}

// TestBridgeRefusesAConnectionToAnotherAddress — переданное соединение ведёт не
// туда, куда велит карта адресов: мост не стартует, а не ходит молча мимо
// соединения, о котором спрашивает готовность.
func TestBridgeRefusesAConnectionToAnotherAddress(t *testing.T) {
	conn, err := grpc.NewClient("127.0.0.1:2", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_, err = NewMux(t.Context(), map[string]string{"iam": "127.0.0.1:1"}, map[string]*grpc.ClientConn{"iam": conn}, nil, 30*time.Second)
	if err == nil {
		t.Fatal("NewMux принял соединение «iam» к 127.0.0.1:2 при адресе 127.0.0.1:1")
	}
	if !strings.Contains(err.Error(), "iam") {
		t.Fatalf("отказ не называет ключ адреса: %v", err)
	}
}

// TestBridgeServesOnceReadinessSaysReadyAfterBackendReturns — наблюдаемое Ф12-35
// (в): служба лежит, мост бьётся о неё, служба возвращается; первый же «готов»
// `/readyz` обязан значить, что глаголы моста отвечают, — ни одного 503.
func TestBridgeServesOnceReadinessSaysReadyAfterBackendReturns(t *testing.T) {
	b := newIAMBackend(t)
	bridge, readyz := edgeOf(t, b.addr)

	// Служба лежит: мост получает отказ соединения и уводит свой таймер.
	downSince := time.Now()
	for _, p := range bridgePaths {
		if w := serveGet(bridge, p); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET %s при лежащей службе: %d %s — ожидался 503 отказа соединения", p, w.Code, w.Body.String())
		}
	}
	// Подъём — после второго повтора таймера (не позже 3,12 с) и раньше третьего
	// (не раньше 4,13 с): окно, где ни одно соединение моста не пробует сам.
	time.Sleep(time.Until(downSince.Add(3400 * time.Millisecond)))
	b.start(t)

	deadline := time.Now().Add(15 * time.Second)
	for {
		w := serveGet(readyz, "/readyz")
		if w.Code == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("/readyz не сказал «готов» за 15 с после подъёма службы: %d %s", w.Code, w.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, p := range bridgePaths {
		w := serveGet(bridge, p)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s сразу после «готов» в /readyz: %d %s — соединение моста осталось в отказе, "+
				"хотя готовность сказала «готов»", p, w.Code, w.Body.String())
		}
	}
}
