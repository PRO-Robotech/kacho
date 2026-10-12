// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_deadline_behaviour_test.go — поведение предела края на вызове к службе
// о сессии (#2713), белый ящик: подаётся малый callTimeout, чтобы проверить
// поведение детерминированно и быстро. Предикат снятия #2713 дословно: медленный
// сосед, отвечающий позже предела, даёт ОТКАЗ ПО ПРЕДЕЛУ и не зависает; законный
// близнец — сосед, отвечающий в пределе, — проходит.
package clients

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
)

// blockingSessionServer — дублёр службы о сессии. block=true: держит вызов до
// отмены контекста края (отвечает ПОЗЖЕ предела). block=false: отвечает сразу
// (в пределе) — законный близнец.
type blockingSessionServer struct {
	iamv1.UnimplementedInternalHumanSessionServiceServer
	iamv1.UnimplementedInternalSessionRevocationsServiceServer
	block bool
}

func (s *blockingSessionServer) Resolve(ctx context.Context, _ *iamv1.ResolveHumanSessionRequest) (*iamv1.ResolveHumanSessionResponse, error) {
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &iamv1.ResolveHumanSessionResponse{Found: true, Session: &iamv1.HumanSession{UserId: "usr-1"}}, nil
}

func (s *blockingSessionServer) SessionCutoffOf(ctx context.Context, _ *iamv1.SessionCutoffOfRequest) (*iamv1.SessionCutoffOfResponse, error) {
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &iamv1.SessionCutoffOfResponse{Found: false}, nil
}

func dialBlocking(t *testing.T, s *blockingSessionServer) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	iamv1.RegisterInternalHumanSessionServiceServer(srv, s)
	iamv1.RegisterInternalSessionRevocationsServiceServer(srv, s)
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

// TestSessionRevocationsAdapter_SlowPeerIsBoundedByDeadline — негатив предиката
// #2713: медленный сосед (отвечает позже предела) даёт отказ ПО ПРЕДЕЛУ и не
// подвешивает вызов. Проверяются обе названные полосы: ResolveHumanSession и
// SessionCutoffOf. Каждый вызов идёт в горутине под сторожевым таймером: если
// край НЕ поставил предела, вызов завис бы — сторож роняет пробу «завис», а не
// зависает сам.
func TestSessionRevocationsAdapter_SlowPeerIsBoundedByDeadline(t *testing.T) {
	ad := NewSessionRevocationsAdapter(dialBlocking(t, &blockingSessionServer{block: true}))
	ad.callTimeout = 100 * time.Millisecond

	assertDeadline := func(name string, call func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- call() }()
		select {
		case err := <-done:
			if status.Code(err) != codes.DeadlineExceeded {
				t.Fatalf("%s: медленный сосед обязан давать отказ по пределу (DeadlineExceeded), получено %v", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: вызов ЗАВИС — край не поставил свой предел на вызове к службе о сессии (#2713)", name)
		}
	}

	assertDeadline("ResolveHumanSession", func() error {
		_, _, err := ad.ResolveHumanSession(context.Background(), "s1")
		return err
	})
	assertDeadline("SessionCutoffOf", func() error {
		_, _, err := ad.SessionCutoffOf(context.Background(), "usr-1")
		return err
	})
}

// TestSessionRevocationsAdapter_PeerWithinDeadlinePasses — позитив/законный
// близнец предиката #2713: сосед, отвечающий В ПРЕДЕЛЕ, проходит без отказа.
// Молчание здесь ничего не доказывало бы в одиночку — доказывает пара с
// негативом выше.
func TestSessionRevocationsAdapter_PeerWithinDeadlinePasses(t *testing.T) {
	ad := NewSessionRevocationsAdapter(dialBlocking(t, &blockingSessionServer{block: false}))
	ad.callTimeout = 2 * time.Second

	sess, found, err := ad.ResolveHumanSession(context.Background(), "s1")
	if err != nil || !found || sess.UserID != "usr-1" {
		t.Fatalf("сосед в пределе обязан проходить: sess=%+v found=%v err=%v", sess, found, err)
	}
	if _, _, err := ad.SessionCutoffOf(context.Background(), "usr-1"); err != nil {
		t.Fatalf("SessionCutoffOf у соседа в пределе: %v", err)
	}
}
