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
 * покрывает: домен без журнала (iam), исход мутации (событие пишется в
 * транзакции ресурсной строки, поэтому у ПРОВАЛИВШЕЙСЯ мутации события нет
 * вовсе), сводные величины и предметы, журналом не ведомые (тег реестра живёт
 * в OCI-данных; тип диска, зона и регион не несут ни проекта, ни типа объекта
 * модели прав).
 *
 * Здесь стояло «домены без журнала (iam, storage, registry)» — и блочное
 * хранение с реестром попали в этот перечень ошибочно: журналы у них есть, а
 * консоль их не отображала. Расхождение держит гейт
 * `ui-future/deploy/console_stream_owner_coverage_test.go`.
 *
 * Требование эпика — «по каждому оставшемуся названа причина» — держится этим
 * гейтом, а не обещанием: перечень опросов растёт с каждой новой страницей, и
 * причина, названная один раз в отчёте, устарела бы молча.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ФОРМ ЗАПИСИ ОПРОСА В ЭТОМ ДЕРЕВЕ ДВЕ, И СУДЯТСЯ ОНИ ОДИНАКОВО
 *
 * Прежняя редакция знала ОДНУ — свойство запроса `refetchInterval`. Вторая
 * форма, повторитель браузера (`setInterval` / `window.setInterval`), делает
 * ровно то же самое и была ВНЕ НАБЛЮДЕНИЯ: не находкой и не разрешением, а
 * ничем. Замер, которым это нашлось (`e62ab6681`, дерево консоли): свойством
 * запроса записано 34 места, повторителем — 2, и обоим повторителям причина не
 * называлась ни разу.
 *
 * Цена молчания измерена, а не предположена: `dashboard/src/hooks/`
 * `use-module-counts.ts` тикает раз в минуту, и шесть его повторителей вместе
 * читают четырнадцать списочных путей по тысяче элементов НА ВКЛАДКУ — то есть
 * самый дорогой опрос консоли жил в форме, которой гейт не видел. Предмет и
 * предикат снятия — задача продукта #1632. И он же нёс подпорку: комментарий рядом
 * объяснял, что интервал подняли с 15 с до 60 с, «чтобы снять основную фоновую
 * нагрузку», — отсрочка без предмета, ровно то, что гейт и обязан ловить.
 *
 * Односторонний счёт здесь не годится: «мест опроса N» одним числом скрывает
 * ровно тот случай, ради которого форма добавлена. Перепись печатает величины
 * ПО ФОРМАМ раздельно, и расширение распознавателя обязано менять осмотренное.
 *
 * Третьей формы дерево не производит: рекурсивного `setTimeout`, ставящего сам
 * себя заново, в прод-коде консоли нет ни одного. Все 11 его вхождений вне
 * `e2e/` одноразовы — гашение подсказки о копировании (4), дребезг ввода (2),
 * снятие уведомления, автоснятие плашки операции по исходу (2) и ограниченная
 * очередь перечитываний после мутации (1). Ветки под рекурсию здесь нет намеренно: у неё не было бы
 * производителя, а значит и доказательства работоспособности — распознаватель,
 * которому нечего узнавать, молчит одинаково и когда предмета нет, и когда он
 * сломан.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ТРЕБУЕТСЯ ОТ КАЖДОГО МЕСТА ОПРОСА В НЕ-ТЕСТОВОМ ДЕРЕВЕ КОНСОЛИ
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
 * Причина ищется в тривии, прилипшей к САМОМУ месту опроса: у свойства запроса —
 * к свойству, у повторителя — к его инструкции (комментарий над `const t =
 * setInterval(…)` стоит над инструкцией, а не между `=` и вызовом). Читать весь
 * файл нельзя: одна причина где-то наверху прикрывала бы все опросы разом.
 *
 * ГРАНИЦА ОБХОДА названа, а не подразумевается: сквозные пробы (`e2e/`) обходятся
 * НАРАВНЕ с прод-кодом, и это решение, а не недосмотр. Опрос запрещён и там —
 * проба ждёт условие, а не тикает (`e2e-flow.md` §4), — поэтому требование
 * «назови причину» для неё столь же уместно. Обе формы судятся одинаково широко:
 * узкая для одной формы граница была бы различием, которого никто не решал.
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

