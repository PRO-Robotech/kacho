// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestRecoveryUnaryInterceptor_panicToInternal — паника в unary-handler'е
// перехватывается, RPC отвечает фиксированным INTERNAL, а значение паники НЕ
// echo-ится клиенту (CWE-209) и процесс не падает (CWE-248/755 DoS-backstop).
func TestRecoveryUnaryInterceptor_panicToInternal(t *testing.T) {
	intr := recoveryUnaryInterceptor(quietLogger())
	_, err := intr(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.geo.v1.RegionService/Get"},
		func(context.Context, any) (any, error) { panic("secret-internal-detail boom") },
	)
	if status.Code(err) != codes.Internal {
		t.Fatalf("panic must map to INTERNAL, got %v", err)
	}
	if strings.Contains(err.Error(), "boom") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("panic value leaked to client: %v", err)
	}
}

// TestRecoveryUnaryInterceptor_passthrough — без паники interceptor прозрачен
// (возвращает результат/ошибку handler'а как есть).
func TestRecoveryUnaryInterceptor_passthrough(t *testing.T) {
	intr := recoveryUnaryInterceptor(quietLogger())
	resp, err := intr(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/x/Y"},
		func(context.Context, any) (any, error) { return "ok", nil },
	)
	if err != nil || resp != "ok" {
		t.Fatalf("passthrough failed: resp=%v err=%v", resp, err)
	}
}

type fakeServerStream struct{ grpc.ServerStream }

func (fakeServerStream) Context() context.Context { return context.Background() }

// TestRecoveryStreamInterceptor_panicToInternal — паника в stream-handler'е тоже
// перехватывается и не echo-ит значение.
func TestRecoveryStreamInterceptor_panicToInternal(t *testing.T) {
	intr := recoveryStreamInterceptor(quietLogger())
	err := intr(nil, fakeServerStream{},
		&grpc.StreamServerInfo{FullMethod: "/x/Y"},
		func(any, grpc.ServerStream) error { panic("boom-stream") },
	)
	if status.Code(err) != codes.Internal {
		t.Fatalf("stream panic must map to INTERNAL, got %v", err)
	}
	if strings.Contains(err.Error(), "boom") {
		t.Fatalf("stream panic value leaked: %v", err)
	}
}
