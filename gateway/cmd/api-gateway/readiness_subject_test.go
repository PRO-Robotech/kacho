// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// readiness_subject_test.go — клиент субъекта ходит к службе доступа ТЕМ
// соединением, которое опрашивает `/readyz` края (kacho#3284, класс моста).
//
// ПРИЗНАК. Тот же, что у моста (Ф12-35 (в)): после переката службы доступа
// `/readyz` — «готов», а вызов идёт соединением, о котором готовность не
// говорит, и отвечает сразу отказом соединения. Клиент субъекта — путь входа по
// токену (`LookupSubject`), базовой полосы и проверки администратора системы:
// отказ там — отказ входа, а не одного глагола.
//
// МЕХАНИЗМ. Клиент набирал службу доступа СВОИМ соединением (grpc.NewClient по
// `IAMInternalAddr`) сверх соединения `iamInternal` карты бэкендов, которое
// опрашивает `/readyz`. После разрыва каждое восстанавливается по своему
// таймеру, и «готов» о соединении входа не говорил ничего.
//
// ПОРЯДОК ДЕТЕРМИНИРОВАН так же, как в пробе моста
// (`restmux/bridge_readiness_connection_test.go`): клиент бьётся о лежащую
// службу первым, его таймер к подъёму ушёл на третий шаг; соединение готовности
// впервые спрошено после подъёма.

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/health"
	"github.com/PRO-Robotech/kacho/gateway/internal/upstreamfailure"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

const subjectProbeUser = "usr00000000000003284"

type subjectProbeIAM struct {
	iamv1.UnimplementedInternalIAMServiceServer
}

func (subjectProbeIAM) LookupSubject(context.Context, *iamv1.LookupSubjectRequest) (*iamv1.LookupSubjectResponse, error) {
	return &iamv1.LookupSubjectResponse{
		Subject: &iamv1.LookupSubjectResponse_User{User: &iamv1.User{Id: subjectProbeUser}},
	}, nil
}

// acceptCounting считает принятые транспортные соединения: единица счёта —
// TCP-соединение края со службой.
type acceptCounting struct {
	net.Listener
	accepted *atomic.Int64
}

func (l acceptCounting) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return c, err
}

// subjectBackend — внутренний слушатель службы доступа на постоянном адресе,
// который сначала лежит, а потом поднимается (под за сервисом после переката).
// Адрес удержан (kacho#3331): пока служба лежит, подключение отвергается и
// занять адрес не может никто, а подъём — слушание на том же сокете, без окна.
type subjectBackend struct {
	addr     string
	held     *privateloopback.Held
	accepted atomic.Int64
	gs       *grpc.Server
}

func newSubjectBackend(t *testing.T) *subjectBackend {
	t.Helper()
	h := privateloopback.Hold(t)
	b := &subjectBackend{addr: h.Addr(), held: h}
	t.Cleanup(b.stop)
	return b
}

func (b *subjectBackend) start(t *testing.T) {
	t.Helper()
	lis := b.held.Listen(t)
	gs := grpc.NewServer()
	iamv1.RegisterInternalIAMServiceServer(gs, subjectProbeIAM{})
	healthpb.RegisterHealthServer(gs, grpchealth.NewServer())
	b.gs = gs
	go func() { _ = gs.Serve(acceptCounting{Listener: lis, accepted: &b.accepted}) }()
}

func (b *subjectBackend) stop() {
	if b.gs != nil {
		b.gs.Stop()
		b.gs = nil
	}
}

// servingAddr — публичный слушатель службы доступа (ключ `iam`), всегда
// обслуживающий: он критичен для готовности, но предмет пробы не он.
func servingAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	healthpb.RegisterHealthServer(gs, grpchealth.NewServer())
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	return lis.Addr().String()
}

