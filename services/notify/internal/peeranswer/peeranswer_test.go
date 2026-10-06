// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package peeranswer_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/notify/internal/peeranswer"
)

// wantGenus — род каждого кода gRPC, выписанный здесь НЕЗАВИСИМО от таблицы
// пакета (З23): «недоступен» — нет ответа, UNAVAILABLE, DEADLINE_EXCEEDED
// (отмена вызова своим контекстом — тоже «ответа нет»); «отказ» — любой иной
// код; OK — ответ получен. Имена — канонические имена кодов контракта gRPC.
var wantGenus = []struct {
	code  codes.Code
	label string
	genus peeranswer.Genus
}{
	{codes.OK, "OK", peeranswer.Answered},
	{codes.Canceled, "CANCELLED", peeranswer.Unavailable},
	{codes.Unknown, "UNKNOWN", peeranswer.Refused},
	{codes.InvalidArgument, "INVALID_ARGUMENT", peeranswer.Refused},
	{codes.DeadlineExceeded, "DEADLINE_EXCEEDED", peeranswer.Unavailable},
	{codes.NotFound, "NOT_FOUND", peeranswer.Refused},
	{codes.AlreadyExists, "ALREADY_EXISTS", peeranswer.Refused},
	{codes.PermissionDenied, "PERMISSION_DENIED", peeranswer.Refused},
	{codes.ResourceExhausted, "RESOURCE_EXHAUSTED", peeranswer.Refused},
	{codes.FailedPrecondition, "FAILED_PRECONDITION", peeranswer.Refused},
	{codes.Aborted, "ABORTED", peeranswer.Refused},
	{codes.OutOfRange, "OUT_OF_RANGE", peeranswer.Refused},
	{codes.Unimplemented, "UNIMPLEMENTED", peeranswer.Refused},
	{codes.Internal, "INTERNAL", peeranswer.Refused},
	{codes.Unavailable, "UNAVAILABLE", peeranswer.Unavailable},
	{codes.DataLoss, "DATA_LOSS", peeranswer.Refused},
	{codes.Unauthenticated, "UNAUTHENTICATED", peeranswer.Refused},
}

// Перечень пакета — ровно семнадцать кодов контракта gRPC, каждый своей
// строкой; род каждого — по З23, имя — каноническое.
func TestEveryGRPCCodeHasItsOwnRowAndGenus(t *testing.T) {
	got := peeranswer.Codes()
	if len(got) != len(wantGenus) {
		t.Fatalf("перечень кодов пакета: %d, контракт gRPC: %d", len(got), len(wantGenus))
	}
	for i, w := range wantGenus {
		if got[i] != w.code {
			t.Errorf("перечень кодов, позиция %d: %v, ждали %v", i, got[i], w.code)
		}
		if l := peeranswer.CodeLabel(w.code); l != w.label {
			t.Errorf("имя кода %v: %q, ждали %q", w.code, l, w.label)
		}
		if g := peeranswer.GenusOf(w.code); g != w.genus {
			t.Errorf("GenusOf(%v) = %v, ждали %v", w.code, g, w.genus)
		}
		var err error
		if w.code != codes.OK {
			err = status.Error(w.code, "текст сервера")
		}
		a := peeranswer.Classify(err)
		if a.Genus() != w.genus || a.Code() != w.code {
			t.Errorf("Classify(%v) = (род %v, код %v), ждали (род %v, код %v)",
				w.code, a.Genus(), a.Code(), w.genus, w.code)
		}
	}
}

// Код вне контракта gRPC (число с провода) — «отказ» с именем UNKNOWN, а не
// «прочее»: так же grpc-go читает ошибку без статуса (status.Code).
func TestOutOfRangeCodeIsRefusedAsUnknown(t *testing.T) {
	a := peeranswer.Classify(status.Error(codes.Code(42), "x"))
	if a.Genus() != peeranswer.Refused || a.Code() != codes.Unknown {
		t.Fatalf("код 42: (род %v, код %v), ждали (Refused, UNKNOWN)", a.Genus(), a.Code())
	}
	if g := peeranswer.GenusOf(codes.Code(42)); g != peeranswer.Refused {
		t.Fatalf("GenusOf(42) = %v, ждали Refused", g)
	}
	if l := peeranswer.CodeLabel(codes.Code(42)); l != "UNKNOWN" {
		t.Fatalf("имя кода 42: %q, ждали UNKNOWN", l)
	}
}

