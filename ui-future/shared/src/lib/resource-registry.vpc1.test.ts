// VPC-1 contract lock for the Network/Subnet registry specs: placement is a
// server-derived discriminator (never sent), CIDR uses the immutable primary
// anchor + verb-managed additional ranges, and the declared Network supernet is
// required at Create. These assert the pure template/sanitize/hydrate — the
// spec-driven surface the generic Create/Edit forms and detail views consume.

import { REGISTRY } from "./resource-registry";

const asObj = (v: unknown) => v as Record<string, unknown>;

describe("VPC-1 subnet registry contract", () => {
  const subnets = REGISTRY["subnets"];

  it("template carries the placement discriminator + primary anchor, not the retired fields", () => {
    const t = asObj(subnets.template({ projectId: "prj-1" }));
    expect(t._placement).toBe("ZONAL");
    expect(t.ipv4_cidr_primary).toBe("");
    expect(t.ipv6_cidr_primary).toBe("");
    // placement_type is server-derived and NEVER seeded into the form payload.
    expect(t).not.toHaveProperty("placement_type");
    // v4_cidr_blocks[] retired in favour of the primary anchor.
    expect(t).not.toHaveProperty("v4_cidr_blocks");
    expect(t).not.toHaveProperty("v6_cidr_blocks");
    // DhcpOptions retired by design.
    expect(t).not.toHaveProperty("dhcp_options");
  });

  it("sanitize strips the discriminator, drops the inactive channel, and never emits placement_type", () => {
    const zonal = subnets.sanitize!({
      _placement: "ZONAL",
      zone_id: "ru-central1-a",
      region_id: "ru-central1",
      ipv4_cidr_primary: "10.20.0.0/24",
      ipv6_cidr_primary: "",
    });
    expect(zonal).not.toHaveProperty("_placement");
    expect(zonal).not.toHaveProperty("placement_type");
    expect(zonal).not.toHaveProperty("region_id"); // ZONAL → region dropped
    expect(zonal.zone_id).toBe("ru-central1-a");
    expect(zonal.ipv4_cidr_primary).toBe("10.20.0.0/24");
    expect(zonal).not.toHaveProperty("ipv6_cidr_primary"); // empty dropped

    const regional = subnets.sanitize!({
      _placement: "REGIONAL",
      zone_id: "ru-central1-a",
      region_id: "ru-central1",
      ipv6_cidr_primary: "fd00:20::/64",
    });
    expect(regional).not.toHaveProperty("zone_id"); // REGIONAL → zone dropped
    expect(regional.region_id).toBe("ru-central1");
    expect(regional).not.toHaveProperty("placement_type");
  });

  it("hydrate derives the _placement channel from placement_type / region_id", () => {
    expect(asObj(subnets.hydrate!({ placement_type: "REGIONAL", region_id: "ru-central1" }))._placement).toBe(
      "REGIONAL",
    );
    expect(asObj(subnets.hydrate!({ zone_id: "ru-central1-a" }))._placement).toBe("ZONAL");
  });
});

describe("VPC-1 network registry contract", () => {
  const networks = REGISTRY["networks"];

  it("template declares a supernet and drops the retired opt-out flag", () => {
    const t = asObj(networks.template({ projectId: "prj-1" }));
    expect(Array.isArray(t.ipv4_cidr_blocks)).toBe(true);
    expect(Array.isArray(t.ipv6_cidr_blocks)).toBe(true);
    // default-SG + default-RT are provisioned unconditionally server-side.
    expect(t).not.toHaveProperty("create_default_security_group");
  });

  it("sanitize/hydrate round-trip the supernet between {value} form-objects and wire string[]", () => {
    const wire = networks.sanitize!({ ipv4_cidr_blocks: [{ value: "10.20.0.0/16" }, { value: "" }] });
    expect(wire.ipv4_cidr_blocks).toEqual(["10.20.0.0/16"]);
    const form = networks.hydrate!({ ipv4_cidr_blocks: ["10.20.0.0/16"] });
    expect(form.ipv4_cidr_blocks).toEqual([{ value: "10.20.0.0/16" }]);
  });
});
