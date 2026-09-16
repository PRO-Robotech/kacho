// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// human_session_adapter_test.go — адаптер над `InternalHumanSessionService.Resolve`
// и замок разрешения на проводе (Ф3-16, половина края; Ф3-13 — классификация).
//
// Дублёр службы отдаёт НАСТОЯЩИЕ сообщения контракта через настоящий gRPC
// (bufconn), а не подставной читатель: предмет замка — что микросекундные
// величины ДВУХ ответов проходят сериализацию контракта и сравниваются краем
// включающе на суб-секундной оси (Р7, §4.1 п.19). Усечение любой из величин до
// секунды обязано краснеть здесь и молчать на паре, разнесённой на секунду.
package clients_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// serviceStub — дублёр двух вопросов края. Каждый вопрос отвечает ровно тем,
// что задано; незаданный — UNIMPLEMENTED от встроенного каркаса.
type serviceStub struct {
	iamv1.UnimplementedInternalHumanSessionServiceServer
	iamv1.UnimplementedInternalSessionRevocationsServiceServer

	sessions map[string]*iamv1.HumanSession // носитель → сессия
	resolve  error                          // подставной отказ Resolve
	cutoff   *timestamppb.Timestamp         // отсечка субъекта (nil — нет)
	cutoffFn func() error                   // подставной отказ SessionCutoffOf
}

func (s *serviceStub) Resolve(_ context.Context, in *iamv1.ResolveHumanSessionRequest) (*iamv1.ResolveHumanSessionResponse, error) {
	if s.resolve != nil {
		return nil, s.resolve
	}
	if in.GetBearer() == "" {
		return nil, status.Error(codes.InvalidArgument, "bearer: required")
	}
	sess, ok := s.sessions[in.GetBearer()]
	if !ok {
		return &iamv1.ResolveHumanSessionResponse{Found: false}, nil
	}
	return &iamv1.ResolveHumanSessionResponse{Found: true, Session: sess}, nil
}

func (s *serviceStub) SessionCutoffOf(_ context.Context, _ *iamv1.SessionCutoffOfRequest) (*iamv1.SessionCutoffOfResponse, error) {
	if s.cutoffFn != nil {
		if err := s.cutoffFn(); err != nil {
			return nil, err
		}
	}
	if s.cutoff == nil {
		return &iamv1.SessionCutoffOfResponse{Found: false}, nil
	}
	return &iamv1.SessionCutoffOfResponse{Found: true, RevokeBefore: s.cutoff}, nil
}

// dialStub поднимает дублёра на bufconn и открывает к нему соединение.
func dialStub(t *testing.T, stub *serviceStub) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	iamv1.RegisterInternalHumanSessionServiceServer(srv, stub)
	iamv1.RegisterInternalSessionRevocationsServiceServer(srv, stub)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func humanSession(userID string, authAt time.Time) *iamv1.HumanSession {
	return &iamv1.HumanSession{
		UserId:          userID,
		Email:           "a@example.com",
		DisplayName:     "A",
		AuthenticatedAt: timestamppb.New(authAt),
		ExpiresAt:       timestamppb.New(authAt.Add(24 * time.Hour).Truncate(time.Second)),
		AssuranceLevel:  "1",
		EmailVerified:   true,
	}
}

// laneOverStub — полоса личности края поверх НАСТОЯЩЕГО адаптера.
func laneOverStub(t *testing.T, conn *grpc.ClientConn) http.Handler {
	t.Helper()
	ad := clients.NewSessionRevocationsAdapter(conn)
	a := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", nil,
		slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithHumanSession(ad).
		WithSessionCutoffCheck(ad, time.Hour)
	return a.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
}

func present(chain http.Handler, bearer string) int {
	req := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: bearer})
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)
	return rec.Code
}

