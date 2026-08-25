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
// kacho.cloud.validation family, which has now been retired from the contracts
// in full: it had no enforcer anywhere on the request path, so it constrained
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
// This remote carries its own registry — a compute-domain one, not a copy of the
// shared registry — so the lock lives here too: a fix landing in one registry and
// not the other is the very class this sweep exists to catch. (The @shared alias
// does resolve here, in the bundle and in jest alike; it is a shared *registry*
// that this remote does not use, not a missing alias.)

import { REGISTRY } from "./resource-registry";

/** apiPath → the fields without which the edge refuses the Create call. */
const REQUIRED_BY_API_PATH: Record<string, string[]> = {
  // compute.v1 — an instance is project-scoped and placed in a zone.
  "/compute/v1/instances": ["project_id", "zone_id"],
  // compute.v1 — the edge refuses CreateGuestAccessKey for NO field
  // annotation on any field. The ground truth for this row is therefore the
  // use-case that refuses the call, not the descriptor: services/compute
  // `guestaccesskey` rejects an absent project_id ("projectId is required"), a
  // name outside 1..63 ("name must be 1..63 characters") and an absent or
  // unparsable public_key ("publicKey is required …"). Citing the weaker source
  // would have let the form ship a create that cannot succeed.
  "/compute/v1/guestAccessKeys": ["project_id", "name", "public_key"],
  // CreatePlacementGroup is refused for no field: the use-case
  // refuses an empty name, an unset strategy and an unset anchor itself, with a
  // named field each time. The empty list is the contract as declared, not an
  // omission — and the form still offers all four inputs.
  "/compute/v1/placementGroups": [],
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
    // Instance + GuestAccessKey. Второй заведён вместе с цепочкой загрузки
    // (#377): ключ входа в гостя — ресурс со своим жизненным циклом, полем
    // машины его нельзя ни отозвать, ни заменить.
    // Третий — группа размещения (#368), заведена параллельной линией той же волны.
    // Число не «сколько получилось», а сколько раздел ОБЯЗАН предлагать: меньше —
    // значит один ресурс молча выпал из создания.
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
