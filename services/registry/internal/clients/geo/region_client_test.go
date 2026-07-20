// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package geo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	geopb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	geoclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/geo"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// fakeRegionSvc — stub geopb.RegionServiceClient: Get возвращает предустановленный
// (reg,err). Остальные методы наследуются от embedded-интерфейса (не вызываются).
type fakeRegionSvc struct {
	geopb.RegionServiceClient
	reg *geopb.Region
	err error
}

func (f fakeRegionSvc) Get(context.Context, *geopb.GetRegionRequest, ...grpc.CallOption) (*geopb.Region, error) {
	return f.reg, f.err
}

// REG-1 F4 — geo adapter маппит gRPC-status RegionService.Get в domain-sentinel'ы
// (fail-closed). NotFound → ErrInvalidArg (region-not-found на input-time);
// nil-conn / empty-id — ранний reject без сетевого вызова.
func TestRegionClient_RegionExists_Mapping(t *testing.T) {
	t.Run("found_ok", func(t *testing.T) {
		c := geoclient.NewFromStubs(fakeRegionSvc{reg: &geopb.Region{Id: "eu-north-1"}})
		require.NoError(t, c.RegionExists(context.Background(), "eu-north-1"))
	})
	t.Run("not_found_invalid_arg", func(t *testing.T) {
		c := geoclient.NewFromStubs(fakeRegionSvc{err: status.Error(codes.NotFound, "no region")})
		err := c.RegionExists(context.Background(), "eu-west-9")
		require.True(t, errors.Is(err, regerrors.ErrInvalidArg), "NotFound → ErrInvalidArg, got %v", err)
	})
	t.Run("permission_denied_hidden_as_invalid_arg", func(t *testing.T) {
		c := geoclient.NewFromStubs(fakeRegionSvc{err: status.Error(codes.PermissionDenied, "denied")})
		err := c.RegionExists(context.Background(), "eu-north-1")
		require.True(t, errors.Is(err, regerrors.ErrInvalidArg), "PermissionDenied не лик'ается — ErrInvalidArg, got %v", err)
	})
	t.Run("nil_conn_fail_closed", func(t *testing.T) {
		c := geoclient.New(nil)
		require.True(t, errors.Is(c.RegionExists(context.Background(), "eu-north-1"), regerrors.ErrUnavailable))
	})
	t.Run("empty_region_id", func(t *testing.T) {
		c := geoclient.NewFromStubs(fakeRegionSvc{reg: &geopb.Region{}})
		require.True(t, errors.Is(c.RegionExists(context.Background(), ""), regerrors.ErrInvalidArg))
	})
}
