// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Заглушку перемещения предлагает ОДИН предикат, а не перечень у каждой поверхности.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Пункт «Переместить» открывает окно-заглушку, печатающее REST-вызов, которого
// нет. Какому ресурсу его не предлагать, решает ОДИН закрытый перечень
// `MOVE_INCAPABLE` и выведенный из него предикат `resourceIsMoveCapable`
// (`molecules/RowActionsMenu`). Предикат спрашивает ДВА слагаемых — имя спеки и
// адрес её коллекции, — потому что один доменный объект бывает представлен в
// реестре дважды.
//
// Поверхностей, производящих этот пункт, в консоли ДВЕ: меню строки списка и
// меню карточки ресурса. Пока каждая решает вопрос по-своему, «перемещать
// нечем» становится двумя местами об одном предмете — и они расходятся молча,
// потому что ни сборка, ни модульная проба одной поверхности этого не видят.
//
// ЧТО НАБЛЮДАЛОСЬ (перемерено на этом дереве). Меню карточки несло
// СОБСТВЕННЫЙ перечень из пяти имён и комментарий «те же ресурсы, что в
// RowActionsMenu». Комментарий был ложен: в каноне тринадцать имён, и восьми в
// перечне карточки не было —
//
//     compute-instances · disk-types · registries · repositories ·
//     snapshots · tags · users · volumes
//
// то есть на карточке этих восьми арендатору предлагалась заглушка, которую
// строка списка того же ресурса уже подавляла. Второго слагаемого (адрес
// коллекции) перечень карточки не имел вовсе, поэтому читающие близнецы
// каталога размещения обходили его целиком — тот самый класс, закрытый для
// строки и открытый для карточки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
//	A. ПЕРЕПИСЬ. Названы ОБЕ величины — сколько файлов осмотрено и сколько из
//	   них производят пункт. Одно число скрывает ровно тот случай, ради
//	   которого гейт заведён: пустой обход неотличим от чистого дерева.
//	B. ПРЕДИКАТ ОБЪЯВЛЕН РОВНО ОДИН РАЗ. Иначе «единый источник» держится
//	   именем, а не деревом: второе объявление разошлось бы с первым молча.
//	C. ПРЕДМЕТ. Каждый производитель пункта решает вопрос КАНОНИЧЕСКИМ
//	   предикатом. Файл, производящий подпись и не ссылающийся на предикат,
//	   решает его сам — то есть завёл второе место.
//	D. ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Производители в дереве ЕСТЬ. Без него C
//	   выполнялся бы сам собой на консоли, где пункта нет нигде, — и гейт
//	   зеленел бы ровно тогда, когда сторожить стало нечего.

import path from "node:path";
import { existsSync, readFileSync } from "node:fs";
import { discoverApps, sourceFiles } from "./shared-symbol-sweep";
import { CANON_PREDICATE, MOVE_LABEL, parseMoveStubSource } from "./move-stub-single-predicate";

/**
 * Корень консоли ищется ПОДЪЁМОМ, а не собирается из числа сегментов.
 *
 * Пробу запускает jest каждого приложения-workspace, то есть рабочий каталог у
 * неё разный; путь, выведенный из числа `..`, был бы свойством того, ОТКУДА
 * позвали. Не найти корень — ОТКАЗ, а не пропуск: обход по несуществующему
 * дереву дал бы ноль исходников и молчаливое «нарушений нет».
 */
function uiRoot(): string {
  let dir = path.resolve(process.cwd());
  for (;;) {
    if (existsSync(path.join(dir, "shared", "src", "components"))) return dir;
    const up = path.dirname(dir);
    if (up === dir) {
      throw new Error(
        `корень консоли не найден подъёмом от ${process.cwd()}: ожидается каталог с shared/src/components. ` +
          `Проба не прочитала ничего и не вправе молчать`,
      );
    }
    dir = up;
  }
}

const UI_ROOT = uiRoot();

/** Все исходники консоли: общий модуль и каждое приложение — из ДЕРЕВА, не списком. */
function consoleSources(): { file: string; source: string }[] {
  const roots = [path.join(UI_ROOT, "shared", "src"), ...discoverApps(UI_ROOT).map((a) => path.join(UI_ROOT, a, "src"))];
  const seen = new Set<string>();
  const out: { file: string; source: string }[] = [];
  for (const root of roots) {
    for (const file of sourceFiles(root)) {
      if (seen.has(file)) continue;
      seen.add(file);
      out.push({ file, source: readFileSync(file, "utf8") });
    }
  }
  return out;
}

const SOURCES = consoleSources();
const FACTS = SOURCES.map(({ file, source }) => ({ file, ...parseMoveStubSource(file, source) }));
const PRODUCERS = FACTS.filter((f) => f.producesMoveStub);
const rel = (f: string) => path.relative(UI_ROOT, f);

describe("«Переместить» предлагает один предикат, а не перечень у каждой поверхности", () => {
  it("дерево прочитано — перепись названа", () => {
    process.stdout.write(
      `\n[заглушка перемещения] исходников консоли ${SOURCES.length} · производят пункт «${MOVE_LABEL}» ` +
        `${PRODUCERS.length} (${PRODUCERS.map((p) => `${rel(p.file)}:${p.producerLines.join(",")}`).join(" · ")}) · ` +
        `решают каноническим предикатом ${PRODUCERS.filter((p) => p.usesCanonicalPredicate).length}\n`,
    );
    // Пустой обход — отказ, а не молчаливое «нарушений нет».
    expect(SOURCES.length).toBeGreaterThan(0);
  });

  it("канонический предикат объявлен ровно один раз", () => {
    const declaring = FACTS.filter((f) => f.declaresCanonicalPredicate).map((f) => rel(f.file));
    expect(declaring).toHaveLength(1);
  });

  it("каждый производитель пункта решает каноническим предикатом", () => {
    // Находка называет КООРДИНАТУ: файл и строку, где производится подпись, —
    // иначе читатель ищет второе место сам.
    const ownDecision = PRODUCERS.filter((p) => !p.usesCanonicalPredicate).map(
      (p) =>
        `${rel(p.file)}:${p.producerLines.join(",")} — производит «${MOVE_LABEL}», ` +
        `но не ссылается на ${CANON_PREDICATE}: решает вопрос своим перечнем`,
    );
    expect(ownDecision).toEqual([]);
  });

  it("положительный контроль: производители пункта в дереве есть", () => {
    // Без этого утверждение выше зеленеет на консоли, где пункта нет вовсе.
    expect(PRODUCERS.length).toBeGreaterThan(0);
  });
});
