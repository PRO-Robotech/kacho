// Geography (Region / Zone) — the placement axis every placeable resource is
// pinned to. Owned by kacho-geo, the leaf platform-topology service.
//
// The console speaks to the PUBLIC surface only (kacho#3094):
//
//   GET    /geo/v1/regions[/{id}]        RegionService.Get/List
//   POST   /geo/v1/regions               RegionService.Create
//   PATCH  /geo/v1/regions/{id}          RegionService.Update   (countryCode, status)
//   DELETE /geo/v1/regions/{id}          RegionService.Delete
//   …and the same five verbs on /geo/v1/zones (ZoneService; Update — status only).
//
// Reads are the ambient catalog every authenticated tenant reads. Mutations are
// the administrator's verbs: `system_admin` on the cluster, the same permission
// and the same assurance floor as their InternalRegionService/InternalZoneService
// twins (#3092). The infra° block is NOT part of the public input or output —
// assigning it stays an act of the cluster-internal plane, and the console does
// not offer it: a field the edge drops in silence would be a field that lies.
//
// The public projection carries no raw admin `status`; the edit form derives it
// from openForPlacement° and placementBlockedReason (`geoStatusOfRegion` /
// `geoStatusOfZone` below), and leaves it unsaid where the projection does not
// determine it.
//
// Field names below are snake_case because api/client.ts converts the wire
// camelCase on the way in (and back on the way out).

import type { Operation } from "./types";

// ── Paths ────────────────────────────────────────────────────────────────────

export const GEO_REGIONS_PATH = "/geo/v1/regions";
export const GEO_ZONES_PATH = "/geo/v1/zones";

// ── Enums ────────────────────────────────────────────────────────────────────

/** Raw admin maintenance flag — an input of Create/Update; not on the public read. */
export type GeoStatus = "GEO_STATUS_UNSPECIFIED" | "UP" | "DOWN";

/**
 * Why a zone's derived openForPlacement° is false, resolved by the service in one
 * call. Emitted on the public Zone only — on a Region it would collapse into a
 * bijection of openForPlacement°.
 */
export type PlacementBlockedReason = "PLACEMENT_BLOCKED_REASON_UNSPECIFIED" | "NONE" | "ZONE_DOWN" | "REGION_DOWN";

// ── Public projections ───────────────────────────────────────────────────────

export interface Region {
  /**
   * Admin-assigned immutable slug PK, e.g. "region-1".
   *
   * This is the ONLY identity a Region has. There is no separate display name:
   * the id is written by hand by the cloud administrator and is human-readable
   * by construction, so a second label carried nothing the id did not, and gave
   * the two spellings somewhere to drift apart (#716).
   */
  id: string;
  created_at?: string;
  /** ISO-3166 alpha-2, optional. */
  country_code?: string;
  /** Derived (= status==UP). Administrative availability, not a capacity promise. */
  open_for_placement?: boolean;
  /** Advisory read-time rollup; int64, so it arrives as a JSON string. */
  open_zone_count_hint?: string | number;
}

export interface Zone {
  /** Admin-assigned immutable slug PK; coupled as regionId + "-" + suffix.
   *  The only identity a Zone has — see Region above (#716). */
  id: string;
  region_id: string;
  created_at?: string;
  /** Derived (= zone.status==UP && region.status==UP). */
  open_for_placement?: boolean;
  placement_blocked_reason?: PlacementBlockedReason;
}

// ── List envelopes ───────────────────────────────────────────────────────────

export interface ListRegionsResponse {
  regions?: Region[];
  next_page_token?: string;
}

export interface ListZonesResponse {
  zones?: Zone[];
  next_page_token?: string;
}

/** Every geo mutation answers with an Operation (done=true immediately). */
export type GeoMutationResponse = Partial<Operation> | { operation?: Operation };

// ── Pure helpers ─────────────────────────────────────────────────────────────

