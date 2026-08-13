import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { changeDiskTypeAllowed } from "./ChangeDiskTypeDialog";

/**
 * Действия-глаголы storage: предусловие смены типа диска и форма тел запросов.
 *
 * Путь каждого глагола сверяется с деревом proto отдельно — гейтом
 * `shared/src/test/console-verb-routes-exist.test.ts`, который резолвит голову
 * литерала в константу файла. Здесь проверяется то, чего он не видит: ПОЛЯ тела
 * и условие, при котором кнопка доступна.
 *
 * Тела читаются из исходника `api/resources.ts` статически, а не собираются
 * вызовом: вызов потребовал бы поднять клиент и сеть, а предмет утверждения —
 * имена полей, которые контракт объявил обязательными.
 */

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../../../../../..");
const resourcesSrc = readFileSync(path.resolve(here, "../../../api/resources.ts"), "utf8");
const protoDir = path.join(repoRoot, "proto/kacho/cloud/storage/v1");

/** Поля `(required) = true` одного сообщения запроса. */
function requiredFields(protoFile: string, message: string): string[] {
  const src = readFileSync(path.join(protoDir, protoFile), "utf8");
  const block = new RegExp(`message ${message} \\{([\\s\\S]*?)\\n\\}`).exec(src);
  if (block === null) return [];
  return [
    ...block[1].matchAll(/(?:^|\n)\s*(?:[\w.<>, ]+?)\s+([a-z_]+)\s*=\s*\d+\s*\[[^\]]*?\(required\)\s*=\s*true/g),
  ].map((m) => m[1]);
}

describe("объём осмотренного", () => {
  it("контракт и исходник вызовов прочитаны", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного».
    expect(resourcesSrc.length).toBeGreaterThan(500);
    expect(requiredFields("volume_service.proto", "ChangeDiskTypeRequest").length).toBeGreaterThan(0);
    expect(requiredFields("snapshot_service.proto", "CopySnapshotRequest").length).toBeGreaterThan(0);
    expect(requiredFields("image_service.proto", "CopyImageRequest").length).toBeGreaterThan(0);
    // Контроль извлекателя: необязательное поле не должно попадать в список.
    expect(requiredFields("snapshot_service.proto", "CopySnapshotRequest")).not.toContain("description");
  });
});

describe("смена типа диска — предусловие названо ДО отправки", () => {
  it("доступна там, где край её принимает", () => {
    // Перечень — предусловие RPC: из прочих состояний он отвечает
    // FAILED_PRECONDITION, и кнопка, отправляющая заведомый отказ, — обещание,
    // которого продукт не держит.
    expect(changeDiskTypeAllowed("AVAILABLE")).toBe(true);
    expect(changeDiskTypeAllowed("IN_USE")).toBe(true);
  });

  it("недоступна во всех прочих состояниях, включая уже идущий перенос", () => {
    expect(changeDiskTypeAllowed("CREATING")).toBe(false);
    expect(changeDiskTypeAllowed("MIGRATING")).toBe(false);
    expect(changeDiskTypeAllowed("DELETING")).toBe(false);
    expect(changeDiskTypeAllowed("ERROR")).toBe(false);
    // Молчание сервера не превращается в разрешение.
    expect(changeDiskTypeAllowed(undefined)).toBe(false);
    expect(changeDiskTypeAllowed("")).toBe(false);
  });
});

describe("тело запроса несёт всё, что контракт объявил обязательным", () => {
  it("смена типа диска шлёт disk_type_id", () => {
    // `volume_id` едет сегментом пути, а не телом, — его требование выполняется
    // самим адресом.
    const required = requiredFields("volume_service.proto", "ChangeDiskTypeRequest").filter((f) => f !== "volume_id");
    expect(required).toEqual(["disk_type_id"]);
    expect(resourcesSrc).toContain("disk_type_id: diskTypeId");
  });

  it("копия снимка и образа спрашивает проект и цель", () => {
    // `project_id` обязателен, хотя выглядит выводимым из источника: именно он —
    // объект вопроса о правах («создать» спрашивают у проекта). Забыв его,
    // консоль получала бы отказ, у которого нет поля, на которое сослаться.
    const snap = requiredFields("snapshot_service.proto", "CopySnapshotRequest").filter((f) => f !== "snapshot_id");
    expect(snap.sort()).toEqual(["project_id", "target_zone_id"]);
    const img = requiredFields("image_service.proto", "CopyImageRequest").filter((f) => f !== "image_id");
    expect(img.sort()).toEqual(["project_id", "target_region_id"]);
  });
});
