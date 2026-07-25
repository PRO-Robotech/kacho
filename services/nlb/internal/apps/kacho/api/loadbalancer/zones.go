// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// checkDisabledAnnounceZones — общая валидация drain-набора (Create + Update):
// REGIONAL-only; каждая зона ∈ регион; набор не покрывает все зоны региона.
//
// Пустой drain-набор проверять нечем — geo не нужен (ранний выход ниже). Но
// если набор ЗАДАН, а geo-клиент не сконфигурирован, проверка «зона ∈ регион» и
// «набор не покрывает все зоны» невыполнима → мутация fail-closed
// `Unavailable`, а не «пропустить geo-проверки» (несконфигурированное ребро —
// неверная конфигурация стенда, не режим работы; boot-guard config.Validate
// ловит её на старте в production).
func checkDisabledAnnounceZones(ctx context.Context, zc ZoneClient, placement domain.PlacementType, regionID string, zones []string) error {
	if len(zones) == 0 {
		return nil
	}
	if placement != domain.PlacementRegional {
		return status.Error(codes.InvalidArgument,
			"disabled_announce_zones is only valid for REGIONAL load balancer")
	}
	if zc == nil {
		return status.Error(codes.Unavailable, "zone lookup unavailable")
	}
	regionZones, err := zc.ListZoneIDsInRegion(ctx, regionID)
	if err != nil {
		return zonePeerErr(err)
	}
	inRegion := make(map[string]struct{}, len(regionZones))
	for _, z := range regionZones {
		inRegion[z] = struct{}{}
	}
	drained := make(map[string]struct{}, len(zones))
	for _, z := range zones {
		if _, ok := inRegion[z]; !ok {
			return status.Errorf(codes.InvalidArgument,
				"zone %s is not in region %s", z, regionID)
		}
		drained[z] = struct{}{}
	}
	// Набор не должен покрывать ВСЕ зоны региона (VIP стал бы недостижим).
	if len(regionZones) > 0 && len(drained) >= len(inRegion) {
		return status.Error(codes.InvalidArgument,
			"disabled_announce_zones must not cover all zones of the region")
	}
	return nil
}

// normalizeZones — dedup + стабильный порядок набора зон (для DB-записи и Equal).
func normalizeZones(zones []string) []string {
	if len(zones) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(zones))
	out := make([]string, 0, len(zones))
	for _, z := range zones {
		if z == "" {
			continue
		}
		if _, ok := seen[z]; ok {
			continue
		}
		seen[z] = struct{}{}
		out = append(out, z)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
