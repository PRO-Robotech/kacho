// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package protoconv — ЕДИНЫЙ источник конверсии domain→proto для kacho-geo.
// Two-projection: публичные Region/Zone (LEAN — id/name/derived-signals, БЕЗ
// сырого status и infra°) vs Internal{Region,Zone} (FULL — status + infra°,
// только :9091). Публичная проекция используется и тонким handler (Get/List), и
// use-case marshaller (Operation.response). Централизация убирает риск дрейфа:
// новое поле добавляется в ОДНОМ месте, и сырой admin-флаг/инфра физически не
// попадают в public-message (two-projection через РАЗНЫЕ messages — security.md).
package protoconv

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
)

// Region конвертирует domain.Region → public geov1.Region (LEAN). НЕ несёт
// status/infra° (two-projection). openForPlacement°/openZoneCountHint° — derived.
func Region(r *domain.Region) *geov1.Region {
	return &geov1.Region{
		Id:                r.ID,
		Name:              r.Name,
		CountryCode:       r.CountryCode,
		OpenForPlacement:  r.OpenForPlacement(),
		OpenZoneCountHint: r.OpenZoneCount,
		CreatedAt:         ts(r.CreatedAt),
	}
}

// Zone конвертирует domain.Zone → public geov1.Zone (LEAN). НЕ несёт status/infra°.
// openForPlacement°/placementBlockedReason° — derived (учитывают status родит-региона).
func Zone(z *domain.Zone) *geov1.Zone {
	return &geov1.Zone{
		Id:                     z.ID,
		RegionId:               z.RegionID,
		Name:                   z.Name,
		OpenForPlacement:       z.OpenForPlacement(),
		PlacementBlockedReason: geov1.PlacementBlockedReason(z.PlacementBlockedReason()),
		CreatedAt:              ts(z.CreatedAt),
	}
}

// InternalRegion конвертирует domain.Region → FULL geov1.InternalRegion (status +
// infra°). Только :9091 (GetInternal) — НИКОГДА на public.
func InternalRegion(r *domain.Region) *geov1.InternalRegion {
	return &geov1.InternalRegion{
		Id:          r.ID,
		Name:        r.Name,
		CountryCode: r.CountryCode,
		Status:      geov1.GeoStatus(r.Status),
		Infra: &geov1.RegionInfra{
			NumericInfraId: r.Infra.NumericInfraID,
			CapacityHint:   r.Infra.CapacityHint,
		},
		CreatedAt: ts(r.CreatedAt),
	}
}

// InternalZone конвертирует domain.Zone → FULL geov1.InternalZone (status +
// infra°). Только :9091 (GetInternal) — НИКОГДА на public.
func InternalZone(z *domain.Zone) *geov1.InternalZone {
	return &geov1.InternalZone{
		Id:       z.ID,
		RegionId: z.RegionID,
		Name:     z.Name,
		Status:   geov1.GeoStatus(z.Status),
		Infra: &geov1.ZoneInfra{
			NumericInfraId:     z.Infra.NumericInfraID,
			HostClasses:        z.Infra.HostClasses,
			FailureDomainCount: z.Infra.FailureDomainCount,
			UnderlayAnchor:     z.Infra.UnderlayAnchor,
			CapacityHint:       z.Infra.CapacityHint,
		},
		CreatedAt: ts(z.CreatedAt),
	}
}

// ts — единый timestamp-формат Kachō: усечение до секунд перед проекцией в proto.
func ts(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t.Truncate(time.Second))
}
