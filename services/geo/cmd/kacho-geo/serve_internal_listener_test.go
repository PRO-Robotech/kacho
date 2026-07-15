// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
)

// fakeServeServer — тестовый двойник gracefulServer: Serve сразу возвращает
// заданную ошибку (без реального listen'а).
type fakeServeServer struct{ err error }

func (f fakeServeServer) Serve(net.Listener) error { return f.err }

// TestRunInternalListener_fatalError_cancelsRootCtx — фатальная (не graceful
// ErrServerStopped) ошибка internalSrv.Serve обязана снести весь процесс через
// cancel() root-ctx, а не только залогироваться: иначе admin-плоскость :9091
// (InternalRegion/ZoneService) молча ложится, процесс остаётся «здоровым» на
// public :9090, оркестратор не рестартит (нет non-zero exit). Наблюдаемое
// поведение — root-ctx отменён (симметрия с public-листенером) И ошибка
// ВОЗВРАЩЕНА вызывающему, чтобы runServe отдал её в main.log.Fatal → non-zero exit.
func TestRunInternalListener_fatalError_cancelsRootCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fatal := errors.New("fatal accept error")
	got := runInternalListener(fakeServeServer{err: fatal}, nil, cancel, quietLogger())

	if !errors.Is(got, fatal) {
		t.Fatalf("runInternalListener must return the fatal Serve error (for non-zero exit), got %v", got)
	}
	select {
	case <-ctx.Done():
		// ожидаемо: fatal Serve-ошибка отменила root-ctx.
	default:
		t.Fatal("root ctx not cancelled after fatal internal Serve error — admin plane would be silently down")
	}
}

// TestRunInternalListener_gracefulStop_doesNotCancel — штатный graceful shutdown
// (Serve → grpc.ErrServerStopped после GracefulStop) НЕ должен трактоваться как
// фатальный: cancel() не вызывается (иначе двойной teardown/гонка на штатном
// пути) и ошибка НЕ возвращается (graceful-stop — не exit-условие).
func TestRunInternalListener_gracefulStop_doesNotCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if got := runInternalListener(fakeServeServer{err: grpc.ErrServerStopped}, nil, cancel, quietLogger()); got != nil {
		t.Fatalf("graceful ErrServerStopped must not be surfaced as fatal, got %v", got)
	}
	select {
	case <-ctx.Done():
		t.Fatal("root ctx cancelled on graceful ErrServerStopped — should be treated as normal shutdown")
	default:
		// ожидаемо: graceful-stop не отменяет root-ctx.
	}
}

// TestServeResult_internalFatalSurfacedWhenPublicNil — сердцевина фикса
// асимметрии exit-кода: когда фатальная ошибка internal-листенера вызывает
// cancel(), shutdown-горутина делает grpcSrv.GracefulStop() и публичный
// grpcSrv.Serve() по контракту grpc-go возвращает nil. Если exit-код считается
// ТОЛЬКО по публичной ошибке (nil), процесс завершается кодом 0 — оркестратор с
// restartPolicy=OnFailure/Never не рестартит, admin-плоскость тихо недоступна.
// serveResult обязан отдать наверх ошибку internal-листенера, когда публичная nil.
func TestServeResult_internalFatalSurfacedWhenPublicNil(t *testing.T) {
	internalFatal := errors.New("internal listener crashed")
	if got := serveResult(nil, internalFatal); !errors.Is(got, internalFatal) {
		t.Fatalf("serveResult(nil, internalFatal) must surface internal error for non-zero exit, got %v", got)
	}
}

// TestServeResult_publicErrorWins — публичная ошибка Serve приоритетнее: это
// первичный сигнал отказа public-листенера; internal-ошибка (если есть) —
// вторична.
func TestServeResult_publicErrorWins(t *testing.T) {
	pub := errors.New("public listener crashed")
	if got := serveResult(pub, errors.New("internal too")); !errors.Is(got, pub) {
		t.Fatalf("serveResult must prefer the public error, got %v", got)
	}
}

// TestServeResult_bothNil — штатный graceful shutdown обоих листенеров: nil →
// exit 0.
func TestServeResult_bothNil(t *testing.T) {
	if got := serveResult(nil, nil); got != nil {
		t.Fatalf("serveResult(nil, nil) must be nil (clean exit), got %v", got)
	}
}
