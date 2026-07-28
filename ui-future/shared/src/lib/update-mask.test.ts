import {
  buildCreateBody,
  buildUpdateBody,
  computeUpdateMask,
  snakeToCamelPath,
  stripFormOnlyKeys,
} from "./update-mask";
import type { FormField } from "./form-schema";

const str = (name: string, extra: Partial<FormField> = {}): FormField =>
  ({ name, label: name, type: "string", ...extra }) as FormField;

describe("computeUpdateMask", () => {
  it("includes only mutable fields whose value changed", () => {
    const fields = [str("name"), str("description")];
    const mask = computeUpdateMask({ name: "a", description: "old" }, { name: "a", description: "new" }, fields);
    expect(mask).toEqual(["description"]);
  });

  it("returns an empty mask when nothing changed", () => {
    const fields = [str("name"), str("description")];
    expect(computeUpdateMask({ name: "a" }, { name: "a" }, fields)).toEqual([]);
  });

  it("skips hidden / immutable / editHidden / createOnly and _-prefixed fields", () => {
    const fields = [
      str("hidden_f", { hidden: true }),
      str("immutable_f", { immutable: true }),
      str("edit_hidden_f", { editHidden: true }),
      str("create_only_f", { createOnly: true }),
      str("_discriminator"),
      str("mutable_f"),
    ];
    const original = {
      hidden_f: "a",
      immutable_f: "a",
      edit_hidden_f: "a",
      create_only_f: "a",
      _discriminator: "a",
      mutable_f: "a",
    };
    const current = {
      hidden_f: "b",
      immutable_f: "b",
      edit_hidden_f: "b",
      create_only_f: "b",
      _discriminator: "b",
      mutable_f: "b",
    };
    expect(computeUpdateMask(original, current, fields)).toEqual(["mutable_f"]);
  });

  it("resolves dotted paths via getByPath", () => {
    const fields = [str("labels.env")];
    const mask = computeUpdateMask({ labels: { env: "dev" } }, { labels: { env: "prod" } }, fields);
    expect(mask).toEqual(["labels.env"]);
  });

  it("treats deep structural changes as a diff (JSON compare)", () => {
    const fields = [str("routes")];
    const mask = computeUpdateMask({ routes: [{ dst: "0.0.0.0/0" }] }, { routes: [{ dst: "10.0.0.0/8" }] }, fields);
    expect(mask).toEqual(["routes"]);
  });
});

describe("computeUpdateMask against a spec whose sanitize changes representation", () => {
  // The generic edit path diffs the SANITIZED form object against the stored
  // original. If a spec's sanitize rewrites a value (number → Duration string) and
  // its hydrate rewrites it back for the widget, the two sides stop being
  // comparable and the field is dirty on every save — "nothing changed" never
  // short-circuits, and a field the operator never touched enters update_mask.
  // The original must therefore be stored in the same shape sanitize produces.
  const durationSpec = {
    sanitize: (o: Record<string, unknown>) => ({ ...o, delay: `${o.delay}s` }),
    hydrate: (o: Record<string, unknown>) => ({ ...o, delay: Number(String(o.delay).replace(/s$/, "")) }),
  };
  const fields = [str("delay"), str("name")];

  it("sees no change when the operator changed nothing", () => {
    const fromServer = { delay: "300s", name: "tg-1" };
    const original = durationSpec.sanitize(durationSpec.hydrate({ ...fromServer }));
    const edited = durationSpec.sanitize(durationSpec.hydrate({ ...fromServer }));
    expect(computeUpdateMask(original, edited, fields)).toEqual([]);
  });

  it("still sees the change when the operator made one", () => {
    const fromServer = { delay: "300s", name: "tg-1" };
    const original = durationSpec.sanitize(durationSpec.hydrate({ ...fromServer }));
    const edited = durationSpec.sanitize({ ...durationSpec.hydrate({ ...fromServer }), delay: 45 });
    expect(computeUpdateMask(original, edited, fields)).toEqual(["delay"]);
  });
});

describe("stripFormOnlyKeys", () => {
  it("drops leading-underscore keys, which are widget state and not request fields", () => {
    expect(stripFormOnlyKeys({ _placement: "ZONAL", zone_id: "z1" })).toEqual({ zone_id: "z1" });
  });

  it("drops them at every depth, including inside arrays", () => {
    const form = {
      network_interface_specs: [
        { _ext_mode: "none", _use_existing_nic: false, subnet_id: "sub1" },
        { _addr_cascader: ["a", "b"], subnet_id: "sub2", primary_v4_address_spec: { _x: 1, address: "10.0.0.5" } },
      ],
    };
    expect(stripFormOnlyKeys(form)).toEqual({
      network_interface_specs: [
        { subnet_id: "sub1" },
        { subnet_id: "sub2", primary_v4_address_spec: { address: "10.0.0.5" } },
      ],
    });
  });

  it("leaves user-defined map keys alone (labels/annotations are opaque)", () => {
    // Same carve-out as the request/response case transformer: a tenant's label
    // key is data, not a field name, and must not be rewritten or dropped.
    const form = { labels: { _weird: "v", team: "core" }, annotations: { _n: "1" }, _placement: "ZONAL" };
    expect(stripFormOnlyKeys(form)).toEqual({ labels: { _weird: "v", team: "core" }, annotations: { _n: "1" } });
  });

  it("passes scalars and null through untouched", () => {
    expect(stripFormOnlyKeys("x")).toBe("x");
    expect(stripFormOnlyKeys(null)).toBeNull();
    expect(stripFormOnlyKeys(7)).toBe(7);
  });
});

