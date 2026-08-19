// GEO-1 contract lock for the Region/Zone registry specs.
//
// Region/Zone is a two-projection resource: the public RegionService/ZoneService
// serve a lean tenant-facing read (id/name/countryCode/createdAt + the derived
// openForPlacement°), while the raw admin `status` and the whole infra° block
// exist only on the Internal* projection behind /geo/v1/internal/… . Admin CRUD
// lives there too — POST/PATCH/DELETE on the public path is not routed at all.

import { GEO_INTERNAL_REGIONS_PATH, GEO_INTERNAL_ZONES_PATH } from "@shared/api/geo";
import { editReadPath, mutationBasePath, REGISTRY, resourceProjectPath, type ResourceSpec } from "./resource-registry";

const columnPaths = (spec: ResourceSpec) => spec.columns.map((c) => c.path);
const fieldNames = (spec: ResourceSpec) => (spec.fields ?? []).map((f) => f.name);
const field = (spec: ResourceSpec, name: string) => (spec.fields ?? []).find((f) => f.name === name);
const asObj = (v: unknown) => v as Record<string, unknown>;

describe("geo registry — admin surface routing", () => {
  it.each(["regions", "zones"])("%s reads the public path and mutates the internal one", (id) => {
    const spec = REGISTRY[id];
    expect(spec.apiPath).toBe(`/geo/v1/${id}`);
    expect(spec.admin?.basePath).toBe(id === "regions" ? GEO_INTERNAL_REGIONS_PATH : GEO_INTERNAL_ZONES_PATH);
    expect(mutationBasePath(spec)).toBe(spec.admin!.basePath);
  });

  it("reads the edit form from the Internal projection — the mutable fields are not on the public one", () => {
    // `status` and infra° never appear on the public Region/Zone, so hydrating an
    // edit form from the public read would show the operator a blank status and
    // let them flip it blind.
    expect(REGISTRY.regions.admin?.readForEdit).toBe(true);
    expect(REGISTRY.zones.admin?.readForEdit).toBe(true);
    expect(editReadPath(REGISTRY.regions, "ru-central1")).toBe("/geo/v1/internal/regions/ru-central1");
    expect(editReadPath(REGISTRY.zones, "ru-central1-a")).toBe("/geo/v1/internal/zones/ru-central1-a");
  });

  it("keeps reading a resource without an admin surface on its own path", () => {
    expect(mutationBasePath(REGISTRY.networks)).toBe(REGISTRY.networks.apiPath);
    expect(editReadPath(REGISTRY.networks, "net-1")).toBe(`${REGISTRY.networks.apiPath}/net-1`);
  });

  it.each(["regions", "zones"])("%s declares that its mutations answer with an Operation", (id) => {
    // Catalog mutations complete synchronously but still return an Operation
    // (done=true immediately). A response without an operation id is therefore a
    // contract violation, not a success — the flag is what lets the pages say so.
    expect(REGISTRY[id].mutationsReturnOperation).toBe(true);
  });

  it.each(["regions", "zones"])("%s offers the Internal projection as a detail tab", (id) => {
    expect(REGISTRY[id].internalGetPath).toBe(`/geo/v1/internal/${id}/{id}`);
  });
});

describe("geo registry — two-projection", () => {
  // The sweep is taken from the API PATH, not from a list of spec ids. Naming the
  // ids is how this lock was lost once: `regions`/`zones` were migrated and
  // stayed green while `compute-regions`/`compute-zones` — different ids, same
  // `/geo/v1/…` reads — kept a "Статус" column bound to a field the public
  // projection does not carry. Two specs of one resource, and the assertion only
  // ever looked at one of them.
  const geoPublicSpecs = Object.entries(REGISTRY)
    .filter(([, spec]) => spec.apiPath === "/geo/v1/regions" || spec.apiPath === "/geo/v1/zones")
    .map(([id]) => id);

  it("sweeps every spec that reads the public geo catalogue", () => {
    // Guards the sweep against silently measuring nothing, and states its size:
    // four specs address the two public paths (regions/zones + the two Compute-
    // labelled mirrors the pickers reference).
    expect(geoPublicSpecs.length).toBeGreaterThanOrEqual(4);
  });

  it.each(geoPublicSpecs)("%s never puts infra° or the raw status on a public column", (id) => {
    const paths = columnPaths(REGISTRY[id]);
    expect(paths.filter((p) => p.startsWith("infra"))).toEqual([]);
    // The public projection has no `status` field at all — Zone reserves the name
    // outright (`reserved 3; reserved "status"` in geo/v1/zone.proto) and Region
    // never had one, so a column bound to it renders an empty cell forever.
    expect(paths).not.toContain("status");
  });

  it.each(geoPublicSpecs)("%s carries no display name — the id is the identity (#716)", (id) => {
    // Утверждение об ОТСУТСТВИИ, и оно нужно отдельно от перечня полей формы:
    // читающие спеки (`compute-regions`/`compute-zones`) формы не имеют вовсе,
    // а колонку, привязанную к снятому полю, рисовали бы вечным прочерком —
    // ровно то, что `ui.md` §Правило 9 запрещает («поле без источника не
    // показывается»).
    expect(columnPaths(REGISTRY[id])).not.toContain("name");
  });

  it.each(geoPublicSpecs)("%s surfaces the placement signal that replaced the status", (id) => {
    // Not the mirror image of the assertion above: dropping the dead column is
    // only half the fix. What the operator needs is the field the trunk actually
    // answers with — `open_for_placement` on both Region and Zone.
    expect(columnPaths(REGISTRY[id])).toContain("open_for_placement");
  });
});

