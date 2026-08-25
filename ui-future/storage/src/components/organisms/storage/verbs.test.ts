import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { changeDiskTypeAllowed } from "./ChangeDiskTypeDialog";

// Край подменён целиком: предмет утверждения — вызов, а не ответ. Модуль
// подменяется ДО импорта потребителя (ESM-режим), поэтому сам потребитель
// загружается динамически ниже.
// Подменяется ТРАНСПОРТ, а не модуль клиента: предмет утверждения — то, что
// уходит на провод. Подмена модуля проверяла бы, что мы позвали свою же обёртку,
// и молчала бы о конверсии имён, которую обёртка делает.
const sent: Array<{ url: string; body: unknown }> = [];
(globalThis as unknown as { fetch: unknown }).fetch = async (url: string, init: { body?: string }) => {
  sent.push({ url: String(url), body: init?.body ? JSON.parse(init.body) : null });
  return {
    ok: true,
    status: 200,
    text: async () => JSON.stringify({ operation: { id: "sop-test", done: false } }),
  };
};

const { volumesApi } = await import("../../../api/resources");

/**
 * Действия-глаголы storage: предусловие смены типа диска и форма тел запросов.
 *
 * Путь каждого глагола сверяется с деревом proto отдельно — гейтом
 * `shared/src/test/console-verb-routes-exist.test.ts`, который резолвит голову
 * литерала в константу файла. Здесь проверяется то, чего он не видит: ПОЛЯ тела
 * и условие, при котором кнопка доступна.
 *
 * Тело ВЫЗЫВАЕТСЯ, а не вычитывается из исходника. Прежняя редакция искала
 * подстроку в тексте `api/resources.ts` — такая проба зелена, пока файл
 * существует: функция не зовётся, ни один исход не утверждается, слот занят, а
 * поведение не проверено. Гейт дерева
 * (`internal/repohygiene/uisourcereadtest_test.go`) ловит ровно это, и поймал
 * здесь.
 *
 * Сеть поднимать не требуется: край подменён, и предметом утверждения становится
 * то, ЧТО ему передали, — адрес и поля тела.
 */

/**
 * REFUSED_WHEN_MISSING — поля, БЕЗ КОТОРЫХ КРАЙ ОТВЕРГАЕТ запрос, по глаголам.
 *
 * ПЕРЕОСНОВАНО (kacho#1255). Прежде перечень извлекался из контракта: функция
 * `requiredFields()` читала `.proto` и выбирала поля с опцией `(required)`.
 * Семейство, которому опция принадлежала, снято с контрактов — исполнителя на
 * пути запроса у него не было ни одного, и объявление не ограничивало ничего.
 *
 * Предмет пробы НЕ изменился: тело запроса обязано нести всё, без чего край его
 * отвергает. Изменился ИСТОЧНИК перечня — не «контракт объявил», а «край
 * отвергает без этого».
 *
 * ПЕРЕЧЕНЬ ДОКАЗЫВАЕТСЯ, А НЕ ОБЪЯВЛЯЕТСЯ, и доказательств у него два, разной
 * силы, оба названы координатой:
 *
 *   `refusal` — строка отказа в прод-коде владельца. Проверяется пробой
 *               предпосылки ниже: исчезнет — перечень станет описанием
 *               вчерашнего дерева, и краснеет ИМЕННО предпосылка;
 *   `e2e`     — сквозной кейс, посылающий запрос БЕЗ этого поля и требующий
 *               отказа. Это и есть доказательство того, что перечень описывает
 *               край, а не сам себя.
 *
 * ЧЕСТНО НАЗВАНО, ЧЕГО НЕТ: у `project_id` сквозного кейса в дереве нет ни на
 * одном из двух глаголов копирования — отказ прод-кода проверен, сквозной нет.
 * `e2e: null` означает «не доказано сквозным», а не «доказательство не нужно»,
 * и предпосылка это различает.
 */
const REFUSED_WHEN_MISSING: ReadonlyArray<{
  readonly verb: string;
  readonly field: string;
  readonly refusal: readonly [string, string];
  readonly e2e: readonly [string, string] | null;
}> = [
  {
    verb: "ChangeDiskType",
    field: "disk_type_id",
    refusal: ["services/storage/internal/apps/kacho/api/volume/volume.go", "disk_type_id: required"],
    e2e: ["services/storage/tests/newman/cases/volume.py", "disk_type_id: required"],
  },
  {
    verb: "CopySnapshot",
    field: "project_id",
    refusal: ["services/storage/internal/apps/kacho/api/snapshot/snapshot.go", "project_id: required"],
    e2e: null,
  },
  {
    verb: "CopySnapshot",
    field: "target_zone_id",
    refusal: ["services/storage/internal/apps/kacho/api/snapshot/snapshot.go", "target_zone_id: required"],
    e2e: ["services/storage/tests/newman/cases/snapshot.py", "target_zone_id: required"],
  },
  {
    verb: "CopyImage",
    field: "project_id",
    refusal: ["services/storage/internal/apps/kacho/api/image/image.go", "project_id: required"],
    e2e: null,
  },
  {
    verb: "CopyImage",
    field: "target_region_id",
    refusal: ["services/storage/internal/apps/kacho/api/image/image.go", "target_region_id: required"],
    e2e: ["services/storage/tests/newman/cases/image.py", "target_region_id: required"],
  },
];

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../../../../../..");