describe("buildUpdateBody", () => {
  // The Update* request messages carry `<resource>_id` (bound from the URL),
  // `update_mask`, and the mutable fields — nothing else. Anything extra is an
  // unknown field at the edge, which parses request bodies with
  // DiscardUnknown: it is dropped silently and the caller still gets 200.
  const getProjection = {
    id: "net-abc",
    project_id: "prj-1",
    created_at: "2026-07-28T00:00:00Z",
    status: "ACTIVE",
    default_security_group_id: "sg-1",
    name: "edited",
    description: "",
    labels: { env: "dev" },
  };

  it("sends exactly the masked fields plus the mask", () => {
    const body = buildUpdateBody(getProjection, ["name"]);
    expect(body).toEqual({ name: "edited", update_mask: "name" });
  });

  it("never forwards server-owned fields read back from the GET projection", () => {
    const body = buildUpdateBody(getProjection, ["name", "labels"])!;
    expect(Object.keys(body).sort()).toEqual(["labels", "name", "update_mask"]);
    for (const serverOwned of ["id", "project_id", "created_at", "status", "default_security_group_id"]) {
      expect(body).not.toHaveProperty(serverOwned);
    }
  });

  it("renders the mask in the camelCase form the edge accepts", () => {
    // protojson rejects a FieldMask path containing "_" outright, so a
    // snake_case path is not a lenient spelling — it is a 400.
    const body = buildUpdateBody({ route_table_id: "rt-1", v4_cidr_blocks: [] }, ["route_table_id", "v4_cidr_blocks"]);
    expect(body!.update_mask).toBe("routeTableId,v4CidrBlocks");
  });

  it("keeps a dotted mask path nested, matching the message shape", () => {
    const body = buildUpdateBody({ vm_spec: { user_data: "#cloud-config", junk: 1 }, id: "ins-1" }, ["vm_spec.user_data"]);
    expect(body).toEqual({ vm_spec: { user_data: "#cloud-config" }, update_mask: "vmSpec.userData" });
  });

  it("strips form-only keys out of a masked value", () => {
    const body = buildUpdateBody({ nics: [{ _ext_mode: "auto", subnet_id: "sub1" }] }, ["nics"]);
    expect(body).toEqual({ nics: [{ subnet_id: "sub1" }], update_mask: "nics" });
  });

  it("applies the labels/annotations carve-out to a masked map, same as a created body", () => {
    // A masked `labels` is still an opaque tenant map. The carve-out must not
    // depend on whether the map arrived as a whole body or as one masked field —
    // otherwise editing a resource silently drops a label that creating it keeps.
    const labels = { _weird: "v", team: "core" };
    expect(buildUpdateBody({ labels }, ["labels"])).toEqual({ labels, update_mask: "labels" });
    expect(buildCreateBody({ labels })).toEqual({ labels });
  });

  it("does not invent a value for a masked path the form does not carry", () => {
    // A mask entry whose value is absent serialises the field away (JSON drops
    // undefined), so the server sees the masked field unset and clears it. That is
    // the FieldMask contract — but it means a mask must only ever name paths the
    // form actually produced. computeUpdateMask does, by construction: it emits a
    // path only after reading both sides of it.
    expect(buildUpdateBody({}, ["description"])).toEqual({ description: undefined, update_mask: "description" });
    expect(JSON.parse(JSON.stringify(buildUpdateBody({}, ["description"])))).toEqual({ update_mask: "description" });
  });

  it("returns null for an empty mask — there is no request to send", () => {
    expect(buildUpdateBody(getProjection, [])).toBeNull();
  });
});

describe("snakeToCamelPath", () => {
  it("converts a snake_case path segment to camelCase", () => {
    expect(snakeToCamelPath("v4_cidr_blocks")).toBe("v4CidrBlocks");
  });

  it("leaves a path without underscores unchanged", () => {
    expect(snakeToCamelPath("name")).toBe("name");
  });

  it("converts each dotted segment independently", () => {
    expect(snakeToCamelPath("static_routes.next_hop")).toBe("staticRoutes.nextHop");
  });
});
