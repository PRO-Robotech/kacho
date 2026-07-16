import { REGISTRY, resourceProjectPath } from "./resource-registry";

describe("storage resource-registry", () => {
  it("volumes / snapshots / disk-types зарегистрированы с верными apiPath", () => {
    expect(REGISTRY.volumes.apiPath).toBe("/storage/v1/volumes");
    expect(REGISTRY.snapshots.apiPath).toBe("/storage/v1/snapshots");
    expect(REGISTRY["disk-types"].apiPath).toBe("/storage/v1/diskTypes");
  });

  it("disk-types — read-only (нет create/update/delete)", () => {
    expect(REGISTRY["disk-types"].ops).toEqual({ create: false, update: false, delete: false });
  });

  it("volume sanitize переводит size_gib (ГиБ) → size_bytes (байты) и чистит пустой снимок", () => {
    const out = REGISTRY.volumes.sanitize!({ size_gib: 10, source_snapshot_id: "", name: "v" });
    expect(out.size_bytes).toBe(String(10 * 1024 * 1024 * 1024));
    expect(out.size_gib).toBeUndefined();
    expect(out.source_snapshot_id).toBeUndefined();
  });

  it("resourceProjectPath строит storage-scoped SPA-путь", () => {
    expect(resourceProjectPath("volumes", "proj-1")).toBe("/projects/proj-1/storage/volumes");
    expect(resourceProjectPath("volumes", null)).toBeNull();
  });
});
