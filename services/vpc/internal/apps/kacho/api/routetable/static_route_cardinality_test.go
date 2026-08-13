// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

// static_route_cardinality_test.go — потолок числа статических маршрутов на
// таблице маршрутизации, на уровне use-case.
//
// # Предмет
//
// Набор `static_routes` приходит телом запроса целиком (Create и Update с маской
// `static_routes`), и его длину выбирает вызывающий. До этой правки у него не
// было потолка НИ ОДНОГО: девять соседних наборов домена свои потолки несли
// (`domain/types.go`), а маршруты — нет, и единственным ограничителем оставался
// предел размера одного gRPC-сообщения. Стоимость длины платится трижды:
// синхронным разбором каждой записи (`netip.ParsePrefix` + `ParseAddr`),
// сериализацией набора в JSONB воркером и полной выдачей набора в КАЖДОМ
// `Get`/`List` этой таблицы и в payload каждого её события outbox.
//
// # Что утверждают пробы
//
// Границу с обеих сторон (BVA): ровно потолок ПРОХОДИТ, потолок+1 отвергается.
// Положительная половина обязательна — без неё отрицание зеленело бы и на
// проверке, отвергающей любой непустой набор.
//
// Отказ проверяется по ТРЁМ признакам, а не только по коду: код
// `InvalidArgument`, имя поля в `BadRequest.FieldViolation` и текст с величиной
// потолка. Код сам по себе неотличим от любого другого отказа валидации этого же
// запроса (например по формату префикса) — проба, спрашивающая только код,
// зеленела бы на снятой проверке кардинальности.
//
// И отдельно — что отказ СИНХРОННЫЙ: ни `Operation`, ни строки ресурса после
// него не появляется. Отказ, доехавший до воркера, вернул бы вызывающему успех
// на запрос, который выполнен не будет.

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// makeStaticRoutes — n различающихся законных маршрутов. Каждый сам по себе
// проходит валидацию (`10.a.b.0/24` без host-bits, next-hop — валидный IPv4),
// поэтому отказ на таком наборе может быть только по его ДЛИНЕ.
func makeStaticRoutes(n int) []domain.StaticRoute {
	out := make([]domain.StaticRoute, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, domain.StaticRoute{
			DestinationPrefix: fmt.Sprintf("10.%d.%d.0/24", i/256, i%256),
			NextHopAddress:    "192.168.0.1",
		})
	}
	return out
}

// assertOverCapRefusal — три признака отказа по кардинальности.
func assertOverCapRefusal(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err, "набор длиннее потолка обязан быть отвергнут")
	st, ok := status.FromError(err)
	require.True(t, ok, "отказ обязан быть gRPC-статусом")
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), fmt.Sprintf("at most %d static routes", domain.MaxStaticRoutes),
		"текст обязан назвать величину потолка — иначе вызывающий не знает, до чего сокращать набор")

	var field string
	for _, d := range st.Details() {
		if br, isBR := d.(*errdetails.BadRequest); isBR && len(br.GetFieldViolations()) > 0 {
			field = br.GetFieldViolations()[0].GetField()
		}
	}
	assert.Equal(t, "static_routes", field,
		"отказ обязан назвать ПОЛЕ: без имени поля он неотличим от отказа по любому другому полю запроса")
}

func TestCreateUseCase_StaticRoutesCardinality_Boundary(t *testing.T) {
	t.Run("ровно потолок проходит", func(t *testing.T) {
		kr := kachomock.NewRepository()
		or := repomock.NewOpsRepo()
		net := makeNetwork(t, kr)
		uc := NewCreateRouteTableUseCase(kr, &repomock.ProjectClient{OK: true}, or)

		op, err := uc.Execute(context.Background(), domain.RouteTable{
			ProjectID:    "f1",
			NetworkID:    net.ID,
			Name:         domain.RcNameVPC("rt-at-cap"),
			StaticRoutes: makeStaticRoutes(domain.MaxStaticRoutes),
		})
		require.NoError(t, err, "набор ровно на потолке обязан проходить")
		saved := repomock.AwaitOpDone(t, or, op.ID)
		require.True(t, saved.Done)
		require.Nil(t, saved.Error)

		rts := kr.RouteTables()
		require.Len(t, rts, 1)
		assert.Len(t, rts[0].StaticRoutes, domain.MaxStaticRoutes)
	})

	t.Run("потолок+1 отвергается синхронно", func(t *testing.T) {
		kr := kachomock.NewRepository()
		or := repomock.NewOpsRepo()
		net := makeNetwork(t, kr)
		uc := NewCreateRouteTableUseCase(kr, &repomock.ProjectClient{OK: true}, or)

		_, err := uc.Execute(context.Background(), domain.RouteTable{
			ProjectID:    "f1",
			NetworkID:    net.ID,
			Name:         domain.RcNameVPC("rt-over-cap"),
			StaticRoutes: makeStaticRoutes(domain.MaxStaticRoutes + 1),
		})
		assertOverCapRefusal(t, err)

		ops, _, lerr := or.List(context.Background(), operations.ListFilter{})
		require.NoError(t, lerr)
		assert.Empty(t, ops, "отказ обязан стоять ДО создания Operation")
		assert.Empty(t, kr.RouteTables(), "отвергнутый запрос не оставляет ресурса")
	})
}

