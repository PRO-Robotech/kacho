import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { stripComments } from "@shared/test/strip-comments";

/**
 * Гейт: консоль не читает поле, снятое с контракта.
 *
 * # Класс
 *
 * Ветка кода, выбираемая по полю, чьи номер и имя у сообщения зарезервированы,
 * не выбирается НИКОГДА: край такого поля не отдаёт и не принимает. Со стороны
 * она неотличима от работающей — компилируется, читается, обсуждается на обзоре
 * как поддержка ещё одного случая. Это `doc-truthfulness` в коде: утверждение,
 * пережившее свой предмет.
 *
 * Три формы, все три найдены за один заход по дереву (#512):
 *
 *   ВЕТВЬ      цепочка выбора вида цели правила группы безопасности несла
 *              четвёртую ветвь на снятом поле — она не выбиралась ни разу;
 *   СТРОКА     карточка слушателя показывала «Порт на цели» из снятого поля —
 *              прочерк ВСЕГДА, и читается он как «не задано», а не «такого нет»;
 *   ТИП        объявление поля в интерфейсе консоли — обещание ручки, которой у
 *              контракта нет; следующий читатель принимает его за доступную.
 *
 * # Предикат, и почему он не ловит однофамильцев
 *
 * `reserved "x"` связывает имя с ОДНИМ сообщением, поэтому «имя встречается в
 * reserved» находкой не является: `name`, `status`, `project_id`, `ttl_seconds`
 * зарезервированы у одного сообщения и живы у десятка других. Гейт считает
 * снятым только имя, которое НЕ объявлено живым полем НИ У ОДНОГО сообщения
 * дерева, — тогда ссылка на него из консоли не может относиться ни к чему
 * живому, и вопрос «о каком сообщении речь» не встаёт вовсе. Замер на заведении:
 * имён в `reserved` 65, из них снятых полностью 41, однофамильцев 24.
 *
 * # Чего гейт НЕ видит, и это сказано, а не умолчано
 *
 * Односложные снятые имена (`view`, `requirements`) он ищет только в позиции
 * ОБРАЩЕНИЯ к свойству (`.view`, `view?:`), но не в строковом литерале: литерал
 * `"view"` в консоли почти всегда означает не поле контракта, а слово. Змеиные
 * имена ищутся и в литерале — `"target_port"` ничем другим быть не может.
 * Следствие: `getByPath(data, "view")` этот гейт пропустит. Ослабление
 * НАМЕРЕННОЕ: первый же ложный срабат отключил бы проверку целиком, а слепое
 * пятно здесь ровно на двух именах и названо.
 *
 * Второе, чего он не видит: поле, снятое у ОДНОГО сообщения и живое у другого
 * (`ttl_seconds`). Такую находку нельзя отличить от законной, не сопоставив тип
 * консоли с сообщением контракта, — а сопоставления по имени между ними нет
 * (`SgRule` против `SecurityGroupRule`). Заводить догадку по имени значило бы
 * краснеть на законном коде.
 *
 * # Объём осмотренного и собственная предпосылка
 *
 * Печатается перепись: файлов контракта, имён в `reserved`, живых имён, снятых
 * полностью, исходников консоли прочитано, находок. Предпосылка утверждается
 * отдельно — пустой обход дерева (переименовали каталог, сменили расширение) не
 * имеет права выглядеть как чистое дерево.
 *
 * # Пустой перечень — цель, а не поломка
 *
 * Ноль находок гейт проходит: ноль и есть то состояние, ради которого он
 * заведён. Способность упасть доказывается инъекцией на синтетике в обе стороны
 * — дефектная форма краснеет с координатой, законная молчит.
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const repoRoot = path.resolve(consoleRoot, "..");

// ─────────────────────────────────────────────────────────────────────────────
// Разбор контракта.
// ─────────────────────────────────────────────────────────────────────────────

const RESERVED_NAMES = /reserved\s+((?:"[a-zA-Z_0-9]+"\s*,?\s*)+);/g;
const MESSAGE_FIELD = /^\s*(?:repeated\s+|optional\s+)?[\w.<>, ]+\s+([a-z_][a-zA-Z_0-9]*)\s*=\s*\d+/;
const MAP_FIELD = /^\s*map<[^>]+>\s+([a-z_][a-zA-Z_0-9]*)\s*=\s*\d+/;
const ENUM_OPEN = /^\s*enum\s+\w+/;
const ENUM_VALUE = /^\s*([A-Z][A-Z_0-9]*)\s*=\s*\d+/;

interface ContractNames {
  reserved: Set<string>;
  live: Set<string>;
  files: number;
}

/**
 * Имена контракта: зарезервированные У СООБЩЕНИЙ и живые (поля + значения
 * перечислений).
 *
 * Значения перечислений попадают в «живые» намеренно: `reserved` в `enum`
 * снимает ЗНАЧЕНИЕ, а не поле, и без этого разделения `DELETING`, снятое у
 * одного перечисления, читалось бы как снятое имя поля — и гейт краснел бы на
 * каждой карте состояний консоли.
 */
