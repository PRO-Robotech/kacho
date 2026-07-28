// COMP-1 contract lock for the compute-instances spec served by the vpc remote.
//
// Ground truth: proto/kacho/cloud/compute/v1/instance_service.proto —
// CreateInstanceRequest RESERVES 6/7/9 with the names platform_id,
// resources_spec, boot_disk_spec (and secondary_disk_specs). They are not
// deprecated-but-tolerated, they are gone; the single sizing channel is
// machine_type_id and the single OS entry is boot_source{type,id}, gated by
// instance_kind (VM XOR CONTAINER). UpdateInstanceRequest reserves the same
// names again and its mutable set is name/description/labels/service_account_id
// plus the STOPPED-gated machine_type_id/cpu_guarantee_percent.

import { REGISTRY } from "./resource-registry";
import { computeUpdateMask } from "./update-mask";

const asObj = (v: unknown) => v as Record<string, unknown>;

// Every name RESERVED on CreateInstanceRequest / UpdateInstanceRequest.
const RETIRED = [
  "platform_id",
  "resources_spec",
  "boot_disk_spec",
  "secondary_disk_specs",
  "scheduling_policy",
  "placement_policy",
  "metadata_options",
  "gpu_settings",
  "reserved_instance_pool_id",
  "application",
  "image",
];

function keysDeep(v: unknown, prefix = ""): string[] {
  if (Array.isArray(v)) return v.flatMap((it) => keysDeep(it, prefix));
  if (v && typeof v === "object") {
    return Object.entries(v as Record<string, unknown>).flatMap(([k, vv]) => [
      prefix ? `${prefix}.${k}` : k,
      ...keysDeep(vv, prefix ? `${prefix}.${k}` : k),
    ]);
  }
  return [];
}

describe("compute-instances registry contract (COMP-1)", () => {
  const spec = REGISTRY["compute-instances"];

  it("templates the current create channels, not the retired ones", () => {
    const t = asObj(spec.template({ projectId: "prj-1" }));
    expect(t.instance_kind).toBe("VM");
    expect(t).toHaveProperty("machine_type_id");
    expect(asObj(t.boot_source)).toHaveProperty("type");
    expect(asObj(t.boot_source)).toHaveProperty("id");
    for (const retired of RETIRED) {
      expect(t).not.toHaveProperty(retired);
    }
  });

  it("declares no form field addressing a retired request field", () => {
    const names = (spec.fields ?? []).map((f) => f.name);
    for (const name of names) {
      const head = name.replace(/^_/, "").split(".")[0];
      expect(RETIRED).not.toContain(head);
    }
    expect(names).toContain("machine_type_id");
    expect(names).toContain("instance_kind");
  });

  it("sanitize emits no retired field and no form-only key for a VM", () => {
    const body = spec.sanitize!({
      ...asObj(spec.template({ projectId: "prj-1" })),
      name: "vm-1",
      zone_id: "ru-central1-a",
      machine_type_id: "mt-standard-2",
      boot_source: { type: "storage.image", id: "img-x:22.04", name: "Ubuntu", resolved_digest: "sha256:…" },
      ssh_public_keys: "ssh-ed25519 AAAA user@host\n\n",
    });
    const all = keysDeep(body);
    for (const retired of RETIRED) {
      expect(all).not.toContain(retired);
    }
    expect(all.filter((k) => k.split(".").pop()!.startsWith("_"))).toEqual([]);
    // boot_source is narrowed to the two input fields — name/resolved_digest are
    // output-only and rejected on input.
    expect(body.boot_source).toEqual({ type: "storage.image", id: "img-x:22.04" });
    expect(body.ssh_public_keys).toEqual(["ssh-ed25519 AAAA user@host"]);
    expect(body).not.toHaveProperty("container_spec");
  });

  it("keeps exactly the kind-matching spec branch for a CONTAINER", () => {
    const body = spec.sanitize!({
      ...asObj(spec.template({ projectId: "prj-1" })),
      instance_kind: "CONTAINER",
      machine_type_id: "mt-standard-2",
      boot_source: { type: "registry.image", id: "ml/bert:cu121" },
    });
    expect(body).toHaveProperty("container_spec");
    expect(body).not.toHaveProperty("vm_spec");
    expect(body).not.toHaveProperty("ssh_public_keys");
  });

  it("masks only fields UpdateInstanceRequest still carries", () => {
    const fields = spec.fields ?? [];
    const before = { name: "a", description: "a", machine_type_id: "mt-1", cpu_guarantee_percent: 0 };
    const after = { name: "b", description: "b", machine_type_id: "mt-2", cpu_guarantee_percent: 50 };
    const mask = computeUpdateMask(before, after, fields);
    const mutable = [
      "name",
      "description",
      "labels",
      "service_account_id",
      "machine_type_id",
      "cpu_guarantee_percent",
      "placement_group_id",
      "ssh_public_keys",
      "vm_spec",
      "metadata",
    ];
    for (const path of mask) {
      expect(mutable).toContain(path.split(".")[0]);
    }
    expect(mask).toContain("machine_type_id");
  });
});
