// Wire-level contract lock for the iam client calls that hand-write a request
// body. Ground truth: proto/kacho/cloud/iam/v1/access_binding_service.proto.
//
// These assert what actually leaves the browser, not what the TypeScript literal
// looks like — api/client.ts camel-cases request keys on the way out, so the two
// differ, and the edge parses the body with DiscardUnknown (an unknown key is
// dropped in silence and the caller still gets 200).

import { buildCreateAccessBindingBody, iamApi } from "./iam";

type Captured = { url: string; method: string; body: Record<string, unknown> };

function captureFetch(): { calls: Captured[] } {
  const calls: Captured[] = [];
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    calls.push({
      url: String(url),
      method: String(init.method),
      body: init.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {},
    });
    return {
      ok: true,
      status: 200,
      statusText: "OK",
      text: async () => JSON.stringify({ operation: { id: "opr-1", done: false } }),
    } as unknown as Response;
  }) as unknown as typeof fetch;
  return { calls };
}

describe("buildCreateAccessBindingBody", () => {
  // Ground truth: CreateAccessBindingRequest in
  // proto/kacho/cloud/iam/v1/access_binding_service.proto.
  //   required : subjects[] (or the legacy subject_type/subject_id pair),
  //              role_id, scope_type (dotted), scope_id, target
  //   tombstone: tags 9/10/11 with the names target_ref / scope_ref — a body
  //              keyed on scope_ref names no field of this message
  //   renamed  : resource_type/resource_id → scope_type/scope_id (F7)

  it("anchors the grant with the dotted scope_type + scope_id pair", () => {
    expect(
      buildCreateAccessBindingBody({
        subjects: [{ type: "SUBJECT_TYPE_USER", id: "usr-1" }],
        roleId: "rol-1",
        scopeTier: "ACCOUNT",
        scopeId: "acc-1",
      }),
    ).toEqual({
      subjects: [{ type: "SUBJECT_TYPE_USER", id: "usr-1" }],
      role_id: "rol-1",
      scope_type: "iam.account",
      scope_id: "acc-1",
      target: { all_in_scope: {} },
    });
  });

  it("maps the cluster tier to its dotted name and singleton anchor", () => {
    const body = buildCreateAccessBindingBody({
      subjects: [{ type: "SUBJECT_TYPE_GROUP", id: "grp-1" }],
      roleId: "rol-1",
      scopeTier: "CLUSTER",
      scopeId: "",
    });
    expect(body.scope_type).toBe("iam.cluster");
    expect(body.scope_id).toBe("cluster_kacho_root");
  });

  it("uses the project tier's dotted name", () => {
    expect(
      buildCreateAccessBindingBody({
        subjects: [{ type: "SUBJECT_TYPE_SERVICE_ACCOUNT", id: "sva-1" }],
        roleId: "rol-1",
        scopeTier: "PROJECT",
        scopeId: "prj-1",
      }).scope_type,
    ).toBe("iam.project");
  });

  it("carries a per-object target when one is given", () => {
    const body = buildCreateAccessBindingBody({
      subjects: [{ type: "SUBJECT_TYPE_USER", id: "usr-1" }],
      roleId: "rol-1",
      scopeTier: "PROJECT",
      scopeId: "prj-1",
      targetResources: [{ type: "compute.instance", id: "ins-1" }],
    });
    expect(body.target).toEqual({ resources: { resources: [{ type: "compute.instance", id: "ins-1" }] } });
  });

  it("never emits a name the message reserved or renamed away", () => {
    const body = buildCreateAccessBindingBody({
      subjects: [{ type: "SUBJECT_TYPE_USER", id: "usr-1" }],
      roleId: "rol-1",
      scopeTier: "ACCOUNT",
      scopeId: "acc-1",
    });
    for (const gone of ["scope_ref", "target_ref", "resource_type", "resource_id"]) {
      expect(body).not.toHaveProperty(gone);
    }
  });

  it("refuses to build a grant with no subject rather than sending an empty one", () => {
    expect(() =>
      buildCreateAccessBindingBody({ subjects: [], roleId: "rol-1", scopeTier: "ACCOUNT", scopeId: "acc-1" }),
    ).toThrow();
  });

  it("refuses a non-cluster tier with no anchor id", () => {
    expect(() =>
      buildCreateAccessBindingBody({
        subjects: [{ type: "SUBJECT_TYPE_USER", id: "usr-1" }],
        roleId: "rol-1",
        scopeTier: "PROJECT",
        scopeId: "",
      }),
    ).toThrow();
  });
});

describe("updateAccessBindingDeletionProtection", () => {
  const originalFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("names the masked field the way protojson accepts it", async () => {
    const { calls } = captureFetch();
    await iamApi.updateAccessBindingDeletionProtection("acb-1", false);

    expect(calls).toHaveLength(1);
    expect(calls[0].method).toBe("PATCH");
    expect(calls[0].url).toBe("/iam/v1/accessBindings/acb-1");
    // protojson's FieldMask decoder rejects any path containing "_" outright:
    // "deletion_protection" is not a lenient spelling of the field, it is a 400.
    expect(calls[0].body.updateMask).toBe("deletionProtection");
    expect(calls[0].body).toEqual({ deletionProtection: false, updateMask: "deletionProtection" });
  });
});
