// Гейт: всякий список консоли ОБЪЯВЛЯЕТ область своих ручек (#373).
//
// ПРЕДМЕТ. Списки курсорные: страница приезжает с сервера, общего числа нет. Всё,
// что делается в браузере поверх приехавшего, судит о прочитанной части — и
// врёт тем убедительнее, чем больше у арендатора ресурсов. Одну поверхность
// (страницу списка) уже починили поимённо; класс от этого не закрылся —
// четырнадцать мест рендера таблицы из пятнадцати молчали, а четыре копии
// вкладки связанного ресурса показывали одну страницу и без продолжения.
//
// ЧТО ЭТОТ ГЕЙТ ТРЕБУЕТ ОТ КОДА, КОТОРОГО ЕЩЁ НЕТ:
//   1. каждое место рендера `ResourceTable` НАЗЫВАЕТ полноту набора (`complete`);
//   2. каждый исходник, сужающий прочитанный список в браузере ручкой
//      пользователя, ССЫЛАЕТСЯ на общий словарь области (`@shared/lib/list-scope`)
//      — то есть называет область теми же словами, что и соседи;
//   3. реализация `ResourceTable` в дереве ОДНА: вторая копия неизбежно
//      отстанет от первой и вернёт умолчание, которое здесь запрещается.
//
// ПРЕДПОСЫЛКА ГЕЙТА И ЕЁ ПРОВЕРКА. Гейт читает исходники по синтаксическому
// признаку, поэтому обязан заявлять объём осмотренного: «ноль находок» на нуле
// прочитанных файлов означает не чистоту, а слепоту. Ниже — три утверждения о
// самой переписи: файлов прочитано много, места рендера найдены, сужающие
// поверхности найдены. Способность гейта упасть доказана инъекцией в
// `list-scope-declared.injection.test.ts` — тем же предикатом, что судит дерево
// (`list-scope-census.ts`), а не его копией.

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { NOT_APPS, sourceFiles } from "./shared-symbol-sweep";
import {
  declaresCompleteness,
  declaresResourceTable,
  declaresScope,
  narrowsLoadedList,
  renderSiteCount,
} from "./list-scope-census";

const UI_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

function existsDir(p: string): boolean {
  return existsSync(p) && statSync(p).isDirectory();
}

/** Каталоги приложений плюс `shared`: всё, что участвует в сборке консоли. */
function consoleRoots(): string[] {
  const apps = readdirSync(UI_ROOT, { withFileTypes: true })
    .filter((e) => e.isDirectory() && !NOT_APPS.has(e.name) && !e.name.startsWith("."))
    .map((e) => e.name);
  return [...apps, "shared"].map((n) => path.join(UI_ROOT, n, "src")).filter((p) => existsDir(p));
}

interface Source {
  rel: string;
  text: string;
}

/**
 * Каталог `src/test/` — ОСНАСТКА прогона, а не поверхность консоли.
 *
 * Он исключается не ради удобства: сами предикаты этого гейта живут там и
 * содержат в тексте и `<ResourceTable`, и вызовы сужения — в виде регулярных
 * выражений. Без исключения гейт находил бы САМ СЕБЯ и требовал бы от своего
 * предиката объявить полноту набора. Тот же класс, что «проверка находит слово
 * в комментарии, объясняющем эту проверку».
 *
 * Цена исключения названа: список, отрисованный из `src/test/`, гейт не увидит.
 * Списков там нет by construction — это харнесс, а не экраны.
 */
function isHarness(rel: string): boolean {
  return rel.split(path.sep).includes("test");
}

function allSources(): Source[] {
  const out: Source[] = [];
  for (const root of consoleRoots()) {
    for (const f of sourceFiles(root)) {
      const rel = path.relative(UI_ROOT, f);
      if (isHarness(rel)) continue;
      out.push({ rel, text: readFileSync(f, "utf8") });
    }
  }
  return out;
}

const SOURCES = allSources();

/**
 * Освобождения. Пусты намеренно — и обязаны такими остаться: запись здесь есть
 * место, куда ложь возвращается незамеченной. Каждая будущая запись обязана
 * нести причину и предикат снятия, а самоистечение проверяется ниже.
 */
const EXEMPT: ReadonlySet<string> = new Set<string>([]);

describe("перепись читает дерево, а не воздух", () => {
  it("исходников консоли прочитано много", () => {
    expect(SOURCES.length).toBeGreaterThan(200);
  });

  it("места рендера таблицы ресурса найдены", () => {
    const total = SOURCES.reduce((acc, s) => acc + renderSiteCount(s.text), 0);
    // eslint-disable-next-line no-console
    console.log(
      `[#373] исходников прочитано: ${SOURCES.length}; мест рендера ResourceTable: ${total}; ` +
        `поверхностей с клиентским сужением: ${SOURCES.filter((s) => narrowsLoadedList(s.text)).length}`,
    );
    expect(total).toBeGreaterThan(5);
  });

  it("поверхности с клиентским сужением найдены", () => {
    expect(SOURCES.filter((s) => narrowsLoadedList(s.text)).length).toBeGreaterThan(3);
  });
});

describe("каждое место рендера таблицы называет полноту набора", () => {
  it("нет ни одного места без `complete`", () => {
    const bad = SOURCES.filter((s) => renderSiteCount(s.text) > 0)
      .filter((s) => !EXEMPT.has(s.rel))
      // Проверяется ФАЙЛ, а не отдельный тег: разбор JSX регулярным выражением
      // разошёлся бы с настоящим синтаксисом на первом же переносе строки. Файл
      // с двумя таблицами, где `complete` объявлен у одной, гейт пропустит —
      // сегодня таких файлов нет (перепись выше печатает и число мест, и число
      // файлов), а появятся — предикат придётся усилить разбором, а не
      // расширением освобождений.
      .filter((s) => !declaresCompleteness(s.text));
    expect(bad.map((s) => s.rel)).toEqual([]);
  });
});

describe("каждая сужающая поверхность называет область общими словами", () => {
  it("нет ни одной, которая сужает и молчит", () => {
    const bad = SOURCES.filter((s) => narrowsLoadedList(s.text))
      .filter((s) => !EXEMPT.has(s.rel))
      .filter((s) => !declaresScope(s.text));
    expect(bad.map((s) => s.rel)).toEqual([]);
  });
});

describe("реализация таблицы ресурса в дереве одна", () => {
  it("нет второй копии ResourceTable", () => {
    const impls = SOURCES.filter((s) => declaresResourceTable(s.text));
    expect(impls.map((s) => s.rel)).toEqual([
      path.join("shared", "src/components/organisms/ResourceTable/ResourceTable.tsx"),
    ]);
  });
});

describe("освобождения самоистекают", () => {
  it("нет записи, которой нечего освобождать", () => {
    const stale = [...EXEMPT].filter((rel) => !SOURCES.some((s) => s.rel === rel));
    expect(stale).toEqual([]);
  });
});
