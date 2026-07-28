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

import { REGISTRY, applyFieldDefaults } from "./resource-registry";
import { buildCreateBody } from "./update-mask";

const asObj = (v: unknown) => v as Record<string, unknown>;

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
  "/compute/v1/disks": ["project_id", "zone_id", "size"],
  "/compute/v1/images": ["project_id"],
  "/compute/v1/snapshots": ["project_id", "disk_id"],
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
    expect(createCapable.length).toBeGreaterThanOrEqual(20);
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
