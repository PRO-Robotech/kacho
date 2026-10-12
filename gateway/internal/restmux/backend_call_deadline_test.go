// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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

// TestRestBridgeStreamBackendCallCarriesDeadline — обвязочный близнец
// предыдущей пробы для СТРИМ-грани: дозвон идёт ЧЕРЕЗ прод-обвязку
// restBridgeDialOpts (а не через ручной dialWithStreamDeadline и не прямым
// вызовом перехватчика), поэтому проба ловит и МОНТАЖ WithChainStreamInterceptor
// на реальном соединении моста, а не только поведение самого перехватчика.
// Unary-грань это же ловит соседней пробой (снятие WithChainUnaryInterceptor);
// stream-грань без этой пробы оставалась замаскированной отсутствием.
//
// Вызов делается стримом с context.Background() (без дедлайна) — если дедлайн
// появится у сервера, его поставил смонтированный стрим-перехватчик моста.
func TestRestBridgeStreamBackendCallCarriesDeadline(t *testing.T) {
	dialer, obs := backendObservingDeadline(t)

	opts := append(restBridgeDialOpts("iam", nil), dialer)
	conn, err := grpc.NewClient("passthrough:///bufnet", opts...)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	cs, err := conn.NewStream(context.Background(), streamDesc(), streamProbeMethod)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	_ = cs.SendMsg(&emptypb.Empty{})
	_ = cs.CloseSend()
	var out emptypb.Empty
	_ = cs.RecvMsg(&out)

	select {
	case o := <-obs:
		if !o.has {
			t.Fatalf("стрим-backend увидел контекст БЕЗ дедлайна: dial-опции " +
				"REST-моста обязаны монтировать стрим-предел на соединение (#2713)")
		}
		if o.remaining <= 0 || o.remaining > restBridgeCallTimeout {
			t.Fatalf("дедлайн вне ожидаемого окна: remaining=%s, предел=%s",
				o.remaining, restBridgeCallTimeout)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("стрим-backend не получил вызов")
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

// ── стрим-пара: backendCallDeadlineStreamInterceptor ────────────────────────
//
// Прод-код перехватчика одобрен и НЕ меняется. Прод-фикс уже на месте, поэтому
// честный красный этих проб показан ИНЪЕКЦИЕЙ (замыканием перехватчика в no-op),
// как у гейта класса, а не отсутствием фикса.

const streamProbeMethod = "/kacho.cloud.restmuxprobe.v1.Backend/Sub"

func streamDesc() *grpc.StreamDesc {
	return &grpc.StreamDesc{StreamName: "Sub", ServerStreams: true, ClientStreams: true}
}

func dialWithStreamDeadline(t *testing.T, dialer grpc.DialOption, d time.Duration) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainStreamInterceptor(backendCallDeadlineStreamInterceptor(d)),
		dialer)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestRestBridgeStreamDeadline_SlowNeighborFailsFast — негатив предиката #2713
// для СТРИМ-грани: сосед, отвечающий позже предела, даёт отказ по пределу
// (DeadlineExceeded) и не зависает — под сторожевым таймером. Инъекция no-op
// перехватчика краснит это: стрим уходит без предела, RecvMsg висит до соседа
// (5 s), сторожевой таймер (2 s) срабатывает раньше.
func TestRestBridgeStreamDeadline_SlowNeighborFailsFast(t *testing.T) {
	const limit = 100 * time.Millisecond
	dialer := backendSleeping(t, 5*time.Second) // сосед намного медленнее предела
	conn := dialWithStreamDeadline(t, dialer, limit)

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		cs, err := conn.NewStream(context.Background(), streamDesc(), streamProbeMethod)
		if err != nil {
			done <- err
			return
		}
		_ = cs.SendMsg(&emptypb.Empty{})
		_ = cs.CloseSend()
		var out emptypb.Empty
		done <- cs.RecvMsg(&out)
	}()

	select {
	case err := <-done:
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("ожидался DeadlineExceeded, получено: %v", err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("стрим не отдал управление по пределу: elapsed=%s (предел=%s)", elapsed, limit)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("стрим ушёл БЕЗ предела и завис (сторожевой таймер): перехватчик не ставит дедлайн")
	}
}

// TestRestBridgeStreamDeadline_NeighborWithinLimitPasses — позитив (законный
// близнец): стрим-сосед, отвечающий в пределе, проходит без отказа.
func TestRestBridgeStreamDeadline_NeighborWithinLimitPasses(t *testing.T) {
	dialer := backendSleeping(t, 0) // отвечает сразу
	conn := dialWithStreamDeadline(t, dialer, restBridgeCallTimeout)

	cs, err := conn.NewStream(context.Background(), streamDesc(), streamProbeMethod)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := cs.SendMsg(&emptypb.Empty{}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := cs.CloseSend(); err != nil {
		t.Fatalf("close send: %v", err)
	}
	var out emptypb.Empty
	if err := cs.RecvMsg(&out); err != nil {
		t.Fatalf("стрим в пределе обязан проходить, получено: %v", err)
	}
}

// fakeClientStream — управляемый ClientStream: его Context() отдаёт заданный
// контекст, чтобы детерминированно проверить ветки перехватчика (горутина
// на успехе, cancel на ошибке) без гонок и без опоры на runtime.NumGoroutine.
type fakeClientStream struct{ ctx context.Context }

func (f *fakeClientStream) Header() (metadata.MD, error) { return nil, nil }
func (f *fakeClientStream) Trailer() metadata.MD         { return nil }
func (f *fakeClientStream) CloseSend() error             { return nil }
func (f *fakeClientStream) Context() context.Context     { return f.ctx }
func (f *fakeClientStream) SendMsg(any) error            { return nil }
func (f *fakeClientStream) RecvMsg(any) error            { return nil }

// TestRestBridgeStreamDeadline_GoroutineExitsOnStreamEnd — ветка успеха: на
// живом потоке перехватчик плодит горутину `<-stream.Context().Done()→cancel`.
// Проба доказывает, что горутина завершается по концу потока (не течёт): по
// завершению потока childCtx (созданный перехватчиком WithTimeout) обязан быть
// отменён горутиной. Инъекция no-op краснит: без горутины childCtx == входной
// контекст, конца потока он не слышит → сторож срабатывает.
func TestRestBridgeStreamDeadline_GoroutineExitsOnStreamEnd(t *testing.T) {
	streamCtx, endStream := context.WithCancel(context.Background())
	defer endStream()

	var childCtx context.Context
	streamer := func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		childCtx = ctx
		return &fakeClientStream{ctx: streamCtx}, nil
	}

	interceptor := backendCallDeadlineStreamInterceptor(restBridgeCallTimeout)
	cs, err := interceptor(context.Background(), &grpc.StreamDesc{}, nil, streamProbeMethod, streamer)
	if err != nil || cs == nil {
		t.Fatalf("открытие потока: cs=%v err=%v", cs, err)
	}
	// Поток жив — предел ещё не должен был сработать.
	select {
	case <-childCtx.Done():
		t.Fatal("предел сработал до конца потока")
	default:
	}
	// Поток завершился — горутина обязана проснуться и отменить childCtx.
	endStream()
	select {
	case <-childCtx.Done():
		// горутина отработала cancel() и вышла — течи нет
	case <-time.After(2 * time.Second):
		t.Fatal("горутина перехватчика не завершилась по концу потока: течь горутины/контекста")
	}
}

// TestRestBridgeStreamDeadline_CancelsOnStreamerError — ветка ошибки создания
// потока: перехватчик обязан пробросить ошибку И немедленно отменить свой
// контекст (иначе течёт). Инъекция no-op краснит: без отмены childCtx == входной
// контекст и не отменяется → сторож срабатывает.
func TestRestBridgeStreamDeadline_CancelsOnStreamerError(t *testing.T) {
	boom := errors.New("dial boom")
	var childCtx context.Context
	streamer := func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		childCtx = ctx
		return nil, boom
	}

	interceptor := backendCallDeadlineStreamInterceptor(restBridgeCallTimeout)
	cs, err := interceptor(context.Background(), &grpc.StreamDesc{}, nil, streamProbeMethod, streamer)
	if !errors.Is(err, boom) {
		t.Fatalf("ошибка создания потока обязана проброситься, получено: %v", err)
	}
	if cs != nil {
		t.Fatal("на ошибке создания поток обязан быть nil")
	}
	select {
	case <-childCtx.Done():
		// контекст отменён немедленно — течи нет
	case <-time.After(time.Second):
		t.Fatal("контекст не отменён на ошибке создания потока: течь контекста")
	}
}