// TestHumanSessionAdapter_F3_16_MicrosecondCutoffIsComparedInclusivelyOnTheWire —
// замок разрешения: S1 в t₁ (µs ≠ 0), отсечка t₁ − 1 µs → S1 годна; S0 с
// моментом ровно t₁ − 1 µs → отвергнута.
func TestHumanSessionAdapter_F3_16_MicrosecondCutoffIsComparedInclusivelyOnTheWire(t *testing.T) {
	t1 := time.Date(2026, 9, 16, 12, 0, 0, 123456000, time.UTC) // .123456 с
	cut := t1.Add(-time.Microsecond)
	stub := &serviceStub{
		sessions: map[string]*iamv1.HumanSession{
			"s1": humanSession("usr-1", t1),
			"s0": humanSession("usr-1", cut), // ровно момент отсечки
		},
		cutoff: timestamppb.New(cut),
	}
	chain := laneOverStub(t, dialStub(t, stub))

	if code := present(chain, "s1"); code != http.StatusOK {
		t.Fatalf("S1 (t₁) при отсечке t₁−1µs обязана быть годной, получено %d — усечение до секунды на одной из сторон делает пару сравнимой только на целых секундах", code)
	}
	if code := present(chain, "s0"); code != http.StatusUnauthorized {
		t.Fatalf("S0 (t₁−1µs) при отсечке t₁−1µs обязана отвергаться — граница включающая (F4d-22), получено %d", code)
	}
	// Отрицательный контроль замка: пара, разнесённая на целую секунду,
	// проходит и на усечённом сравнении — молчание здесь ничего не доказывает,
	// доказывает первая половина.
	stub.sessions["s2"] = humanSession("usr-1", t1.Add(time.Second))
	if code := present(chain, "s2"); code != http.StatusOK {
		t.Fatalf("сессия на секунду позже отсечки: %d", code)
	}
}

// TestHumanSessionAdapter_F3_13_ClassifiesUnavailableAndUnimplemented — коды
// транспорта переводятся АДАПТЕРОМ: UNIMPLEMENTED — в типизированный признак
// окна раската, всё прочее — отказ без подмены; «сессии нет» — не ошибка.
func TestHumanSessionAdapter_F3_13_ClassifiesUnavailableAndUnimplemented(t *testing.T) {
	t1 := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	stub := &serviceStub{sessions: map[string]*iamv1.HumanSession{"s1": humanSession("usr-1", t1)}}
	ad := clients.NewSessionRevocationsAdapter(dialStub(t, stub))
	ctx := context.Background()

	sess, found, err := ad.ResolveHumanSession(ctx, "s1")
	if err != nil || !found || sess.UserID != "usr-1" || !sess.AuthenticatedAt.Equal(t1) || sess.AssuranceLevel != "1" || !sess.EmailVerified {
		t.Fatalf("живая сессия: %+v found=%v err=%v", sess, found, err)
	}
	if _, found, err := ad.ResolveHumanSession(ctx, "unknown"); err != nil || found {
		t.Fatalf("«сессии нет» обязано быть found=false без ошибки: found=%v err=%v", found, err)
	}

	stub.resolve = status.Error(codes.Unimplemented, "no such method")
	if _, _, err := ad.ResolveHumanSession(ctx, "s1"); !errors.Is(err, middleware.ErrHumanSessionUnsupported) {
		t.Fatalf("UNIMPLEMENTED обязан переводиться в ErrHumanSessionUnsupported, получено %v", err)
	}
	stub.resolve = status.Error(codes.Unavailable, "store down")
	if _, _, err := ad.ResolveHumanSession(ctx, "s1"); err == nil || errors.Is(err, middleware.ErrHumanSessionUnsupported) {
		t.Fatalf("UNAVAILABLE обязан оставаться ошибкой без подмены, получено %v", err)
	}
	// Неклассифицированный ответ (любой иной код) — тоже ошибка, а не проход.
	stub.resolve = status.Error(codes.Internal, "boom")
	if _, _, err := ad.ResolveHumanSession(ctx, "s1"); err == nil {
		t.Fatal("неклассифицированный ответ обязан быть ошибкой (F4d-29: корзины «прочее» нет)")
	}

	// Вторая половина: SessionCutoffOf отдаёт микросекунды не усечёнными.
	stub.resolve = nil
	cut := t1.Add(123456 * time.Microsecond)
	stub.cutoff = timestamppb.New(cut)
	got, found, err := ad.SessionCutoffOf(ctx, "usr-1")
	if err != nil || !found || !got.Equal(cut) {
		t.Fatalf("отсечка на проводе: %v found=%v err=%v, ожидалось %v", got, found, err, cut)
	}
}