/**
 * Does this zone id belong to this region id, per the coupling zone.proto
 * declares — `id == regionId + "-" + <zoneSuffix>`, strict startsWith?
 *
 * This is a pre-submit check for the admin form, where the operator supplies
 * BOTH ids by hand; the service re-checks it and stays authoritative. It is not,
 * and must not become, a way to work out which region a zone is in: that answer
 * only ever comes from resolving the zone at its owner.
 *
 * The strictness matters — "region-10-a" is not a zone of "region-1", because
 * the character after the prefix is '0', not '-'. A bare separator with no
 * suffix ("region-1-") is rejected too: it is not a valid slug.
 */
export function zoneBelongsToRegion(zoneId: string, regionId: string): boolean {
  if (!zoneId || !regionId) return false;
  const prefix = `${regionId}-`;
  return zoneId.startsWith(prefix) && zoneId.length > prefix.length;
}

const BLOCKED_REASON_TEXT: Record<string, string> = {
  ZONE_DOWN: "Зона выведена из обслуживания администратором",
  REGION_DOWN: "Регион выведен из обслуживания администратором",
};

/**
 * Human-readable cause behind a closed zone, or null when there is nothing to
 * say — including for a reason this build does not know, which is left unsaid
 * rather than guessed.
 */
export function placementBlockedText(reason: PlacementBlockedReason | undefined): string | null {
  if (!reason) return null;
  return BLOCKED_REASON_TEXT[reason] ?? null;
}

export interface PlacementLabel {
  text: string;
  tone: "open" | "closed" | "unknown";
}

/**
 * How to render openForPlacement°. An absent field is "unknown", never "closed":
 * the difference is between a server that said no and a server that said nothing.
 */
export function openForPlacementLabel(open: boolean | undefined): PlacementLabel {
  if (open === true) return { text: "Открыт", tone: "open" };
  if (open === false) return { text: "Закрыт", tone: "closed" };
  return { text: "—", tone: "unknown" };
}

/**
 * Read an int64 count that arrives as a JSON string. Anything that is not a
 * non-negative integer comes back as null — a silent 0 would read as "no open
 * zones", which is a different statement from "no hint was sent".
 */
export function readCountHint(value: unknown): number | null {
  if (typeof value === "number") {
    return Number.isInteger(value) && value >= 0 ? value : null;
  }
  if (typeof value !== "string" || value.trim() === "") return null;
  if (!/^\d+$/.test(value.trim())) return null;
  const n = Number(value.trim());
  return Number.isSafeInteger(n) ? n : null;
}

/**
 * The raw admin status a public Region implies: openForPlacement° = status==UP.
 * The public mux emits unpopulated fields, so `false` arrives as `false`; an
 * absent field is not a status and comes back `undefined`, never a guessed DOWN.
 */
export function geoStatusOfRegion(region: Pick<Region, "open_for_placement">): GeoStatus | undefined {
  if (region.open_for_placement === true) return "UP";
  if (region.open_for_placement === false) return "DOWN";
  return undefined;
}

/**
 * The raw admin status a public Zone implies. openForPlacement° = zone UP &&
 * region UP, and placementBlockedReason has zone precedence (zone DOWN ⇒
 * ZONE_DOWN; else region DOWN ⇒ REGION_DOWN), so the zone's own status is fully
 * determined: open ⇒ UP; ZONE_DOWN ⇒ DOWN; REGION_DOWN ⇒ UP. Anything else
 * (no reason on a closed zone, an unknown reason) is left unsaid.
 */
export function geoStatusOfZone(zone: Pick<Zone, "open_for_placement" | "placement_blocked_reason">): GeoStatus | undefined {
  if (zone.open_for_placement === true) return "UP";
  if (zone.open_for_placement !== false) return undefined;
  if (zone.placement_blocked_reason === "ZONE_DOWN") return "DOWN";
  if (zone.placement_blocked_reason === "REGION_DOWN") return "UP";
  return undefined;
}