// subjectEdgeOf — клиент субъекта и `/readyz` края, собранные так же, как их
// собирает корень (main.go: dialBackends → subjectClientOf и readinessTargets).
func subjectEdgeOf(t *testing.T, iamInternalAddr string) (lookup func(string) error, readyz http.Handler) {
	t.Helper()
	cfg := config.Config{IAMAddr: servingAddr(t), IAMInternalAddr: iamInternalAddr, ListenAddr: ":0"}
	tally := upstreamfailure.NewTally(upstreamKeys(cfg))
	backends, closeAll, err := dialBackends(cfg, tally)
	if err != nil {
		t.Fatalf("dialBackends: %v", err)
	}
	t.Cleanup(closeAll)
	subject, err := subjectClientOf(cfg, backends, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("subjectClientOf: %v", err)
	}
	targets, critical := readinessTargets(backends, nil, false)
	lookup = func(externalID string) error {
		_, err := subject.LookupByExternalID(context.Background(), externalID)
		return err
	}
	return lookup, health.HTTPReadyz(targets, critical, slog.New(slog.DiscardHandler))
}

func readyzCode(h http.Handler) (int, string) {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return w.Code, w.Body.String()
}

// TestSubjectClientOpensNoConnectionBesideTheOneReadinessAsks — механизм, без
// таймеров: после вопроса готовности и вопроса о субъекте внутренний слушатель
// службы доступа принял ОДНО соединение края.
func TestSubjectClientOpensNoConnectionBesideTheOneReadinessAsks(t *testing.T) {
	b := newSubjectBackend(t)
	b.start(t)
	lookup, readyz := subjectEdgeOf(t, b.addr)

	if code, body := readyzCode(readyz); code != http.StatusOK {
		t.Fatalf("/readyz при поднятой службе: %d %s", code, body)
	}
	if err := lookup("ext-mechanism"); err != nil {
		t.Fatalf("вопрос о субъекте при поднятой службе: %v", err)
	}
	if got := b.accepted.Load(); got != 1 {
		t.Fatalf("внутренний слушатель службы доступа принял соединений края: %d, ожидалось 1 — "+
			"клиент субъекта ходит не тем соединением, которое опрашивает /readyz", got)
	}
}

// TestSubjectLookupAnswersOnceReadinessSaysReadyAfterBackendReturns —
// наблюдаемое: служба лежит, вход бьётся о неё, служба возвращается; первый же
// «готов» `/readyz` обязан значить, что вопрос о субъекте отвечает.
func TestSubjectLookupAnswersOnceReadinessSaysReadyAfterBackendReturns(t *testing.T) {
	b := newSubjectBackend(t)
	lookup, readyz := subjectEdgeOf(t, b.addr)

	// Служба лежит: клиент субъекта получает отказ соединения и уводит таймер.
	downSince := time.Now()
	if err := lookup("ext-down"); err == nil {
		t.Fatal("вопрос о субъекте прошёл при лежащей службе — условие отказа не создано")
	}
	// Подъём — после второго повтора таймера (не позже 3,12 с) и раньше третьего
	// (не раньше 4,13 с): окно, где соединение клиента само не пробует.
	time.Sleep(time.Until(downSince.Add(3400 * time.Millisecond)))
	b.start(t)

	deadline := time.Now().Add(15 * time.Second)
	for {
		code, body := readyzCode(readyz)
		if code == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("/readyz не сказал «готов» за 15 с после подъёма службы: %d %s", code, body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := lookup("ext-after-ready"); err != nil {
		t.Fatalf("вопрос о субъекте сразу после «готов» в /readyz: %v — соединение входа осталось в "+
			"отказе, хотя готовность сказала «готов»", err)
	}
}

// TestSubjectClientRefusesAConnectionToAnotherAddress — соединение `iamInternal`
// в карте ведёт не туда, куда велит адрес внутреннего слушателя: клиент
// субъекта не собирается, а не ходит молча мимо соединения, о котором
// спрашивает готовность.
func TestSubjectClientRefusesAConnectionToAnotherAddress(t *testing.T) {
	cfg := config.Config{IAMInternalAddr: "127.0.0.1:1", ListenAddr: ":0"}
	backends, closeAll, err := dialBackends(config.Config{IAMInternalAddr: "127.0.0.1:2", ListenAddr: ":0"}, nil)
	if err != nil {
		t.Fatalf("dialBackends: %v", err)
	}
	t.Cleanup(closeAll)
	_, err = subjectClientOf(cfg, backends, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("клиент субъекта принял соединение «iamInternal» к 127.0.0.1:2 при адресе 127.0.0.1:1")
	}
	if !strings.Contains(err.Error(), "iamInternal") {
		t.Fatalf("отказ не называет ключ адреса: %v", err)
	}
}
