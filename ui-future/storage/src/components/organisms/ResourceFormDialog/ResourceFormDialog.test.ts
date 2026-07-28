// Parity lock for this remote's copy of the form → wire helpers.
//
// The generic CRUD organisms are vendored per remote, so a fix landed in one copy
// silently misses the others. These assert the same contract the shared copy
// asserts (shared/src/lib/update-mask.test.ts): an Update body carries exactly the
// masked fields, the mask is spelled the way protojson reads it, and form-only
// `_`-keys never reach the wire — where api/client.ts would camel-case them into
// `Placement` / `AddressKind` / `BootSource` and the edge would discard them in
// silence, answering 200 for something it never did.

import { buildCreateBody, buildUpdateBody, stripFormOnlyKeys } from "./ResourceFormDialog";

describe("stripFormOnlyKeys", () => {
  it("drops leading-underscore keys at every depth, including inside arrays", () => {
    expect(
      stripFormOnlyKeys({
        _source_kind: "snapshot",
        nics: [{ _ext_mode: "auto", subnet_id: "sub1", addr: { _x: 1, ip: "10.0.0.5" } }],
      }),
    ).toEqual({ nics: [{ subnet_id: "sub1", addr: { ip: "10.0.0.5" } }] });
  });

  it("leaves user-defined map keys alone (labels/annotations are opaque)", () => {
    expect(stripFormOnlyKeys({ labels: { _weird: "v" }, _k: 1 })).toEqual({ labels: { _weird: "v" } });
  });
});

describe("buildUpdateBody", () => {
  const getProjection = {
    id: "vol-abc",
    project_id: "prj-1",
    created_at: "2026-07-28T00:00:00Z",
    status: "READY",
    name: "edited",
    labels: { env: "dev" },
  };

  it("sends exactly the masked fields plus the mask", () => {
    expect(buildUpdateBody(getProjection, ["name"])).toEqual({ name: "edited", update_mask: "name" });
  });

  it("never forwards server-owned fields read back from the GET projection", () => {
    const body = buildUpdateBody(getProjection, ["name", "labels"])!;
    expect(Object.keys(body).sort()).toEqual(["labels", "name", "update_mask"]);
  });

  it("renders the mask in the camelCase form the edge accepts", () => {
    // protojson rejects a FieldMask path containing "_" outright.
    expect(buildUpdateBody({ size_gib: 20 }, ["size_gib"])!.update_mask).toBe("sizeGib");
  });

  it("returns null for an empty mask — there is no request to send", () => {
    expect(buildUpdateBody(getProjection, [])).toBeNull();
  });
});

describe("buildCreateBody", () => {
  it("keeps the domain payload and drops only the widget state", () => {
    expect(buildCreateBody({ _source_kind: "blank", name: "v1", size_gib: 10 })).toEqual({ name: "v1", size_gib: 10 });
  });
});
