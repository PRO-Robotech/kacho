// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Panic-recovery interceptors — defense-in-depth backstop против DoS (CWE-248 /
// CWE-755 / CWE-400): grpc-go по умолчанию НЕ восстанавливает панику handler'а,
// поэтому неперехваченный nil-deref / out-of-range в любом handler-goroutine
// убил бы весь процесс, уронив ОБА листенера (public :9090 + internal :9091) и
// весь глобальный Region/Zone-каталог до рестарта. Recovery-interceptor ловит
// панику, логирует со stack-trace (server-side) и возвращает клиенту
// фиксированный codes.Internal БЕЗ значения паники (CWE-209: не течёт внутренняя
// деталь наружу). Ставится ПЕРВЫМ (outermost) в цепочке — оборачивает и все
// нижележащие interceptor'ы (principal-extract, authz), и сам handler.

import (
	"context"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// recoveryUnaryInterceptor восстанавливает панику unary-handler'а → INTERNAL.
func recoveryUnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("recovered panic in unary handler",
					"method", info.FullMethod, "panic", r, "stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// recoveryStreamInterceptor восстанавливает панику stream-handler'а → INTERNAL.
func recoveryStreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("recovered panic in stream handler",
					"method", info.FullMethod, "panic", r, "stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(srv, ss)
	}
}
