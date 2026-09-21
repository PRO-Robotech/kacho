// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// route_refusal_observed_test.go — отказ по маршруту ОСТАВЛЯЕТ СЛЕД.
//
// # Предмет
//
// Это точный близнец отказа «этот слушатель такого не обслуживает» на полосе
// HTTP: тот же вопрос, тот же ответ, та же причина — путь административный, а
// слушатель внешний. У близнеца на полосе HTTP есть и запись в журнале, и своя
// полоса счётчика. Здесь не было ни того, ни другого: отказ производился молча.
//
// Молчание здесь хуже, чем кажется. Отказ по маршруту — это ровно то событие,
// по которому видно, что кто-то перебирает административную поверхность; и
// именно оно не попадало НИ В ОДИН наблюдаемый: ни в журнал (звено стоит
// снаружи журнала доступа), ни в счётчик (его не было).
//
// # Что утверждается
//
// Своя величина растёт на отказе и НЕ растёт на пропуске; запись печатается на
// уровне, который процесс печатает, и несёт имя метода.
package proxy_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

func observerWithSink() (*proxy.RouteRefusalObserver, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	// Порог — тот же, что ставит корень процесса.
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return proxy.NewRouteRefusalObserver(logger), buf
}

// TestRouteRefusalGrowsItsOwnLaneAndLeavesARecord — отказ по маршруту виден в
// обоих наблюдаемых.
func TestRouteRefusalGrowsItsOwnLaneAndLeavesARecord(t *testing.T) {
	obs, buf := observerWithSink()
	const internalMethod = "/kacho.cloud.vpc.v1.InternalAddressPoolService/Get"

	before := obs.Refused()
	_, err := proxy.UnaryRefuseInternalRoute(obs)(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: internalMethod},
		func(context.Context, any) (any, error) { return "served", nil })

	if status.Code(err) != codes.NotFound {
		t.Fatalf("отказ по маршруту обязан быть NotFound, получен %v — проба меряет не тот предмет",
			status.Code(err))
	}
	if got := obs.Refused(); got != before+1 {
		t.Errorf("своя величина отказа по маршруту не выросла: было %d, стало %d. "+
			"Близнец на полосе HTTP считается, этот — нет, и перебор административной "+
			"поверхности на нативной полосе не виден ничем.", before, got)
	}
	if !strings.Contains(buf.String(), "InternalAddressPoolService") {
		t.Errorf("отказ по маршруту не оставил записи с именем метода. Звено стоит СНАРУЖИ "+
			"журнала доступа, поэтому кроме собственной записи следа у него нет:\n%s", buf.String())
	}
}

// TestRouteRefusalLaneIsSilentOnALawfulCall — законный близнец: метод, который
// этот слушатель обслуживает, ни величину не растит, ни записи не оставляет.
// Без него «величина выросла» не отличалось бы от величины, растущей на всём.
func TestRouteRefusalLaneIsSilentOnALawfulCall(t *testing.T) {
	obs, buf := observerWithSink()
	before := obs.Refused()

	served := false
	_, err := proxy.UnaryRefuseInternalRoute(obs)(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/Get"},
		func(context.Context, any) (any, error) { served = true; return "served", nil })

	if err != nil || !served {
		t.Fatalf("законный метод обязан быть пропущен: served=%v err=%v", served, err)
	}
	if got := obs.Refused(); got != before {
		t.Errorf("величина отказа выросла на ЗАКОННОМ вызове: было %d, стало %d", before, got)
	}
	if buf.Len() != 0 {
		t.Errorf("законный вызов оставил запись — полоса пишет на всём подряд:\n%s", buf.String())
	}
}

// TestStreamRouteRefusalIsObservedToo — потоковая форма несущая: проксируемый
// трафик домена идёт через неё, и наблюдаемость у неё обязана быть та же.
func TestStreamRouteRefusalIsObservedToo(t *testing.T) {
	obs, buf := observerWithSink()
	const internalMethod = "/kacho.cloud.geo.v1.InternalZoneService/Get"

	before := obs.Refused()
	err := proxy.StreamRefuseInternalRoute(obs)(nil, nopServerStream{},
		&grpc.StreamServerInfo{FullMethod: internalMethod},
		func(any, grpc.ServerStream) error { return nil })

	if status.Code(err) != codes.NotFound {
		t.Fatalf("потоковый отказ обязан быть NotFound, получен %v", status.Code(err))
	}
	if got := obs.Refused(); got != before+1 {
		t.Errorf("потоковая форма не растит величину: было %d, стало %d", before, got)
	}
	if !strings.Contains(buf.String(), "InternalZoneService") {
		t.Errorf("потоковый отказ не оставил записи:\n%s", buf.String())
	}
}

// nopServerStream — поток, у которого спрашивают только контекст.
type nopServerStream struct{ grpc.ServerStream }

func (nopServerStream) Context() context.Context { return context.Background() }