/**
 * Хуки, объявляющие покрытие потоком.
 *
 * Их ДВА, и это не форк: предметы разные. `useResourceStream` перечитывает
 * названный ключ запроса и потому неотделим от react-query; `useStreamCoverage`
 * отдаёт только признак и годится вызывающему, у которого клиента запросов нет
 * вовсе (витрина считает счётчики своим загрузчиком, #1632). Клиент потока при
 * этом один на оба — `subscriptionHub()`, и единственность его дома держит
 * братский гейт `console-stream-client-single`.
 *
 * Перечень закрыт и ИСТЕКАЕТ САМ: имя, у которого в дереве не осталось
 * объявления, — находка, а не «про запас». Иначе распознаватель хранил бы
 * мёртвое имя и молчал бы одинаково — и когда покрытия нет, и когда его
 * перестали узнавать.
 */
const COVERAGE_HOOKS = ["useResourceStream", "useStreamCoverage"] as const;

/** Повторитель браузера — вторая законная форма записи опроса. */
const INTERVAL_SCHEDULER = "setInterval";

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (SKIP_DIRS.has(entry) || entry.startsWith(".")) continue;
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) sourceFiles(full, acc);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) acc.push(full);
  }
  return acc;
}

/** Форма записи опроса. Считаются раздельно — см. шапку. */
export type PollForm = "query" | "interval";