export function contractNames(protoSources: Array<{ rel: string; text: string }>): ContractNames {
  const reserved = new Set<string>();
  const live = new Set<string>();
  for (const { text } of protoSources) {
    const body = stripComments(text, { keepLines: true });
    let inEnum = false;
    let depth = 0;
    for (const line of body.split("\n")) {
      if (ENUM_OPEN.test(line)) {
        inEnum = true;
        depth = 0;
      }
      if (inEnum) {
        depth += (line.match(/\{/g) ?? []).length - (line.match(/\}/g) ?? []).length;
        const v = ENUM_VALUE.exec(line);
        if (v) live.add(v[1]);
        if (depth <= 0 && line.includes("}")) inEnum = false;
        continue;
      }
      RESERVED_NAMES.lastIndex = 0;
      let m: RegExpExecArray | null;
      while ((m = RESERVED_NAMES.exec(line)) !== null) {
        for (const q of m[1].match(/"[a-zA-Z_0-9]+"/g) ?? []) reserved.add(q.slice(1, -1));
      }
      const f = MESSAGE_FIELD.exec(line) ?? MAP_FIELD.exec(line);
      if (f) live.add(f[1]);
    }
  }
  return { reserved, live, files: protoSources.length };
}

/** Имена, снятые ПОЛНОСТЬЮ: зарезервированы и не живы нигде. */
export function retiredNames(names: ContractNames): string[] {
  return [...names.reserved].filter((n) => !names.live.has(n)).sort();
}

function camelOf(snake: string): string {
  const [head, ...rest] = snake.split("_");
  return head + rest.map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join("");
}

/**
 * Ссылка на снятое имя в исходнике консоли — координата и имя, либо `null`.
 *
 * Комментарии снимаются ДО поиска: гейт обязан читать исполняемую часть. Иначе
 * он краснел бы на объяснении, почему поля больше нет, — то есть запрещал бы
 * называть предмет запрета, что само по себе отдельный класс.
 */
export function retiredReferences(
  source: string,
  retired: string[],
): Array<{ line: number; name: string; text: string }> {
  const found: Array<{ line: number; name: string; text: string }> = [];
  const lines = stripComments(source, { keepLines: true }).split("\n");
  const original = source.split("\n");
  for (const name of retired) {
    const forms = name.includes("_") ? [name, camelOf(name)] : [name];
    for (const form of forms) {
      const e = form.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      const patterns = [new RegExp(`\\.${e}\\b`), new RegExp(`^\\s*${e}\\??\\s*:`)];
      // Литерал считается ссылкой только у змеиного имени: односложное слово в
      // кавычках почти всегда не поле контракта (см. шапку).
      if (name.includes("_")) patterns.push(new RegExp(`["'\`]${e}["'\`]`));
      lines.forEach((line, i) => {
        if (patterns.some((p) => p.test(line))) {
          found.push({ line: i + 1, name, text: (original[i] ?? "").trim().slice(0, 120) });
        }
      });
    }
  }
  return found.sort((a, b) => a.line - b.line);
}

// ─────────────────────────────────────────────────────────────────────────────
// Обход дерева.
// ─────────────────────────────────────────────────────────────────────────────

const SKIP_DIRS = new Set(["node_modules", "dist", "build", ".git", "coverage", "e2e"]);

function walk(root: string, keep: (rel: string) => boolean, acc: string[] = [], base = root): string[] {
  let entries: string[];
  try {
    entries = readdirSync(root);
  } catch {
    return acc;
  }
  for (const entry of entries) {
    const full = path.join(root, entry);
    let s;
    try {
      s = statSync(full);
    } catch {
      continue;
    }
    if (s.isDirectory()) {
      if (!SKIP_DIRS.has(entry)) walk(full, keep, acc, base);
      continue;
    }
    const rel = path.relative(base, full);
    if (keep(rel)) acc.push(full);
  }
  return acc;
}

const protoFiles = walk(path.join(repoRoot, "proto"), (rel) => rel.endsWith(".proto"));
const protoSources = protoFiles.map((f) => ({
  rel: path.relative(repoRoot, f),
  text: readFileSync(f, "utf8"),
}));

const NAMES = contractNames(protoSources);
const RETIRED = retiredNames(NAMES);

const consoleFiles = walk(
  consoleRoot,
  (rel) => /\.(ts|tsx)$/.test(rel) && !/\.(test|spec)\.(ts|tsx)$/.test(rel),
);

interface Finding {
  where: string;
  name: string;
  text: string;
}

const FINDINGS: Finding[] = [];
for (const file of consoleFiles) {
  const rel = path.relative(consoleRoot, file);
  for (const hit of retiredReferences(readFileSync(file, "utf8"), RETIRED)) {
    FINDINGS.push({ where: `${rel}:${hit.line}`, name: hit.name, text: hit.text });
  }
}

