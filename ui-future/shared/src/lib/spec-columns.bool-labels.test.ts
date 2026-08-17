// Колонка с форматом «логическое» обязана НАЗВАТЬ оба исхода.
//
// Класс. Формат `bool` заведён ради правила 6 `ui.md`: булево свойство
// называется следствием, а не словом «Да». Но подписи объявляются колонкой, и
// колонка, объявившая формат без подписей, попадает в ветку прочерка — то есть
// показывает пользователю «—» там, где сервер прислал факт. Это тише прежнего
// `true` и потому хуже: прочерк читается как «данных нет».
//
// Умолчания у подписей нет намеренно. «Да»/«Нет» в качестве умолчания вернули бы
// ровно тот дефект, ради которого формат заведён, — только теперь объявленным, и
// ни один обзор диффа его бы не отличил.
//
// Источник истины — реестры ВСЕХ приложений консоли, а не список рядом с
// тестом: формат общий, и объявить колонку им может любой модуль.
//
// Вердикт — по содержимому; объём осмотренного утверждается отдельно, чтобы
// «находок нет» было отличимо от «ничего не прочитано».

import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
// shared/src/lib → ui-future
const CONSOLE_ROOT = path.resolve(HERE, "../../..");

const CONSOLE_APPS = ["host", "dashboard", "shared", "vpc", "compute", "storage", "nlb", "registry", "iam", "system"];

function registryFiles(): string[] {
  const out: string[] = [];
  for (const app of CONSOLE_APPS) {
    const dir = path.join(CONSOLE_ROOT, app, "src", "lib");
    let entries: string[];
    try {
      entries = readdirSync(dir);
    } catch {
      continue;
    }
    for (const e of entries) {
      const abs = path.join(dir, e);
      if (statSync(abs).isFile() && /^resource-registry.*\.tsx?$/.test(e) && !e.includes(".test.")) out.push(abs);
    }
  }
  return out;
}

/**
 * Объявления колонок с `format:"bool"` и признак наличия подписей.
 *
 * Разбор идёт по ОБЪЕКТУ колонки, а не по строке с форматом: подписи стоят
 * соседним полем и на той же строке их не бывает. Границей объекта служит
 * ближайшая открывающая скобка перед `format` и парная ей — так предикат не
 * путает соседние колонки между собой.
 */
export function boolColumns(source: string): { snippet: string; hasLabels: boolean }[] {
  const out: { snippet: string; hasLabels: boolean }[] = [];
  const re = /format:\s*"bool"/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(source)) !== null) {
    // Начало объекта колонки — ближайшая `{` слева, у которой баланс сходится.
    let depth = 0;
    let start = -1;
    for (let i = m.index; i >= 0; i--) {
      const ch = source[i];
      if (ch === "}") depth++;
      else if (ch === "{") {
        if (depth === 0) {
          start = i;
          break;
        }
        depth--;
      }
    }
    if (start < 0) continue;
    let end = -1;
    depth = 0;
    for (let i = start; i < source.length; i++) {
      const ch = source[i];
      if (ch === "{") depth++;
      else if (ch === "}") {
        depth--;
        if (depth === 0) {
          end = i;
          break;
        }
      }
    }
    if (end < 0) continue;
    const snippet = source.slice(start, end + 1);
    out.push({ snippet, hasLabels: /boolLabels\s*:/.test(snippet) });
  }
  return out;
}

const files = registryFiles();
const findings: string[] = [];
let declared = 0;
for (const f of files) {
  for (const c of boolColumns(readFileSync(f, "utf8"))) {
    declared++;
    if (!c.hasLabels) findings.push(`${path.relative(CONSOLE_ROOT, f)}: ${c.snippet.replace(/\s+/g, " ").slice(0, 90)}`);
  }
}

describe("колонка «логическое» называет оба исхода", () => {
  it(`перепись: реестров прочитано ${files.length}, колонок с форматом «логическое» ${declared}`, () => {
    // Пять приложений из десяти держат собственный реестр; остальные ходят в
    // общий. Ноль прочитанных реестров означал бы, что все утверждения ниже
    // вакуумно истинны — сдвинутый корень выглядел бы как чистое дерево.
    expect(files.length).toBeGreaterThanOrEqual(5);
    // Формат применён хотя бы где-то: иначе гейт стережёт то, чего нет, и
    // переживёт свой предмет незамеченным.
    expect(declared).toBeGreaterThan(0);
  });

  it("подписи объявлены у каждой такой колонки", () => {
    expect(findings).toEqual([]);
  });

  it("разбор находит колонку без подписей и не трогает соседнюю с ними", () => {
    // Инъекция в обе стороны прямо здесь: без неё «находок нет» означало бы
    // лишь, что предикат не распознаёт объявление вовсе.
    const injected = `
      columns: [
        { header: "Имя", path: "name", format: "text" },
        { header: "Плохая", path: "flag", format: "bool" },
        { header: "Хорошая", path: "other", format: "bool", boolLabels: { yes: "Включено", no: "Выключено" } },
      ]`;
    const got = boolColumns(injected);
    expect(got.map((c) => c.hasLabels)).toEqual([false, true]);
  });
});
