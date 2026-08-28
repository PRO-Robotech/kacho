// A preset seeds a create form from the URL. Its key set is therefore whatever the
// URL carries — caller-chosen, not bounded by the resource's schema — and a preset
// key lands in the request body exactly like a typed one.
//
// Today no route feeds an unknown one, so nothing is broken. But "safe because of
// the current route graph" is not safe: a new link with a stray query param, or a
// detail page remounted with its params preserved, would put that param on the
// wire, where the edge would drop it in silence. Bound the preset to the schema
// instead, so the guarantee holds by construction.

import { presetFieldsForSpec } from "@shared/lib/preset-fields";
import type { FormField } from "@shared/lib/form-schema";

const str = (name: string): FormField => ({ name, label: name, type: "string" }) as FormField;
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
