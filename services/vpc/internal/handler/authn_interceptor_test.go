// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// mockServerStream — минимальный grpc.ServerStream с настраиваемым ctx для
// прогона stream-interceptor'а в тестах.
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context { return m.ctx }

// TestAuthNUnary_PrincipalProductionPasses — regression против fe3455 production-auth
// бага: api-gateway форвардит identity как x-kacho-principal-* (operations.WithPrincipal).
// Запрос с реальным forwarded-принципалом проходит AuthN-guard дальше, к per-object
// authz-интерсептору (реальный гейт). До фикса guard отвергал его, и
// аутентифицированный+авторизованный юзер получал 403.
func TestAuthNUnary_PrincipalProductionPasses(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	ctx = operations.WithPrincipal(ctx, operations.Principal{Type: "user", ID: "usr7j2yp1v24tx90tcv7"})
	interceptor := AuthNUnaryInterceptor(true)
	called := false
	h := func(context.Context, any) (any, error) { called = true; return nil, nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/List"}
	if _, err := interceptor(ctx, struct{}{}, info, h); err != nil {
		t.Fatalf("production-запрос с forwarded-принципалом обязан пройти AuthN-guard, got: %v", err)
	}
	if !called {
		t.Fatal("downstream handler не был вызван")
	}
}

// TestAuthNStream_PrincipalProductionPasses — то же для server-stream RPC.
func TestAuthNStream_PrincipalProductionPasses(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	ctx = operations.WithPrincipal(ctx, operations.Principal{Type: "service_account", ID: "sa9kx2"})
	interceptor := AuthNStreamInterceptor(true)
	called := false
	h := func(any, grpc.ServerStream) error { called = true; return nil }
	info := &grpc.StreamServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/List"}
	if err := interceptor(nil, &mockServerStream{ctx: ctx}, info, h); err != nil {
		t.Fatalf("production stream-запрос с forwarded-принципалом обязан пройти, got: %v", err)
	}
	if !called {
		t.Fatal("downstream stream-handler не был вызван")
	}
}

// callInterceptor — helper: прогон unary interceptor с заданными metadata.
func callInterceptor(t *testing.T, productionMode bool, fullMethod string, md metadata.MD) error {
	t.Helper()
	ctx := metadata.NewIncomingContext(context.Background(), md)
	interceptor := AuthNUnaryInterceptor(productionMode)
	noopHandler := func(context.Context, any) (any, error) { return nil, nil }
	info := &grpc.UnaryServerInfo{FullMethod: fullMethod}
	_, err := interceptor(ctx, struct{}{}, info, noopHandler)
	return err
}

// TestAuthNUnary_AnonymousDevPasses — dev-mode пропускает anonymous (backward-compat
// только для in-process фикстур; развёрнутый стенд работает в production-mode).
func TestAuthNUnary_AnonymousDevPasses(t *testing.T) {
	if err := callInterceptor(t, false, "/svc/M", metadata.MD{}); err != nil {
		t.Fatalf("dev-mode anonymous должен пройти, got: %v", err)
	}
}

// TestAuthNUnary_AnonymousProductionRejected — production-mode anonymous → PermissionDenied.
func TestAuthNUnary_AnonymousProductionRejected(t *testing.T) {
	err := callInterceptor(t, true, "/svc/M", metadata.MD{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ожидался PermissionDenied, got: %v", err)
	}
}

// TestAuthNUnary_ClientClaimedIdentityIsNotAuthN — заголовок, который звонящий
// написал о себе сам, аутентификацией не является.
//
// Раньше `x-kacho-project-id` поднимал identity, и вызывающий БЕЗ принципала проходил
// production-mode guard, просто прислав любой project-id; `x-kacho-admin` на
// internal-листенере поднимал ещё и «cluster-admin». Ни то, ни другое не форвардит ни
// один компонент платформы (cross-service вызовы несут только x-kacho-principal-*),
// то есть это были поля, заполняемые исключительно тем, кого они же и авторизовали.
func TestAuthNUnary_ClientClaimedIdentityIsNotAuthN(t *testing.T) {
	for name, md := range map[string]metadata.MD{
		"self-claimed project": {"x-kacho-project-id": []string{"prj_f1"}},
		"self-claimed admin":   {"x-kacho-admin": []string{"true"}},
		"both":                 {"x-kacho-project-id": []string{"prj_f1"}, "x-kacho-admin": []string{"true"}},
		"arbitrary header":     {"x-some-header": []string{"evil@attacker"}},
	} {
		t.Run(name, func(t *testing.T) {
			err := callInterceptor(t, true, "/kacho.cloud.vpc.v1.InternalAddressPoolService/Create", md)
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("сочинённая вызывающим metadata не является AuthN, ожидался PermissionDenied, got: %v", err)
			}
		})
	}
}

// TestAuthNUnary_AnonymousPrincipalIsNotAnIdentity — подставленный шлюзом anonymous и
// SystemPrincipal()-fallback означают «личность не предъявлена», а не «предъявлена
// привилегированная»: production-mode обязан их отвергнуть.
func TestAuthNUnary_AnonymousPrincipalIsNotAnIdentity(t *testing.T) {
	for name, p := range map[string]operations.Principal{
		"gateway anonymous": {Type: "system", ID: "anonymous"},
		"system bootstrap":  {Type: "system", ID: "bootstrap"},
		"empty":             {},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
			ctx = operations.WithPrincipal(ctx, p)
			h := func(context.Context, any) (any, error) { return nil, nil }
			info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.InternalAddressPoolService/Create"}
			_, err := AuthNUnaryInterceptor(true)(ctx, struct{}{}, info, h)
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("анонимность не является личностью, ожидался PermissionDenied, got: %v", err)
			}
		})
	}
}
