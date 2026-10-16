// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package upstreamfailure_test

// upstreamfailure_test.go — kacho#3221: три причины отказа вызова соседу
// различимы на приборе, и каждая получена от НАСТОЯЩЕЙ библиотеки gRPC, а не
// выписанной строкой. Текст отказа «адрес не разрешается» порождает балансировщик
// библиотеки, поэтому проба идёт через настоящий `grpc.NewClient` обеими
// политиками балансировки, какими край набирает соседей: round_robin (мост и
// маршрутизатор) и умолчание pick_first (клиент субъекта). Подъём библиотеки,
// сменивший текст, краснит эту пробу, а не гасит причину молча.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/upstreamfailure"
)

// policies — политики балансировки, какими край набирает соседей.
var policies = map[string][]grpc.DialOption{
	"round_robin": {grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`)},
	"pick_first":  nil,
}

var schemeSeq atomic.Int64

// callThrough зовёт проверку здоровья через настоящий клиент на target и
// возвращает отказ, каким его видит вызывающий.
func callThrough(t *testing.T, target string, budget time.Duration, extra ...grpc.DialOption) error {
	t.Helper()
	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, extra...)
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(%s): %v", target, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err == nil {
		t.Fatalf("вызов на %s прошёл — условия отказа не создано", target)
	}
	return err
}

// manualTarget — адрес со своим разрешателем, которому проба диктует исход.
func manualTarget(t *testing.T, apply func(r *manual.Resolver)) (string, grpc.DialOption) {
	t.Helper()
	scheme := fmt.Sprintf("kachoprobe%d", schemeSeq.Add(1))
	r := manual.NewBuilderWithScheme(scheme)
	r.BuildCallback = func(resolver.Target, resolver.ClientConn, resolver.BuildOptions) {
		go apply(r)
	}
	return scheme + ":///upstream", grpc.WithResolvers(r)
}

// TestUnresolvedAddressIsItsOwnCause — адрес, который не разрешается, — своя
// причина, на обеих политиках и обоих способах, какими разрешатель сообщает
// о неудаче: ошибкой и пустым набором адресов.
func TestUnresolvedAddressIsItsOwnCause(t *testing.T) {
	modes := map[string]func(r *manual.Resolver){
		// Так сообщает разрешатель dns библиотеки о сбое самого запроса имени
		// (срок, временная ошибка сервера имён): `handleDNSError` в
		// internal/resolver/dns. Имени, которого нет (NXDOMAIN), он отвечает
		// пустым набором — это второй способ ниже; его же дал замер посылки на
		// настоящем имени `.invalid` («no children to pick from»).
		"resolver_error": func(r *manual.Resolver) {
			r.CC().ReportError(errors.New("dns: A record lookup error: lookup kacho-absent.kacho.svc on 10.96.0.10:53: i/o timeout"))
		},
		"zero_addresses": func(r *manual.Resolver) { r.UpdateState(resolver.State{}) },
	}
	n := 0
	for pname, popts := range policies {
		for mname, mode := range modes {
			target, withR := manualTarget(t, mode)
			err := callThrough(t, target, 2*time.Second, append([]grpc.DialOption{withR}, popts...)...)
			got, ok := upstreamfailure.Classify(err)
			if !ok || got != upstreamfailure.CauseUnresolved {
				t.Errorf("%s/%s: причина %q (учтено=%v), ожидалась %q; отказ библиотеки: %v",
					pname, mname, got, ok, upstreamfailure.CauseUnresolved, err)
			}
			n++
		}
	}
	t.Logf("перепись: сочетаний политика×способ %d", n)
}

// TestStoppedServiceIsUnavailableNotUnresolved — законный близнец: адрес
// разрешается, слушателя нет (служба остановлена). Та же величина 503 у
// вызывающего, ДРУГАЯ причина на приборе.
func TestStoppedServiceIsUnavailableNotUnresolved(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close() // адрес настоящий, слушателя за ним больше нет
	for pname, popts := range policies {
		err := callThrough(t, addr, 2*time.Second, popts...)
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("%s: предпосылка — у вызывающего UNAVAILABLE, получено %v", pname, err)
		}
		got, ok := upstreamfailure.Classify(err)
		if !ok || got != upstreamfailure.CauseUnavailable {
			t.Errorf("%s: причина %q (учтено=%v), ожидалась %q; отказ: %v", pname, got, ok, upstreamfailure.CauseUnavailable, err)
		}
	}
}

// TestServiceAnsweringUnavailableIsUnavailable — служба жива и сама отвечает
// UNAVAILABLE: это «служба не может», не адрес.
func TestServiceAnsweringUnavailableIsUnavailable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, unavailableHealth{})
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	err = callThrough(t, l.Addr().String(), 2*time.Second)
	got, ok := upstreamfailure.Classify(err)
	if !ok || got != upstreamfailure.CauseUnavailable {
		t.Errorf("причина %q (учтено=%v), ожидалась %q; отказ: %v", got, ok, upstreamfailure.CauseUnavailable, err)
	}
}

type unavailableHealth struct {
	healthpb.UnimplementedHealthServer
}

func (unavailableHealth) Check(context.Context, *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return nil, status.Error(codes.Unavailable, "database is down")
}

// TestExceededBudgetIsTheDeadlineCause — сосед принимает соединение и молчит;
// бюджет 1ms истекает. Причина — срок, а не «служба лежит».
func TestExceededBudgetIsTheDeadlineCause(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, slowHealth{})
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	for pname, popts := range policies {
		err := callThrough(t, l.Addr().String(), time.Millisecond, popts...)
		got, ok := upstreamfailure.Classify(err)
		if !ok || got != upstreamfailure.CauseDeadline {
			t.Errorf("%s: причина %q (учтено=%v), ожидалась %q; отказ: %v", pname, got, ok, upstreamfailure.CauseDeadline, err)
		}
	}
}

type slowHealth struct {
	healthpb.UnimplementedHealthServer
}

func (slowHealth) Check(ctx context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestAnswersOfTheServiceAreNotUpstreamFailures — ответ службы по существу
// (нет, запрещено, неверно) и отмена вызывающим причиной отказа соседа не
// являются: клетка о них молчит.
func TestAnswersOfTheServiceAreNotUpstreamFailures(t *testing.T) {
	for _, err := range []error{
		nil,
		status.Error(codes.NotFound, "not found"),
		status.Error(codes.PermissionDenied, "denied"),
		status.Error(codes.InvalidArgument, "bad"),
		status.Error(codes.Unauthenticated, "who"),
		status.Error(codes.Canceled, "context canceled"),
	} {
		if got, ok := upstreamfailure.Classify(err); ok {
			t.Errorf("%v учтён причиной %q — это ответ службы, а не отказ соседа", err, got)
		}
	}
}

// TestTallyHasEveryCellWithZeroAndCountsThroughTheInterceptor — клетки
// существуют нулями до первого события, перехватчик вызова учитывает отказ под
// своим соседом, а незаявленный сосед меткой не становится.
func TestTallyHasEveryCellWithZeroAndCountsThroughTheInterceptor(t *testing.T) {
	tally := upstreamfailure.NewTally([]string{"geo", "iamInternal"})
	cells := tally.Snapshot()
	if want := 2 * len(upstreamfailure.Causes()); len(cells) != want {
		t.Fatalf("клеток %d, ожидалось %d (соседи × причины)", len(cells), want)
	}
	for _, c := range cells {
		if c.Count != 0 {
			t.Fatalf("клетка %+v не нулевая до первого события", c)
		}
	}

	target, withR := manualTarget(t, func(r *manual.Resolver) { r.UpdateState(resolver.State{}) })
	_ = callThrough(t, target, 2*time.Second, withR,
		grpc.WithChainUnaryInterceptor(tally.UnaryClientInterceptor("geo")))
	tally.Record("not-declared", status.Error(codes.Unavailable, "x"))

	counts := map[string]uint64{}
	for _, c := range tally.Snapshot() {
		counts[c.Upstream+"/"+string(c.Cause)] = c.Count
	}
	if counts["geo/unresolved"] != 1 {
		t.Errorf("geo/unresolved = %d, ожидалась 1: %v", counts["geo/unresolved"], counts)
	}
	if _, leaked := counts["not-declared/unavailable"]; leaked {
		t.Error("незаявленный сосед стал меткой — словарь меток не закрыт")
	}
	if len(counts) != 2*len(upstreamfailure.Causes()) {
		t.Errorf("клеток после событий %d — словарь вырос", len(counts))
	}
}

// TestNilTallyIsInert — несобранный учёт не роняет вызов.
func TestNilTallyIsInert(t *testing.T) {
	var tally *upstreamfailure.Tally
	tally.Record("geo", status.Error(codes.Unavailable, "x"))
	if cells := tally.Snapshot(); len(cells) != 0 {
		t.Fatalf("у несобранного учёта клетки: %v", cells)
	}
}
