// Регион и зона — РАЗНЫЕ координаты размещения, и на экране их различает только
// глиф: подпись вида («REGIONAL»/«ZONAL») снята решением владельца 2026-08-12 как
// повтор того, что уже сказано иконкой и адресом перехода. Значит глиф обязан
// различать — иначе снятие подписи оставило бы пользователя без признака вовсе.
//
// Прежде оба несли `AppstoreOutlined`, который вдобавок служит УМОЛЧАНИЕМ для
// незнакомого ресурса: регион, зона и «иконки нет» выглядели одинаково.
//
// Обход — по всему дереву, а не по одному файлу: `ResourceIcon` форкнут в пяти
// пакетах, и правка одной копии молча не доезжает до остальных. Проба читает
// исходники, потому что в тестовом окружении глифы подменены ОДНИМ пустым
// компонентом — по рендеру они неразличимы by construction, и утверждение о
// различии на нём было бы вакуумным.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const UI_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const REL = "src/components/organisms/form/ResourceIcon/ResourceIcon.tsx";

/**
 * Прослойка — не копия: файл, который только реэкспортирует, карты иконок не
 * объявляет, и требовать от него ключей значило бы требовать их от чужого файла.
 *
 * Распознавание по СОДЕРЖИМОМУ, а не по длине или имени: короткая копия
 * прослойкой не считается, иначе сведение можно было бы подделать усечением.
 */
function isShim(src: string): boolean {
  const code = src
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "" && !l.startsWith("//") && !l.startsWith("*") && !l.startsWith("/*"));
  return code.length > 0 && code.every((l) => /^export\s+(\*|\{[^}]*\})\s+from\s+["']/.test(l));
}

/** Все ФАЙЛЫ карты иконок в дереве — найденные, а не выписанные. */
function iconFiles(): { pkg: string; src: string }[] {
  return readdirSync(UI_ROOT)
    .filter((name) => !name.startsWith(".") && name !== "node_modules")
    .filter((name) => statSync(path.join(UI_ROOT, name)).isDirectory())
    .map((pkg) => ({ pkg, file: path.join(UI_ROOT, pkg, REL) }))
    .filter((e) => existsSync(e.file))
    .map((e) => ({ pkg: e.pkg, src: readFileSync(e.file, "utf8") }));
}

/** Настоящие карты — те файлы, которые объявляют иконки сами. */
function iconMaps(): { pkg: string; src: string }[] {
  return iconFiles().filter((e) => !isShim(e.src));
}

/** Прослойки, делегирующие в `shared/`. Считаются отдельно и вслух. */
function iconShims(): { pkg: string; src: string }[] {
  return iconFiles().filter((e) => isShim(e.src));
}

const glyphOf = (src: string, key: string): string =>
  new RegExp(`\\b${key}: <(\\w+)`).exec(src)?.[1] ?? "";

/** Глиф, которым рисуется НЕИЗВЕСТНЫЙ ресурс: совпасть с ним значит не отличаться от «иконки нет». */
const fallbackOf = (src: string): string => /ICONS\[specId\] \?\? <(\w+)/.exec(src)?.[1] ?? "";

describe("иконки каталога размещения различают регион и зону", () => {
  it("перепись: карты иконок найдены и в каждой читаются оба ключа", () => {
    // «Ноль нарушений» обязано быть отличимо от «ноль прочитанного»: переезд
    // компонента иначе дал бы пустой обход и зелёный вердикт.
    const maps = iconMaps();
    const shims = iconShims();
    // Порог опущен с двух до одной НАМЕРЕННО и вместе с предметом: карт в дереве
    // было пять, форк сведён, и осталась одна — общая. Требование «копий не
    // меньше двух» после этого требовало бы РОВНО ТОГО, что мы устраняли.
    // Взамен утверждается, что делегирующие прослойки на месте: «карта одна»
    // должно быть отличимо от «остальные пакеты потеряли компонент вовсе».
    expect(maps.length).toBeGreaterThanOrEqual(1);
    expect(shims.length).toBeGreaterThan(0);
    // eslint-disable-next-line no-console
    console.log(`[icons] карт: ${maps.length} (${maps.map((m) => m.pkg).join(", ")}); прослоек: ${shims.length}`);
    for (const m of maps) {
      expect({ pkg: m.pkg, regions: glyphOf(m.src, "regions") !== "" }).toEqual({ pkg: m.pkg, regions: true });
      expect({ pkg: m.pkg, zones: glyphOf(m.src, "zones") !== "" }).toEqual({ pkg: m.pkg, zones: true });
    }
  });

  it("во всех копиях глиф региона отличается от глифа зоны", () => {
    for (const m of iconMaps()) {
      const regions = glyphOf(m.src, "regions");
      const zones = glyphOf(m.src, "zones");
      expect({ pkg: m.pkg, distinct: regions !== zones }).toEqual({ pkg: m.pkg, distinct: true });
    }
  });

  it("ни один из них не совпадает с глифом неизвестного ресурса", () => {
    // Положительный контроль рядом с отрицанием: без него проба зеленела бы на
    // карте, где регион и зона просто поменяли местами два одинаково-дефолтных
    // глифа — различие между собой было бы, а различие с «иконки нет» ушло.
    for (const m of iconMaps()) {
      const fallback = fallbackOf(m.src);
      expect({ pkg: m.pkg, fallbackFound: fallback !== "" }).toEqual({ pkg: m.pkg, fallbackFound: true });
      expect({ pkg: m.pkg, regionNotFallback: glyphOf(m.src, "regions") !== fallback }).toEqual({
        pkg: m.pkg,
        regionNotFallback: true,
      });
      expect({ pkg: m.pkg, zoneNotFallback: glyphOf(m.src, "zones") !== fallback }).toEqual({
        pkg: m.pkg,
        zoneNotFallback: true,
      });
    }
  });
});