describe("geo registry — Region", () => {
  const regions = REGISTRY.regions;

  it("surfaces the derived placement signal and the country descriptor", () => {
    const paths = columnPaths(regions);
    expect(paths).toContain("open_for_placement");
    expect(paths).toContain("country_code");
    expect(paths).toContain("open_zone_count_hint");
  });

  it("accepts exactly the fields the admin Create takes", () => {
    expect(fieldNames(regions)).toEqual(["id", "country_code", "status", "infra.numeric_infra_id"]);
  });

  it("pins the id as an immutable admin-assigned slug", () => {
    const id = field(regions, "id")!;
    expect(id.required).toBe(true);
    expect(id.immutable).toBe(true);
    // Same slug invariant the service enforces: lowercase alnum segments.
    expect((id as { pattern?: string }).pattern).toBe("^[a-z][a-z0-9]*(-[a-z0-9]+)*$");
  });

  it("pins countryCode to ISO-3166 alpha-2 and leaves it optional", () => {
    const cc = field(regions, "country_code")!;
    expect(cc.required).toBeFalsy();
    expect((cc as { pattern?: string }).pattern).toBe("^([A-Z]{2})?$");
  });

  it("exposes the raw admin status as UP/DOWN only", () => {
    const status = field(regions, "status")!;
    expect(status.type).toBe("enum");
    expect((status as { options: { value: string }[] }).options.map((o) => o.value)).toEqual(["UP", "DOWN"]);
    // No default on the field: the edit form must not synthesise a status for a
    // resource whose fetched value was absent.
    expect(status).not.toHaveProperty("default");
  });

  it("seeds Create closed to placement — the service default is DOWN, fail-safe", () => {
    const t = asObj(regions.template({}));
    expect(t.status).toBe("DOWN");
    expect(t.id).toBe("");
  });

  it("keeps numericInfraId settable once and never editable", () => {
    const n = field(regions, "infra.numeric_infra_id")!;
    expect(n.immutable).toBe(true);
  });

  it("does not offer the region capacity rollup as an input — it is not settable", () => {
    expect(fieldNames(regions)).not.toContain("infra.capacity_hint");
  });

  it("drops the empty optionals instead of sending blanks", () => {
    const out = regions.sanitize!({
      id: "ru-central1",
      country_code: "",
      status: "DOWN",
      infra: { numeric_infra_id: "" },
    });
    expect(out).not.toHaveProperty("country_code");
    expect(out).not.toHaveProperty("infra");
    expect(out.id).toBe("ru-central1");
  });

  it("keeps the optionals it was given", () => {
    const out = regions.sanitize!({
      id: "ru-central1",
      country_code: "RU",
      status: "UP",
      infra: { numeric_infra_id: "42" },
    });
    expect(out.country_code).toBe("RU");
    expect(out.infra).toEqual({ numeric_infra_id: "42" });
  });

  it("offers the placement filter server-side, so it applies to the whole list", () => {
    const toggle = (regions.listFilters ?? []).find((f) => f.kind === "toggle");
    expect(toggle?.param).toBe("open_for_placement");
  });
});

