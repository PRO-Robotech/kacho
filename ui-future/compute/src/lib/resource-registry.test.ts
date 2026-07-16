import { REGISTRY, resourceProjectPath } from "./resource-registry";

describe("compute resource-registry", () => {
  it("compute-instances зарегистрирован с верным apiPath и project-scope", () => {
    expect(REGISTRY["compute-instances"].apiPath).toBe("/compute/v1/instances");
    expect(REGISTRY["compute-instances"].scope).toBe("project");
  });

  it("instance sanitize переводит memory_gib → resources_spec.memory (байты) и чистит пустой image", () => {
    const out = REGISTRY["compute-instances"].sanitize!({
      memory_gib: 2,
      image: "",
      resources_spec: { cores: 2, cpu_guarantee_percent: 0 },
    });
    const rs = out.resources_spec as Record<string, unknown>;
    expect(rs.memory).toBe(String(2 * 1024 * 1024 * 1024));
    expect(rs.cores).toBe(2);
    expect(out.memory_gib).toBeUndefined();
    expect(out.image).toBeUndefined();
  });

  it("resourceProjectPath строит compute-scoped SPA-путь", () => {
    expect(resourceProjectPath("compute-instances", "proj-1")).toBe("/projects/proj-1/compute/instances");
  });
});
