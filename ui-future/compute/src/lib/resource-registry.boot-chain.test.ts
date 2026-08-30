// Ключ входа в гостя — РЕСУРС консоли, а не поле машины.
//
// Ground truth — два контракта:
//   proto/kacho/cloud/compute/v1/guest_access_key.proto        (ресурс ключа)
//   proto/kacho/cloud/compute/v1/guest_access_key_service.proto (/compute/v1/guestAccessKeys)
//
// Контракт объясняет это прямым текстом: ключ, переданный полем, живёт ровно
// столько, сколько машина, и его нельзя ни отозвать, ни заменить, ни узнать, где
// ещё он используется. Сервер есть, провайдер инфраструктуры его знает — консоль
// не знала вовсе (#377).
//
// ЗДЕСЬ ОСТАЛОСЬ ТО, ЧТО ПРИНАДЛЕЖИТ ЭТОМУ МОДУЛЮ: запись ресурса в реестре,
// её форма создания и видимость в НАВИГАЦИИ раздела — последнее в общей суите
// не проверить, навигация у каждого раздела своя.
//
// Утверждения про форму машины (ключи и образ выбираются списком, ветка OCI
// отвергается словами, пустой список не уезжает) отсюда СНЯТЫ: они стояли
// второй копией того же лока при второй копии реестра. Реестр сведён к
// единственному на всю консоль (#406), копия формы осталась одна, и лок теперь
// один — `shared/src/lib/resource-registry.instance-boot-chain.test.ts`,
// исполняемый в том числе прогоном этого модуля. Перепись копий по-прежнему
// выводится из дерева гейтом `scripts/check-instance-boot-chain-parity.mjs`:
// вторая копия, заведённая завтра, станет находкой, а не вторым расхождением.

import { REGISTRY } from "./resource-registry";
import { COMPUTE_NAVIGATION } from "@/navigation";

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
