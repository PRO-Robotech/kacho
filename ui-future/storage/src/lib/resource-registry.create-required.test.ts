// Every spec that offers Create must be able to express what Create requires.
//
// A required field the form has no way to carry is not a cosmetic gap: the call
// is refused, so the resource cannot be created from the console at all — and the
// button that offers it is a promise the product does not keep. This is the
// counterpart of resource-registry.request-fields.test.ts, which locks the other
// direction (a field the form sends that the message does not declare).
//
// Ground truth: the fields WITHOUT WHICH THE EDGE REFUSES the Create call, cited
// per entry below. A spec is measured by the inputs its form declares — see
// operatorSettableFields for why that, and not the assembled body, is the thing
// to measure.
//
// The source changed in kacho#1255 and the change is not cosmetic. This used to
// read `the (required) = true set of each Create*Request` — an option of the
// field-constraint extension family, which has now been retired from the
// contracts in full (the file declaring it is gone, so the option no longer
// compiles): it had no enforcer anywhere on the request path, so it constrained
// nothing while looking like a guarantee, and on two credential-issue fields it
// declared the exact OPPOSITE of what the edge does. Refusal is the only source
// that cannot silently diverge from behaviour.
//
// The entries themselves did not move: each was adjudicated against the refusing
// code, and the sets came out the same. What changed is what they are answerable
// to.
//
// The table is exhaustive by construction: a create-capable spec with no entry
// fails, so adding one to the registry forces its contract to be stated here.
//
// This remote carries its own copy of the registry (there is no @shared alias
// here), so the lock lives here too — a fix landing in one copy and not the
// other is the very class this sweep exists to catch.

import { REGISTRY } from "./resource-registry";

/** apiPath → the fields without which the edge refuses the Create call. */
const REQUIRED_BY_API_PATH: Record<string, string[]> = {
  // storage.v1 — CreateVolumeRequest requires project_id, zone_id and disk_type_id;
  // a snapshot names the volume it is taken from; an image is regional.
  "/storage/v1/volumes": ["project_id", "zone_id", "disk_type_id"],
  "/storage/v1/snapshots": ["project_id", "source_volume_id"],
  "/storage/v1/images": ["project_id", "region_id"],
};

/**
 * The Create inputs one spec puts in front of the operator.
 *
 * ONLY declared form fields count. A key that merely turns up in the assembled
 * body does not, and the difference is the whole point: `template` can hardcode a
 * value and `sanitize` can synthesise one key from another, and in both cases the
 * operator has no input for it — what ships is a constant the product chose, not
 * something the form expresses. Measuring the body is how this lock was lost
 * once: deleting the target-group port field left `template` seeding `port: 80`,
 * so the body still carried the key, the sweep stayed green, and the form had no
 * way to set the backend port at all.
 *
 * `hidden` fields DO count — they are filled from the caller's context
 * (`template({projectId, accountId})`), which the operator chooses in the project
 * switcher. project_id / account_id are the only hidden names any entry below
 * relies on. `updateOnly` fields do NOT — they are absent from Create entirely.
 *
 * A dotted name covers its head: declaring `health_check.tcp.port` is a way to
 * fill `health_check`.
 */
function operatorSettableFields(specId: string): Set<string> {
  const spec = REGISTRY[specId];
  return new Set((spec.fields ?? []).filter((f) => !f.updateOnly).map((f) => f.name.split(".")[0]));
}

const createCapable = Object.entries(REGISTRY)
  .filter(([, spec]) => spec.ops.create)
  .map(([id, spec]) => [id, spec.apiPath] as const);

describe("every create-capable spec can express what Create requires", () => {
  it("offers Create for at least the resources this registry is meant to create", () => {
    // Guards the sweep against silently measuring nothing.
    expect(createCapable.length).toBe(3);
  });

  it.each(createCapable)("%s (%s)", (specId, apiPath) => {
    const required = REQUIRED_BY_API_PATH[apiPath];
    // No entry means nobody stated what this create needs — that is a gap in the
    // lock, not a pass.
    expect(required).toBeDefined();
    const settable = operatorSettableFields(specId);
    expect(required!.filter((f) => !settable.has(f))).toEqual([]);
  });
});
