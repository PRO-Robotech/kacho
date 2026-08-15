// Источник тома — ветвление, а не три всегда-видимых поля.
//
// Ground truth: proto/kacho/cloud/storage/v1/volume_service.proto,
// CreateVolumeRequest. Поля 9 и 10 — source_snapshot_id и source_image_id — оба
// необязательные и оба помечены «Mutually exclusive» комментарием контракта:
// пусто в обоих означает чистый том. То есть у источника РОВНО три исхода, и
// форма обязана выражать выбор между ними, а не предлагать заполнить два поля,
// из которых сервер примет одно.
//
// Почему это не косметика: без поля образа из свежего проекта нельзя получить
// загрузочный том вовсе. Том создаётся пустым либо из снимка, снимок — из тома,
// образ — из тома или снимка; круг замкнут, и пользователь видит не отказ, а
// форму, в которой нечего выбрать.
//
// Образец ветвления взят у соседнего ресурса того же реестра: `images` уже
// выражает «снимок XOR том» дискриминатором `_source_kind` + `visibleWhen`.
// Здесь исходов три, а не два — добавляется «пустой том».

import { REGISTRY } from "./resource-registry";
import type { RefField } from "@shared/lib/form-schema";

const volumes = REGISTRY["volumes"];
const fieldsByName = new Map((volumes.fields ?? []).map((f) => [f.name, f]));

describe("том: источник выбирается ветвлением, а не тремя полями", () => {
  it("образ объявлен полем формы — списком, не строкой", () => {
    const f = fieldsByName.get("source_image_id");
    expect(f).toBeDefined();
    expect(f!.type).toBe("ref");
    expect((f as RefField).refResource).toBe("images");
    // Образы проекта, а не чужие: без сужения список показал бы образы, из
    // которых том всё равно не создастся.
    expect((f as RefField).refProjectScoped).toBe(true);
  });

  it("обе ветки источника скрыты за одним дискриминатором и не видны одновременно", () => {
    const snap = fieldsByName.get("source_snapshot_id");
    const img = fieldsByName.get("source_image_id");
    expect(snap?.visibleWhen).toEqual({ field: "_source_kind", equals: "snapshot" });
    expect(img?.visibleWhen).toEqual({ field: "_source_kind", equals: "image" });

    const kind = fieldsByName.get("_source_kind");
    expect(kind?.type).toBe("enum");
    // Ровно три исхода — столько же, сколько у контракта.
    expect(kind && "options" in kind ? kind.options.map((o) => o.value) : []).toEqual(["empty", "snapshot", "image"]);
    // По умолчанию — пустой том: это единственная ветка, которой не нужен
    // предмет в проекте, поэтому она и открывается первой.
    expect(kind && "default" in kind ? kind.default : undefined).toBe("empty");
  });

  it("на проводе остаётся ровно одна ветка, а дискриминатор не уезжает вовсе", () => {
    const base = { project_id: "prj-1", zone_id: "z", disk_type_id: "dt", size_gib: 10 };

    const empty = volumes.sanitize!({ ...base, _source_kind: "empty", source_snapshot_id: "", source_image_id: "" });
    expect(empty).not.toHaveProperty("_source_kind");
    expect(empty).not.toHaveProperty("source_snapshot_id");
    expect(empty).not.toHaveProperty("source_image_id");

    // Ветка «из снимка» не уносит с собой значение, набранное в другой ветке:
    // пользователь мог переключить дискриминатор уже после выбора образа.
    const fromSnap = volumes.sanitize!({
      ...base,
      _source_kind: "snapshot",
      source_snapshot_id: "snp-1",
      source_image_id: "img-1",
    });
    expect(fromSnap.source_snapshot_id).toBe("snp-1");
    expect(fromSnap).not.toHaveProperty("source_image_id");

    const fromImg = volumes.sanitize!({
      ...base,
      _source_kind: "image",
      source_snapshot_id: "snp-1",
      source_image_id: "img-1",
    });
    expect(fromImg.source_image_id).toBe("img-1");
    expect(fromImg).not.toHaveProperty("source_snapshot_id");
  });

  it("отказ до отправки — только в ветке, у которой есть предмет", () => {
    // Отрицание.
    expect(volumes.validate!({ _source_kind: "image", source_image_id: "" })).toBe(
      "Выберите образ, из которого создаётся том.",
    );
    expect(volumes.validate!({ _source_kind: "snapshot", source_snapshot_id: "" })).toBe(
      "Выберите снимок, из которого восстанавливается том.",
    );
    // Положительный контроль в обе стороны: выбранный предмет проходит, и
    // «пустой том» проходит БЕЗ выбора — иначе отрицание зеленело бы на
    // проверке, которая просто всегда отказывает.
    expect(volumes.validate!({ _source_kind: "image", source_image_id: "img-1" })).toBeNull();
    expect(volumes.validate!({ _source_kind: "snapshot", source_snapshot_id: "snp-1" })).toBeNull();
    expect(volumes.validate!({ _source_kind: "empty" })).toBeNull();
  });

  it("шаблон формы открывается на ветке, у которой нет предусловий", () => {
    const t = volumes.template({ projectId: "prj-1" }) as Record<string, unknown>;
    expect(t._source_kind).toBe("empty");
    expect(t.source_image_id).toBe("");
  });
});