// Ошибка без статуса gRPC (клиент её не производит — значит, её произвёл
// наш же переходник) — «отказ» UNKNOWN: громкий сигнал, а не тихое «ждём».
func TestNonStatusErrorIsRefused(t *testing.T) {
	a := peeranswer.Classify(errors.New("переходник сломан"))
	if a.Genus() != peeranswer.Refused || a.Code() != codes.Unknown {
		t.Fatalf("ошибка без статуса: (род %v, код %v), ждали (Refused, UNKNOWN)", a.Genus(), a.Code())
	}
}

// Истечение и отмена своего контекста — «ответа нет», даже обёрнутые.
func TestOwnContextErrorsAreNoAnswer(t *testing.T) {
	for _, c := range []struct {
		err  error
		code codes.Code
	}{
		{fmt.Errorf("вызов: %w", context.DeadlineExceeded), codes.DeadlineExceeded},
		{fmt.Errorf("вызов: %w", context.Canceled), codes.Canceled},
	} {
		a := peeranswer.Classify(c.err)
		if a.Genus() != peeranswer.Unavailable || a.Code() != c.code {
			t.Errorf("%v: (род %v, код %v), ждали (Unavailable, %v)", c.err, a.Genus(), a.Code(), c.code)
		}
	}
}

// Ответ без варианта — «отказ» с кодом OK: ответ пришёл, решения в нём нет
// (УК69, NTF-2 Е8 (б)).
func TestMalformedAnswerIsRefusedWithCodeOK(t *testing.T) {
	a := peeranswer.Malformed()
	if a.Genus() != peeranswer.Refused || a.Code() != codes.OK {
		t.Fatalf("Malformed: (род %v, код %v), ждали (Refused, OK)", a.Genus(), a.Code())
	}
}

// Нулевое значение Answer не выдаёт себя ни за один род: классификатор его
// не производит.
func TestZeroAnswerHasNoGenus(t *testing.T) {
	var a peeranswer.Answer
	for _, g := range []peeranswer.Genus{peeranswer.Answered, peeranswer.Unavailable, peeranswer.Refused} {
		if a.Genus() == g {
			t.Fatalf("нулевой Answer читается родом %v", g)
		}
	}
}

// healthStub — сервер настоящего транспорта: отвечает тем, что задала проба.
type healthStub struct {
	healthpb.UnimplementedHealthServer
	answer func(ctx context.Context) error
}

func (h healthStub) Check(ctx context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	if err := h.answer(ctx); err != nil {
		return nil, err
	}
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func serve(t *testing.T, answer func(ctx context.Context) error) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель пробы: %v", err)
	}
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, healthStub{answer: answer})
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return ln.Addr().String(), srv.Stop
}

