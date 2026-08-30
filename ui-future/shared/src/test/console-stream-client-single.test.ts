import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

/**
 * Гейт: КЛИЕНТ ПОТОКА В КОНСОЛИ ОДИН.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ (решение владельца 2026-08-22, тело #1021)
 *
 * «Клиент подписки в консоли — один переиспользуемый механизм, а не свой у
 * каждого модуля. Признак нарушения: `EventSource`/клиент потока встречается
 * больше чем в одном месте дерева консоли.»
 *
 * Довод не стилистический, и он уже измерен в этом дереве: консоль форкнута по
 * девяти микрофронтендам, и копии расходятся МОЛЧА (`ui.md` §«Незакрытый форк»:
 * из ста парных файлов разошлась четверть). Второй клиент потока разошёлся бы с
 * первым в том, что не видно на экране, — в возобновлении с позиции, в разборе
 * покрытия, в поведении на закрытии канала, — и разошёлся бы именно там, где
 * расхождение читается как «изменений не было».
 *
 * До этого гейта свойство держалось ВНИМАНИЕМ: сам запрет был записан, а
 * проверки у него не было ни одной.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРИЗНАКОВ ДВА, ПОТОМУ ЧТО ФОРМ ВТОРОГО КЛИЕНТА ТОЖЕ ДВЕ
 *
 * · ПОСТРОЕНИЕ приёмника — `new EventSource(…)`. Имя приёмника принадлежит
 *   браузеру и переименованию не подлежит, поэтому признак невозможно обойти
 *   переименованием — в отличие от признака по имени нашего хука.
 * · АДРЕС ПОТОКА — строковый литерал `/subscription/v1/events`. Клиент, читающий
 *   тот же адрес обычным запросом (`fetch` + разбор тела), приёмника не строит и
 *   первому признаку невидим by construction; адрес же назвать обязан.
 *
 * Обоим признакам разрешён РОВНО ОДИН файл — `shared/src/lib/subscription/hub.ts`.
 * Модули берут механизм оттуда (`useResourceStream` → `subscriptionHub()`), и
 * второго дома у него нет.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ РАЗБОР, А НЕ ПОИСК ПО ОБРАЗЦУ
 *
 * Оба признака встречаются в ПРОЗЕ этого дерева: `hub.ts` объясняет
 * `EventSource` четырьмя комментариями, `subjects.ts` называет адрес потока,
 * рассказывая про контракт арендатору, и эта самая шапка называет оба. Гейт по
 * подстроке краснел бы на собственном объяснении. Судятся УЗЛЫ — построение и
 * строковый литерал, — а не текст.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ИЗ ОБХОДА ВЫВЕДЕНО И ПОЧЕМУ
 *
 * · ПРОБЫ (`*.test.ts(x)`) — подделка приёмника и утверждение об адресе суть их
 *   работа (`hub.test.ts` строит `FakeSource`, проба оболочки хранения
 *   утверждает адрес дословно). Требовать от них единственности значило бы
 *   запретить проверять предмет.
 * · СКВОЗНЫЕ ПРОБЫ (`e2e/`) — они говорят с КРАЕМ, а не с консолью, и делают это
 *   намеренно своей формой: одна открывает поток `EventSource`-ом в контексте
 *   страницы, другая держит СВОЮ копию адреса (`e2e/specs/fixtures.ts`). Копия
 *   здесь — не форк, а независимость пробы: проба, берущая координату у предмета
 *   проверки, подтверждала бы сама себя.
 *
 * Обе границы названы здесь, а не подразумеваются: невыраженная граница обхода
 * превращает «ноль находок» в «ноль прочитанного».
 */

const consoleRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../..",
);

/** Единственный законный дом клиента потока — от корня дерева консоли. */
const HUB = "shared/src/lib/subscription/hub.ts";

/** Имя приёмника событий браузера. Принадлежит платформе, переименованию не подлежит. */
const RECEIVER = "EventSource";

/** Адрес потока. Совпадение ищется вхождением: клиент вправе собрать запрос. */
const STREAM_PATH_FRAGMENT = "subscription/v1/events";

const SKIP_DIRS = new Set([
  "node_modules",
  "dist",
  "build",
  "coverage",
  ".vite",
  ".turbo",
  "playwright-report",
  "test-results",
  // Сквозные пробы говорят с краем, а не с консолью — см. шапку.
  "e2e",
]);

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (SKIP_DIRS.has(entry) || entry.startsWith(".")) continue;
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) sourceFiles(full, acc);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry))
      acc.push(full);
  }
  return acc;
}

export interface ClientMarks {
  /** Сколько раз файл СТРОИТ приёмник событий. */
  constructs: number;
  /** Сколько строковых литералов файла несут адрес потока. */
  addresses: number;
}

/** Судит УЗЛЫ: построение приёмника и строковый литерал с адресом. Проза,
 *  называющая то же самое, признаком не является. */
export function markStreamClient(src: ts.SourceFile): ClientMarks {
  let constructs = 0;
  let addresses = 0;
  const visit = (node: ts.Node): void => {
    if (
      ts.isNewExpression(node) &&
      ts.isIdentifier(node.expression) &&
      node.expression.text === RECEIVER
    ) {
      constructs += 1;
    }
    if (
      (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) &&
      node.text.includes(STREAM_PATH_FRAGMENT)
    ) {
      addresses += 1;
    }
    ts.forEachChild(node, visit);
  };
  visit(src);
  return { constructs, addresses };
}

