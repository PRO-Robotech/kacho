// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
)

// fakeZoneSvc / fakeRegionSvc — двойники geo-стабов. Отвечают ровно тем кодом,
// который назван тестом: полоса выбирается по ОТВЕТУ ВЛАДЕЛЬЦА, и двойник обязан
// уметь произвести каждый ответ, который производит владелец.
type fakeZoneSvc struct {
	geov1.ZoneServiceClient
	err error
}

func (f *fakeZoneSvc) Get(context.Context, *geov1.GetZoneRequest, ...grpc.CallOption) (*geov1.Zone, error) {
	return nil, f.err
}

func (f *fakeZoneSvc) List(context.Context, *geov1.ListZonesRequest, ...grpc.CallOption) (*geov1.ListZonesResponse, error) {
	return nil, f.err
}

type fakeRegionSvc struct {
	geov1.RegionServiceClient
	err error
}

func (f *fakeRegionSvc) Get(context.Context, *geov1.GetRegionRequest, ...grpc.CallOption) (*geov1.Region, error) {
	return nil, f.err
}

func reasonTokenOf(t *testing.T, err error) string {
	t.Helper()
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}
	return ""
}

// Полоса peer-validate к geo на ВСЕХ четырёх точках storage. Проверяется то, что
// увидит клиент, — то есть результат ПОСЛЕ сервисного маппера: полоса обязана
// пережить sentinel-слой, иначе машинный признак теряется ровно там, где
// собирается ответ.
func TestGeoLaneSurvivesServiceMapper(t *testing.T) {
	miss := status.Error(codes.NotFound, "Zone x not found")

	for _, tc := range []struct {
		name string
		call func(*GeoClient) error
		typ  string
	}{
		{"EnsureZoneExists", func(c *GeoClient) error {
			return c.EnsureZoneExists(context.Background(), "z-nope")
		}, "geo.zone"},
		{"RegionOfZone", func(c *GeoClient) error {
			_, err := c.RegionOfZone(context.Background(), "z-nope")
			return err
		}, "geo.zone"},
		{"EnsureRegionExists", func(c *GeoClient) error {
			return c.EnsureRegionExists(context.Background(), "r-nope")
		}, "geo.region"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &GeoClient{cli: &fakeZoneSvc{err: miss}, regionCli: &fakeRegionSvc{err: miss}}
			err := serviceerr.ToStatus(tc.call(c))

			require.Equal(t, codes.FailedPrecondition, status.Code(err),
				"промах peer-валидации — FAILED_PRECONDITION (§9 п.1)")
			require.Equal(t, "PEER_RESOURCE_MISSING", reasonTokenOf(t, err),
				"машинный признак обязан пережить sentinel-слой сервиса")
		})
	}
}

// ZonesOfRegion идёт отдельной пробой: у неё промах приходит другим кодом
// владельца (InvalidArgument на неизвестный region-фильтр), и полоса обязана
// быть той же. Ветка, собиравшая ответ сама, — это то, как один сервис начинает
// отвечать двумя кодами на одну ситуацию.
func TestZonesOfRegionUnknownRegionIsSameLane(t *testing.T) {
	c := &GeoClient{cli: &fakeZoneSvc{err: status.Error(codes.InvalidArgument, "bad region filter")}}
	_, err := c.ZonesOfRegion(context.Background(), "r-nope")
	err = serviceerr.ToStatus(err)

	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "PEER_RESOURCE_MISSING", reasonTokenOf(t, err))
}

// Владелец недоступен — своя полоса, fail-closed. Утверждается вместе с
// промахом: отрицание в одиночку зеленеет и на мёртвом клиенте, поэтому рядом
// стоит положительный контроль другой полосы.
func TestGeoUnavailableIsItsOwnLane(t *testing.T) {
	down := status.Error(codes.Unavailable, "connection refused to 10.0.0.9:9090")
	c := &GeoClient{cli: &fakeZoneSvc{err: down}, regionCli: &fakeRegionSvc{err: down}}

	err := serviceerr.ToStatus(c.EnsureZoneExists(context.Background(), "z1"))
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, "PEER_UNAVAILABLE", reasonTokenOf(t, err))
	require.NotContains(t, status.Convert(err).Message(), "10.0.0.9",
		"сырой адрес пира наружу не течёт")
}