describe("geo registry — Zone", () => {
  const zones = REGISTRY.zones;

  it("surfaces the derived placement signal and why it is blocked", () => {
    const paths = columnPaths(zones);
    expect(paths).toContain("region_id");
    expect(paths).toContain("open_for_placement");
    expect(paths).toContain("placement_blocked_reason");
  });

  it("accepts exactly the fields the admin Create takes", () => {
    expect(fieldNames(zones)).toEqual([
      "id",
      "region_id",
      "status",
      "infra.numeric_infra_id",
      "infra.host_classes",
      "infra.failure_domain_count",
      "infra.underlay_anchor",
      "infra.capacity_hint",
    ]);
  });

  it("pins regionId immutable — moving a zone would break every placed resource", () => {
    const r = field(zones, "region_id")!;
    expect(r.type).toBe("ref");
    expect((r as { refResource: string }).refResource).toBe("regions");
    expect(r.required).toBe(true);
    expect(r.immutable).toBe(true);
  });

  it("rejects a zone id that is not prefixed by its region id", () => {
    expect(zones.validate!({ id: "ru-central1-a", region_id: "ru-central1" })).toBeNull();
    const msg = zones.validate!({ id: "eu-north1-a", region_id: "ru-central1" });
    expect(msg).toContain("ru-central1");
    // Counter-example the service spells out: the next character must be '-'.
    expect(zones.validate!({ id: "ru-central10-a", region_id: "ru-central1" })).not.toBeNull();
  });

  it("does not complain before both ids have been typed", () => {
    expect(zones.validate!({ id: "", region_id: "ru-central1" })).toBeNull();
    expect(zones.validate!({ id: "ru-central1-a", region_id: "" })).toBeNull();
  });

  it("edits the mutable infra° block and keeps numericInfraId out of it", () => {
    expect(field(zones, "infra.numeric_infra_id")!.immutable).toBe(true);
    expect(field(zones, "infra.host_classes")!.immutable).toBeFalsy();
    expect(field(zones, "infra.failure_domain_count")!.type).toBe("int");
    expect(field(zones, "infra.capacity_hint")!.type).toBe("enum");
  });

  it("turns the host-class textarea into the repeated field the service expects", () => {
    const out = zones.sanitize!({
      id: "ru-central1-a",
      region_id: "ru-central1",
      name: "A",
      status: "UP",
      infra: { host_classes: "std-1\n  gpu-a100  \n\n", failure_domain_count: 3 },
    });
    expect(asObj(out.infra).host_classes).toEqual(["std-1", "gpu-a100"]);
    expect(asObj(out.infra).failure_domain_count).toBe(3);
  });

  it("reads the repeated field back into the textarea for editing", () => {
    const form = zones.hydrate!({
      id: "ru-central1-a",
      infra: { host_classes: ["std-1", "gpu-a100"] },
    });
    expect(asObj(asObj(form).infra).host_classes).toBe("std-1\ngpu-a100");
  });

  it("does not send regionId on update — the service reserved that field", () => {
    // Update carries no region_id at all; leaving it in the body would be dropped
    // silently, and leaving it in the mask is a synchronous InvalidArgument.
    const mutable = (zones.fields ?? []).filter((f) => !f.immutable && !f.hidden).map((f) => f.name);
    expect(mutable).not.toContain("region_id");
  });

  it("offers both server-side filters: by region and by placement", () => {
    const filters = zones.listFilters ?? [];
    expect(filters.find((f) => f.kind === "toggle")?.param).toBe("open_for_placement");
    const ref = filters.find((f) => f.kind === "ref");
    expect(ref?.param).toBe("region_id");
    expect(ref?.kind === "ref" && ref.refSpecId).toBe("regions");
  });
});

describe("geo registry — where the list lives", () => {
  // Region/Zone are cluster-scoped and are mounted at /system/*, not under a
  // project. Routing them through the project-scoped helper produced a path that
  // does not exist, so "back" from a region — and the redirect after deleting one
  // — landed in IAM projects.
  it("resolves the listing path without a project", () => {
    expect(resourceProjectPath("regions", null)).toBe("/system/regions");
    expect(resourceProjectPath("zones", undefined)).toBe("/system/zones");
    expect(resourceProjectPath("address-pools", null)).toBe("/system/address-pools");
  });

  it("ignores a project that happens to be selected — the catalog is not in it", () => {
    expect(resourceProjectPath("regions", "prj-1")).toBe("/system/regions");
  });

  it("leaves project-scoped resources alone", () => {
    expect(resourceProjectPath("networks", "prj-1")).toBe("/projects/prj-1/vpc/networks");
    expect(resourceProjectPath("networks", null)).toBeNull();
  });
});