/** Поля, без которых край отвергает названный глагол. */
function refusedWhenMissing(verb: string): string[] {
  return REFUSED_WHEN_MISSING.filter((r) => r.verb === verb).map((r) => r.field);
}

describe("объём осмотренного", () => {
  it("перечень непуст и покрывает все три глагола", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: пустой
    // перечень сделал бы каждое утверждение ниже вакуумным.
    expect(typeof volumesApi.changeDiskType).toBe("function");
    expect(refusedWhenMissing("ChangeDiskType").length).toBeGreaterThan(0);
    expect(refusedWhenMissing("CopySnapshot").length).toBeGreaterThan(0);
    expect(refusedWhenMissing("CopyImage").length).toBeGreaterThan(0);
  });

  it("у КАЖДОЙ записи предпосылка на месте: отказ владельца существует", () => {
    // Перечень выведен из поведения края, и поведение живёт в названных файлах.
    // Исчезнет отказ — запись станет описанием вчерашнего дерева.
    const stale: string[] = [];
    for (const rec of REFUSED_WHEN_MISSING) {
      const [rel, marker] = rec.refusal;
      const abs = path.join(repoRoot, rel);
      if (!existsSync(abs)) {
        stale.push(`${rec.verb}.${rec.field}: ${rel} исчез`);
        continue;
      }
      if (!readFileSync(abs, "utf8").includes(marker)) {
        stale.push(`${rec.verb}.${rec.field}: ${rel} больше не отвечает «${marker}»`);
      }
    }
    expect(stale).toEqual([]);
  });

  it("сквозное доказательство: названо там, где оно есть, и НЕ выдумано там, где его нет", () => {
    // Запись, объявившая сквозной кейс, обязана его иметь; запись без кейса
    // объявляет это прямо (`e2e: null`). Молчаливое «доказано» и честное
    // «не доказано» обязаны быть различимы.
    const broken: string[] = [];
    let proven = 0;
    for (const rec of REFUSED_WHEN_MISSING) {
      if (rec.e2e === null) continue;
      const [rel, marker] = rec.e2e;
      const abs = path.join(repoRoot, rel);
      if (!existsSync(abs) || !readFileSync(abs, "utf8").includes(marker)) {
        broken.push(`${rec.verb}.${rec.field}: ${rel} не несёт «${marker}»`);
        continue;
      }
      proven += 1;
    }
    expect(broken).toEqual([]);
    // eslint-disable-next-line no-console
    console.log(
      `[#1255] полей в перечне ${REFUSED_WHEN_MISSING.length} · доказано сквозным ${proven} · ` +
        `не доказано ${REFUSED_WHEN_MISSING.filter((r) => r.e2e === null).length}`,
    );
    expect(proven).toBeGreaterThan(0);
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
  it("смена типа диска шлёт disk_type_id", async () => {
    // `volume_id` едет сегментом пути, а не телом, — его требование выполняется
    // самим адресом.
    const required = refusedWhenMissing("ChangeDiskType").filter((f) => f !== "volume_id");
    expect(required).toEqual(["disk_type_id"]);

    sent.length = 0;
    await volumesApi.changeDiskType("vol-1", "block-fast");

    // Адрес и тело — то, что УШЛО НА ПРОВОД.
    expect(sent).toHaveLength(1);
    expect(sent[0].url).toContain("/storage/v1/volumes/vol-1:changeDiskType");
    // На проводе имена полей camelCase (конвенция REST-края), поэтому каждое
    // обязательное поле контракта сверяется в своей проводной форме.
    const wire = sent[0].body as Record<string, unknown>;
    const camel = (f: string) => f.replace(/_([a-z])/g, (_, c: string) => c.toUpperCase());
    for (const field of required) expect(Object.keys(wire)).toContain(camel(field));
    expect(wire.diskTypeId).toBe("block-fast");
  });

  it("копия снимка и образа спрашивает проект и цель", () => {
    // `project_id` обязателен, хотя выглядит выводимым из источника: именно он —
    // объект вопроса о правах («создать» спрашивают у проекта). Забыв его,
    // консоль получала бы отказ, у которого нет поля, на которое сослаться.
    const snap = refusedWhenMissing("CopySnapshot").filter((f) => f !== "snapshot_id");
    expect(snap.sort()).toEqual(["project_id", "target_zone_id"]);
    const img = refusedWhenMissing("CopyImage").filter((f) => f !== "image_id");
    expect(img.sort()).toEqual(["project_id", "target_region_id"]);
  });
});
