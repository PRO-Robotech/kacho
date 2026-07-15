// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Interceptor-цепочки обоих листенеров kacho-storage. Порядок (outermost→innermost):
// recovery → logging → principal-extract (cert-identity → trusted-principal) → authz.
// Оба листенера (public :9090 и internal :9091) строятся ОДИНАКОВО (AuthN+AuthZ
// везде — internal НЕ освобождён, security.md).
//
// authz — реальный per-RPC InternalIAMService.Check (corelib check.NewInterceptor):
// composition root (serve.go) строит его из cfg.AuthZIAMGRPCAddr и передаёт сюда
// последним звеном цепочки (fail-closed, читает caller-principal из уже извлечённого
// контекста). Пустой адрес → authz-звено не подключается (грациозный dev-старт без
// kacho-iam), AuthN (mTLS+principal) сохраняется; production ОБЯЗАН задать адрес.

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// recoveryUnaryInterceptor восстанавливает панику unary-handler'а → фиксированный
// INTERNAL (без leak'а значения паники; ставится ПЕРВЫМ, оборачивает всё ниже — DoS-
// backstop, grpc-go по умолчанию НЕ восстанавливает панику handler-goroutine).
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

// recoveryStreamInterceptor — stream-аналог recoveryUnaryInterceptor.
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

// loggingUnaryInterceptor логирует метод, gRPC-код и длительность через slog
// (структурно, без PII — коррелируем по не-PII полям).
func loggingUnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logger.Info("grpc unary",
			"method", info.FullMethod,
			"code", status.Code(err).String(),
			"duration_ms", time.Since(start).Milliseconds())
		return resp, err
	}
}

// unaryChain собирает упорядоченную unary-цепочку ОДНОГО листенера:
// recovery (outermost) → logging → principal-extract (cert-identity → trusted-
// principal) → authz. forwarders — allow-list SAN'ов доверенных форвардеров
// end-user principal'а (api-gateway SA). authz — реальный InternalIAMService.Check-
// интерсептор (corelib authz), собранный composition root'ом из конфига; nil, если
// authz не сконфигурирован (dev-старт без kacho-iam) → per-RPC Check пропускается,
// AuthN (mTLS+principal) сохраняется. Одинаков на ОБОИХ листенерах (security.md).
func unaryChain(logger *slog.Logger, forwarders []string, authz grpc.UnaryServerInterceptor) []grpc.UnaryServerInterceptor {
	chain := []grpc.UnaryServerInterceptor{
		recoveryUnaryInterceptor(logger),
		loggingUnaryInterceptor(logger),
		grpcsrv.UnaryCertIdentityExtract(),
		grpcsrv.UnaryTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(forwarders...)),
	}
	if authz != nil {
		chain = append(chain, authz)
	}
	return chain
}

// streamChain — stream-аналог unaryChain (тот же инвариант порядка).
func streamChain(logger *slog.Logger, forwarders []string, authz grpc.StreamServerInterceptor) []grpc.StreamServerInterceptor {
	chain := []grpc.StreamServerInterceptor{
		recoveryStreamInterceptor(logger),
		grpcsrv.StreamCertIdentityExtract(),
		grpcsrv.StreamTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(forwarders...)),
	}
	if authz != nil {
		chain = append(chain, authz)
	}
	return chain
}
