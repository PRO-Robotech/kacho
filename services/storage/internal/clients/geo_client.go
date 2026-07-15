// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clients — adapter-слой gRPC-клиентов к peer-сервисам kacho-storage.
// Реализует port-интерфейсы use-case (volume.GeoClient / volume.IAMClient /
// snapshot.IAMClient). grpc-stubs живут ЗДЕСЬ, не в use-case (dependency rule).
// Каждый внешний вызов несёт собственный context.WithTimeout (architecture.md
// per-call deadline) — неотвечающий peer не вешает горутину навсегда. Fail-closed:
// peer недоступен → Unavailable (мутация не проходит на unknown состоянии).
package clients

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/auth"
	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
)

// peerCallTimeout — per-call deadline любого peer-RPC (geo/iam). Configured, не
// сырой request-ctx (architecture.md): неотвечающий peer (GC/overload/half-open TCP)
// иначе повесил бы request-горутину до конца request-ctx.
const peerCallTimeout = 3 * time.Second

// GeoClient — клиент ребра storage→geo (валидация zone_id через ZoneService.Get).
type GeoClient struct {
	cli geov1.ZoneServiceClient
}

// NewGeoClient создаёт GeoClient поверх готового *grpc.ClientConn к kacho-geo
// public (:9090). conn может быть nil в dev-скелете (peer ещё не подключён) —
// тогда любой вызов fail-closed через Unavailable.
func NewGeoClient(conn *grpc.ClientConn) *GeoClient {
	c := &GeoClient{}
	if conn != nil {
		c.cli = geov1.NewZoneServiceClient(conn)
	}
	return c
}

// EnsureZoneExists валидирует zone_id через kacho-geo (ZoneService.Get) на
// request-path Create. Несуществующая/невалидная зона → InvalidArgument
// "unknown zone id '<X>'" (зеркалит vpc/compute→geo). Peer недоступен → Unavailable
// (fail-closed для мутации). Identity вызывающего форвардится (auth.PropagateOutgoing).
func (c *GeoClient) EnsureZoneExists(ctx context.Context, zoneID string) error {
	if c.cli == nil {
		return status.Error(codes.Unavailable, "storage→geo ZoneService not configured")
	}
	cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()
	if _, err := c.cli.Get(auth.PropagateOutgoing(cctx), &geov1.GetZoneRequest{ZoneId: zoneID}); err != nil {
		switch status.Code(err) {
		case codes.NotFound, codes.InvalidArgument:
			return fmt.Errorf("%w: unknown zone id '%s'", ports.ErrInvalidArg, zoneID)
		default:
			return status.Error(codes.Unavailable, "geo zone validation unavailable")
		}
	}
	return nil
}

var _ volume.GeoClient = (*GeoClient)(nil)
