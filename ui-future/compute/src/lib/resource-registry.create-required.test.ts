// Every spec that offers Create must be able to express what Create requires.
//
// A required field the form has no way to carry is not a cosmetic gap: the call
// is refused, so the resource cannot be created from the console at all — and the
// button that offers it is a promise the product does not keep. This is the
// counterpart of resource-registry.request-fields.test.ts, which locks the other
// direction (a field the form sends that the message does not declare).
//
// Ground truth: the `(required) = true` set of each Create*Request under
// proto/kacho/cloud/**, cited per entry below. A spec is measured through the
// chain the create page uses — spec.template → applyFieldDefaults → spec.sanitize
// → buildCreateBody — plus the fields the operator can fill in, because a required
// field the operator types is expressible even when the template leaves it blank.
//
// The table is exhaustive by construction: a create-capable spec with no entry
// fails, so adding one to the registry forces its contract to be stated here.
//
// This remote carries its own copy of the registry (there is no @shared alias
// here), so the lock lives here too — a fix landing in one copy and not the
// other is the very class this sweep exists to catch.

import { REGISTRY, applyFieldDefaults } from "./resource-registry";
import { buildCreateBody } from "@/components/organisms/ResourceFormDialog";

const asObj = (v: unknown) => v as Record<string, unknown>;

/** apiPath → the `(required) = true` fields of the Create request it posts to. */
const REQUIRED_BY_API_PATH: Record<string, string[]> = {
  // compute.v1 — an instance is project-scoped and placed in a zone.
  "/compute/v1/instances": ["project_id", "zone_id"],
};

/** Top-level keys a create can carry: the assembled body, plus what the form can fill. */
function expressibleKeys(specId: string): Set<string> {
  const spec = REGISTRY[specId];
  const seeded = applyFieldDefaults(spec.fields, asObj(spec.template({ projectId: "prj-1", accountId: "acc-1" })));
  const body = buildCreateBody(spec.sanitize ? spec.sanitize(seeded) : seeded);
  const keys = new Set(Object.keys(body));
  for (const f of spec.fields ?? []) {
    if (f.updateOnly) continue;
    keys.add(f.name.split(".")[0]);
  }
  return keys;
}

const createCapable = Object.entries(REGISTRY)
  .filter(([, spec]) => spec.ops.create)
  .map(([id, spec]) => [id, spec.apiPath] as const);

describe("every create-capable spec can express what Create requires", () => {
  it("offers Create for at least the resources this registry is meant to create", () => {
    // Guards the sweep against silently measuring nothing.
    expect(createCapable.length).toBe(1);
  });

  it.each(createCapable)("%s (%s)", (specId, apiPath) => {
    const required = REQUIRED_BY_API_PATH[apiPath];
    // No entry means nobody stated what this create needs — that is a gap in the
    // lock, not a pass.
    expect(required).toBeDefined();
    const keys = expressibleKeys(specId);
    expect(required!.filter((f) => !keys.has(f))).toEqual([]);
  });
});
