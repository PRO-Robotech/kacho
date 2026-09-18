// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

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
	"google.golang.org/protobuf/types/known/emptypb"
)

// deadlineObs — то, что backend увидел в контексте входящего вызова: был ли на
// нём дедлайн и сколько до него оставалось. Передаётся из server-handler'а по
// каналу, чтобы у чтения теста был happens-before с записью в горутине сервера.
type deadlineObs struct {
	has       bool
	remaining time.Duration
}

// backendObservingDeadline поднимает bufconn-сервер, который на ЛЮБОЙ метод
// (UnknownServiceHandler — так же, как grpc-gateway дозванивается через мост)
// снимает Deadline входящего контекста и шлёт наблюдение в канал. Возвращает
// bufconn-диалер для клиента и канал наблюдений.
func backendObservingDeadline(t *testing.T) (grpc.DialOption, <-chan deadlineObs) {
	t.Helper()
	obs := make(chan deadlineObs, 1)
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(
		func(_ any, stream grpc.ServerStream) error {
			dl, ok := stream.Context().Deadline()
			o := deadlineObs{has: ok}
			if ok {
				o.remaining = time.Until(dl)
			}
			select {
			case obs <- o:
			default:
			}
			// Тело ответа неважно — тест смотрит только на дедлайн, который
			// сервер увидел ДО первого сообщения (он приходит grpc-timeout
			// заголовком при создании потока).
			return nil
		}))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	dialer := grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	})
	return dialer, obs
}

// TestRestBridgeBackendCallCarriesDeadline — предикат снятия #2713 (клауза
// REST-моста): backend-вызов, ушедший через dial-опции REST-моста, обязан нести
// собственный дедлайн ДАЖЕ когда входящий контекст его не несёт. Иначе при
// перегруженном соседе вызов висит, пока клиент сам не разорвёт соединение:
// сервер края ограничивает чтение запроса (ReadTimeout), но не ожидание соседа.
//
// Вызов делается с context.Background() (без дедлайна) — единственным
// источником дедлайна на проводе остаётся сам мост. RED (до фикса): в
// restBridgeDialOpts нет перехватчика с пределом → сервер видит контекст без
// дедлайна → has=false.
func TestRestBridgeBackendCallCarriesDeadline(t *testing.T) {
	dialer, obs := backendObservingDeadline(t)

	opts := append(restBridgeDialOpts("iam", nil), dialer)
	conn, err := grpc.NewClient("passthrough:///bufnet", opts...)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Background — БЕЗ дедлайна: если он появится у сервера, его поставил мост.
	_ = conn.Invoke(context.Background(), "/kacho.cloud.restmuxprobe.v1.Backend/Call",
		&emptypb.Empty{}, &emptypb.Empty{})

	select {
	case o := <-obs:
		if !o.has {
			t.Fatalf("backend увидел контекст БЕЗ дедлайна: dial-опции REST-моста " +
				"обязаны ставить предел даже на входящем без дедлайна (#2713)")
		}
		if o.remaining <= 0 || o.remaining > restBridgeCallTimeout {
			t.Fatalf("дедлайн вне ожидаемого окна: remaining=%s, предел=%s",
				o.remaining, restBridgeCallTimeout)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("backend не получил вызов")
	}
}

// backendSleeping поднимает bufconn-сервер, чей handler спит sleep перед ответом
// (unary echo пустого сообщения). Возвращает bufconn-диалер клиента.
func backendSleeping(t *testing.T, sleep time.Duration) grpc.DialOption {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(
		func(_ any, stream grpc.ServerStream) error {
			if sleep > 0 {
				select {
				case <-time.After(sleep):
				case <-stream.Context().Done():
					return stream.Context().Err()
				}
			}
			var in emptypb.Empty
			if err := stream.RecvMsg(&in); err != nil {
				return err
			}
			return stream.SendMsg(&emptypb.Empty{})
		}))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	})
}

func dialWithUnaryDeadline(t *testing.T, dialer grpc.DialOption, d time.Duration) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(backendCallDeadlineUnaryInterceptor(d)),
		dialer)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestRestBridgeDeadline_SlowNeighborFailsFast — негатив предиката #2713: сосед,
// отвечающий ПОЗЖЕ предела, даёт отказ по пределу (DeadlineExceeded) и НЕ
// зависает — вызов возвращается около предела, а не ждёт ответа соседа.
func TestRestBridgeDeadline_SlowNeighborFailsFast(t *testing.T) {
	const limit = 100 * time.Millisecond
	dialer := backendSleeping(t, 2*time.Second) // сосед медленнее предела
	conn := dialWithUnaryDeadline(t, dialer, limit)

	start := time.Now()
	err := conn.Invoke(context.Background(), "/kacho.cloud.restmuxprobe.v1.Backend/Call",
		&emptypb.Empty{}, &emptypb.Empty{})
	elapsed := time.Since(start)

	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("ожидался DeadlineExceeded, получено: %v", err)
	}
	// Возврат около предела, а не по ответу соседа (2s): без зависания.
	if elapsed > time.Second {
		t.Fatalf("вызов не отдал управление по пределу: elapsed=%s (предел=%s)", elapsed, limit)
	}
}

// TestRestBridgeDeadline_NeighborWithinLimitPasses — позитив (законный близнец):
// сосед, отвечающий В ПРЕДЕЛЕ, проходит без отказа.
func TestRestBridgeDeadline_NeighborWithinLimitPasses(t *testing.T) {
	dialer := backendSleeping(t, 0) // отвечает сразу
	conn := dialWithUnaryDeadline(t, dialer, restBridgeCallTimeout)

	if err := conn.Invoke(context.Background(), "/kacho.cloud.restmuxprobe.v1.Backend/Call",
		&emptypb.Empty{}, &emptypb.Empty{}); err != nil {
		t.Fatalf("сосед в пределе обязан проходить, получено: %v", err)
	}
}
