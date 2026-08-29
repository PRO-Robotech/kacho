import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

/**
 * Гейт: ОПРОС ЛИБО СНЯТ ПОТОКОМ, ЛИБО ОБЪЯСНЁН.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ (#1021, DoD эпика #1016 п.1)
 *
 * Консоль опрашивала списки каждые три секунды и карточки каждые пять —
 * независимо от того, менялось ли что-нибудь. Поток изменений теперь есть, и
 * опрос снят там, где поток покрывает предмет. Осталось то, что поток не
 * покрывает: домены без журнала (iam, storage, registry), исход мутации
 * (событие пишется в транзакции ресурсной строки, поэтому у ПРОВАЛИВШЕЙСЯ
 * мутации события нет вовсе), сводные величины.
 *
 * Требование эпика — «по каждому оставшемуся названа причина» — держится этим
 * гейтом, а не обещанием: перечень опросов растёт с каждой новой страницей, и
 * причина, названная один раз в отчёте, устарела бы молча.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ТРЕБУЕТСЯ ОТ КАЖДОГО `refetchInterval` В НЕ-ТЕСТОВОМ ДЕРЕВЕ КОНСОЛИ
 *
 * ЛИБО его выражение опирается на признак покрытия, полученный от
 * `useResourceStream` (тогда опрос выключается ровно тогда, когда поток
 * доказанно покрыл вид), ЛИБО рядом стоит причина, начинающаяся маркером
 * `поллинг остаётся:`.
 *
 * Признак покрытия узнаётся НЕ ПО ИМЕНИ переменной, а по её ПРОИСХОЖДЕНИЮ:
 * гейт собирает имена, связанные с вызовом `useResourceStream`, включая
 * переименование при развёртывании (`{ streamed: netStreamed }`). Правило по
 * имени переживало бы переименование и молча пропускало бы любую переменную,
 * названную удачно.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ГЕЙТ НЕ ДЕЛАЕТ И ГОВОРИТ ОБ ЭТОМ ПРЯМО
 *
 * Он не судит, ВЕРНА ли названная причина: «поток этого домена не объявлен»
 * машинно от «мне было лень» не отличить. Он ловит МОЛЧАЛИВОЕ внесение — тот
 * путь, которым опрос и заводится: строка `refetchInterval: 5_000` в новом
 * компоненте не выглядит решением и не обсуждается на обзоре.
 *
 * Он также не судит ВЕЛИЧИНУ интервала: три секунды и тридцать — разные
 * решения, но оба решения, и назвать их обязан автор.
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

const SKIP_DIRS = new Set([
  "node_modules",
  "dist",
  "build",
  "coverage",
  ".vite",
  ".turbo",
  "playwright-report",
  "test-results",
]);

/** Маркер причины. Слово, а не номер задачи: решение и отсрочка выглядят
 *  одинаково, различает их только человек, и он обязан это написать. */
const REASON_MARKER = "поллинг остаётся:";

/** Сколько знаков обязано стоять ПОСЛЕ маркера. Пустая причина — не причина. */
const REASON_MIN_CHARS = 20;

/** Хук, объявляющий покрытие. Единственный источник признака на всё дерево. */
const COVERAGE_HOOK = "useResourceStream";

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (SKIP_DIRS.has(entry) || entry.startsWith(".")) continue;
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) sourceFiles(full, acc);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) acc.push(full);
  }
  return acc;
}

export interface PollSite {
  line: number;
  streamGated: boolean;
  reasoned: boolean;
  expression: string;
}

