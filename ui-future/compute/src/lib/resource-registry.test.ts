import { REGISTRY, resourceProjectPath } from "./resource-registry";

describe("compute resource-registry (COMP-1 redesign)", () => {
  it("compute-instances зарегистрирован с верным apiPath и project-scope", () => {
    expect(REGISTRY["compute-instances"].apiPath).toBe("/compute/v1/instances");
    expect(REGISTRY["compute-instances"].scope).toBe("project");
  });

  it("machine-types — read-only cluster-scoped каталог (нет create/update/delete)", () => {
    expect(REGISTRY["machine-types"].apiPath).toBe("/compute/v1/machineTypes");
    expect(REGISTRY["machine-types"].scope).toBe("global");
    expect(REGISTRY["machine-types"].ops).toEqual({ create: false, update: false, delete: false });
  });

  it("VM sanitize: оставляет vm_spec, режет container_spec, ssh textarea → string[], boot_source до {type,id}", () => {
    const out = REGISTRY["compute-instances"].sanitize!({
      instance_kind: "VM",
      machine_type_id: "mt-std2",
      boot_source: { type: "storage.image", id: "img-x:22.04", image_kind: "STORAGE_IMAGE", name: "should-drop" },
      vm_spec: { user_data: "#cloud-config", metadata_options: { metadata_endpoint: "ENABLED" } },
      container_spec: { restart_policy: "NEVER", working_dir: "" },
      ssh_public_keys: "ssh-ed25519 AAA one@host\n  ssh-ed25519 BBB two@host  \n",
      service_account_id: "",
    });
    expect(out.container_spec).toBeUndefined();
    expect(out.vm_spec).toEqual({ user_data: "#cloud-config", metadata_options: { metadata_endpoint: "ENABLED" } });
    expect(out.boot_source).toEqual({ type: "storage.image", id: "img-x:22.04" });
    expect(out.ssh_public_keys).toEqual(["ssh-ed25519 AAA one@host", "ssh-ed25519 BBB two@host"]);
    // Пустой service_account_id не уходит на wire.
    expect(out.service_account_id).toBeUndefined();
  });

  it("CONTAINER sanitize: оставляет container_spec, режет vm_spec/ssh, пустой working_dir не шлёт", () => {
    const out = REGISTRY["compute-instances"].sanitize!({
      instance_kind: "CONTAINER",
      machine_type_id: "mt-gpu1",
      boot_source: { type: "registry.image", id: "ml/bert:cu121" },
      vm_spec: { user_data: "x" },
      container_spec: { restart_policy: "ON_FAILURE", working_dir: "" },
      ssh_public_keys: "ssh-ed25519 AAA one@host",
      assign_external_address: true,
    });
    expect(out.vm_spec).toBeUndefined();
    expect(out.ssh_public_keys).toBeUndefined();
    expect(out.assign_external_address).toBeUndefined();
    expect(out.container_spec).toEqual({ restart_policy: "ON_FAILURE" });
  });

  it("hydrate: service_account (Referrer) → service_account_id для edit-формы", () => {
    const out = REGISTRY["compute-instances"].hydrate!({
      id: "ins-1",
      service_account: { type: "iam.service_account", id: "sva-42", name: "ci" },
    });
    expect(out.service_account_id).toBe("sva-42");
  });

  it("resourceProjectPath строит compute-scoped SPA-путь", () => {
    expect(resourceProjectPath("compute-instances", "proj-1")).toBe("/projects/proj-1/compute/instances");
    expect(resourceProjectPath("machine-types", "proj-1")).toBe("/projects/proj-1/compute/machine-types");
  });
});
