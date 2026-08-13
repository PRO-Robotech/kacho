import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  CAPABILITIES,
  LIFECYCLE_LABEL,
  TIER_LABEL,
  acceptsNewVolumes,
  lifecycleLabel,
  tierLabel,
} from "./storage-disk-type";

/**
 * Словарь типа диска: полнота против контракта + правило 6 канона консоли.
 *
 * Две разные вещи проверяются здесь намеренно вместе, потому что ломаются они
 * одинаково тихо:
 *
 *   (1) ПОЛНОТА. Значение перечисления, которого нет в таблице, показывается
 *       сырым токеном (`IO_MAX` вместо «Предельный ввод-вывод»). Ошибки нет,
 *       ячейка заполнена, вердикта нет ни у одного теста. Источник истины —
 *       `proto/kacho/cloud/storage/v1/disk_type.proto`, а не список рядом:
 *       список разошёлся бы с контрактом молча.
 *
 *   (2) ФОРМА ФРАЗЫ. Правило 6: булево свойство называется СЛЕДСТВИЕМ, а не
 *       ответом «Да». «Да» рядом с подписью «Снимки» не говорит ни что снимки
 *       можно снимать, ни что нельзя, — читателю приходится достраивать смысл
 *       самому. Проверяется механически: обе фразы непусты, различны и ни одна
 *       не является ответом на незаданный вопрос.
 */

const here = path.dirname(fileURLToPath(import.meta.url));
// shared/src/lib → ui-future → корень репозитория, где живёт `proto/`.
const protoPath = path.resolve(
  here,
  "../../../..",
  "proto/kacho/cloud/storage/v1/disk_type.proto",
);
const proto = readFileSync(protoPath, "utf8");

/** Значения `enum <name>` контракта, без `*_UNSPECIFIED`. */
function enumValues(name: string): string[] {
  const block = new RegExp(`enum ${name} \\{([\\s\\S]*?)\\n  \\}`).exec(proto);
  if (block === null) return [];
  return [...block[1].matchAll(/^\s*([A-Z][A-Z0-9_]*)\s*=\s*\d+\s*;/gm)].map(
    (m) => m[1],
  );
}

/** Поля `message Capabilities` контракта — имена в snake_case, как на проводе. */
function capabilityFields(): string[] {
  // `\n {2}\}` — закрывающая скобка вложенного сообщения (отступ два пробела);
  // счётный вид вместо двух литеральных пробелов, которые глазом не различаются.
  const block = /message Capabilities \{([\s\S]*?)\n {2}\}/.exec(proto);
  if (block === null) return [];
  return [...block[1].matchAll(/^\s*bool\s+([a-z_]+)\s*=\s*\d+\s*;/gm)].map(
    (m) => m[1],
  );
}

/** Ответ на незаданный вопрос: подпись, не называющая предмет. */
const EMPTY_ANSWERS = [
  "да",
  "нет",
  "есть",
  "поддерживается",
  "не поддерживается",
  "включено",
  "выключено",
];

describe("словарь типа диска полон против контракта", () => {
  it("объём осмотренного: контракт прочитан и перечисления из него извлечены", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: переехавший
    // proto иначе сделал бы все утверждения ниже тождественно истинными.
    expect(proto.length).toBeGreaterThan(1000);
    expect(enumValues("PerformanceTier").length).toBeGreaterThan(1);
    expect(enumValues("Lifecycle").length).toBeGreaterThan(1);
    expect(capabilityFields().length).toBeGreaterThan(1);
    // Контроль извлекателя в обе стороны: известное значение обязано найтись,
    // выдуманное — нет.
    expect(enumValues("PerformanceTier")).toContain("IO_MAX");
    expect(enumValues("PerformanceTier")).not.toContain("WARP_SPEED");
  });

  it("каждый ярус контракта назван словами", () => {
    const missing = enumValues("PerformanceTier").filter(
      (v) => !(v in TIER_LABEL),
    );
    expect(missing).toEqual([]);
  });

  it("каждое состояние обращения названо словами", () => {
    const missing = enumValues("Lifecycle").filter(
      (v) => !(v in LIFECYCLE_LABEL),
    );
    expect(missing).toEqual([]);
  });

  it("каждая способность контракта названа парой фраз", () => {
    const declared = CAPABILITIES.map((c) => c.path).sort();
    // Точное равенство в ОБЕ стороны: недостающая способность не показывается
    // вовсе, а лишняя показывается вечно ложной — сервер её не присылает.
    expect(declared).toEqual([...capabilityFields()].sort());
  });

  it("незнакомое значение возвращается как есть, а не выдумывается и не глотается", () => {
    // Словарь мог пополниться раньше консоли. Показать сырой токен честнее, чем
    // промолчать или подставить чужую подпись.
    expect(tierLabel("QUANTUM")).toBe("QUANTUM");
    expect(lifecycleLabel("FROZEN")).toBe("FROZEN");
    // Пустое значение — не токен: строки нет вовсе (правило 9).
    expect(tierLabel("")).toBeNull();
    expect(tierLabel(undefined)).toBeNull();
    expect(lifecycleLabel(undefined)).toBeNull();
  });
});

describe("правило 6 — булево названо СЛЕДСТВИЕМ, а не ответом «Да»", () => {
  it.each(CAPABILITIES.map((c) => [c.path, c] as const))(
    "%s — обе фразы называют предмет",
    (_path, cap) => {
      expect(cap.yes.trim().length).toBeGreaterThan(0);
      expect(cap.no.trim().length).toBeGreaterThan(0);
      // Две разные фразы, а не одна с отрицанием-заглушкой.
      expect(cap.yes).not.toBe(cap.no);
      for (const phrase of [cap.yes, cap.no]) {
        expect(EMPTY_ANSWERS).not.toContain(phrase.trim().toLowerCase());
        // Фраза-следствие длиннее односложного ответа: «Да»/«Нет» и их синонимы
        // отсекаются и по существу (список выше), и по форме.
        expect(phrase.trim().split(/\s+/).length).toBeGreaterThan(1);
      }
    },
  );

  it("состояние обращения тоже названо следствием, а не токеном", () => {
    // Вопрос у этого поля ровно один — принимает ли класс НОВЫЕ тома, — и обе
    // подписи обязаны на него отвечать, иначе «Выведен» читается как «мои тома
    // пропали».
    expect(LIFECYCLE_LABEL.ACTIVE).toMatch(/приним/i);
    expect(LIFECYCLE_LABEL.DEPRECATED).toMatch(/не создаются/i);
    expect(LIFECYCLE_LABEL.ACTIVE).not.toBe("ACTIVE");
  });

  it("принимает ли класс новые тома — один предикат, и молчание не разрешение", () => {
    expect(acceptsNewVolumes("ACTIVE")).toBe(true);
    expect(acceptsNewVolumes("DEPRECATED")).toBe(false);
    expect(acceptsNewVolumes("RETIRED")).toBe(false);
    // Неизвестное состояние и отсутствие поля не превращаются в разрешение.
    expect(acceptsNewVolumes(undefined)).toBe(false);
    expect(acceptsNewVolumes("LIFECYCLE_UNSPECIFIED")).toBe(false);
    expect(acceptsNewVolumes("SOMETHING_NEW")).toBe(false);
  });
});
