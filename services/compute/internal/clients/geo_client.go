// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/peer"
	"github.com/PRO-Robotech/kacho/pkg/retry"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// defaultZoneCallTimeout — per-call deadline applied to EVERY geo.v1.ZoneService.Get
// attempt (audit-r6 P1). retry.OnUnavailable only classifies codes.Unavailable —
// an app-slow (not down) geo peer that never responds has no bound of its own and
// would hang the caller for the lifetime of the inbound ctx (LRO worker opTimeout,
// ~4min), exhausting worker slots under load. No configurable knob exists for this
// edge yet (unlike authzfilter.Config.Timeout) — package default const, per
// architecture.md's documented fallback.
const defaultZoneCallTimeout = 5 * time.Second

// GeoClient реализует ports.ZoneRegistry через gRPC к kacho-geo
// (geo.v1.ZoneService.Get) — источник existence-check для Instance.zone_id.
// Geography (Region/Zone) — домен leaf-сервиса kacho-geo; compute зонами не
// владеет и их не обслуживает, лишь валидирует свой zone_id как consumer.
//
// Контракт ZoneRegistry (см. mapZoneRefErr): geo NOT_FOUND → ports.ErrNotFound
// (→ InvalidArgument "Zone <id> not found", fail-closed на мутации Instance);
// транспортная ошибка (geo недоступен) → проброс gRPC Unavailable (→ Unavailable
// "zone check: ...", fail-closed для мутаций).
//
// W1.4-паритет с iam/vpc-клиентами: outgoing ctx обёрнут auth.PropagateOutgoing,
// чтобы geo-side authz-Check (security.md: per-RPC Check на каждом RPC) видел
// реального caller'а; retry.OnUnavailable сглаживает транзиентные обрывы.
type GeoClient struct {
	zones geov1.ZoneServiceClient
	// regions — тот же владелец Geography, другой его сервис. Регион нужен
	// группе размещения с РЕГИОНАЛЬНЫМ якорем: у неё зоны нет by construction,
	// и проверить её координату зональным вопросом нельзя.
	regions geov1.RegionServiceClient
	// timeout — per-call deadline applied to every Get attempt (defaultZoneCallTimeout
	// unless overridden, e.g. by a test seam via direct field assignment).
	timeout time.Duration
}

// NewGeoClient создаёт GeoClient поверх gRPC-conn к kacho-geo (:9090,
// public ZoneService.Get).
func NewGeoClient(conn *grpc.ClientConn) *GeoClient {
	return &GeoClient{
		zones:   geov1.NewZoneServiceClient(conn),
		regions: geov1.NewRegionServiceClient(conn),
		timeout: defaultZoneCallTimeout,
	}
}

// NewGeoClientWith создаёт GeoClient поверх готового geov1.ZoneServiceClient
// (seam для unit-тестов с fake-клиентом).
func NewGeoClientWith(zones geov1.ZoneServiceClient) *GeoClient {
	return &GeoClient{zones: zones, timeout: defaultZoneCallTimeout}
}

// NewGeoClientWithBoth — шов для проб, которым нужны обе половины владельца.
func NewGeoClientWithBoth(zones geov1.ZoneServiceClient, regions geov1.RegionServiceClient) *GeoClient {
	return &GeoClient{zones: zones, regions: regions, timeout: defaultZoneCallTimeout}
}

// GetRegion валидирует существование region_id через geo.v1.RegionService.Get.
//
// Полосы отказа — те же, что у зоны, и по той же причине: отказ владельца в
// правах и негодная по его мнению ссылка обязаны приезжать терминальным
// отказом, а не «повтори позже». Недоступность владельца — fail-closed на
// мутации: регион, существование которого мы не подтвердили, принять нельзя.
func (c *GeoClient) GetRegion(ctx context.Context, regionID string) error {
	return retry.OnUnavailable(ctx, func(ctx context.Context) error {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		_, rerr := c.regions.Get(auth.PropagateOutgoing(callCtx), &geov1.GetRegionRequest{RegionId: regionID})
		if rerr != nil {
			if peer.Classify(rerr).RefusedReference() {
				return ports.ErrNotFound
			}
			return rerr
		}
		return nil
	})
}

