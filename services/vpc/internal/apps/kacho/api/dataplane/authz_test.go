// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

const (
	watchMethod  = "/kacho.cloud.vpc.v1.InternalDataplaneService/WatchIntent"
	reportMethod = "/kacho.cloud.vpc.v1.InternalDataplaneService/ReportIntentApplied"
)

// fakeServerStream — минимальный серверный стрим для прогона звена решения о
// доступе. Ничего не отдаёт: предмет проб ниже — дошло ли дело до обработчика.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeServerStream) Context() context.Context { return s.ctx }

func principalCtx(typ, id string) context.Context {
	return operations.WithPrincipal(context.Background(), operations.Principal{
		Type: typ, ID: id, DisplayName: "test",
	})
}

func decisionLink(t *testing.T, allow bool, seen *[]string) *authz.Interceptor {
	t.Helper()
	return authz.NewInterceptor(authz.InterceptorOptions{
		Cache:       authz.NewCache(0),
		ServiceName: "kacho-vpc-test",
		// Карта — ТА ЖЕ, что у процесса: она выводится из аннотаций тех же
		// дескрипторов. Собери её здесь литералом — и проба утверждала бы о своей
		// копии, а не о том, что исполняет сервис.
		Map: check.PermissionMap(),
		Client: authz.CheckClientFunc(func(_ context.Context, subject, relation, object string) (bool, error) {
			*seen = append(*seen, strings.Join([]string{subject, relation, object}, " "))
			return allow, nil
		}),
	})
}

// Поток гейтится решением о доступе ТАК ЖЕ, как обычный вызов: отказ модели
// закрывает обработчик.
//
// Предмет — не «есть ли проверка в обработчике» (её там нет и быть не должно), а
// то, что метод ПОПАЛ В КАРТУ, из которой звено берёт правило. Метода, которого
// в карте нет, звено не пропускает вовсе (fail-closed), поэтому обе половины
// ниже — про существо, а не про форму.
func TestStreamIsGatedByTheDecisionLink(t *testing.T) {
	var seen []string
	intr := decisionLink(t, false, &seen)

	handlerCalled := false
	handler := func(any, grpc.ServerStream) error {
		handlerCalled = true
		return nil
	}
	err := intr.Stream()(nil,
		&fakeServerStream{ctx: principalCtx("service_account", "sva_dataplane")},
		&grpc.StreamServerInfo{FullMethod: watchMethod, IsServerStream: true},
		handler)

	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, handlerCalled, "обработчик потока вызван при отказе модели")
	require.Len(t, seen, 1, "решение о доступе не спрашивалось вовсе")
	assert.Equal(t, "service_account:sva_dataplane system_viewer cluster:cluster_kacho_root", seen[0])
}

// Положительный контроль: при разрешении модели обработчик потока вызывается.
//
// Без него проба выше зеленела бы на звене, которое отвергает всё подряд, — то
// есть на потоке, которого нет.
func TestStreamRunsWhenTheModelAllows(t *testing.T) {
	var seen []string
	intr := decisionLink(t, true, &seen)

	handlerCalled := false
	handler := func(any, grpc.ServerStream) error {
		handlerCalled = true
		return nil
	}
	err := intr.Stream()(nil,
		&fakeServerStream{ctx: principalCtx("service_account", "sva_dataplane")},
		&grpc.StreamServerInfo{FullMethod: watchMethod, IsServerStream: true},
		handler)

	require.NoError(t, err)
	assert.True(t, handlerCalled, "обработчик потока не вызван при разрешении модели")
	require.Len(t, seen, 1)
}

// Неназванный вызывающий не проходит на поток.
//
// За этим методом нет второй линии: обработчик личность не спрашивает вовсе,
// поэтому пропуск здесь означал бы отдачу намерения по всем арендаторам тому,
// кто не назвался.
func TestStreamRefusesAnUnnamedCaller(t *testing.T) {
	var seen []string
	intr := decisionLink(t, true, &seen)

	handlerCalled := false
	err := intr.Stream()(nil,
		&fakeServerStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: watchMethod, IsServerStream: true},
		func(any, grpc.ServerStream) error { handlerCalled = true; return nil })

	require.Error(t, err)
	assert.False(t, handlerCalled, "поток открыт вызывающему, который не назвался")
	assert.Empty(t, seen, "решение о доступе спрашивали про неназванного вызывающего")
}

