import { REGISTRY, resourceProjectPath } from "./resource-registry";

describe("storage resource-registry", () => {
  it("volumes / snapshots / images / disk-types зарегистрированы с верными apiPath", () => {
    expect(REGISTRY.volumes.apiPath).toBe("/storage/v1/volumes");
    expect(REGISTRY.snapshots.apiPath).toBe("/storage/v1/snapshots");
    expect(REGISTRY.images.apiPath).toBe("/storage/v1/images");
    expect(REGISTRY["disk-types"].apiPath).toBe("/storage/v1/diskTypes");
  });

  it("image sanitize (STOR-1): source XOR — snapshot-kind шлёт снимок и режет том, form-only _source_kind срезан", () => {
    const snapOut = REGISTRY.images.sanitize!({
      _source_kind: "snapshot",
      region_id: "ru-1",
      source_snapshot_id: "snp-1",
      source_volume_id: "vol-should-drop",
    });
    expect(snapOut._source_kind).toBeUndefined();
    expect(snapOut.source_snapshot_id).toBe("snp-1");
    expect(snapOut.source_volume_id).toBeUndefined();

    const volOut = REGISTRY.images.sanitize!({
      _source_kind: "volume",
      region_id: "ru-1",
      source_snapshot_id: "snp-should-drop",
      source_volume_id: "vol-1",
    });
    expect(volOut.source_volume_id).toBe("vol-1");
    expect(volOut.source_snapshot_id).toBeUndefined();
  });

  it("image validate: пустой активный источник → ошибка; заполненный → null", () => {
    expect(REGISTRY.images.validate!({ _source_kind: "snapshot", source_snapshot_id: "" })).toMatch(/источник/i);
    expect(REGISTRY.images.validate!({ _source_kind: "volume", source_volume_id: "vol-1" })).toBeNull();
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
