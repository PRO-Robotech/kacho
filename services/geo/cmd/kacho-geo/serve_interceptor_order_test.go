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

// Guard-тесты load-bearing инварианта порядка интерсепторов (finding sec-hardening-r9b:
// раньше порядок собирался императивным append/prepend по четырём срезам в runServe,
// без единой точки и без теста — правка могла молча сдвинуть recovery с index 0
// (паника escaping → DoS) или поставить authz перед principal-extract (Check без
// субъекта)). Теперь порядок — в assembleUnaryChain/assembleStreamChain, а этот тест
// его фиксирует: recovery outermost (index 0), authz ПОСЛЕ principal.

// traceUnary — sentinel-интерсептор, отмечающий свой вызов в trace и прозрачно
// делегирующий дальше.
func traceUnary(name string, trace *[]string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		*trace = append(*trace, name)
		return handler(ctx, req)
	}
}

func traceStream(name string, trace *[]string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		*trace = append(*trace, name)
		return handler(srv, ss)
	}
}

// runUnaryChain исполняет []grpc.UnaryServerInterceptor как это делает
// grpc.ChainUnaryInterceptor: chain[0] — outermost (вызывается первым, оборачивает
// остальные и final-handler).
func runUnaryChain(chain []grpc.UnaryServerInterceptor, final grpc.UnaryHandler) (any, error) {
	h := final
	for i := len(chain) - 1; i >= 0; i-- {
		intr := chain[i]
		next := h
		h = func(ctx context.Context, req any) (any, error) {
			return intr(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/test/M"}, next)
		}
	}
	return h(context.Background(), nil)
}

func runStreamChain(chain []grpc.StreamServerInterceptor, ss grpc.ServerStream, final grpc.StreamHandler) error {
	h := final
	for i := len(chain) - 1; i >= 0; i-- {
		intr := chain[i]
		next := h
		h = func(srv any, stream grpc.ServerStream) error {
			return intr(srv, stream, &grpc.StreamServerInfo{FullMethod: "/test/M"}, next)
		}
	}
	return h(nil, ss)
}

// TestAssembleUnaryChain_order — recovery первым, principal-цепочка сохраняет
// порядок, authz — последним (после principal). len = recovery + principal + authz.
func TestAssembleUnaryChain_order(t *testing.T) {
	var trace []string
	principal := []grpc.UnaryServerInterceptor{
		traceUnary("principal-1", &trace),
		traceUnary("principal-2", &trace),
	}
	authz := traceUnary("authz", &trace)
	chain := assembleUnaryChain(recoveryUnaryInterceptor(quietLogger()), principal, authz)
	if len(chain) != 4 {
		t.Fatalf("chain len = %d, want 4 (recovery+2 principal+authz)", len(chain))
	}
	resp, err := runUnaryChain(chain, func(context.Context, any) (any, error) {
		trace = append(trace, "handler")
		return "ok", nil
	})
	if err != nil || resp != "ok" {
		t.Fatalf("chain resp=%v err=%v, want ok/nil", resp, err)
	}
	// recovery не отмечается в trace, но обязан быть outermost; authz строго после
	// principal-цепочки, handler — глубже всех.
	want := []string{"principal-1", "principal-2", "authz", "handler"}
	if strings.Join(trace, ",") != strings.Join(want, ",") {
		t.Fatalf("execution order = %v, want %v (authz must run after principal-extract)", trace, want)
	}
}

// TestAssembleUnaryChain_recoveryOutermost — паника в handler'е ловится recovery
// (доказывает, что recovery на index 0, outermost): principal НЕ восстанавливает
// панику, поэтому если бы recovery не был снаружи — паника вышла бы из цепочки и
// уронила тест. Итог: фиксированный INTERNAL без утечки значения паники.
func TestAssembleUnaryChain_recoveryOutermost(t *testing.T) {
	var trace []string
	principal := []grpc.UnaryServerInterceptor{traceUnary("principal", &trace)}
	// authz==nil → слот authz отсутствует.
	chain := assembleUnaryChain(recoveryUnaryInterceptor(quietLogger()), principal, nil)
	if len(chain) != 2 {
		t.Fatalf("chain len = %d, want 2 (recovery+principal, no authz)", len(chain))
	}
	_, err := runUnaryChain(chain, func(context.Context, any) (any, error) {
		trace = append(trace, "handler")
		panic("secret-panic boom")
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("panic must be recovered to INTERNAL (recovery outermost), got %v", err)
	}
	if strings.Contains(err.Error(), "boom") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("panic value leaked to client: %v", err)
	}
	if strings.Join(trace, ",") != "principal,handler" {
		t.Fatalf("trace = %v, want [principal handler] (principal ran, then handler panicked under recovery)", trace)
	}
}

// TestAssembleStreamChain_order_and_recoveryOutermost — stream-аналог: порядок
// recovery→principal→authz и recovery outermost (панику stream-handler'а ловит).
func TestAssembleStreamChain_order_and_recoveryOutermost(t *testing.T) {
	var trace []string
	principal := []grpc.StreamServerInterceptor{traceStream("principal", &trace)}
	authz := traceStream("authz", &trace)
	chain := assembleStreamChain(recoveryStreamInterceptor(quietLogger()), principal, authz)
	if len(chain) != 3 {
		t.Fatalf("chain len = %d, want 3 (recovery+principal+authz)", len(chain))
	}
	err := runStreamChain(chain, fakeServerStream{}, func(any, grpc.ServerStream) error {
		trace = append(trace, "handler")
		return nil
	})
	if err != nil {
		t.Fatalf("stream chain err = %v, want nil", err)
	}
	if strings.Join(trace, ",") != "principal,authz,handler" {
		t.Fatalf("stream order = %v, want [principal authz handler]", trace)
	}

	trace = nil
	chain = assembleStreamChain(recoveryStreamInterceptor(quietLogger()), principal, nil)
	perr := runStreamChain(chain, fakeServerStream{}, func(any, grpc.ServerStream) error {
		panic("stream-boom")
	})
	if status.Code(perr) != codes.Internal {
		t.Fatalf("stream panic must be recovered to INTERNAL (recovery outermost), got %v", perr)
	}
	if strings.Contains(perr.Error(), "boom") {
		t.Fatalf("stream panic value leaked: %v", perr)
	}
}
