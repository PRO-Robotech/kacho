// The load balancer create body, assembled the way the page assembles it.
//
// Ground truth: proto/kacho/cloud/loadbalancer/v1/network_load_balancer_service.proto
// and services/nlb/internal/apps/kacho/api/loadbalancer/vip_source.go.
//
//   • `placement` (EXTERNAL_REGIONAL | INTERNAL_REGIONAL | INTERNAL_ZONAL) is the
//     SOLE authoritative mode input. `type` and `placement_type` are derived
//     output-only projections, kept on the request only so that a client setting
//     one earns an explicit InvalidArgument.
//   • At least one of v4_source / v6_source is required.
//   • validateSourceTypeMatrix: a `subnet_id` arm is valid ONLY for an INTERNAL
//     load balancer, a `public {}` arm ONLY for an EXTERNAL one.
//   • disabled_announce_zones applies to REGIONAL placements only.
//
// The form stopped sending `type` / `placement_type` — correctly, they are
// write-rejects. Everything downstream that still READ them from the form object
// therefore reads undefined and falls back to INTERNAL/ZONAL, so the mode the
// operator chose stops reaching the VIP arm the body carries. The assertions below
// go through the real chain — applyFieldDefaults → spec.validate → spec.sanitize →
// buildCreateBody — because that is what decides the bytes.

import { REGISTRY, applyFieldDefaults } from "./resource-registry";
import { buildCreateBody } from "@shared/lib/update-mask";

const spec = REGISTRY["load-balancers"];
const asObj = (v: unknown) => v as Record<string, unknown>;

/** The form object a freshly opened create page holds, plus the operator's edits. */
function formObject(edits: Record<string, unknown> = {}): Record<string, unknown> {
  const seeded = applyFieldDefaults(spec.fields, asObj(spec.template({ projectId: "prj-1" })));
  return { ...seeded, ...edits };
}

/** The body that leaves the page for POST /nlb/v1/networkLoadBalancers. */
function createBody(edits: Record<string, unknown> = {}): Record<string, unknown> {
  const obj = formObject(edits);
  return buildCreateBody(spec.sanitize ? spec.sanitize(obj) : obj);
}

describe("load balancer create body follows the placement the operator chose", () => {
  it("submits with nothing but the default placement — a public VIP needs no picking", () => {
    // EXTERNAL_REGIONAL is the form default. A public VIP is allocated by the
    // platform, so there is nothing left for the operator to choose; the client
    // guard must not stand in the way of a body the server accepts.
    const obj = formObject();
    expect(obj.placement).toBe("EXTERNAL_REGIONAL");
    expect(spec.validate?.(obj) ?? null).toBeNull();

    const body = createBody();
    expect(body.v4_source).toEqual({ public: {} });
    // The derived projections stay off the wire: writing either is an explicit reject.
    expect(body).not.toHaveProperty("type");
    expect(body).not.toHaveProperty("placement_type");
  });

  it("never sends a subnet arm on an EXTERNAL placement", () => {
    // A subnet source is valid only for an INTERNAL load balancer. A stale subnet
    // selection left over from an INTERNAL draft must not survive the switch —
    // the emitted arm follows `placement`, not what the widget happens to hold.
    const body = createBody({
      placement: "EXTERNAL_REGIONAL",
      vip_source: {
        _v4_mode: "subnet",
        v4: { subnet_id: "sub-abcdefghijklmnopq", address_id: "" },
        _v6_mode: "subnet",
        v6: { subnet_id: "", address_id: "" },
      },
    });
    expect(body.v4_source).toEqual({ public: {} });
  });

  it("sends the subnet arm on an INTERNAL placement", () => {
    const edits = {
      placement: "INTERNAL_ZONAL",
      vip_source: {
        _v4_mode: "subnet",
        v4: { subnet_id: "sub-abcdefghijklmnopq", address_id: "" },
        _v6_mode: "subnet",
        v6: { subnet_id: "", address_id: "" },
      },
    };
    expect(spec.validate?.(formObject(edits)) ?? null).toBeNull();
    const body = createBody(edits);
    expect(body.v4_source).toEqual({ subnet_id: "sub-abcdefghijklmnopq" });
    expect(body).not.toHaveProperty("v6_source");
  });

  it("still refuses an INTERNAL placement with no source picked for either family", () => {
    // The guard exists because the server rejects a body with no family at all;
    // on INTERNAL there is no auto arm, so an empty picker is a real error.
    const msg = spec.validate?.(formObject({ placement: "INTERNAL_REGIONAL" }));
    expect(typeof msg).toBe("string");
  });

  it("carries the drain zones on a REGIONAL placement and drops them on a ZONAL one", () => {
    // disabled_announce_zones is REGIONAL-only. It was being dropped unconditionally,
    // so the deny-list the operator picked never reached the server.
    const zones = ["ru-central1-a"];
    expect(createBody({ placement: "EXTERNAL_REGIONAL", disabled_announce_zones: zones }).disabled_announce_zones).toEqual(
      zones,
    );
    expect(
      createBody({ placement: "INTERNAL_REGIONAL", disabled_announce_zones: zones }).disabled_announce_zones,
    ).toEqual(zones);
    expect(createBody({ placement: "INTERNAL_ZONAL", disabled_announce_zones: zones })).not.toHaveProperty(
      "disabled_announce_zones",
    );
  });

  it("gates the drain-zone field on a field the form object actually carries", () => {
    // A visibleWhen keyed on a field the object no longer has is a field that can
    // never be shown — the same defect one layer up from the body.
    const field = (spec.fields ?? []).find((f) => f.name === "disabled_announce_zones");
    const gate = field?.visibleWhen;
    expect(gate).toBeDefined();
    expect(Object.keys(formObject())).toContain(gate!.field);
  });
});
