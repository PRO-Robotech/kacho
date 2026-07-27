// Contract lock for the geo (Region/Zone) client surface.
//
// Ground truth: proto/kacho/cloud/geo/v1/{region,zone,geo_common}.proto plus the
// services on top of them — public read-only RegionService/ZoneService under
// /geo/v1/{regions,zones}, admin CRUD under /geo/v1/internal/… . The public
// projection is lean: the raw `status` and the whole infra° block live on the
// Internal* projection only (two-projection).

import {
  GEO_INTERNAL_REGIONS_PATH,
  GEO_INTERNAL_ZONES_PATH,
  GEO_REGIONS_PATH,
  GEO_ZONES_PATH,
  openForPlacementLabel,
  placementBlockedText,
  readCountHint,
  zoneBelongsToRegion,
} from "./geo";

describe("geo REST paths", () => {
  it("keeps reads on the public surface and mutations on the internal one", () => {
    // Reads are project-scope EXEMPT public RPCs; the gateway serves them on the
    // external listener.
    expect(GEO_REGIONS_PATH).toBe("/geo/v1/regions");
    expect(GEO_ZONES_PATH).toBe("/geo/v1/zones");
    // Admin CRUD lives on the self-describing /geo/v1/internal/… segment. A
    // POST/PATCH/DELETE on the public path is deliberately not routed at all.
    expect(GEO_INTERNAL_REGIONS_PATH).toBe("/geo/v1/internal/regions");
    expect(GEO_INTERNAL_ZONES_PATH).toBe("/geo/v1/internal/zones");
  });
});

describe("zoneBelongsToRegion", () => {
  // Validates the id coupling that zone.proto declares, for a form where the
  // operator types BOTH ids by hand (`id == regionId + "-" + <zoneSuffix>`,
  // strict startsWith). It never infers a region from a zone id — that inference
  // is banned, and nothing here reads a region out of a zone.
  it("accepts a suffix of the region id", () => {
    expect(zoneBelongsToRegion("ru-central1-a", "ru-central1")).toBe(true);
    expect(zoneBelongsToRegion("eu-north1-quux", "eu-north1")).toBe(true);
  });

  it("rejects a prefix not followed by a separator + suffix", () => {
    expect(zoneBelongsToRegion("ru-central1", "ru-central1")).toBe(false);
    expect(zoneBelongsToRegion("ru-central1-", "ru-central1")).toBe(false);
    expect(zoneBelongsToRegion("ru-central11-a", "ru-central1")).toBe(false);
  });

  it("rejects an unrelated region and treats missing input as not-coupled", () => {
    expect(zoneBelongsToRegion("ru-central1-a", "eu-north1")).toBe(false);
    expect(zoneBelongsToRegion("", "ru-central1")).toBe(false);
    expect(zoneBelongsToRegion("ru-central1-a", "")).toBe(false);
  });
});

describe("placementBlockedText", () => {
  it("explains a closed zone by its precedence-resolved cause", () => {
    expect(placementBlockedText("ZONE_DOWN")).toContain("Зона");
    expect(placementBlockedText("REGION_DOWN")).toContain("егион");
  });

  it("says nothing when there is nothing to explain", () => {
    expect(placementBlockedText("NONE")).toBeNull();
    expect(placementBlockedText("PLACEMENT_BLOCKED_REASON_UNSPECIFIED")).toBeNull();
    expect(placementBlockedText(undefined)).toBeNull();
  });

  it("does not invent an explanation for a reason it does not know", () => {
    // Forward-compat: a reason added later must not be rendered as one of the
    // known causes.
    expect(placementBlockedText("SOMETHING_NEW" as never)).toBeNull();
  });
});

describe("openForPlacementLabel", () => {
  it("distinguishes open, closed and unknown", () => {
    expect(openForPlacementLabel(true)).toEqual({ text: "Открыт", tone: "open" });
    expect(openForPlacementLabel(false)).toEqual({ text: "Закрыт", tone: "closed" });
    // An absent field must not be shown as "closed" — that would claim knowledge
    // the client does not have.
    expect(openForPlacementLabel(undefined)).toEqual({ text: "—", tone: "unknown" });
  });
});

describe("readCountHint", () => {
  it("reads an int64 that arrives as a JSON string", () => {
    expect(readCountHint("3")).toBe(3);
    expect(readCountHint("0")).toBe(0);
    expect(readCountHint(7)).toBe(7);
  });

  it("returns null rather than a silent zero for anything unreadable", () => {
    expect(readCountHint(undefined)).toBeNull();
    expect(readCountHint(null)).toBeNull();
    expect(readCountHint("")).toBeNull();
    expect(readCountHint("many")).toBeNull();
    expect(readCountHint(-1)).toBeNull();
    expect(readCountHint(1.5)).toBeNull();
  });
});
