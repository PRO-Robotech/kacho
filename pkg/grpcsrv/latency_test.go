// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package grpcsrv_test

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// Задержка обслуженного вызова обязана быть НАБЛЮДАЕМОЙ, и наблюдаться она
// обязана так, чтобы отказ не смешивался с успехом.
//
// Пробы гоняют интерсептор так, как его гоняет сервер — с настоящим
// `grpc.UnaryServerInfo`, — и читают ЗНАЧЕНИЯ из реестра, а не факт вызова.
// Проверка «интерсептор позвал измеритель» осталась бы зелёной при измерителе,
// который кладёт величину не в тот ряд.

func observedValues(t *testing.T, reg *prometheus.Registry, name string) []*dto.Metric {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == name {
			return f.GetMetric()
		}
	}
	return nil
}

func labelOf(m *dto.Metric, key string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == key {
			return l.GetValue()
		}
	}
	return ""
}

func TestServerLatency_ObservesDurationAndOutcome(t *testing.T) {
	reg := prometheus.NewRegistry()
	l, err := grpcsrv.NewServerLatency(reg)
	require.NoError(t, err)

	itc := l.UnaryServerInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/Get"}

	_, err = itc(context.Background(), nil, info,
		func(context.Context, any) (any, error) { return "ok", nil })
	require.NoError(t, err)

	ms := observedValues(t, reg, "kacho_grpc_server_handling_seconds")
	require.Len(t, ms, 1, "успешный вызов обязан дать РОВНО ОДИН ряд задержки")
	require.Equal(t, "kacho.cloud.vpc.v1.NetworkService", labelOf(ms[0], "grpc_service"))
	require.Equal(t, "Get", labelOf(ms[0], "grpc_method"))
	require.Equal(t, "ok", labelOf(ms[0], "outcome"))
	require.Equal(t, uint64(1), ms[0].GetHistogram().GetSampleCount())
}

func TestServerLatency_FailureIsADifferentRow(t *testing.T) {
	reg := prometheus.NewRegistry()
	l, err := grpcsrv.NewServerLatency(reg)
	require.NoError(t, err)

	itc := l.UnaryServerInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/Create"}

	_, _ = itc(context.Background(), nil, info,
		func(context.Context, any) (any, error) { return "ok", nil })
	_, err = itc(context.Background(), nil, info,
		func(context.Context, any) (any, error) {
			return nil, status.Error(codes.FailedPrecondition, "network is not empty")
		})
	require.Error(t, err)

	// Задержка отказа и задержка успеха — РАЗНЫЕ величины: быстрый отказ
	// занижает хвост, медленный завышает. Смешать их значит получить число,
	// которое не описывает ни один из двух случаев.
	ms := observedValues(t, reg, "kacho_grpc_server_handling_seconds")
	require.Len(t, ms, 2, "успех и отказ обязаны лечь в РАЗНЫЕ ряды")

	outcomes := map[string]uint64{}
	for _, m := range ms {
		outcomes[labelOf(m, "outcome")] = m.GetHistogram().GetSampleCount()
	}
	require.Equal(t, uint64(1), outcomes["ok"])
	require.Equal(t, uint64(1), outcomes["error"])

	// Полный код остаётся у счётчика: там ряд дёшев, и различить
	// «не хватило прав» от «предусловие не выполнено» без него нельзя.
	cs := observedValues(t, reg, "kacho_grpc_server_handled_total")
	require.Len(t, cs, 2)
	codesSeen := map[string]float64{}
	for _, m := range cs {
		codesSeen[labelOf(m, "grpc_code")] = m.GetCounter().GetValue()
	}
	require.Equal(t, float64(1), codesSeen["OK"])
	require.Equal(t, float64(1), codesSeen["FailedPrecondition"])
}

func TestServerLatency_BucketsResolveWhereDecisionsAreMade(t *testing.T) {
	reg := prometheus.NewRegistry()
	l, err := grpcsrv.NewServerLatency(reg)
	require.NoError(t, err)

	_, _ = l.UnaryServerInterceptor()(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.geo.v1.ZoneService/List"},
		func(context.Context, any) (any, error) { return "ok", nil })

	ms := observedValues(t, reg, "kacho_grpc_server_handling_seconds")
	require.Len(t, ms, 1)
	buckets := ms[0].GetHistogram().GetBucket()

	// Умолчание библиотеки начинается с 5 мс: чтение из своей базы укладывается
	// в единицы миллисекунд, и все такие вызовы свалились бы в ПЕРВУЮ корзину —
	// p50 и p90 стали бы неразличимы там, где живёт большинство запросов.
	require.LessOrEqual(t, buckets[0].GetUpperBound(), 0.001,
		"нижняя граница обязана разрешать субмиллисекундные чтения")

	// Потолок умолчания — 10 с; мутация с обращением к соседу и материализацией
	// прав законно переваливает за него, и хвост терял бы разрешение.
	require.GreaterOrEqual(t, buckets[len(buckets)-1].GetUpperBound(), 30.0,
		"верхняя граница обязана вмещать законно долгую мутацию")

	var between float64
	for _, b := range buckets {
		if ub := b.GetUpperBound(); ub >= 0.001 && ub <= 0.1 {
			between++
		}
	}
	require.GreaterOrEqual(t, between, float64(6),
		"сетка обязана сгущаться там, где принимаются решения — между миллисекундой и сотней")
}

func TestServerLatency_UnparsableMethodKeepsLabelsBounded(t *testing.T) {
	reg := prometheus.NewRegistry()
	l, err := grpcsrv.NewServerLatency(reg)
	require.NoError(t, err)

	_, _ = l.UnaryServerInterceptor()(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "мусор"},
		func(context.Context, any) (any, error) { return nil, errors.New("x") })

	ms := observedValues(t, reg, "kacho_grpc_server_handling_seconds")
	require.Len(t, ms, 1)
	require.Equal(t, "unknown", labelOf(ms[0], "grpc_service"),
		"неразобранное имя обязано дать ОГРАНИЧЕННУЮ метку, а не пустую: "+
			"пустая сливает разные методы в один ряд и делает величину неверной молча")
}

func TestServerLatency_NilMeterIsATransparentPassThrough(t *testing.T) {
	var l *grpcsrv.ServerLatency
	resp, err := l.UnaryServerInterceptor()(context.Background(), "req",
		&grpc.UnaryServerInfo{FullMethod: "/x.Y/Z"},
		func(_ context.Context, r any) (any, error) { return r, nil })
	require.NoError(t, err)
	require.Equal(t, "req", resp, "нулевой измеритель обязан пропускать вызов насквозь")
}

func TestServerLatency_DoubleRegistrationIsAnErrorNotAPanic(t *testing.T) {
	reg := prometheus.NewRegistry()
	_, err := grpcsrv.NewServerLatency(reg)
	require.NoError(t, err)

	// Повторная регистрация — ошибка ВЫЗЫВАЮЩЕГО, а не причина ронять процесс:
	// слушатель, поднятый дважды по недосмотру композиционного корня, обязан
	// сказать об этом, а не умереть в момент, когда причина уже не видна.
	_, err = grpcsrv.NewServerLatency(reg)
	require.Error(t, err)
}