// GetZone валидирует существование zone_id через geo.v1.ZoneService.Get.
// Найдено → nil. geo NOT_FOUND → ports.ErrNotFound (mapZoneRefErr → InvalidArgument).
// geo недоступен (после retry) → проброс gRPC Unavailable (mapZoneRefErr → Unavailable).
//
// Каждая попытка (в т.ч. каждый retry.OnUnavailable-повтор) несёт собственный
// context.WithTimeout(c.timeout) — app-slow geo (peer жив, но не отвечает) бьётся
// per-call deadline'ом, а не висит до inbound ctx (worker opTimeout), см. audit-r6 P1.
func (c *GeoClient) GetZone(ctx context.Context, zoneID string) error {
	return retry.OnUnavailable(ctx, func(ctx context.Context) error {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		_, rerr := c.zones.Get(auth.PropagateOutgoing(callCtx), &geov1.GetZoneRequest{ZoneId: zoneID})
		if rerr != nil {
			// Полосу выбирает носитель (pkg/peer). Прежде распознавался ОДИН код —
			// NotFound; отказ владельца в правах и негодная по его мнению ссылка
			// уходили наружу сырым ответом соседа и приезжали арендатору
			// недоступностью, то есть «повтори позже» на терминальный отказ.
			if peer.Classify(rerr).RefusedReference() {
				return ports.ErrNotFound
			}
			return rerr
		}
		return nil
	})
}

// RegionOfZone возвращает region_id зоны через geo.v1.ZoneService.Get
// (`Zone.region_id`). Регион НИКОГДА не выводится из имени зоны — имена региона и
// зоны произвольны, единственный авторитет — владелец Geography (kacho-geo).
// Ошибки — как у GetZone: NOT_FOUND → ports.ErrNotFound; недоступность geo →
// проброс gRPC Unavailable (fail-closed на мутации). Пустой ответ трактуется как
// неразрешимый (не «регион не важен»). Per-call deadline на каждой попытке.
func (c *GeoClient) RegionOfZone(ctx context.Context, zoneID string) (string, error) {
	var region string
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		resp, rerr := c.zones.Get(auth.PropagateOutgoing(callCtx), &geov1.GetZoneRequest{ZoneId: zoneID})
		if rerr != nil {
			// Полосу выбирает носитель (pkg/peer). Прежде распознавался ОДИН код —
			// NotFound; отказ владельца в правах и негодная по его мнению ссылка
			// уходили наружу сырым ответом соседа и приезжали арендатору
			// недоступностью, то есть «повтори позже» на терминальный отказ.
			if peer.Classify(rerr).RefusedReference() {
				return ports.ErrNotFound
			}
			return rerr
		}
		region = resp.GetRegionId()
		return nil
	})
	if err != nil {
		return "", err
	}
	if region == "" {
		return "", status.Error(codes.Unavailable, "zone region unresolved")
	}
	return region, nil
}

// NoopGeoClient — заглушка для KACHO_COMPUTE_SKIP_PEER_VALIDATION=true (zone
// existence-check отключён → любая зона «существует») и для unit/newman без
// поднятого kacho-geo. GetZone всегда успешен (любая зона существует).
type NoopGeoClient struct{}

// GetZone всегда возвращает nil без обращения к geo.
func (NoopGeoClient) GetZone(_ context.Context, _ string) error {
	return nil
}

// RegionOfZone без geo резолвить нечего: регион зоны из её имени НЕ выводится,
// поэтому заглушка отвечает fail-closed Unavailable (а не «подходит любой»).
func (NoopGeoClient) RegionOfZone(_ context.Context, _ string) (string, error) {
	return "", status.Error(codes.Unavailable, "geo zone→region validation disabled")
}

// ensure compile-time: both impls satisfy the use-case port.
var (
	_ ports.ZoneRegistry = (*GeoClient)(nil)
	_ ports.ZoneRegistry = NoopGeoClient{}
)