func TestUpdateUseCase_StaticRoutesCardinality_Boundary(t *testing.T) {
	// Существующая таблица маршрутизации — предмет обоих сценариев Update.
	newExisting := func(t *testing.T) (*kachomock.Repository, *repomock.OpsRepo, string) {
		t.Helper()
		kr := kachomock.NewRepository()
		or := repomock.NewOpsRepo()
		net := makeNetwork(t, kr)
		createUC := NewCreateRouteTableUseCase(kr, &repomock.ProjectClient{OK: true}, or)
		op, err := createUC.Execute(context.Background(), domain.RouteTable{
			ProjectID: "f1", NetworkID: net.ID, Name: domain.RcNameVPC("rt-upd"),
		})
		require.NoError(t, err)
		saved := repomock.AwaitOpDone(t, or, op.ID)
		require.Nil(t, saved.Error)
		rts := kr.RouteTables()
		require.Len(t, rts, 1)
		return kr, or, rts[0].ID
	}

	t.Run("маска static_routes: ровно потолок проходит", func(t *testing.T) {
		kr, or, rtID := newExisting(t)
		uc := NewUpdateRouteTableUseCase(kr, or)
		op, err := uc.Execute(context.Background(), UpdateInput{
			RouteTableID: rtID,
			RouteTable:   domain.RouteTable{StaticRoutes: makeStaticRoutes(domain.MaxStaticRoutes)},
			UpdateMask:   []string{"static_routes"},
		})
		require.NoError(t, err)
		saved := repomock.AwaitOpDone(t, or, op.ID)
		require.Nil(t, saved.Error)
		rts := kr.RouteTables()
		require.Len(t, rts, 1)
		assert.Len(t, rts[0].StaticRoutes, domain.MaxStaticRoutes)
	})

	t.Run("маска static_routes: потолок+1 отвергается", func(t *testing.T) {
		kr, or, rtID := newExisting(t)
		uc := NewUpdateRouteTableUseCase(kr, or)
		_, err := uc.Execute(context.Background(), UpdateInput{
			RouteTableID: rtID,
			RouteTable:   domain.RouteTable{StaticRoutes: makeStaticRoutes(domain.MaxStaticRoutes + 1)},
			UpdateMask:   []string{"static_routes"},
		})
		assertOverCapRefusal(t, err)
		rts := kr.RouteTables()
		require.Len(t, rts, 1)
		assert.Empty(t, rts[0].StaticRoutes, "отвергнутый Update не меняет строку")
	})

	// Пустая маска — full-object PATCH: набор маршрутов применяется тоже
	// (`applyRouteTableMask`), поэтому потолок обязан действовать и здесь.
	// Эта ветка идёт в валидации ОТДЕЛЬНОЙ дорожкой от списка полей маски, и
	// проверка, поставленная только в ту дорожку, её бы не закрыла.
	t.Run("full-object PATCH: потолок+1 отвергается", func(t *testing.T) {
		kr, or, rtID := newExisting(t)
		uc := NewUpdateRouteTableUseCase(kr, or)
		_, err := uc.Execute(context.Background(), UpdateInput{
			RouteTableID: rtID,
			RouteTable: domain.RouteTable{
				Name:         domain.RcNameVPC("rt-upd"),
				StaticRoutes: makeStaticRoutes(domain.MaxStaticRoutes + 1),
			},
			UpdateMask: nil,
		})
		assertOverCapRefusal(t, err)
		rts := kr.RouteTables()
		require.Len(t, rts, 1)
		assert.Empty(t, rts[0].StaticRoutes, "отвергнутый full-PATCH не меняет строку")
	})

	t.Run("full-object PATCH: ровно потолок проходит", func(t *testing.T) {
		kr, or, rtID := newExisting(t)
		uc := NewUpdateRouteTableUseCase(kr, or)
		op, err := uc.Execute(context.Background(), UpdateInput{
			RouteTableID: rtID,
			RouteTable: domain.RouteTable{
				Name:         domain.RcNameVPC("rt-upd"),
				StaticRoutes: makeStaticRoutes(domain.MaxStaticRoutes),
			},
			UpdateMask: nil,
		})
		require.NoError(t, err)
		saved := repomock.AwaitOpDone(t, or, op.ID)
		require.Nil(t, saved.Error)
		rts := kr.RouteTables()
		require.Len(t, rts, 1)
		assert.Len(t, rts[0].StaticRoutes, domain.MaxStaticRoutes)
	})
}
