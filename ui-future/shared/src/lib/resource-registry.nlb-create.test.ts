// The NLB create bodies this registry assembles, against what the service accepts.
//
// Ground truth: proto/kacho/cloud/loadbalancer/v1/* and
// services/nlb/internal/apps/kacho/api/loadbalancer/vip_source.go +
// services/nlb/internal/domain/types.go.
//
//   • resolveVipSources: a load balancer MUST declare a vip source for at least one
//     ip family — a body carrying neither v4_source nor v6_source is rejected
//     outright, so a form that cannot express one cannot create at all.
//   • validateSourceTypeMatrix: `subnet_id` is valid ONLY for INTERNAL, `public {}`
//     ONLY for EXTERNAL. `placement` is the sole input that decides which.
//   • LbPort.Validate: a listener port outside [1,65535] is InvalidArgument, and 0
//     is outside it — so a template that seeds 0 hands the operator a body the
//     server refuses unless they notice and overwrite it.
//
// Assertions go through the chain the create page uses — applyFieldDefaults →
// spec.validate → spec.sanitize → buildCreateBody — because that is what decides
// the bytes. Asserting the template instead would pass over a default that
// applyFieldDefaults puts back.

import { REGISTRY, applyFieldDefaults } from "./resource-registry";
import { buildCreateBody } from "./update-mask";

const asObj = (v: unknown) => v as Record<string, unknown>;

function formObject(specId: string, edits: Record<string, unknown> = {}): Record<string, unknown> {
  const spec = REGISTRY[specId];
  const seeded = applyFieldDefaults(spec.fields, asObj(spec.template({ projectId: "prj-1", accountId: "acc-1" })));
  return { ...seeded, ...edits };
}

function createBody(specId: string, edits: Record<string, unknown> = {}): Record<string, unknown> {
  const spec = REGISTRY[specId];
  const obj = formObject(specId, edits);
  return buildCreateBody(spec.sanitize ? spec.sanitize(obj) : obj);
}

describe("load balancer create can express the vip source the service requires", () => {
  it("declares a source for a family on the default placement", () => {
    const spec = REGISTRY["load-balancers"];
    const obj = formObject("load-balancers");
    expect(obj.placement).toBe("EXTERNAL_REGIONAL");
    expect(spec.validate?.(obj) ?? null).toBeNull();

    const body = createBody("load-balancers");
    // EXTERNAL takes the platform-allocated public VIP; nothing to pick.
    expect(body.v4_source).toEqual({ public: {} });
    expect(body).not.toHaveProperty("type");
    expect(body).not.toHaveProperty("placement_type");
  });

  it("sends the subnet arm only on an INTERNAL placement", () => {
    const subnet = { _v4_source: "subnet", v4_source: { subnet_id: "sub-abcdefghijklmnopq" } };
    expect(createBody("load-balancers", { placement: "INTERNAL_ZONAL", ...subnet }).v4_source).toEqual({
      subnet_id: "sub-abcdefghijklmnopq",
    });
    // The same widget state under EXTERNAL must not emit a subnet arm the service rejects.
    expect(createBody("load-balancers", { placement: "EXTERNAL_REGIONAL", ...subnet }).v4_source).toEqual({
      public: {},
    });
  });

  it("omits a family with nothing chosen rather than sending an empty reference", () => {
    const body = createBody("load-balancers", {
      placement: "INTERNAL_REGIONAL",
      _v4_source: "subnet",
      v4_source: { subnet_id: "sub-abcdefghijklmnopq" },
      _v6_source: "address",
      v6_source: { address_id: "" },
    });
    expect(body).not.toHaveProperty("v6_source");
    // A blank id in a chosen oneof arm is a request the service answers with
    // «required» — the family is dropped instead.
    expect(body.v4_source).toEqual({ subnet_id: "sub-abcdefghijklmnopq" });
  });

  it("refuses before submit when no family has a source", () => {
    const spec = REGISTRY["load-balancers"];
    const msg = spec.validate?.(formObject("load-balancers", { placement: "INTERNAL_REGIONAL", _v4_source: "off" }));
    expect(typeof msg).toBe("string");
  });

  it("keeps the form-only mode discriminators off the wire", () => {
    const body = createBody("load-balancers");
    expect(Object.keys(body).filter((k) => k.startsWith("_"))).toEqual([]);
  });

  it("gates every conditional vip field on a field the form object carries", () => {
    // A visibleWhen keyed on something the object does not hold is a field that
    // can never be shown — an input the operator is offered on paper only.
    const obj = formObject("load-balancers");
    for (const f of REGISTRY["load-balancers"].fields ?? []) {
      if (!f.visibleWhen) continue;
      expect(Object.keys(obj)).toContain(f.visibleWhen.field);
    }
  });
});

describe("listener create does not seed a port the contract rejects", () => {
  it("leaves the port unset rather than defaulting to one outside [1,65535]", () => {
    const body = createBody("listeners");
    // 0 is not a port. Seeding it turns «operator did not type a port» into a
    // body the service answers «port must be in range [1, 65535]».
    expect(body.port === undefined || (typeof body.port === "number" && body.port >= 1)).toBe(true);
  });

  it("carries the port the operator typed", () => {
    expect(createBody("listeners", { port: 443 }).port).toBe(443);
  });
});
