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
import { setByPath } from "./path";
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
    });
    const all = keysDeep(body);
    for (const retired of RETIRED) {
      expect(all).not.toContain(retired);
    }
    expect(all.filter((k) => k.split(".").pop()!.startsWith("_"))).toEqual([]);
    // boot_source is narrowed to the two input fields — name/resolved_digest are
    // output-only and rejected on input.
    expect(body.boot_source).toEqual({ type: "storage.image", id: "img-x:22.04" });
    // sshPublicKeys снят с формы вместе с приёмом на сервере.
    expect(body).not.toHaveProperty("ssh_public_keys");
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

  it("can express the network channel the server demands, either way", () => {
    // Create requires networkInterfaceSpecs OR useDefaultNetwork. sanitize drops
    // NIC items with neither subnet nor nic_id (they are not a
    // NetworkInterfaceSpec), so a form that cannot also set useDefaultNetwork
    // leaves the caller with a refusal and no way to satisfy it.
    const names = (spec.fields ?? []).map((f) => f.name);
    expect(names).toContain("use_default_network");
    expect(names).toContain("network_interface_specs");

    const base = asObj(spec.template({ projectId: "prj-1" }));
    const untouched = spec.sanitize!({ ...base, machine_type_id: "mt-1", use_default_network: true });
    expect(untouched).not.toHaveProperty("network_interface_specs");
    expect(untouched.use_default_network).toBe(true);

    const configured = spec.sanitize!({
      ...base,
      machine_type_id: "mt-1",
      network_interface_specs: [
        { _ext_mode: "auto", _use_existing_nic: false, subnet_id: "sub-1", security_group_ids: [{ value: "sg-1" }] },
      ],
    });
    expect(configured.network_interface_specs).toEqual([
      { subnet_id: "sub-1", security_group_ids: ["sg-1"], primary_v4_address_spec: { one_to_one_nat_spec: { ip_version: "IPV4" } } },
    ]);
    // Explicit specs and the project default are alternatives, never both.
    expect(configured).not.toHaveProperty("use_default_network");
    expect(keysDeep(configured).filter((k) => k.split(".").pop()!.startsWith("_"))).toEqual([]);
  });

  it("masks only fields UpdateInstanceRequest still carries", () => {
    // Drive EVERY declared field to a changed value, so the mask is the full set
    // the form can ever emit — not a hand-picked subset that could not fail.
    const fields = spec.fields ?? [];
    let before: Record<string, unknown> = {};
    let after: Record<string, unknown> = {};
    for (const f of fields) {
      before = setByPath(before, f.name, "before");
      after = setByPath(after, f.name, "after");
    }
    const mask = computeUpdateMask(before, after, fields);

    // The fields UpdateInstanceRequest still ACCEPTS, minus instance_id/update_mask.
    // The legacy ones (metadata, network_settings, maintenance_*, serial_port_settings,
    // ssh_public_keys) are refused by the server with the field named, so a form that
    // could ever emit them would hand the operator a 400 — they are not "mutable".
    const mutable = new Set([
      "name",
      "description",
      "labels",
      "service_account_id",
      "machine_type_id",
      "cpu_guarantee_percent",
      "placement_group_id",
      "vm_spec",
    ]);
    expect(mask.filter((path) => !mutable.has(path.split(".")[0]))).toEqual([]);
    // And the immutable / create-only channels stay out of it entirely.
    for (const excluded of ["zone_id", "instance_kind", "boot_source", "container_spec", "use_default_network"]) {
      expect(mask.filter((p) => p.split(".")[0] === excluded)).toEqual([]);
    }
    expect(mask).toContain("machine_type_id");
    expect(mask).toContain("cpu_guarantee_percent");
  });
});
