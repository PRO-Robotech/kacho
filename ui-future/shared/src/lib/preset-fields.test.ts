// A preset seeds a create form from the URL. Its key set is therefore whatever the
// URL carries — caller-chosen, not bounded by the resource's schema — and a preset
// key lands in the request body exactly like a typed one.
//
// Today no route feeds an unknown one, so nothing is broken. But "safe because of
// the current route graph" is not safe: a new link with a stray query param, or a
// detail page remounted with its params preserved, would put that param on the
// wire, where the edge would drop it in silence. Bound the preset to the schema
// instead, so the guarantee holds by construction.

import { presetFieldsForSpec } from "./preset-fields";
import { REGISTRY } from "./resource-registry";
import type { FormField } from "./form-schema";

const str = (name: string): FormField => ({ name, label: name, type: "string" });
const fields = [str("name"), str("subnet_id"), str("internal_ipv4_address_spec.subnet_id"), str("_address_kind")];

describe("presetFieldsForSpec", () => {
  it("keeps a preset that names a field of the form", () => {
    expect(presetFieldsForSpec(fields, { subnet_id: "sub-1", name: "a" })).toEqual({ subnet_id: "sub-1", name: "a" });
  });

  it("keeps a dotted preset that names a nested field of the form", () => {
    expect(presetFieldsForSpec(fields, { "internal_ipv4_address_spec.subnet_id": "sub-1" })).toEqual({
      "internal_ipv4_address_spec.subnet_id": "sub-1",
    });
  });

  it("keeps a form-only discriminator, which never reaches the wire anyway", () => {
    expect(presetFieldsForSpec(fields, { _address_kind: "internal" })).toEqual({ _address_kind: "internal" });
  });

  it("drops a preset the form has no field for", () => {
    // `tab` is a UI concern that happened to be in the query string; `network_id`
    // is a real field of SOME resources but not of this one.
    expect(presetFieldsForSpec(fields, { tab: "rules", network_id: "net-1", subnet_id: "sub-1" })).toEqual({
      subnet_id: "sub-1",
    });
  });

  it("drops everything when the spec declares no fields at all", () => {
    expect(presetFieldsForSpec(undefined, { subnet_id: "sub-1" })).toEqual({});
  });
});

describe("the presets the app actually sets survive the filter", () => {
  // Bounding presets to the schema must not silently disarm the flows that DO
  // work: creating an address inside a subnet, a NIC inside a subnet, and the
  // network-scoped resources. Each key below is set by ResourceCreatePage from a
  // route param or a query string.
  const cases: [string, Record<string, unknown>][] = [
    [
      "addresses",
      {
        "internal_ipv4_address_spec.subnet_id": "sub-1",
        "internal_ipv6_address_spec.subnet_id": "sub-1",
        _address_kind: "internal",
      },
    ],
    ["network-interfaces", { subnet_id: "sub-1" }],
    ["subnets", { network_id: "net-1" }],
    ["route-tables", { network_id: "net-1" }],
    ["security-groups", { network_id: "net-1" }],
  ];

  for (const [specId, presets] of cases) {
    it(`${specId} keeps every preset it is given`, () => {
      expect(presetFieldsForSpec(REGISTRY[specId].fields, presets)).toEqual(presets);
    });
  }

  it("drops the network the page sets for every spec, on specs that have no such field", () => {
    // ResourceCreatePage sets network_id unconditionally when the route carries
    // one; CreateAddressRequest and CreateInstanceRequest declare no such field.
    expect(presetFieldsForSpec(REGISTRY["addresses"].fields, { network_id: "net-1" })).toEqual({});
    expect(presetFieldsForSpec(REGISTRY["compute-instances"].fields, { network_id: "net-1" })).toEqual({});
  });
});
