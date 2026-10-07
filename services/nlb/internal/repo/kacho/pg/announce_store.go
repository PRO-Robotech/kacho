// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// AnnounceStore — pgxpool-хранилище наблюдаемой announce-state anycast-VIP
// (Internal-проекция :9091). Standalone-store вне CQRS Reader/Writer: announce-
// state — не tenant-ресурс с outbox/LRO, а data-plane feedback, поэтому upsert
// идёт одним автокоммитным оператором без outbox-эмита.
//
// Единственный writer — data plane (ReportAnnounceState). Запись идемпотентна
// (ON CONFLICT по (load_balancer_id, zone_id, ip_version)).
//
// Таблица announce-state журналируемой не является: строки журнала подписки
// модуля эта запись не порождает, поэтому помощник записи журнала
// (`journaltx.Begin`) и принципал контекста ей не нужны. Запись не зависит от
// личности вызывающего так же, как до введения инициатора журнала (NTF-3, З4):
// плоскость данных принципала не пересылает.
type AnnounceStore struct {
	pool *pgxpool.Pool
}

// NewAnnounceStore — конструктор. pool создаётся в composition root.
func NewAnnounceStore(pool *pgxpool.Pool) *AnnounceStore {
	return &AnnounceStore{pool: pool}
}

// reportZonesSQL — upsert всего набора зон одним оператором: набор либо
// применяется целиком, либо не применяется вовсе (атомарность оператора, а не
// транзакции из нескольких операторов).
const reportZonesSQL = `
		INSERT INTO kacho_nlb.load_balancer_announce_state
			(load_balancer_id, zone_id, ip_version, bgp_session_up,
			 route_id, vrf_id, kernel_programmed, infra_id, updated_at)
		SELECT $1, z.zone_id, z.ip_version, z.bgp_session_up,
		       z.route_id, z.vrf_id, z.kernel_programmed, z.infra_id, now()
		  FROM unnest($2::text[], $3::text[], $4::boolean[],
		              $5::text[], $6::text[], $7::boolean[], $8::bigint[])
		       AS z(zone_id, ip_version, bgp_session_up,
		            route_id, vrf_id, kernel_programmed, infra_id)
		ON CONFLICT (load_balancer_id, zone_id, ip_version) DO UPDATE SET
			bgp_session_up    = EXCLUDED.bgp_session_up,
			route_id          = EXCLUDED.route_id,
			vrf_id            = EXCLUDED.vrf_id,
			kernel_programmed = EXCLUDED.kernel_programmed,
			infra_id          = EXCLUDED.infra_id,
			updated_at        = now()`

// ReportZones идемпотентно upsert'ит набор per-zone announce-state одного LB
// одним оператором. Пустой набор → no-op (idempotent). FK-violation (LB
// отсутствует) → ErrFailedPrecondition; остальные SQLSTATE сводятся через
// mapPgErr.
//
// Повтор ключа (zone_id, ip_version) в одном наборе сводится к ПОСЛЕДНЕМУ его
// вхождению до оператора: `ON CONFLICT DO UPDATE` не может затронуть строку
// дважды в одном операторе, а последнее вхождение — то, что набор сообщает о
// зоне окончательно (так же применялся набор построчными upsert'ами).
func (s *AnnounceStore) ReportZones(ctx context.Context, lbID string, zones []domain.AnnounceZone) error {
	if len(zones) == 0 {
		return nil
	}
	zones = lastPerZoneKey(zones)
	var (
		zoneIDs    = make([]string, len(zones))
		versions   = make([]string, len(zones))
		bgpUp      = make([]bool, len(zones))
		routeIDs   = make([]string, len(zones))
		vrfIDs     = make([]string, len(zones))
		kernelProg = make([]bool, len(zones))
		infraIDs   = make([]int64, len(zones))
	)
	for i, z := range zones {
		zoneIDs[i], versions[i], bgpUp[i] = z.ZoneID, string(z.IPVersion), z.BGPSessionUp
		routeIDs[i], vrfIDs[i], kernelProg[i], infraIDs[i] = z.RouteID, z.VrfID, z.KernelProgrammed, z.InfraID
	}
	if _, err := s.pool.Exec(ctx, reportZonesSQL,
		lbID, zoneIDs, versions, bgpUp, routeIDs, vrfIDs, kernelProg, infraIDs,
	); err != nil {
		return mapPgErr(err, "load balancer", lbID)
	}
	return nil
}

// lastPerZoneKey оставляет по одному вхождению на ключ (zone_id, ip_version) —
// последнее, — сохраняя порядок первых вхождений.
func lastPerZoneKey(zones []domain.AnnounceZone) []domain.AnnounceZone {
	type key struct {
		zone    string
		version domain.IPVersion
	}
	at := make(map[key]int, len(zones))
	out := make([]domain.AnnounceZone, 0, len(zones))
	for _, z := range zones {
		k := key{zone: z.ZoneID, version: z.IPVersion}
		if i, seen := at[k]; seen {
			out[i] = z
			continue
		}
		at[k] = len(out)
		out = append(out, z)
	}
	return out
}

// LoadState читает наблюдаемую announce-state одного LB: tenant-facing VIP (из
// load_balancers) + per-zone announce-rows. found=false → LB отсутствует (→
// NotFound в use-case). ObservedAt = max(updated_at) по зонам.
func (s *AnnounceStore) LoadState(ctx context.Context, lbID string) (*kacho.AnnounceStateRecord, bool, error) {
	rec := &kacho.AnnounceStateRecord{LoadBalancerID: lbID}

	err := s.pool.QueryRow(ctx,
		`SELECT address_v4, address_v6 FROM kacho_nlb.load_balancers WHERE id = $1`, lbID).
		Scan(&rec.AddressV4, &rec.AddressV6)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, mapPgErr(err, "load balancer", lbID)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT zone_id, ip_version, bgp_session_up, route_id, vrf_id,
		       kernel_programmed, infra_id, updated_at
		  FROM kacho_nlb.load_balancer_announce_state
		 WHERE load_balancer_id = $1
		 ORDER BY zone_id, ip_version`, lbID)
	if err != nil {
		return nil, false, mapPgErr(err, "AnnounceState", lbID)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			z   kacho.AnnounceZoneRecord
			ipv string
		)
		if err := rows.Scan(&z.ZoneID, &ipv, &z.BGPSessionUp, &z.RouteID,
			&z.VrfID, &z.KernelProgrammed, &z.InfraID, &z.UpdatedAt); err != nil {
			return nil, false, mapPgErr(err, "AnnounceState", lbID)
		}
		z.IPVersion = domain.IPVersion(ipv)
		if z.UpdatedAt.After(rec.ObservedAt) {
			rec.ObservedAt = z.UpdatedAt
		}
		rec.Zones = append(rec.Zones, z)
	}
	if err := rows.Err(); err != nil {
		return nil, false, mapPgErr(err, "AnnounceState", lbID)
	}
	return rec, true, nil
}