func call(t *testing.T, addr string, timeout time.Duration) error {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("клиент пробы: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	return err
}

// Травма настоящего входа: ошибки, которые производит транспорт grpc-go, а не
// выписанные пробой статусы (NTF1-F11 — слушатель остановлен; срок вызова;
// отказ сервера; ответ получен).
func TestRealTransportErrorsAreClassified(t *testing.T) {
	t.Run("слушатель остановлен — недоступен", func(t *testing.T) {
		addr, stop := serve(t, func(context.Context) error { return nil })
		stop()
		a := peeranswer.Classify(call(t, addr, 2*time.Second))
		if a.Genus() != peeranswer.Unavailable {
			t.Fatalf("остановленный слушатель: (род %v, код %v), ждали Unavailable", a.Genus(), a.Code())
		}
	})
	t.Run("срок вызова истёк — недоступен", func(t *testing.T) {
		addr, _ := serve(t, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
		a := peeranswer.Classify(call(t, addr, 100*time.Millisecond))
		if a.Genus() != peeranswer.Unavailable || a.Code() != codes.DeadlineExceeded {
			t.Fatalf("срок: (род %v, код %v), ждали (Unavailable, DEADLINE_EXCEEDED)", a.Genus(), a.Code())
		}
	})
	t.Run("сервер отказал в праве — отказ", func(t *testing.T) {
		addr, _ := serve(t, func(context.Context) error { return status.Error(codes.PermissionDenied, "permission denied") })
		a := peeranswer.Classify(call(t, addr, 2*time.Second))
		if a.Genus() != peeranswer.Refused || a.Code() != codes.PermissionDenied {
			t.Fatalf("PERMISSION_DENIED: (род %v, код %v), ждали (Refused, PERMISSION_DENIED)", a.Genus(), a.Code())
		}
	})
	t.Run("ответ получен", func(t *testing.T) {
		addr, _ := serve(t, func(context.Context) error { return nil })
		a := peeranswer.Classify(call(t, addr, 2*time.Second))
		if a.Genus() != peeranswer.Answered || a.Code() != codes.OK {
			t.Fatalf("ответ: (род %v, код %v), ждали (Answered, OK)", a.Genus(), a.Code())
		}
	})
}

// series — значения серий семейства name по меткам (метки склеены «|» в
// порядке объявления). Серии нет — ключа нет.
func series(t *testing.T, reg prometheus.Gatherer, name string) map[string]float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("сбор метрик: %v", err)
	}
	out := map[string]float64{}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			key := ""
			for i, l := range m.GetLabel() {
				if i > 0 {
					key += "|"
				}
				key += l.GetName() + "=" + l.GetValue()
			}
			out[key] = m.GetCounter().GetValue()
		}
	}
	return out
}

// Сигнал misconfigured: серии источника заведены нулём до первого сигнала,
// каждый сигнал — +1 в своей клетке, соседняя клетка не трогается.
func TestMisconfiguredSignalIsCountedPerSourceAndCause(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	sig, err := peeranswer.NewSignals(reg)
	if err != nil {
		t.Fatalf("NewSignals: %v", err)
	}
	sig.Declare("probe")
	got := series(t, reg, "notify_misconfigured_total")
	if len(got) == 0 || len(got) != len(peeranswer.Causes()) {
		t.Fatalf("серий misconfigured после Declare: %d, ждали %d > 0", len(got), len(peeranswer.Causes()))
	}
	sig.Misconfigured("probe", peeranswer.CauseResolveSendRefused)
	sig.Misconfigured("probe", peeranswer.CauseResolveSendRefused)
	got = series(t, reg, "notify_misconfigured_total")
	if v := got["cause=resolve_send_refused|source=probe"]; v != 2 {
		t.Fatalf("misconfigured{probe, refused} = %v, ждали 2 (серии: %v)", v, got)
	}
	if v, ok := got["cause=resolve_send_protocol|source=probe"]; !ok || v != 0 {
		t.Fatalf("misconfigured{probe, protocol} = %v (есть: %v), ждали заведённый 0", v, ok)
	}
}

// Перечень причин закрыт и непуст; повторов нет.
func TestCausesAreClosedAndUnique(t *testing.T) {
	cs := peeranswer.Causes()
	if len(cs) == 0 {
		t.Fatal("перечень причин пуст — счётчику нечего заводить")
	}
	seen := map[peeranswer.Cause]bool{}
	for _, c := range cs {
		if c == "" || seen[c] {
			t.Fatalf("причина %q пуста или повторена", c)
		}
		seen[c] = true
	}
	for _, c := range []peeranswer.Cause{peeranswer.CauseResolveSendRefused, peeranswer.CauseResolveSendProtocol} {
		if !seen[c] {
			t.Errorf("причина %q объявлена константой, но не в перечне", c)
		}
	}
}

func TestNewSignalsRefusesNilRegistry(t *testing.T) {
	if _, err := peeranswer.NewSignals(nil); err == nil {
		t.Fatal("NewSignals(nil) — ждали ошибку программы")
	}
}
