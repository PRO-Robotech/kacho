// Вторая копия формы машины обязана нести ту же возможность, что и первая.
//
// Форма машины объявлена ДВАЖДЫ — здесь и в `compute/src/lib/resource-registry.tsx`
// — и наборы полей у копий разные (здесь есть сетевые интерфейсы и hostname,
// там их нет). Разойтись они могут не только составом: правка, доехавшая до
// одной копии, оставляет второй путь пользователя без возможности, и разницу он
// читает как «другое место продукта».
//
// Поэтому лок стоит в ОБЕИХ копиях с одинаковыми утверждениями, а перепись
// «сколько всего копий» держит `scripts/check-instance-boot-chain-parity.mjs`:
// третья копия, заведённая завтра, становится находкой, а не третьим
// расхождением.
//
// Ground truth: proto/kacho/cloud/compute/v1/instance_service.proto —
// `guest_access_key_ids` (номер 39, `[(size) = "<=32"]`), `boot_source` (29).
// Сервер принимает в `boot_source.id` ровно `img-<base32>`:
// `validateBootSource` зовёт `corevalidate.ResourceID("Image", "img", …)`.

import { REGISTRY } from "./resource-registry";
import type { ArrayField, RefField } from "./form-schema";

const instances = REGISTRY["compute-instances"];
const fieldsByName = new Map((instances.fields ?? []).map((f) => [f.name, f]));

describe("форма машины (копия shared): ключи и образ выбираются, а не набираются", () => {
  it("guest_access_key_ids — список ключей проекта", () => {
    const f = fieldsByName.get("guest_access_key_ids") as ArrayField | undefined;
    expect(f).toBeDefined();
    expect(f!.type).toBe("array");
    const inner = f!.itemFields[0] as RefField;
    expect(inner.type).toBe("ref");
    expect(inner.refResource).toBe("guest-access-keys");
    expect(inner.refProjectScoped).toBe(true);
    expect(f!.maxItems).toBe(32);
  });

  it("boot_source.id — список образов, а не свободная строка", () => {
    const f = fieldsByName.get("boot_source.id") as RefField | undefined;
    expect(f).toBeDefined();
    expect(f!.type).toBe("ref");
    expect(f!.refResource).toBe("images");
    expect(f!.refProjectScoped).toBe(true);
    expect(f!.visibleWhen).toEqual({ field: "boot_source.type", equals: "storage.image" });
  });

  it("образы и ключи объявлены целями ссылки — иначе списки не из чего собрать", () => {
    expect(REGISTRY["images"]?.apiPath).toBe("/storage/v1/images");
    expect(REGISTRY["images"]?.payloadKey).toBe("images");
    expect(REGISTRY["guest-access-keys"]?.apiPath).toBe("/compute/v1/guestAccessKeys");
    expect(REGISTRY["guest-access-keys"]?.payloadKey).toBe("guest_access_keys");
  });

  it("ключи уезжают на провод плоским списком идентификаторов", () => {
    const out = instances.sanitize!({
      instance_kind: "VM",
      machine_type_id: "mt-std2",
      boot_source: { type: "storage.image", id: "img-9k2m4x7q1n8p" },
      use_default_network: true,
      guest_access_key_ids: [{ value: "gak-1" }, { value: "gak-2" }],
    });
    expect(out.guest_access_key_ids).toEqual(["gak-1", "gak-2"]);
  });

  it("ветка OCI отвергается словами до отправки, storage-ветка проходит", () => {
    // Отрицание: сервер эту ветку не принимает — `validateBootSource` отвечает
    // «bootSource.type registry.image is not accepted yet: a registry image has
    // no durable address today».
    const refused = instances.validate!({ boot_source: { type: "registry.image", id: "" } });
    expect(refused).toContain("registry.image");
    // Положительный контроль: законная ветка проходит.
    expect(instances.validate!({ boot_source: { type: "storage.image", id: "img-9k2m4x7q1n8p" } })).toBeNull();
  });

  it("пустой список ключей не уезжает вовсе", () => {
    const out = instances.sanitize!({
      instance_kind: "VM",
      machine_type_id: "mt-std2",
      boot_source: { type: "storage.image", id: "img-9k2m4x7q1n8p" },
      use_default_network: true,
      guest_access_key_ids: [{ value: "" }],
    });
    expect(out.guest_access_key_ids).toBeUndefined();
  });
});
