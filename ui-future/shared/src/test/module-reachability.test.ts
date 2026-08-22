import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { discoverApps, entryPoints, walkApp } from "./module-reachability";

/**
 * Гейт достижимости: не-тестовый модуль приложения обязан иметь путь от одного
 * из ОБЪЯВЛЕННЫХ входов этого приложения.
 *
 * ЗАЧЕМ. Мёртвый модуль не «лежит про запас»: он выглядит работающим, его правят
 * при правках соседей, он попадает в поиск и в обзор изменения — и первым же
 * читателем принимается за живой. Это уже стоило работы дважды: вторая
 * поверхность управления доступом дожила до неработоспособности, а копия
 * оболочки раздела администрирования разошлась с оригиналом (#447, #556).
 * Обходчик, которым эти числа были получены впервые, писался разово и в дереве
 * не остался — поэтому следующий такой модуль снова дожил бы до находки
 * владельца. Гейт и есть ответ: находкой он становится сам (#563).
 *
 * ЧТО ЭТОТ ГЕЙТ НЕ ЛОВИТ — названо, а не умолчано:
 *   • мёртвый код `shared/` — там своих входов нет, и вопрос ставится иначе
 *     (обход от достижимых множеств всех приложений). Отдельный предмет;
 *   • модуль, достижимый ТОЛЬКО из комментария или строкового литерала вида
 *     `import("…")`: обходчик читает исходник, а не разобранное дерево. Ошибка
 *     направлена в сторону «достижим» — гейт может пропустить мёртвое, но не
 *     объявит мёртвым живое.
 *
 * ВЕДОМОСТЬ САМОИСТЕКАЕТ. Запись про модуль, которого в дереве нет, и запись про
 * модуль, ставший достижимым, — находки. Иначе послабление пережило бы свой
 * предмет и унесло бы с собой следующую слепую зону.
 */

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const LEDGER_FILE = path.join(uiRoot, "shared/src/test/module-reachability-ledger.json");

interface Ledger {
  /** Почему ведомость вообще существует и когда исчезнет. */
  comment: string[];
  /** "<приложение>/<путь от каталога приложения>": причина */
  allowed: Record<string, string>;
}

const ledger = JSON.parse(readFileSync(LEDGER_FILE, "utf8")) as Ledger;

const APPS = discoverApps(uiRoot);
const WALKS = APPS.map((app) => walkApp(uiRoot, app));

const keyOf = (app: string, module: string) => `${app}/${module}`;

const allUnreachable = WALKS.flatMap((w) => w.unreachable.map((m) => keyOf(w.app, m)));
const allModules = WALKS.flatMap((w) => w.modules.map((m) => keyOf(w.app, m)));
const allReachable = new Set(WALKS.flatMap((w) => w.reachable.map((m) => keyOf(w.app, m))));
const moduleExists = new Set(allModules);

describe("предпосылка гейта: обход читает то, что думает, что читает", () => {
  // Запрет обоснован фактом о дереве (входы объявлены здесь, импорты выглядят
  // так). Факт меняется — запрет становится ложью, поэтому гейт заявляет о нём
  // сам, а не подразумевает.
  it(`приложений найдено: ${APPS.length} [${APPS.join(", ")}]`, () => {
    expect(APPS.length).toBeGreaterThan(0);
  });

  it("у каждого приложения есть хотя бы один объявленный вход", () => {
    const withoutEntry = APPS.filter((app) => entryPoints(uiRoot, app).length === 0);
    if (withoutEntry.length > 0) {
      throw new Error(
        `у приложений [${withoutEntry.join(", ")}] не найдено НИ ОДНОГО входа. Гейт читает ` +
          `не то: либо сменилась форма блока exposes в vite.config.ts, либо ушёл ` +
          `src/main.tsx. С нулём входов недостижимым оказывается ВСЁ, и находки гейта ` +
          `перестают что-либо значить`,
      );
    }
  });

  it("обход нашёл достижимые модули, а не пустоту", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: обход,
    // который перестал резолвить импорты, объявил бы мёртвым всё дерево.
    if (allReachable.size === 0) {
      throw new Error(
        `прочитано модулей ${allModules.length}, достижимых 0 — обход не резолвит импорты ` +
          `вовсе (сменились алиасы, расширения или раскладка). Это отказ гейта, а не находка`,
      );
    }
  });
});

describe("каждый не-тестовый модуль приложения достижим от объявленного входа", () => {
  it(
    `перепись: приложений ${APPS.length}, модулей прочитано ${allModules.length}, ` +
      `достижимо ${allReachable.size}, недостижимо ${allUnreachable.length}, ` +
      `в ведомости ${Object.keys(ledger.allowed).length}`,
    () => {
      // Перепись — отдельное утверждение, а не строка вывода: без неё «ноль
      // недостижимых» неотличимо от «ноль прочитанных файлов».
      expect(allModules.length).toBeGreaterThan(0);
    },
  );

  it("недостижимых сверх ведомости нет", () => {
    const findings = allUnreachable.filter((k) => !(k in ledger.allowed));
    if (findings.length > 0) {
      const byApp = WALKS.map((w) => {
        const own = findings.filter((k) => k.startsWith(`${w.app}/`));
        return own.length ? `  ${w.app} (входы: ${w.entries.join(", ")}):\n${own.map((k) => `      ${k}`).join("\n")}` : "";
      })
        .filter(Boolean)
        .join("\n");
      throw new Error(
        `недостижимых от объявленных входов модулей: ${findings.length}\n${byApp}\n\n` +
          `Модуль без пути от входа не «лежит про запас»: он выглядит работающим, его ` +
          `правят при правках соседей и принимают за живой. Исходов три — провязать к ` +
          `живой поверхности, снять вместе с его пробами, либо (если это осознанный ` +
          `долг) внести в ${path.relative(uiRoot, LEDGER_FILE)} с ПРИЧИНОЙ и номером задачи`,
      );
    }
  });
});

describe("ведомость самоистекает", () => {
  it("в ведомости нет записей про модули, которых в дереве нет", () => {
    const stale = Object.keys(ledger.allowed).filter((k) => !moduleExists.has(k));
    if (stale.length > 0) {
      throw new Error(
        `записей в ведомости, которым больше нечего исключать: ${stale.length}\n` +
          stale.map((k) => `      ${k}`).join("\n") +
          `\n\nМодуль снят, а послабление осталось. Такая запись переживает свой предмет ` +
          `и унесёт с собой следующую слепую зону — снимите её из ` +
          `${path.relative(uiRoot, LEDGER_FILE)}`,
      );
    }
  });

  it("в ведомости нет записей про модули, ставшие достижимыми", () => {
    const revived = Object.keys(ledger.allowed).filter((k) => allReachable.has(k));
    if (revived.length > 0) {
      throw new Error(
        `записей в ведомости про УЖЕ достижимые модули: ${revived.length}\n` +
          revived.map((k) => `      ${k}`).join("\n") +
          `\n\nДолг закрыт, послабление осталось — снимите его из ` +
          `${path.relative(uiRoot, LEDGER_FILE)}`,
      );
    }
  });

  it("каждая запись ведомости несёт причину, а не пустую строку", () => {
    const withoutReason = Object.entries(ledger.allowed).filter(([, reason]) => reason.trim().length < 10);
    if (withoutReason.length > 0) {
      throw new Error(
        `записей ведомости без причины: ${withoutReason.length}\n` +
          withoutReason.map(([k]) => `      ${k}`).join("\n") +
          `\n\nПослабление без причины неотличимо от забытого`,
      );
    }
  });
});