// ─────────────────────────────────────────────────────────────────────────────

describe("консоль не читает поле, снятое с контракта", () => {
  it("перепись осмотренного названа, и предпосылка выполняется", () => {
    // Утверждение об отсутствии находок стоит ровно столько, сколько прочитано.
    // Пустой обход (переименовали каталог, сменили расширение) обязан краснеть,
    // а не выглядеть чистым деревом.
    expect(protoSources.length).toBeGreaterThan(50);
    expect(consoleFiles.length).toBeGreaterThan(100);
    expect(NAMES.live.size).toBeGreaterThan(200);
    expect(RETIRED.length).toBeGreaterThan(0);
    // Однофамильцы обязаны существовать: если их ноль, разбор `reserved`
    // сломался и «снятыми» окажутся живые имена — гейт покраснеет на всём.
    const homonyms = [...NAMES.reserved].filter((n) => NAMES.live.has(n));
    expect(homonyms.length).toBeGreaterThan(0);
    // eslint-disable-next-line no-console
    console.log(
      `перепись: файлов контракта ${protoSources.length}, имён в reserved ${NAMES.reserved.size}, ` +
        `живых имён ${NAMES.live.size}, снятых полностью ${RETIRED.length}, ` +
        `однофамильцев ${homonyms.length}, исходников консоли ${consoleFiles.length}, ` +
        `находок ${FINDINGS.length}`,
    );
  });

  // Падение печатает координаты. Что с ними делать: у сообщения номер и имя
  // этого поля зарезервированы, значит край не отдаёт и не принимает его
  // никогда. Ветка на таком поле не выбирается ни разу, строка карточки
  // показывает прочерк всегда, объявление в типе обещает ручку, которой нет.
  // Исхода два — снять вместе с кодом, который поле подпирало, либо вернуть
  // поле в контракт. Третьего («оставим на будущее») не бывает: за таким полем
  // никто не отвечает, и оно переживает своё основание.
  it("ни один исходник консоли не ссылается на имя, снятое с контракта", () => {
    expect(FINDINGS.map((f) => `${f.where} [${f.name}] ${f.text}`)).toEqual([]);
  });

  // ── инъекция: детектор обязан ловить дефект и молчать на законном ──────────

  it("детектор краснеет на дефектной форме и молчит на законной", () => {
    const contract = [
      {
        rel: "proto/kacho/cloud/synthetic/v1/thing.proto",
        text: [
          "message Thing {",
          "  reserved 7;",
          '  reserved "retired_knob";',
          "  string id = 1;",
          "  int32 live_knob = 2;",
          "}",
          "enum ThingStatus {",
          "  THING_STATUS_UNSPECIFIED = 0;",
          "  DELETING = 1;",
          "}",
          "message Other {",
          '  reserved "live_knob";', // однофамилец: снят здесь, жив у Thing
          "  string note = 1;",
          "}",
        ].join("\n"),
      },
    ];
    const names = contractNames(contract);
    const retired = retiredNames(names);

    // Снятым считается только имя, не живое НИ У ОДНОГО сообщения; однофамилец
    // (`live_knob` — снят у `Other`, жив у `Thing`) остаётся живым, а значение
    // перечисления полем не является вовсе, иначе гейт съел бы карты состояний.
    expect(retired).toEqual(["retired_knob"]);
    expect({ liveKnob: names.live.has("live_knob"), enumValue: names.live.has("DELETING") }).toEqual({
      liveKnob: true,
      enumValue: true,
    });

    const defective = [
      "interface Thing {",
      "  retired_knob?: number;",
      "}",
      'const row = { label: "Ручка", value: getByPath<number>(data, "retired_knob") };',
      "const kind = t.retired_knob ? 'retired' : 'live';",
    ].join("\n");
    // Все три формы дефекта — объявление в типе, литерал пути, ветвь — обязаны
    // быть найдены, и каждая со своей координатой.
    expect(retiredReferences(defective, retired).map((h) => h.line)).toEqual([2, 4, 5]);

    const lawful = [
      "// `retired_knob` снят с контракта — объяснение снятия не является ссылкой",
      "interface Thing {",
      "  live_knob?: number;",
      "}",
      'const row = { label: "Ручка", value: getByPath<number>(data, "live_knob") };',
    ].join("\n");
    // Законный близнец обязан молчать: живое поле — это код, а объяснение
    // снятия — комментарий, и запрещать объяснять предмет запрета нельзя.
    expect(retiredReferences(lawful, retired)).toEqual([]);

    // Камелевая форма того же имени — тот же предмет, и она обязана ловиться:
    // край отдаёт JSON именно ею.
    expect(retiredReferences("const v = t.retiredKnob;", retired).length).toBe(1);
  });
});
