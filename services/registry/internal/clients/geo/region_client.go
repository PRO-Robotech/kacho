// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package geo — adapter-клиент к kacho-geo RegionService. Реализует порт
// registry.GeoClient: cross-domain валидация Namespace.region_id на Create
// (geo.v1.RegionService.Get). **Новое runtime-ребро registry→geo** (REG-1 F4;
// ацикличность holds — geo leaf, registry не зовётся обратно). RegionService живёт
// на geo PUBLIC-листенере (:9090) — публичный read-only справочник Geography.
package geo

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	geopb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/retry"

	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// regionCallTimeout — per-call deadline на RegionExists (зеркалит iamCallTimeout —
// resource-read, 5s). retry.OnUnavailable сам по себе НЕ ограничивает время ОДНОГО
// зависшего Get (bounds только backoff МЕЖДУ попытками); без собственного дедлайна
// зависший-но-подключённый geo пинил бы Create-горутину навсегда (architecture.md
// "Per-call deadline на КАЖДОМ внешнем вызове"). Никогда не полагаемся на inbound ctx.
const regionCallTimeout = 5 * time.Second

// Client — adapter к kacho-geo RegionService поверх grpc-conn к PUBLIC-листенеру (:9090).
type Client struct {
	regions geopb.RegionServiceClient
	timeout time.Duration
}

// New оборачивает grpc-conn к kacho-geo PUBLIC-листенеру (:9090 — RegionService.Get).
// nil conn → метод отвечает Unavailable (мутация fail-closed).
func New(conn grpc.ClientConnInterface) *Client {
	if conn == nil {
		return &Client{timeout: regionCallTimeout}
	}
	return &Client{regions: geopb.NewRegionServiceClient(conn), timeout: regionCallTimeout}
}

// NewFromStubs — конструктор для тестов: принимает напрямую stub.
func NewFromStubs(regions geopb.RegionServiceClient) *Client {
	return &Client{regions: regions, timeout: regionCallTimeout}
}

// RegionExists валидирует region-якорь Namespace на Create через RegionService.Get.
// Семантика ошибок (fail-closed для мутации):
//
//	NotFound / InvalidArgument / PermissionDenied → ErrInvalidArg ("region not found")
//	Unavailable / DeadlineExceeded                → ErrUnavailable (мутация fail-closed)
func (c *Client) RegionExists(ctx context.Context, regionID string) error {
	if c.regions == nil {
		return regerrors.ErrUnavailable
	}
	if regionID == "" {
		return regerrors.ErrInvalidArg
	}

	// Per-call deadline — bounds ВЕСЬ retry.OnUnavailable, независимо от inbound ctx.
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		_, gerr := c.regions.Get(auth.PropagateOutgoing(ctx), &geopb.GetRegionRequest{RegionId: regionID})
		return gerr
	})
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return regerrors.ErrUnavailable
	}
	switch st.Code() {
	case codes.NotFound, codes.InvalidArgument, codes.PermissionDenied:
		// existence-hiding parity с iam project-precheck: не лик'аем authz-факт.
		return regerrors.ErrInvalidArg
	case codes.Unavailable, codes.DeadlineExceeded:
		return regerrors.ErrUnavailable
	default:
		return regerrors.ErrUnavailable
	}
}
