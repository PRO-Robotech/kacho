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

// ─────────── новый контракт storage: снятые поля, ссылки, каталог ───────────

describe("поля, снятые с контракта, не читаются реестром", () => {
  it("ни одна колонка и ни одно поле не адресует block_size / performance_tier", () => {
    // Оба сняты с контракта вместе с номером И именем (`reserved`). Колонка,
    // привязанная к снятому полю, рисовала бы пустую ячейку вечно — ошибки нет,
    // вердикта нет ни у одного теста.
    const dead = ["block_size", "performance_tier"];
    const hits: string[] = [];
    for (const [id, spec] of Object.entries(REGISTRY)) {
      for (const col of spec.columns) {
        if (dead.includes(col.path)) hits.push(`${id}.columns[${col.path}]`);
      }
      for (const f of spec.fields ?? []) {
        if (dead.includes(f.name)) hits.push(`${id}.fields[${f.name}]`);
      }
    }
    // Координаты, а не счётчик.
    expect(hits).toEqual([]);
  });

  it("каталог типов дисков читает ярус и состояние обращения", () => {
    // Положительный контроль к отрицанию выше: без него проба зеленела бы и на
    // реестре, из которого выкинули весь каталог.
    const paths = REGISTRY["disk-types"].columns.map((c) => c.path);
    expect(paths).toEqual(expect.arrayContaining(["tier", "lifecycle", "zone_ids"]));
  });
});

describe("правило 2 — ссылка на чужой ресурс рисуется, а не печатается", () => {
  it.each([
    ["volumes", "zone_id"],
    ["volumes", "disk_type_id"],
    ["snapshots", "zone_id"],
    ["snapshots", "source_volume_id"],
    ["images", "region_id"],
  ])("%s.%s — колонка несёт render, а не format: text", (specId, path) => {
    const col = REGISTRY[specId].columns.find((c) => c.path === path);
    expect(col).toBeDefined();
    // `format: "text"` печатает идентификатор — то самое, что правило запрещает.
    expect(col!.format).toBeUndefined();
    expect(typeof col!.render).toBe("function");
  });

  it("зона у снимка — собственный якорь, показанный в списке", () => {
    // Копия переносит снимок в другую зону: без якоря в списке непонятно, какая
    // строка где лежит.
    expect(REGISTRY.snapshots.columns.map((c) => c.path)).toContain("zone_id");
  });
});

describe("resourceProjectPath — глобальный каталог живёт под /system/*", () => {
  it("зона и регион не прогоняются через project-scoped ветку", () => {
    // Иначе получается путь, которого нет: ссылка ведёт в никуда, а «назад» с
    // зоны уводит в чужой раздел.
    expect(resourceProjectPath("zones", "proj-1")).toBe("/system/zones");
    expect(resourceProjectPath("regions", "proj-1")).toBe("/system/regions");
  });

  it("каталог адресуется и без проекта в контексте", () => {
    // Измерения «проект» у него нет вовсе, поэтому требовать проект значило бы
    // не строить ссылку там, где проекта нет.
    expect(resourceProjectPath("zones", null)).toBe("/system/zones");
  });

  it("ресурс проекта остаётся project-scoped (положительный контроль)", () => {
    expect(resourceProjectPath("disk-types", "proj-1")).toBe("/projects/proj-1/storage/disk-types");
    expect(resourceProjectPath("volumes", "proj-1")).toBe("/projects/proj-1/storage/volumes");
    expect(resourceProjectPath("volumes", null)).toBeNull();
  });
});