const marked: { file: string; marks: ClientMarks }[] = [];
let filesRead = 0;
for (const file of sourceFiles(consoleRoot)) {
  const raw = readFileSync(file, "utf8");
  filesRead += 1;
  if (!raw.includes(RECEIVER) && !raw.includes(STREAM_PATH_FRAGMENT)) continue;
  const src = ts.createSourceFile(
    file,
    raw,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  const marks = markStreamClient(src);
  if (marks.constructs > 0 || marks.addresses > 0)
    marked.push({ file: path.relative(consoleRoot, file), marks });
}

const builders = marked.filter((m) => m.marks.constructs > 0);
const addressers = marked.filter((m) => m.marks.addresses > 0);

process.stdout.write(
  `\n  клиент потока консоли: файлов прочитано ${filesRead}, ` +
    `строят приёмник ${builders.length}, называют адрес ${addressers.length}` +
    ` (дом — ${HUB})\n`,
);

describe("клиент потока в консоли — один механизм", () => {
  it("предпосылка: дерево прочитано", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: сдвинется
    // корень или переименуется раскладка — и все утверждения ниже станут
    // вакуумно истинными.
    expect(filesRead).toBeGreaterThan(300);
  });

  it("предпосылка: распознаватель находит САМ дом — иначе он не находит ничего", () => {
    // Положительный контроль. Без него «второго клиента нет» зеленело бы и на
    // дереве, где нет ПЕРВОГО, — то есть там, где распознаватель сломан.
    const hub = marked.find((m) => m.file === HUB);
    expect(
      hub === undefined ? `дом клиента потока ${HUB} не распознан вовсе` : "",
    ).toBe("");
    expect(hub!.marks.constructs).toBeGreaterThan(0);
    expect(hub!.marks.addresses).toBeGreaterThan(0);
  });

  it("приёмник событий строит ровно один файл — дом", () => {
    const outside = builders
      .filter((m) => m.file !== HUB)
      .map((m) => `  ${m.file}`);
    expect(
      outside.length === 0
        ? ""
        : `приёмник потока строят ещё ${outside.length} файлов помимо ${HUB}:\n${outside.join("\n")}\n\n` +
            `Клиент подписки в консоли — ОДИН механизм (решение владельца 2026-08-22, тело #1021). ` +
            `Второй разошёлся бы с первым молча — в возобновлении с позиции, в разборе покрытия, ` +
            `в поведении на закрытии канала, — и расхождение читалось бы как «изменений не было». ` +
            `Механизм берётся из ${HUB} через useResourceStream().`,
    ).toBe("");
  });

  it("адрес потока называет ровно один файл — дом", () => {
    const outside = addressers
      .filter((m) => m.file !== HUB)
      .map((m) => `  ${m.file}`);
    expect(
      outside.length === 0
        ? ""
        : `адрес потока назван ещё в ${outside.length} файлах помимо ${HUB}:\n${outside.join("\n")}\n\n` +
            `Клиент, читающий тот же адрес обычным запросом, приёмника не строит и первому признаку ` +
            `невидим — но адрес назвать обязан. Второй дом адреса означает второй клиент либо вторую ` +
            `копию координаты, которая разойдётся с ${HUB} молча.`,
    ).toBe("");
  });
});

describe("предпосылка гейта: он судит узлы, а не текст", () => {
  const parse = (code: string) => {
    const src = ts.createSourceFile(
      "synthetic.tsx",
      code,
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.TSX,
    );
    return markStreamClient(src);
  };

  it("находит построение приёмника", () => {
    expect(
      parse(`const s = new EventSource("/x", { withCredentials: true });`)
        .constructs,
    ).toBe(1);
  });

  it("находит адрес потока в строковом литерале", () => {
    expect(parse(`const p = "/subscription/v1/events";`).addresses).toBe(1);
  });

  it("находит адрес и в шаблонном литерале без подстановок", () => {
    expect(parse("const p = `/subscription/v1/events`;").addresses).toBe(1);
  });

  it("ЗАКОННЫЙ БЛИЗНЕЦ: проза про приёмник и адрес признаком НЕ является", () => {
    // Ровно та форма, в которой оба имени стоят в шапке этого файла и в
    // комментариях `hub.ts`/`subjects.ts`. Гейт по подстроке краснел бы здесь.
    const marks = parse(`
      // Приёмник \`EventSource\` заголовков не ставит, поэтому личность едет печеньем.
      /* Публичная ручка GET /subscription/v1/events отдаёт кадр открытия. */
      export const nothing = 1;
    `);
    expect(marks).toEqual({ constructs: 0, addresses: 0 });
  });

  it("ЗАКОННЫЙ БЛИЗНЕЦ: ТИП приёмника и его подделка построением не считаются", () => {
    // `hub.ts` объявляет `EventSourceLike`, пробы реализуют его классом-подделкой.
    // Ни то, ни другое приёмника браузера не строит.
    const marks = parse(`
      export interface EventSourceLike { close(): void }
      class FakeSource implements EventSourceLike { close() {} }
      const s: EventSourceLike = new FakeSource();
    `);
    expect(marks.constructs).toBe(0);
  });

  it("ЗАКОННЫЙ БЛИЗНЕЦ: соседний адрес того же домена адресом потока НЕ является", () => {
    expect(parse(`const p = "/subscription/v1/owners";`).addresses).toBe(0);
  });
});