// Оба метода шва стоят в карте прав, и требуемые отношения РАЗНЫЕ.
//
// Право читать поток и право писать подтверждение — разные предметы: первое
// отдаёт намерение, второе решает, что платформа считает применённым. Совпади
// они, любой, кому позволено смотреть, смог бы объявлять применённым что угодно.
//
// Отдельно проверяется, что отношение не является справочным (`viewer`):
// справочное отношение выполнимо подстановочным кортежем `user:*`, то есть
// отвечает «да» каждому аутентифицированному — форма проверки без содержания.
func TestBothSeamMethodsCarryDistinctNonWildcardRelations(t *testing.T) {
	m := check.PermissionMap()

	watch, ok := m.Lookup(watchMethod)
	require.True(t, ok, "поток намерения отсутствует в карте прав: звено отвергнет его как незамапленный")
	report, ok := m.Lookup(reportMethod)
	require.True(t, ok, "подтверждение применения отсутствует в карте прав")

	assert.Equal(t, "system_viewer", watch.Relation)
	assert.Equal(t, "system_admin", report.Relation)
	assert.NotEqual(t, watch.Relation, report.Relation,
		"чтение потока и запись подтверждения гейтятся одним отношением")

	for name, entry := range map[string]authz.RPCEntry{"поток": watch, "подтверждение": report} {
		assert.NotEqual(t, "viewer", entry.Relation,
			"%s гейтится справочным отношением, выполнимым подстановочным кортежем: "+
				"такая проверка отвечает «да» каждому аутентифицированному", name)
	}
}

// Ни один метод шва не отдан на откуп сужению на уровне данных.
//
// `scope_filtered` снимает per-RPC проверку и обещает, что владелец сузит выдачу
// сам. У этого шва сужать нечего: исполнителю датаплейна намерение положено
// целиком, по всем арендаторам. Объявив полосу, мы сняли бы единственную
// проверку и не поставили бы взамен ничего.
func TestSeamMethodsAreNotScopeFiltered(t *testing.T) {
	for _, method := range check.ScopeFilteredRPCs() {
		assert.NotEqual(t, watchMethod, method, "поток намерения объявлен сужаемым на уровне данных")
		assert.NotEqual(t, reportMethod, method, "подтверждение объявлено сужаемым на уровне данных")
	}
}

// Ни один метод шва не выставлен на публичную поверхность.
//
// Поток несёт намерение по ВСЕМ арендаторам и координату изоляции сети; попав на
// внешний слушатель, он раскрыл бы устройство изоляции датаплейна кому угодно с
// валидным токеном. Проверяется по дескриптору службы: имя службы начинается с
// `Internal`, и её регистрация в корне идёт только внутренним регистратором
// (это утверждает проба носителя в cmd/vpc).
func TestSeamServiceIsInternalByItsVeryName(t *testing.T) {
	name := vpcv1.InternalDataplaneService_ServiceDesc.ServiceName
	assert.True(t, strings.HasPrefix(name, "kacho.cloud.vpc.v1.Internal"),
		"служба шва названа не как internal: %s", name)
	require.Len(t, vpcv1.InternalDataplaneService_ServiceDesc.Streams, 1,
		"у службы шва изменился состав потоков — проверь, что новый поток тоже гейтится")
	assert.True(t, vpcv1.InternalDataplaneService_ServiceDesc.Streams[0].ServerStreams)
	assert.False(t, vpcv1.InternalDataplaneService_ServiceDesc.Streams[0].ClientStreams,
		"поток стал двусторонним: подтверждение обязано оставаться отдельным вызовом со СВОИМ правом")
}
