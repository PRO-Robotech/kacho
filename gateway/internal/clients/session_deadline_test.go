// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_deadline_test.go — предел края на каждом вызове к службе о сессии
// (#2713). Проба чёрного ящика: над НАСТОЯЩИМ адаптером поверх настоящего gRPC
// (bufconn). Предмет — что край ставит СВОЙ предел на вызове к внутреннему
// слушателю; сервер видит этот предел как дедлайн в контексте (grpc-timeout).
// Без предела на крае контекст на проводе бессрочен, и вызов к перегруженному
// соседу висит, пока клиент сам не разорвёт соединение.
package clients_test

import (
	"context"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	operationpb "github.com/PRO-Robotech/corelib/api/corelib/operation"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
)

// deadlineWitness — дублёр внутреннего слушателя: для каждого вызванного глагола
// запоминает, НЁС ЛИ контекст входящего вызова собственный предел. Отвечает без
// ошибки — фикстура цела намеренно, чтобы красное приходило РОВНО от отсутствия
// предела, а не от сломанной подпорки.
type deadlineWitness struct {
	iamv1.UnimplementedInternalHumanSessionServiceServer
	iamv1.UnimplementedInternalSessionRevocationsServiceServer
	iamv1.UnimplementedInternalIAMServiceServer

	mu   sync.Mutex
	seen map[string]bool // глагол края → нёс ли контекст дедлайн
}

func (w *deadlineWitness) note(method string, ctx context.Context) {
	_, ok := ctx.Deadline()
	w.mu.Lock()
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	w.seen[method] = ok
	w.mu.Unlock()
}

func (w *deadlineWitness) carried(method string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seen[method]
}

func (w *deadlineWitness) Resolve(ctx context.Context, _ *iamv1.ResolveHumanSessionRequest) (*iamv1.ResolveHumanSessionResponse, error) {
	w.note("ResolveHumanSession", ctx)
	return &iamv1.ResolveHumanSessionResponse{Found: true, Session: &iamv1.HumanSession{UserId: "usr-1"}}, nil
}

func (w *deadlineWitness) SessionCutoffOf(ctx context.Context, _ *iamv1.SessionCutoffOfRequest) (*iamv1.SessionCutoffOfResponse, error) {
	w.note("SessionCutoffOf", ctx)
	return &iamv1.SessionCutoffOfResponse{Found: false}, nil
}

func (w *deadlineWitness) IsRevoked(ctx context.Context, _ *iamv1.IsRevokedRequest) (*iamv1.IsRevokedResponse, error) {
	w.note("IsSessionRevoked", ctx)
	return &iamv1.IsRevokedResponse{Revoked: false}, nil
}

func (w *deadlineWitness) CheckBasicCredentialLive(ctx context.Context, _ *iamv1.CheckBasicCredentialLiveRequest) (*iamv1.CheckBasicCredentialLiveResponse, error) {
	w.note("IsBasicCredentialLive", ctx)
	return &iamv1.CheckBasicCredentialLiveResponse{}, nil
}

func (w *deadlineWitness) Revoke(ctx context.Context, _ *iamv1.RevokeRequest) (*operationpb.Operation, error) {
	w.note("Revoke", ctx)
	return &operationpb.Operation{}, nil
}

// dialWitness поднимает дублёра на bufconn и открывает к нему соединение. Все
// три внутренних службы, к которым адаптер ходит одним каналом, — на одном
// сервере.
func dialWitness(t *testing.T, w *deadlineWitness) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	iamv1.RegisterInternalHumanSessionServiceServer(srv, w)
	iamv1.RegisterInternalSessionRevocationsServiceServer(srv, w)
	iamv1.RegisterInternalIAMServiceServer(srv, w)
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

// TestSessionRevocationsAdapter_EveryCallCarriesItsOwnDeadline — RED для #2713.
//
// Проба подаёт БЕССРОЧНЫЙ контекст (context.Background — без дедлайна). Если
// край ставит свой предел на вызове к службе о сессии, сервер увидит дедлайн в
// контексте; если нет — контекст на проводе бессрочен. Каждый sibling-глагол
// адаптера обязан нести один и тот же предел (architecture.md: «все
// sibling-методы клиента обязаны применять один и тот же configured-timeout»),
// поэтому проверяются все пять, включая не осмотренные в теле #2713
// IsSessionRevoked и Revoke.
func TestSessionRevocationsAdapter_EveryCallCarriesItsOwnDeadline(t *testing.T) {
	w := &deadlineWitness{}
	ad := clients.NewSessionRevocationsAdapter(dialWitness(t, w))
	ctx := context.Background() // БЕССРОЧНЫЙ — предел обязан поставить край

	if _, _, err := ad.ResolveHumanSession(ctx, "s1"); err != nil {
		t.Fatalf("ResolveHumanSession: фикстура обязана отвечать без ошибки, получено %v", err)
	}
	if _, _, err := ad.SessionCutoffOf(ctx, "usr-1"); err != nil {
		t.Fatalf("SessionCutoffOf: фикстура обязана отвечать без ошибки, получено %v", err)
	}
	if _, err := ad.IsSessionRevoked(ctx, "jti-1"); err != nil {
		t.Fatalf("IsSessionRevoked: фикстура обязана отвечать без ошибки, получено %v", err)
	}
	if _, err := ad.IsBasicCredentialLive(ctx, "cred-1"); err != nil {
		t.Fatalf("IsBasicCredentialLive: фикстура обязана отвечать без ошибки, получено %v", err)
	}
	if err := ad.Revoke(ctx, &iamv1.RevokeRequest{}); err != nil {
		t.Fatalf("Revoke: фикстура обязана отвечать без ошибки, получено %v", err)
	}

	// Несущее утверждение: каждый вызванный глагол принёс контекст с дедлайном.
	// Красное приходит РОВНО отсюда — вызовы выше исполнились без ошибки.
	for _, m := range []string{
		"ResolveHumanSession", "SessionCutoffOf", "IsSessionRevoked",
		"IsBasicCredentialLive", "Revoke",
	} {
		if !w.carried(m) {
			t.Errorf("вызов %s ушёл БЕЗ собственного предела: сервер видит контекст без дедлайна (#2713) — перегруженный сосед подвесил бы вызов, пока клиент сам не разорвёт соединение", m)
		}
	}
}
