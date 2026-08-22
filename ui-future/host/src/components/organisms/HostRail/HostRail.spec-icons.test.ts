// Замок на карту иконок рейла: каждый её ключ обязан быть адресуемым разделом.
//
// Карта сама объявляет свой контракт («Ключ — specId = последний сегмент
// nav-path»), а перечень nav-path'ов приходит из navigation.ts каждого remote'а —
// это и есть ground truth, читаемый здесь с диска, а не выписываемый по памяти.
//
// Почему ключ без раздела — не безобидный мусор. Ресурс, снятый с поверхности
// продукта (compute Disk → storage Volume; в стволе `/compute/v1/disks` нет, есть
// `/storage/v1/volumes`), оставляет за собой ключ, который выглядит поддержкой
// раздела и ею не является: глиф не покажется никогда, а следующий контрибьютор
// прочитает карту как перечень того, что консоль умеет. Обратная сторона того же
// дефекта — ПРИШЕДШИЙ ресурс без ключа: он молча получает fallback-глиф.

import { readFileSync, readdirSync, existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const uiRoot = path.resolve(here, "../../../../..");

/** Последние сегменты всех nav-path, объявленных remote'ами. */
function navSegments(): { segments: Set<string>; filesRead: string[] } {
  const filesRead: string[] = [];
  const segments = new Set<string>();
  for (const entry of readdirSync(uiRoot, { withFileTypes: true })) {
    if (!entry.isDirectory() || entry.name === "node_modules") continue;
    const nav = path.join(uiRoot, entry.name, "src/navigation.ts");
    if (!existsSync(nav)) continue;
    filesRead.push(entry.name);
    const src = readFileSync(nav, "utf8");
    for (const m of src.matchAll(/path:\s*"([^"]+)"/g)) {
      const last = m[1].split("/").filter(Boolean).pop();
      if (last) segments.add(last);
    }
  }
  return { segments, filesRead };
}

/** Ключи литерала antdIconBySpec из объявления навигации модулей.
 *  Карта переехала из рейла в общее объявление, когда второй уровень навигации
 *  выделился в ModuleNav: рейл и панель типов берут глифы из ОДНОГО источника,
 *  иначе один и тот же раздел показывался бы двумя разными значками. */
function iconKeys(): string[] {
  const src = readFileSync(path.join(here, "../../../lib/module-navigation.tsx"), "utf8");
  const start = src.indexOf("const antdIconBySpec");
  const end = src.indexOf("\n};", start);
  expect(start).toBeGreaterThan(-1);
  expect(end).toBeGreaterThan(start);
  return [...src.slice(start, end).matchAll(/^ {2}"?([a-z][a-z-]*)"?:/gm)].map((m) => m[1]);
}

describe("HostRail — карта иконок против объявленных разделов", () => {
  const { segments, filesRead } = navSegments();
  const keys = iconKeys();

  it("объём осмотренного назван: сколько navigation-модулей и сколько ключей", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: если парсер
    // сломается, утверждения ниже станут вакуумными — здесь они этого не смогут.
    expect(filesRead.length).toBeGreaterThanOrEqual(7);
    expect(segments.size).toBeGreaterThanOrEqual(25);
    expect(keys.length).toBeGreaterThanOrEqual(20);
  });

  it("каждый ключ карты именует раздел, который remote действительно объявляет", () => {
    const orphans = keys.filter((k) => !segments.has(k));
    expect(orphans).toEqual([]);
  });

  it("живые разделы блочного хранения и sizing'а в карте есть (контроль в обратную сторону)", () => {
    // Без этого предыдущее утверждение зеленело бы и на пустой карте.
    for (const live of ["volumes", "machine-types", "images", "disk-types"]) {
      expect(keys).toContain(live);
      expect([...segments]).toContain(live);
    }
  });
});
