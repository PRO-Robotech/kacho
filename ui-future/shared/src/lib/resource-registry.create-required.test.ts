// Every spec that offers Create must be able to express what Create requires.
//
// A required field the form has no way to carry is not a cosmetic gap: the call
// is refused, so the resource cannot be created from the console at all — and the
// button that offers it is a promise the product does not keep. This is the
// counterpart of resource-registry.request-fields.test.ts, which locks the other
// direction (a field the form sends that the message does not declare).
//
// Ground truth: the `(required) = true` set of each Create*Request under
// proto/kacho/cloud/**, cited per entry below. A spec is measured by the inputs
// its form declares — see operatorSettableFields for why that, and not the
// assembled body, is the thing to measure.
//
// The table is exhaustive by construction: a create-capable spec with no entry
// fails, so adding one to the registry forces its contract to be stated here.

import { REGISTRY } from "./resource-registry";

/** apiPath → the `(required) = true` fields of the Create request it posts to. */
const REQUIRED_BY_API_PATH: Record<string, string[]> = {
  // iam.v1 — CreateAccountRequest{name}; Project/ServiceAccount/Group additionally
  // anchor on the account they live in; CreateRoleRequest marks only `name`.
  "/iam/v1/accounts": ["name"],
  "/iam/v1/projects": ["account_id", "name"],
  "/iam/v1/serviceAccounts": ["account_id", "name"],
  "/iam/v1/groups": ["account_id", "name"],
  "/iam/v1/roles": ["name"],
  // vpc.v1 — every resource is project-scoped; subnet/route-table/security-group
  // additionally name the network, and a NIC names its subnet.
  "/vpc/v1/networks": ["project_id"],
  "/vpc/v1/subnets": ["project_id", "network_id"],
  "/vpc/v1/addresses": ["project_id"],
  "/vpc/v1/routeTables": ["project_id", "network_id"],
  "/vpc/v1/networkInterfaces": ["project_id", "subnet_id"],
  "/vpc/v1/securityGroups": ["project_id", "network_id"],
  "/vpc/v1/gateways": ["project_id"],
  // InternalAddressPoolService.Create marks no field required.
  "/vpc/v1/addressPools": [],
  // compute.v1 — CreateDiskRequest{project_id,zone_id,size};
  // CreateImageRequest{project_id} (region_id is the STORAGE image, not this one);
  // CreateSnapshotRequest{project_id,disk_id}; CreateInstanceRequest{project_id,zone_id}.
  "/compute/v1/instances": ["project_id", "zone_id"],
  // geo.v1 InternalCatalogService — Create{Region,Zone}Request mark no field required.
  "/geo/v1/regions": [],
  "/geo/v1/zones": [],
  // loadbalancer.v1 — a listener inherits its project from the parent balancer;
  // a target group must name the port every target receives traffic on and the
  // single embedded health check.
  "/nlb/v1/networkLoadBalancers": ["project_id"],
  "/nlb/v1/listeners": ["load_balancer_id", "protocol"],
  "/nlb/v1/targetGroups": ["project_id", "health_check", "port"],
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
    //
    // Was 20 until the duplicate block-storage resources were retired: the disk,
    // image and snapshot specs addressing `/compute/v1/*` are gone, and the disk
    // type catalogue now addresses its owner. Lowered deliberately, with the
    // reason recorded — a floor nudged down to whatever the code currently
    // produces stops guarding anything, which is the point of stating why.
    expect(createCapable.length).toBeGreaterThanOrEqual(19);
  });

  it.each(createCapable)("%s (%s)", (specId, apiPath) => {
    const required = REQUIRED_BY_API_PATH[apiPath];
    // No entry means nobody stated what this create needs — that is a gap in the
    // lock, not a pass.
    expect(required).toBeDefined();
    const settable = operatorSettableFields(specId);
    expect(required.filter((f) => !settable.has(f))).toEqual([]);
  });
});