export interface PollSite {
  line: number;
  form: PollForm;
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
      if (
        ts.isCallExpression(n) &&
        ts.isIdentifier(n.expression) &&
        (COVERAGE_HOOKS as readonly string[]).includes(n.expression.text)
      )
        found = true;
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

/** Зовёт ли выражение повторитель браузера — голым именем либо через носитель
 *  (`window.` / `globalThis.`). Носитель не перечисляется: судится ИМЯ метода,
 *  поэтому `self.setInterval` и любой другой носитель узнаются тоже. */
function isIntervalCall(node: ts.Node): node is ts.CallExpression {
  if (!ts.isCallExpression(node)) return false;
  const callee = node.expression;
  if (ts.isIdentifier(callee)) return callee.text === INTERVAL_SCHEDULER;
  if (ts.isPropertyAccessExpression(callee)) return callee.name.text === INTERVAL_SCHEDULER;
  return false;
}

/** Инструкция, к которой прилипла тривия повторителя. У `const t =
 *  setInterval(…)` собственной тривии у вызова нет — комментарий стоит над
 *  инструкцией, и искать причину надо там. */
function enclosingStatement(node: ts.Node): ts.Node {
  let cur: ts.Node = node;
  while (cur.parent && !ts.isStatement(cur)) cur = cur.parent;
  return cur;
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

  /** Причина — в тривии ПЕРЕД указанным узлом, и только в ней. */
  const reasonedBefore = (node: ts.Node): boolean => {
    const lead = raw.slice(node.getFullStart(), node.getStart(src));
    const at = lead.indexOf(REASON_MARKER);
    return at >= 0 && lead.slice(at + REASON_MARKER.length).trim().length >= REASON_MIN_CHARS;
  };

  const visit = (node: ts.Node): void => {
    if (
      ts.isPropertyAssignment(node) &&
      (ts.isIdentifier(node.name) || ts.isStringLiteral(node.name)) &&
      node.name.text === "refetchInterval"
    ) {
      sites.push({
        line: src.getLineAndCharacterOfPosition(node.getStart(src)).line + 1,
        form: "query",
        streamGated: mentionsCoverage(node.initializer),
        reasoned: reasonedBefore(node),
        expression: node.initializer.getText(src).replace(/\s+/g, " ").slice(0, 80),
      });
    } else if (isIntervalCall(node)) {
      // Признак покрытия ищется во ВСЕХ доводах вызова: гасят повторитель и
      // задержкой (`streamed ? undefined : N`), и самим телом. Считать только
      // задержку значило бы завести форму гашения, которой гейт не признаёт.
      const delay = node.arguments.length > 1 ? node.arguments[1].getText(src) : "";
      sites.push({
        line: src.getLineAndCharacterOfPosition(node.getStart(src)).line + 1,
        form: "interval",
        streamGated: node.arguments.some((arg) => mentionsCoverage(arg)),
        reasoned: reasonedBefore(enclosingStatement(node)),
        expression: `${INTERVAL_SCHEDULER}(…, ${delay.replace(/\s+/g, " ")})`.slice(0, 80),
      });
    }
    ts.forEachChild(node, visit);
  };
  visit(src);
  return sites;
}

const inspected: { file: string; sites: PollSite[] }[] = [];
/** Хуки покрытия, у которых в дереве НАШЛОСЬ объявление. */
const declaredHooks = new Set<string>();
let filesRead = 0;
for (const file of sourceFiles(consoleRoot)) {
  const raw = readFileSync(file, "utf8");
  filesRead += 1;
  for (const hook of COVERAGE_HOOKS) {
    if (new RegExp(`export function ${hook}\\b`).test(raw)) declaredHooks.add(hook);
  }
  if (!raw.includes("refetchInterval") && !raw.includes(INTERVAL_SCHEDULER)) continue;
  const src = ts.createSourceFile(file, raw, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const sites = analysePolls(src, raw);
  if (sites.length > 0) inspected.push({ file: path.relative(consoleRoot, file), sites });
}

const all = inspected.flatMap((i) => i.sites.map((s) => ({ ...s, file: i.file })));
const streamGated = all.filter((s) => s.streamGated);
const reasoned = all.filter((s) => !s.streamGated && s.reasoned);
const unexplained = all.filter((s) => !s.streamGated && !s.reasoned);
const byForm = (form: PollForm) => all.filter((s) => s.form === form);

const tally = (form: PollForm): string => {
  const f = byForm(form);
  return (
    `${f.length} — снято потоком ${f.filter((s) => s.streamGated).length}, ` +
    `объяснено ${f.filter((s) => !s.streamGated && s.reasoned).length}, ` +
    `без объяснения ${f.filter((s) => !s.streamGated && !s.reasoned).length}`
  );
};

process.stdout.write(
  `\n  опрос консоли: файлов прочитано ${filesRead}, файлов с опросом ${inspected.length}, ` +
    `мест опроса ${all.length} — снято потоком ${streamGated.length}, ` +
    `объяснено ${reasoned.length}, без объяснения ${unexplained.length}\n` +
    `    по формам: свойство запроса ${tally("query")}\n` +
    `               повторитель браузера ${tally("interval")}\n` +
    `    хуки покрытия: объявлено в дереве ${declaredHooks.size} из ${COVERAGE_HOOKS.length} ` +
    `(${COVERAGE_HOOKS.map((h) => `${h}: ${declaredHooks.has(h) ? "есть" : "НЕТ"}`).join(", ")})\n`,
);

describe("опрос консоли: снят потоком либо объяснён", () => {
  it("предпосылка: дерево прочитано и места опроса найдены", () => {
    // Без этого «ноль находок» означало бы «ноль прочитанного»: переименуют
    // ручку react-query — и гейт станет молча зелёным на любом опросе.
    expect(filesRead).toBeGreaterThan(300);
    expect(all.length).toBeGreaterThan(0);
  });

  it("предпосылка: у КАЖДОГО имени хука покрытия есть объявление в дереве", () => {
    // Самоистечение перечня. Имя, чьё объявление из дерева ушло, распознаватель
    // хранил бы молча — и молчал бы одинаково, когда покрытия нет и когда его
    // перестали узнавать. Снимут хук — эта проба назовёт его по имени, а не
    // оставит мёртвую ветку «про запас».
    const missing = COVERAGE_HOOKS.filter((h) => !declaredHooks.has(h));
    expect(
      missing.length === 0
        ? ""
        : `имён хука покрытия без объявления в дереве ${missing.length}: ${missing.join(", ")}. ` +
          `Либо хук сняли — тогда имя убирается из COVERAGE_HOOKS тем же изменением, ` +
          `либо его перестали узнавать, и тогда каждый опрос, гасившийся им, стал молчаливым.`,
    ).toBe("");
  });

  it("предпосылка: у КАЖДОЙ формы записи есть производитель в дереве", () => {
    // Форма без единого места — распознаватель, работоспособность которого
    // ничем не подтверждена: он одинаково молчит и когда предмета нет, и когда
    // он перестал узнаваться. Обнулится любая — это находка, а не улучшение:
    // либо форму сняли и ветку надо снять вместе с ней, либо перестали узнавать.
    expect(byForm("query").length).toBeGreaterThan(0);
    expect(byForm("interval").length).toBeGreaterThan(0);
  });

  it("поток и вправду снял часть опроса, а не только объявил такую возможность", () => {
    // Положительный контроль к утверждению ниже. Без него «все объяснены»
    // зеленело бы на дереве, где поток не подключён НИГДЕ, а каждому опросу
    // приписана причина.
    expect(streamGated.length).toBeGreaterThan(0);
  });

  it("у каждого оставшегося опроса названа причина", () => {
    const report = unexplained
      .map((s) => `  ${s.file}:${s.line}: [${s.form}] ${s.expression}`)
      .join("\n");
    expect(
      unexplained.length === 0
        ? ""
        : `мест опроса без объяснения ${unexplained.length}:\n${report}\n\n` +
          `Каждое место обязано ЛИБО опираться на признак покрытия из ` +
          `${COVERAGE_HOOKS.map((h) => `${h}()`).join(" / ")} ` +
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

describe("вторая форма: повторитель браузера судится наравне со свойством запроса", () => {
  const parse = (code: string) => {
    const src = ts.createSourceFile("synthetic.tsx", code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    return analysePolls(src, code);
  };

  it("узнаёт признак покрытия ВТОРОГО хука — того, что живёт без react-query", () => {
    // Инъекция по второй форме объявления покрытия. Без неё расширение
    // распознавателя было бы принято на веру: он молчал бы на этом опросе и
    // тогда, когда узнаёт хук, и тогда, когда просто не видит места.
    const sites = parse(`
      const { streamed } = useStreamCoverage({ specIds: ["networks"], projectId, onChanged: reload });
      const t = setInterval(() => { if (streamed) return; void reload(); }, 60_000);
    `);
    expect(sites).toHaveLength(1);
    expect(sites[0].form).toBe("interval");
    expect(sites[0].streamGated).toBe(true);
  });

  it("законный близнец: тот же повторитель БЕЗ признака покрытия остаётся находкой", () => {
    // Контроль в обратную сторону к утверждению выше: узнаётся ПРОИСХОЖДЕНИЕ
    // имени от вызова хука, а не форма записи повторителя.
    const sites = parse(`
      const streamed = someOtherThing();
      const t = setInterval(() => { if (streamed) return; void reload(); }, 60_000);
    `);
    expect(sites[0].streamGated).toBe(false);
    expect(sites[0].reasoned).toBe(false);
  });

  it("находит молчаливый повторитель — голым именем", () => {
    const sites = parse(`const t = setInterval(() => { void reload(); }, 60_000);`);
    expect(sites).toHaveLength(1);
    expect(sites[0].form).toBe("interval");
    expect(sites[0].reasoned).toBe(false);
    expect(sites[0].streamGated).toBe(false);
  });

  it("находит молчаливый повторитель и через носитель (`window.`)", () => {
    const sites = parse(`const t = window.setInterval(() => { void reload(); }, 60_000);`);
    expect(sites).toHaveLength(1);
    expect(sites[0].form).toBe("interval");
    expect(sites[0].reasoned).toBe(false);
  });

  it("молчит на повторителе с названной причиной над его инструкцией", () => {
    const sites = parse(`
      // поллинг остаётся: журнала у этого домена нет, подписаться не на что
      const t = window.setInterval(() => { void reload(); }, 60_000);
    `);
    expect(sites[0].reasoned).toBe(true);
    expect(sites[0].streamGated).toBe(false);
  });

  it("молчит на повторителе, погашенном признаком покрытия", () => {
    const sites = parse(`
      const { streamed } = useResourceStream({ specId: "networks", projectId, invalidate: ["networks"] });
      const t = window.setInterval(() => { void reload(); }, streamed ? 600_000 : 60_000);
    `);
    expect(sites[0].streamGated).toBe(true);
  });

  it("пустая причина у повторителя причиной НЕ считается", () => {
    const sites = parse(`
      // поллинг остаётся: надо
      const t = setInterval(() => { void reload(); }, 60_000);
    `);
    expect(sites[0].reasoned).toBe(false);
  });

  it("причина у ЧУЖОГО повторителя выше этот не прикрывает", () => {
    const sites = parse(`
      // поллинг остаётся: журнала у этого домена нет, подписаться не на что
      const a = setInterval(() => { void one(); }, 60_000);
      const b = setInterval(() => { void two(); }, 60_000);
    `);
    expect(sites.map((s) => s.reasoned)).toEqual([true, false]);
  });

  it("ЗАКОННЫЙ БЛИЗНЕЦ: одноразовый `setTimeout` местом опроса не является", () => {
    // Дребезг ввода, гашение подсказки, снятие уведомления — двенадцать таких
    // мест в дереве. Потребуй гейт причину и от них — его сняли бы первым как
    // краснеющий на верном коде.
    const sites = parse(`
      const t = setTimeout(() => setCopied(false), 1500);
      const d = window.setTimeout(() => setSettled(value), 300);
    `);
    expect(sites).toHaveLength(0);
  });

  it("ЗАКОННЫЙ БЛИЗНЕЦ: снятие повторителя местом опроса не является", () => {
    const sites = parse(`
      function stop(t: number) { window.clearInterval(t); clearInterval(t); }
    `);
    expect(sites).toHaveLength(0);
  });

  it("имя, СЛУЧАЙНО похожее на признак покрытия, повторитель не гасит", () => {
    const sites = parse(`
      const streamed = true;
      const t = setInterval(() => { void reload(); }, streamed ? 600_000 : 60_000);
    `);
    expect(sites[0].streamGated).toBe(false);
  });
});
