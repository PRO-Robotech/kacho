// Geography (Region / Zone) — the placement axis every placeable resource is
// pinned to. Owned by kacho-geo, the leaf platform-topology service.
//
// Two surfaces, and they are not interchangeable:
//
//   public   GET /geo/v1/regions[/{id}]      RegionService  — read-only
//            GET /geo/v1/zones[/{id}]        ZoneService    — read-only
//   internal POST|PATCH|DELETE|GET
//            /geo/v1/internal/{regions,zones}[/{id}]        — admin CRUD +
//                                                             GetInternal
//
// The public read is project-scope EXEMPT (authN-only): every authenticated
// tenant has to read the catalog to launch anything placeable. The admin CRUD
// requires system_admin on the cluster and is routed exclusively through the
// cluster-internal REST listener — a POST/PATCH/DELETE on the public path is not
// routed at all.
//
// Two projections: the public Region/Zone carry only tenant-facing intent plus
// the derived openForPlacement°; the raw admin `status` and the entire infra°
// block exist on InternalRegion/InternalZone alone.
//
// Field names below are snake_case because api/client.ts converts the wire
// camelCase on the way in (and back on the way out).

import type { Operation } from "./types";

// ── Paths ────────────────────────────────────────────────────────────────────

export const GEO_REGIONS_PATH = "/geo/v1/regions";
export const GEO_ZONES_PATH = "/geo/v1/zones";
export const GEO_INTERNAL_REGIONS_PATH = "/geo/v1/internal/regions";
export const GEO_INTERNAL_ZONES_PATH = "/geo/v1/internal/zones";

// ── Enums ────────────────────────────────────────────────────────────────────

/** Raw admin maintenance flag. Lives on the Internal projection only. */
export type GeoStatus = "GEO_STATUS_UNSPECIFIED" | "UP" | "DOWN";

/**
 * Why a zone's derived openForPlacement° is false, resolved by the service in one
 * call. Emitted on the public Zone only — on a Region it would collapse into a
 * bijection of openForPlacement°.
 */
export type PlacementBlockedReason = "PLACEMENT_BLOCKED_REASON_UNSPECIFIED" | "NONE" | "ZONE_DOWN" | "REGION_DOWN";

// ── Public projections ───────────────────────────────────────────────────────

export interface Region {
  /** Admin-assigned immutable slug PK, e.g. "region-1". */
  id: string;
  name: string;
  created_at?: string;
  /** ISO-3166 alpha-2, optional. */
  country_code?: string;
  /** Derived (= status==UP). Administrative availability, not a capacity promise. */
  open_for_placement?: boolean;
  /** Advisory read-time rollup; int64, so it arrives as a JSON string. */
  open_zone_count_hint?: string | number;
}

export interface Zone {
  /** Admin-assigned immutable slug PK; coupled as regionId + "-" + suffix. */
  id: string;
  region_id: string;
  name: string;
  created_at?: string;
  /** Derived (= zone.status==UP && region.status==UP). */
  open_for_placement?: boolean;
  placement_blocked_reason?: PlacementBlockedReason;
}

// ── Internal projections (never on the public surface) ───────────────────────

export interface RegionInfra {
  /** int64 on the wire. Immutable after create. */
  numeric_infra_id?: string;
  /** Read-time rollup of the zone capacity hints; not persisted, not settable. */
  capacity_hint?: string;
}

export interface ZoneInfra {
  /** int64 on the wire. Immutable after create. */
  numeric_infra_id?: string;
  host_classes?: string[];
  failure_domain_count?: number;
  underlay_anchor?: string;
  /** AMPLE | CONSTRAINED | FULL. */
  capacity_hint?: string;
}

export interface InternalRegion {
  id: string;
  name: string;
  created_at?: string;
  country_code?: string;
  status?: GeoStatus;
  infra?: RegionInfra;
}

export interface InternalZone {
  id: string;
  region_id: string;
  name: string;
  created_at?: string;
  status?: GeoStatus;
  infra?: ZoneInfra;
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
