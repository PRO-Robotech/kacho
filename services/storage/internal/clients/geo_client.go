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

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
)

// peerCallTimeout — per-call deadline любого peer-RPC (geo/iam). Configured, не
// сырой request-ctx (architecture.md): неотвечающий peer (GC/overload/half-open TCP)
// иначе повесил бы request-горутину до конца request-ctx.
const peerCallTimeout = 3 * time.Second

// maxZoneListPages — потолок обхода страниц ZoneService.List. Существует, чтобы
// сломанный/зациклившийся page_token пира не превратил один Create в бесконечный
// обход; при исчерпании — Unavailable (fail-closed), а не усечённый набор зон.
const maxZoneListPages = 16

// GeoClient — клиент ребра storage→geo (валидация zone_id через ZoneService.Get для
// Volume; region_id через RegionService.Get для Image — REGIONAL).
type GeoClient struct {
	cli       geov1.ZoneServiceClient
	regionCli geov1.RegionServiceClient
}

// NewGeoClient создаёт GeoClient поверх готового *grpc.ClientConn к kacho-geo
// public (:9090). conn может быть nil в dev-скелете (peer ещё не подключён) —
// тогда любой вызов fail-closed через Unavailable.
func NewGeoClient(conn *grpc.ClientConn) *GeoClient {
	c := &GeoClient{}
	if conn != nil {
		c.cli = geov1.NewZoneServiceClient(conn)
		c.regionCli = geov1.NewRegionServiceClient(conn)
	}
	return c
}

// EnsureZoneExists валидирует zone_id через kacho-geo (ZoneService.Get) на
// request-path Create. Несуществующая/невалидная зона → InvalidArgument
// "unknown zone id '<X>'" (зеркалит vpc/compute→geo). Peer недоступен → Unavailable
// (fail-closed для мутации). Identity вызывающего форвардится (auth.PropagateOutgoing).
// RegionOfZone возвращает region_id зоны (geo.v1.ZoneService.Get →
// `Zone.region_id`). Регион НИКОГДА не выводится из имени зоны — авторитет один,
// владелец Geography. Ошибки зеркалят EnsureZoneExists: неизвестная зона →
// ErrInvalidArg; недоступность geo / пустой region_id → Unavailable (fail-closed
// на мутации — непроверяемое предусловие не считается выполненным).
func (c *GeoClient) RegionOfZone(ctx context.Context, zoneID string) (string, error) {
	if c.cli == nil {
		return "", status.Error(codes.Unavailable, "storage→geo ZoneService not configured")
	}
	cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()
	resp, err := c.cli.Get(auth.PropagateOutgoing(cctx), &geov1.GetZoneRequest{ZoneId: zoneID})
	if err != nil {
		switch status.Code(err) {
		case codes.NotFound, codes.InvalidArgument:
			return "", fmt.Errorf("%w: unknown zone id '%s'", ports.ErrInvalidArg, zoneID)
		default:
			return "", status.Error(codes.Unavailable, "geo zone validation unavailable")
		}
	}
	if resp.GetRegionId() == "" {
		return "", status.Error(codes.Unavailable, "geo zone validation unavailable")
	}
	return resp.GetRegionId(), nil
}

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

// EnsureRegionExists валидирует region_id через kacho-geo (RegionService.Get) на
// request-path Image.Create (REGIONAL/anycast). Несуществующий/невалидный регион →
// InvalidArgument "unknown region id '<X>'" (зеркалит nlb→geo). Peer недоступен →
// Unavailable (fail-closed для мутации). Identity форвардится (auth.PropagateOutgoing).
func (c *GeoClient) EnsureRegionExists(ctx context.Context, regionID string) error {
	if c.regionCli == nil {
		return status.Error(codes.Unavailable, "storage→geo RegionService not configured")
	}
	cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()
	if _, err := c.regionCli.Get(auth.PropagateOutgoing(cctx), &geov1.GetRegionRequest{RegionId: regionID}); err != nil {
		switch status.Code(err) {
		case codes.NotFound, codes.InvalidArgument:
			return fmt.Errorf("%w: unknown region id '%s'", ports.ErrInvalidArg, regionID)
		default:
			return status.Error(codes.Unavailable, "geo region validation unavailable")
		}
	}
	return nil
}

var (
	_ volume.GeoClient = (*GeoClient)(nil)
	_ image.GeoClient  = (*GeoClient)(nil)
)

// zoneListPageSize — размер страницы обхода ZoneService.List. Регион несёт
// единицы зон, поэтому одна страница покрывает его целиком; обход страниц
// оставлен, потому что предел — контракт List, а не предположение о топологии.
const zoneListPageSize = 1000

// ZonesOfRegion возвращает зоны региона (geo.v1.ZoneService.List с фильтром по
// региону) — авторитет один, владелец Geography.
//
// Нужен для placement-когерентности Image.Create: образ REGIONAL, его источник
// (Volume) ЗОНАЛЬНЫЙ, и они когерентны только когда зона источника принадлежит
// региону образа. Набор зон передаётся в insert-CAS, который и принимает решение,
// сверяя ЖИВУЮ строку источника (ban #10 — не проверка до вставки). Регион зоны
// НИКОГДА не выводится разбором имени: data-integrity.md прямо это запрещает —
// строковая деривация молча возвращает пустую строку и превращает проверку в no-op.
//
// Ошибки зеркалят EnsureRegionExists: geo недоступен → Unavailable (fail-closed на
// мутации). Ответ с непустым next_page_token после исчерпания бюджета страниц —
// тоже Unavailable: неполный набор зон отверг бы законный том, поэтому неполноту
// нельзя молча принять за ответ.
func (c *GeoClient) ZonesOfRegion(ctx context.Context, regionID string) ([]string, error) {
	if c.cli == nil {
		return nil, status.Error(codes.Unavailable, "storage→geo ZoneService not configured")
	}
	var (
		out       []string
		pageToken string
	)
	for page := 0; page < maxZoneListPages; page++ {
		// Per-call deadline на КАЖДУЮ страницу (architecture.md): бюджет одной
		// страницы не должен ограничивать весь обход, и наоборот.
		cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
		resp, err := c.cli.List(auth.PropagateOutgoing(cctx), &geov1.ListZonesRequest{
			PageSize:  zoneListPageSize,
			PageToken: pageToken,
			RegionId:  regionID,
		})
		cancel()
		if err != nil {
			switch status.Code(err) {
			case codes.InvalidArgument:
				return nil, fmt.Errorf("%w: unknown region id '%s'", ports.ErrInvalidArg, regionID)
			default:
				return nil, status.Error(codes.Unavailable, "geo zone listing unavailable")
			}
		}
		for _, z := range resp.GetZones() {
			// Фильтр по региону — серверный, но принадлежность подтверждаем и по
			// ответу: набор решает, пройдёт ли чужая зона в образ, поэтому он не
			// должен зависеть от того, применил ли пир фильтр.
			if z.GetRegionId() == regionID && z.GetId() != "" {
				out = append(out, z.GetId())
			}
		}
		pageToken = resp.GetNextPageToken()
		if pageToken == "" {
			return out, nil
		}
	}
	return nil, status.Error(codes.Unavailable, "geo zone listing unavailable")
}
