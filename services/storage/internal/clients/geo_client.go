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
	"time"

	"google.golang.org/grpc"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/peer"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
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
// request-path Create. Несуществующая/невалидная зона → полоса peer-validate:
// FailedPrecondition "unknown zone id '<X>'" + PEER_RESOURCE_MISSING (одна полоса
// с vpc/compute/nlb/registry на этом ребре). Peer недоступен → Unavailable
// (fail-closed для мутации). Identity вызывающего форвардится (auth.PropagateOutgoing).
// RegionOfZone возвращает region_id зоны (geo.v1.ZoneService.Get →
// `Zone.region_id`). Регион НИКОГДА не выводится из имени зоны — авторитет один,
// владелец Geography. Ошибки зеркалят EnsureZoneExists: неизвестная зона →
// полоса промаха peer-валидации; недоступность geo / пустой region_id →
// Unavailable (fail-closed на мутации — непроверяемое предусловие не считается
// выполненным).
func (c *GeoClient) RegionOfZone(ctx context.Context, zoneID string) (string, error) {
	if c.cli == nil {
		return "", zoneLane(peer.OutcomeUnavailable, zoneID, "storage→geo ZoneService not configured")
	}
	cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()
	resp, err := c.cli.Get(auth.PropagateOutgoing(cctx), &geov1.GetZoneRequest{ZoneId: zoneID})
	if err != nil {
		return "", zoneLane(peer.Classify(err), zoneID, zoneUnavailableText)
	}
	if resp.GetRegionId() == "" {
		return "", zoneLane(peer.OutcomeUnavailable, zoneID, zoneUnavailableText)
	}
	return resp.GetRegionId(), nil
}

func (c *GeoClient) EnsureZoneExists(ctx context.Context, zoneID string) error {
	if c.cli == nil {
		return zoneLane(peer.OutcomeUnavailable, zoneID, "storage→geo ZoneService not configured")
	}
	cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()
	if _, err := c.cli.Get(auth.PropagateOutgoing(cctx), &geov1.GetZoneRequest{ZoneId: zoneID}); err != nil {
		return zoneLane(peer.Classify(err), zoneID, zoneUnavailableText)
	}
	return nil
}

// EnsureRegionExists валидирует region_id через kacho-geo (RegionService.Get) на
// request-path Image.Create (REGIONAL/anycast). Несуществующий/невалидный регион →
// полоса peer-validate: FailedPrecondition "unknown region id '<X>'" +
// PEER_RESOURCE_MISSING. Peer недоступен → Unavailable (fail-closed для мутации).
// Identity форвардится (auth.PropagateOutgoing).
func (c *GeoClient) EnsureRegionExists(ctx context.Context, regionID string) error {
	if c.regionCli == nil {
		return regionLane(peer.OutcomeUnavailable, regionID, "storage→geo RegionService not configured")
	}
	cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()
	if _, err := c.regionCli.Get(auth.PropagateOutgoing(cctx), &geov1.GetRegionRequest{RegionId: regionID}); err != nil {
		return regionLane(peer.Classify(err), regionID, regionUnavailableText)
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
		return nil, regionLane(peer.OutcomeUnavailable, regionID, "storage→geo ZoneService not configured")
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
			return nil, regionLane(peer.Classify(err), regionID, regionUnavailableText)
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
	return nil, regionLane(peer.OutcomeUnavailable, regionID, "geo zone listing unavailable")
}

// serviceDomain — источник отказа в ErrorInfo.domain. Назван один раз на сервис:
// повторённый по местам вызова литерал разъезжается молча, и разъедется он
// именно там, где деталь читают машиной, а не глазом.
const serviceDomain = "storage"

// Полосы ребра storage→geo — по одному helper'у на ресурс, и оба сводятся к
// носителю (pkg/peer). Прежде здесь стояли два конструктора и четыре рукописных
// `switch status.Code(err)`, разложивших коды соседа по полосам каждый по-своему:
// три места считали `PermissionDenied` недоступностью, то есть ВРЕМЕННОЙ полосой.
// Отказ в правах повтором не лечится — арендатор получал «повтори позже» на
// нашу собственную неверную настройку, а сама настройка выглядела перебоем у
// соседа.
//
// Тексты не меняются — они часть контракта. Возвращается СОБРАННЫЙ статус, а не
// sentinel: pass-through сервисного маппера (serviceerr.ToStatus) отдаёт его как
// есть, поэтому машинный признак переживает sentinel-слой.
const (
	zoneUnavailableText   = "geo zone validation unavailable"
	regionUnavailableText = "geo region validation unavailable"
)

// zoneLane / regionLane — полоса ответа о чужой зоне и чужом регионе.
//
// Проза промаха — FAILED_PRECONDITION «unknown … id»: чужой идентификатор
// корректен по форме, но у владельца Geography не резолвится, то есть не
// выполнено предусловие на ЧУЖОЙ ресурс (api-conventions.md §By-lane
// code-split). Текст недоступности передаётся вызывающим: различие между
// «не сконфигурировано», «не ответил» и «не уложился в бюджет страниц»
// осмысленно в журнале, а полоса у них одна.
func zoneLane(o peer.Outcome, zoneID, unavailable string) error {
	return o.Status(
		peer.Ref{Service: serviceDomain, ResourceType: "geo.zone", ResourceID: zoneID},
		peer.Prose{Missing: "unknown zone id '%s'", Unavailable: unavailable})
}

func regionLane(o peer.Outcome, regionID, unavailable string) error {
	return o.Status(
		peer.Ref{Service: serviceDomain, ResourceType: "geo.region", ResourceID: regionID},
		peer.Prose{Missing: "unknown region id '%s'", Unavailable: unavailable})
}
