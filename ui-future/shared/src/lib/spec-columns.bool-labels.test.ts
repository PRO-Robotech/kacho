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
 * Копия исходника, в которой комментарии и строковые литералы ЗАМЕНЕНЫ пробелами
 * (переводы строк сохранены, длина не меняется — поэтому смещения остаются
 * общими с оригиналом, и найденное на маске режется из оригинала).
 *
 * Зачем. Гейт обязан читать исполняемую часть, а не прозу. Без маски он краснел
 * на комментарии, который объясняет, почему формат здесь НЕ применён, — и первое
 * же ложное срабатывание его бы отключило. Обратная сторона того же изъяна тише
 * и хуже: текст в строковом литерале засчитывался бы объявлением, а фигурная
 * скобка внутри комментария (`{registryId}` в соседнем пояснении) сбивала бы
 * поиск границ объекта, и находка указывала бы на чужую колонку.
 *
 * Граница применимости названа честно: регулярные литералы маска не разбирает.
 * В реестрах ресурсов их нет — это объявления данных, — а разбирать их пришлось
 * бы полноценным лексером, то есть заводить второй разбор языка ради предмета,
 * которого в осматриваемом дереве не существует.
 */
export function maskProse(source: string): string {
  const out = source.split("");
  const blank = (from: number, to: number) => {
    for (let i = from; i < to && i < out.length; i++) if (out[i] !== "\n") out[i] = " ";
  };
  let i = 0;
  while (i < source.length) {
    const ch = source[i];
    const next = source[i + 1];
    if (ch === "/" && next === "/") {
      const end = source.indexOf("\n", i);
      blank(i, end < 0 ? source.length : end);
      i = end < 0 ? source.length : end;
    } else if (ch === "/" && next === "*") {
      const end = source.indexOf("*/", i + 2);
      const stop = end < 0 ? source.length : end + 2;
      blank(i, stop);
      i = stop;
    } else if (ch === '"' || ch === "'" || ch === "`") {
      // Содержимое литерала гасим, кавычки оставляем: они держат баланс для
      // всякого, кто читает маску дальше.
      let j = i + 1;
      while (j < source.length) {
        if (source[j] === "\\") {
          j += 2;
          continue;
        }
        if (source[j] === ch) break;
        j++;
      }
      blank(i + 1, j);
      i = j + 1;
    } else {
      i++;
    }
  }
  return out.join("");
}

/**
 * Объявления колонок с `format:"bool"` и признак наличия подписей.
 *
 * Разбор идёт по ОБЪЕКТУ колонки, а не по строке с форматом: подписи стоят
 * соседним полем и на той же строке их не бывает. Границей объекта служит
 * ближайшая открывающая скобка перед `format` и парная ей — так предикат не
 * путает соседние колонки между собой.
 *
 * Ищется и размечается МАСКА (см. `maskProse`), а показывается оригинал: судить
 * надо по коду, а называть координату — словами, которые автор действительно
 * написал.
 *
 * Само СЛОВО «bool» на маске стёрто — оно и есть строковый литерал. Поэтому
 * ключ ищется на маске (там он остался кодом), а его ЗНАЧЕНИЕ сверяется в
 * оригинале по тому же смещению. Так объявление отличается от прозы о нём, не
 * теряя себя: искать целиком на маске значило бы не найти ни одного настоящего.
 */
export function boolColumns(source: string): { snippet: string; hasLabels: boolean }[] {
  const out: { snippet: string; hasLabels: boolean }[] = [];
  const masked = maskProse(source);
  // Ключ и открывающая кавычка значения — на маске; содержимое кавычек — ниже.
  const re = /format:\s*"/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(masked)) !== null) {
    const quote = m.index + m[0].length - 1;
    if (!source.startsWith('"bool"', quote)) continue;
    // Начало объекта колонки — ближайшая `{` слева, у которой баланс сходится.
    let depth = 0;
    let start = -1;
    for (let i = m.index; i >= 0; i--) {
      const ch = masked[i];
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
    for (let i = start; i < masked.length; i++) {
      const ch = masked[i];
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
    // Подписи ищутся на маске: `boolLabels` из соседнего пояснения — не
    // объявление, и принять его значило бы простить колонку за комментарий.
    out.push({ snippet, hasLabels: /boolLabels\s*:/.test(masked.slice(start, end + 1)) });
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

  it("объявление в КОММЕНТАРИИ и в СТРОКЕ за объявление не принимается", () => {
    // Гейт читает исполняемую часть, а не прозу. Иначе он краснеет на
    // комментарии, объясняющем, почему формат здесь НЕ применён, — и первое же
    // ложное срабатывание его отключит (`testing.md` §«Гейт читает исполняемую
    // часть, а не текст»). Обратная сторона того же изъяна опаснее: строковый
    // литерал с этим текстом тоже засчитался бы объявлением.
    const injected = `
      columns: [
        // Рисуется render, а не format: "bool", потому что сборщик здесь форк.
        { header: "Прозой", path: "flag", render: (row) => row.flag },
        /* format: "bool" — блочный комментарий, тоже не объявление */
        { header: "Строкой", path: "note", format: "text", hint: 'format: "bool"' },
      ]`;
    expect(boolColumns(injected)).toEqual([]);
  });

  it("настоящее объявление рядом с прозой всё равно найдено — положительный контроль", () => {
    // Без него запрет выше выполнялся бы разбором, который не находит НИЧЕГО.
    const injected = `
      columns: [
        // Здесь про format: "bool" сказано словами.
        { header: "Настоящая", path: "flag", format: "bool" },
      ]`;
    expect(boolColumns(injected).map((c) => c.hasLabels)).toEqual([false]);
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
