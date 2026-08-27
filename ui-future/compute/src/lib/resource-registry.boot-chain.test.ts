// Путь пустого проекта: образ → том → машина, ни на одном шаге не набирая
// идентификатор руками.
//
// Ground truth — три контракта:
//   proto/kacho/cloud/compute/v1/guest_access_key.proto        (ресурс ключа)
//   proto/kacho/cloud/compute/v1/guest_access_key_service.proto (/compute/v1/guestAccessKeys)
//   proto/kacho/cloud/compute/v1/instance_service.proto         (CreateInstanceRequest)
//
// Что здесь закрепляется и почему именно это:
//
//  1. Ключ входа в гостя — РЕСУРС. Контракт объясняет это прямым текстом: ключ,
//     переданный полем, живёт ровно столько, сколько машина, и его нельзя ни
//     отозвать, ни заменить, ни узнать, где ещё он используется. Сервер есть,
//     провайдер инфраструктуры его знает — консоль не знала вовсе.
//
//  2. `guest_access_key_ids` — ссылки по идентификатору, значит СПИСОК. Набор
//     ключей руками означает, что арендатор идёт за идентификатором в другое
//     место, которого в консоли до этой правки не было.
//
//  3. `boot_source.id` — тоже список. Сервер принимает ровно `img-<base32>` и
//     ничего больше: `validateBootSource` зовёт `corevalidate.ResourceID("Image",
//     "img", …)`. Свободная строка здесь означала «набери идентификатор образа
//     по памяти», а её подсказка предлагала две формы, которые сервер отвергает.

import { REGISTRY } from "./resource-registry";
import { COMPUTE_NAVIGATION } from "@/navigation";
import type { ArrayField, RefField } from "@shared/lib/form-schema";

const instances = REGISTRY["compute-instances"];
const instanceFields = new Map((instances.fields ?? []).map((f) => [f.name, f]));

describe("ключ входа в гостя — ресурс консоли, а не поле машины", () => {
  it("объявлен с адресом, ключом полезной нагрузки и полным набором действий", () => {
    const gak = REGISTRY["guest-access-keys"];
    expect(gak).toBeDefined();
    expect(gak.apiPath).toBe("/compute/v1/guestAccessKeys");
    expect(gak.payloadKey).toBe("guest_access_keys");
    expect(gak.scope).toBe("project");
    expect(gak.ops).toEqual({ create: true, update: true, delete: true });
  });

  it("форма создания несёт ровно то, что принимает CreateGuestAccessKeyRequest", () => {
    const names = (REGISTRY["guest-access-keys"].fields ?? []).map((f) => f.name);
    // project_id / name / public_key / labels — весь входной контракт.
    expect(names).toEqual(expect.arrayContaining(["project_id", "name", "public_key", "labels"]));
    // Отпечаток считаем НЕ мы: он output-only, и поле формы для него означало бы,
    // что мы верим чужому счёту.
    expect(names).not.toContain("fingerprint");
  });

  it("виден в навигации — иначе ресурс есть, а дойти до него нельзя", () => {
    const paths = COMPUTE_NAVIGATION.flatMap((s) => s.items.map((i) => i.path));
    expect(paths).toContain("compute/guest-access-keys");
  });
});

describe("форма машины: ключи и образ выбираются, а не набираются", () => {
  it("guest_access_key_ids — список ключей проекта", () => {
    const f = instanceFields.get("guest_access_key_ids") as ArrayField | undefined;
    expect(f).toBeDefined();
    expect(f!.type).toBe("array");
    const inner = f!.itemFields[0] as RefField;
    expect(inner.type).toBe("ref");
    expect(inner.refResource).toBe("guest-access-keys");
    expect(inner.refProjectScoped).toBe(true);
    // Предел держит константа домена MaxGuestAccessKeysPerInstance = 32
    // (services/compute/internal/domain/constants.go); контракт называет её прозой.
    expect(f!.maxItems).toBe(32);
  });

  it("boot_source.id — список образов, а не свободная строка", () => {
    const f = instanceFields.get("boot_source.id") as RefField | undefined;
    expect(f).toBeDefined();
    expect(f!.type).toBe("ref");
    expect(f!.refResource).toBe("images");
    expect(f!.refProjectScoped).toBe(true);
    // Список образов имеет смысл только для источника storage.image: OCI-ветка
    // адресуется парой (реестр, имя), а не идентификатором образа хранилища.
    expect(f!.visibleWhen).toEqual({ field: "boot_source.type", equals: "storage.image" });
  });

  it("образы объявлены целью ссылки — иначе список не из чего собрать", () => {
    const images = REGISTRY["images"];
    expect(images).toBeDefined();
    expect(images.apiPath).toBe("/storage/v1/images");
    expect(images.payloadKey).toBe("images");
    // Здесь это ТОЛЬКО цель ссылки: CRUD образа живёт в своём домене.
    expect(images.ops).toEqual({ create: false, update: false, delete: false });
  });

  it("ключи уезжают на провод плоским списком идентификаторов", () => {
    const out = instances.sanitize!({
      instance_kind: "VM",
      machine_type_id: "mt-std2",
      boot_source: { type: "storage.image", id: "img-9k2m4x7q1n8p" },
      guest_access_key_ids: [{ value: "gak-1" }, { value: "gak-2" }],
    });
    expect(out.guest_access_key_ids).toEqual(["gak-1", "gak-2"]);
  });

  it("ветка OCI отвергается словами до отправки, storage-ветка проходит", () => {
    // Отрицание: сервер эту ветку не принимает — `validateBootSource` отвечает
    // «bootSource.type registry.image is not accepted yet: a registry image has
    // no durable address today». Форма обязана сказать это словами, а не слать
    // запрос, который не может пройти.
    const refused = instances.validate!({ boot_source: { type: "registry.image", id: "" } });
    expect(refused).toContain("registry.image");
    // Положительный контроль: законная ветка проходит. Без него отрицание
    // зеленело бы на проверке, которая просто всегда отказывает.
    expect(instances.validate!({ boot_source: { type: "storage.image", id: "img-9k2m4x7q1n8p" } })).toBeNull();
  });

  it("пустой список ключей не уезжает вовсе", () => {
    const out = instances.sanitize!({
      instance_kind: "VM",
      machine_type_id: "mt-std2",
      boot_source: { type: "storage.image", id: "img-9k2m4x7q1n8p" },
      guest_access_key_ids: [{ value: "" }],
    });
    expect(out.guest_access_key_ids).toBeUndefined();
  });
});