/** Имена, СВЯЗАННЫЕ с вызовом хука покрытия — включая переименование. */
function coverageNames(src: ts.SourceFile): Set<string> {
  const names = new Set<string>();
  const callsHook = (node: ts.Node | undefined): boolean => {
    if (!node) return false;
    let found = false;
    const walk = (n: ts.Node): void => {
      if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && n.expression.text === COVERAGE_HOOK) found = true;
      ts.forEachChild(n, walk);
    };
    walk(node);
    return found;
  };
  const visit = (node: ts.Node): void => {
    if (ts.isVariableDeclaration(node) && callsHook(node.initializer)) {
      if (ts.isIdentifier(node.name)) names.add(node.name.text);
      else if (ts.isObjectBindingPattern(node.name)) {
        for (const el of node.name.elements) if (ts.isIdentifier(el.name)) names.add(el.name.text);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(src);
  return names;
}

export function analysePolls(src: ts.SourceFile, raw: string): PollSite[] {
  const covering = coverageNames(src);
  const sites: PollSite[] = [];

  const mentionsCoverage = (node: ts.Node): boolean => {
    let hit = false;
    const walk = (n: ts.Node): void => {
      if (ts.isIdentifier(n) && covering.has(n.text)) hit = true;
      ts.forEachChild(n, walk);
    };
    walk(node);
    return hit;
  };

  const visit = (node: ts.Node): void => {
    if (
      ts.isPropertyAssignment(node) &&
      (ts.isIdentifier(node.name) || ts.isStringLiteral(node.name)) &&
      node.name.text === "refetchInterval"
    ) {
      const start = node.getFullStart();
      const end = node.getStart(src);
      // Причина ищется в ПРЕДШЕСТВУЮЩЕМ тексте узла — то есть в комментариях,
      // прилипших к самому свойству. Читать весь файл нельзя: одна причина
      // где-то наверху прикрывала бы все опросы файла разом.
      const lead = raw.slice(start, end);
      const at = lead.indexOf(REASON_MARKER);
      sites.push({
        line: src.getLineAndCharacterOfPosition(node.getStart(src)).line + 1,
        streamGated: mentionsCoverage(node.initializer),
        reasoned: at >= 0 && lead.slice(at + REASON_MARKER.length).trim().length >= REASON_MIN_CHARS,
        expression: node.initializer.getText(src).replace(/\s+/g, " ").slice(0, 80),
      });
    }
    ts.forEachChild(node, visit);
  };
  visit(src);
  return sites;
}

const inspected: { file: string; sites: PollSite[] }[] = [];
let filesRead = 0;
for (const file of sourceFiles(consoleRoot)) {
  const raw = readFileSync(file, "utf8");
  filesRead += 1;
  if (!raw.includes("refetchInterval")) continue;
  const src = ts.createSourceFile(file, raw, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const sites = analysePolls(src, raw);
  if (sites.length > 0) inspected.push({ file: path.relative(consoleRoot, file), sites });
}

const all = inspected.flatMap((i) => i.sites.map((s) => ({ ...s, file: i.file })));
const streamGated = all.filter((s) => s.streamGated);
const reasoned = all.filter((s) => !s.streamGated && s.reasoned);
const unexplained = all.filter((s) => !s.streamGated && !s.reasoned);

process.stdout.write(
  `\n  опрос консоли: файлов прочитано ${filesRead}, файлов с опросом ${inspected.length}, ` +
    `мест опроса ${all.length} — снято потоком ${streamGated.length}, ` +
    `объяснено ${reasoned.length}, без объяснения ${unexplained.length}\n`,
);

describe("опрос консоли: снят потоком либо объяснён", () => {
  it("предпосылка: дерево прочитано и места опроса найдены", () => {
    // Без этого «ноль находок» означало бы «ноль прочитанного»: переименуют
    // ручку react-query — и гейт станет молча зелёным на любом опросе.
    expect(filesRead).toBeGreaterThan(300);
    expect(all.length).toBeGreaterThan(0);
  });

  it("поток и вправду снял часть опроса, а не только объявил такую возможность", () => {
    // Положительный контроль к утверждению ниже. Без него «все объяснены»
    // зеленело бы на дереве, где поток не подключён НИГДЕ, а каждому опросу
    // приписана причина.
    expect(streamGated.length).toBeGreaterThan(0);
  });

  it("у каждого оставшегося опроса названа причина", () => {
    const report = unexplained
      .map((s) => `  ${s.file}:${s.line}: refetchInterval: ${s.expression}`)
      .join("\n");
    expect(
      unexplained.length === 0
        ? ""
        : `мест опроса без объяснения ${unexplained.length}:\n${report}\n\n` +
          `Каждое место обязано ЛИБО опираться на признак покрытия из ${COVERAGE_HOOK}() ` +
          `(тогда опрос выключается, как только поток доказанно покрыл вид), ЛИБО нести рядом ` +
          `причину, начинающуюся маркером «${REASON_MARKER}» и длиной не меньше ${REASON_MIN_CHARS} знаков. ` +
          `Молчаливый опрос — постоянная нагрузка, растущая с числом открытых вкладок и не зависящая ` +
          `от того, менялось ли что-нибудь.`,
    ).toBe("");
  });
});

describe("предпосылка гейта: он различает снятый, объяснённый и молчаливый опрос", () => {
  const parse = (code: string) => {
    const src = ts.createSourceFile("synthetic.tsx", code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    return analysePolls(src, code);
  };

  it("молчит на опросе, снятом потоком", () => {
    const sites = parse(`
      const { streamed } = useResourceStream({ specId: "networks", projectId, invalidate: ["networks"] });
      const q = useQuery({ queryKey: ["networks"], refetchInterval: streamed ? false : 3_000 });
    `);
    expect(sites).toHaveLength(1);
    expect(sites[0].streamGated).toBe(true);
  });

  it("узнаёт признак покрытия и под ДРУГИМ именем — правило по происхождению, не по имени", () => {
    const sites = parse(`
      const { streamed: netCovered } = useResourceStream({ specId: "networks", projectId, invalidate: ["networks"] });
      const q = useQuery({ queryKey: ["networks"], refetchInterval: netCovered ? false : 5_000 });
    `);
    expect(sites[0].streamGated).toBe(true);
  });

  it("молчит на опросе с названной причиной", () => {
    const sites = parse(`
      const q = useQuery({
        queryKey: ["users"],
        // поллинг остаётся: журнала у iam нет, подписаться не на что
        refetchInterval: 5_000,
      });
    `);
    expect(sites[0].reasoned).toBe(true);
    expect(sites[0].streamGated).toBe(false);
  });

  it("находит молчаливый опрос", () => {
    const sites = parse(`const q = useQuery({ queryKey: ["users"], refetchInterval: 5_000 });`);
    expect(sites[0].reasoned).toBe(false);
    expect(sites[0].streamGated).toBe(false);
  });

  it("пустая причина причиной НЕ считается", () => {
    const sites = parse(`
      const q = useQuery({
        queryKey: ["users"],
        // поллинг остаётся: нужен
        refetchInterval: 5_000,
      });
    `);
    expect(sites[0].reasoned).toBe(false);
  });

  it("причина, стоящая у ЧУЖОГО опроса выше, этот не прикрывает", () => {
    const sites = parse(`
      const a = useQuery({
        queryKey: ["users"],
        // поллинг остаётся: журнала у iam нет, подписаться не на что
        refetchInterval: 5_000,
      });
      const b = useQuery({ queryKey: ["roles"], refetchInterval: 5_000 });
    `);
    expect(sites.map((s) => s.reasoned)).toEqual([true, false]);
  });

  it("имя переменной, СЛУЧАЙНО похожее на признак покрытия, не считается покрытием", () => {
    const sites = parse(`
      const streamed = true;
      const q = useQuery({ queryKey: ["users"], refetchInterval: streamed ? false : 5_000 });
    `);
    expect(sites[0].streamGated).toBe(false);
  });
});
