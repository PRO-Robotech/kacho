// Contract lock for the geo (Region/Zone) client surface.
//
// Ground truth: proto/kacho/cloud/geo/v1/{region,zone,geo_common}.proto plus the
// public RegionService/ZoneService on top of them — reads AND the administrator's
// Create/Update/Delete under /geo/v1/{regions,zones} (#3092). The console speaks
// to nothing else (#3094): the raw `status` and the infra° block are not on the
// public read, and `status` is derived from the projection.

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import {
  GEO_REGIONS_PATH,
  GEO_ZONES_PATH,
  geoStatusOfRegion,
  geoStatusOfZone,
  openForPlacementLabel,
  placementBlockedText,
  readCountHint,
  zoneBelongsToRegion,
} from "./geo";

describe("geo REST paths", () => {
  it("keeps reads and mutations on the public surface", () => {
    expect(GEO_REGIONS_PATH).toBe("/geo/v1/regions");
    expect(GEO_ZONES_PATH).toBe("/geo/v1/zones");
  });

  it.each(["geo.ts", "cluster.ts"])("%s names no internal path — the external edge does not serve one (#3094)", (file) => {
    // The issue's predicate verbatim: `grep -c '/internal/'` over the two clients is 0.
    const text = readFileSync(fileURLToPath(new URL(`./${file}`, import.meta.url)), "utf8");
    expect(text.length).toBeGreaterThan(0);
    expect(text.split("\n").filter((l) => l.includes("/internal/"))).toEqual([]);
  });
});

describe("raw status derived from the public projection", () => {
  it("region: openForPlacement° is status==UP; absent says nothing", () => {
    expect(geoStatusOfRegion({ open_for_placement: true })).toBe("UP");
    expect(geoStatusOfRegion({ open_for_placement: false })).toBe("DOWN");
    expect(geoStatusOfRegion({})).toBeUndefined();
  });

  it("zone: zone precedence of the blocked reason determines the zone's own status", () => {
    expect(geoStatusOfZone({ open_for_placement: true })).toBe("UP");
    expect(geoStatusOfZone({ open_for_placement: false, placement_blocked_reason: "ZONE_DOWN" })).toBe("DOWN");
    expect(geoStatusOfZone({ open_for_placement: false, placement_blocked_reason: "REGION_DOWN" })).toBe("UP");
    expect(geoStatusOfZone({ open_for_placement: false, placement_blocked_reason: "NONE" })).toBeUndefined();
    expect(geoStatusOfZone({})).toBeUndefined();
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
