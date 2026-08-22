import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

/**
 * Гейт на НЕЛАТИНСКОЕ ИМЯ в коде консоли.
 *
 * Решение владельца (2026-08-20): «никаких русскоязычных вставок в коде. На
 * русском могут быть комментарии и документация, но не сам код». Предмет запрета —
 * ИДЕНТИФИКАТОР: имя переменной, функции, параметра, свойства, типа, импорта.
 * Русский текст, который видит пользователь, русское сообщение пробы, русское
 * название подпробы и русский комментарий — законны и обязаны остаться: они и есть
 * та документация, которую решение разрешает.
 *
 * ПОЧЕМУ РАЗБОР, А НЕ ПОИСК ПО ТЕКСТУ. Кириллицы в этом дереве много и она почти
 * вся законна: подписи интерфейса, заголовки проб, разбор в комментариях. Поиск
 * по символу нашёл бы 37782 строки и не отличил бы имя от подписи — такой «гейт»
 * либо отключили бы первым же ложным срабатыванием, либо он маскировал бы предмет.
 * Разбор различает узлы по построению: сюда попадает `Identifier` и никогда —
 * `StringLiteral`, `JsxText` или комментарий.
 *
 * ЧЕМ ЭТО ВРЕДИТ, а не только «не по канону»:
 *  - нелатинское имя невозможно выбрать инструментом, работающим по имени;
 *    в дереве это уже стоило прогонов — 17 имён Go были набраны омоглифом
 *    (кириллическое `В` вместо латинского `B`), и `go test -run` отвечал
 *    «no tests to run» при живых и на вид исправных пробах;
 *  - омоглиф неотличим глазом: `с`, `о`, `е`, `а`, `р`, `х` совпадают начертанием
 *    с латинскими, поэтому два разных имени выглядят одним;
 *  - имя не набирается с раскладки, на которой пишут код.
 *
 * Перепись печатается в имени пробы и утверждается ненулевой: «ноль находок»
 * обязано быть отличимо от «ноль прочитанного». Смещённый корень или
 * переименованная раскладка роняют гейт громко, а не проходят вхолостую.
 *
 * Собственная предпосылка — вторая проба: синтетика с нелатинским именем обязана
 * находиться, а законный близнец (та же кириллица в строке, в комментарии, в
 * тексте разметки и в ключе-строке) обязан молчать. Без второй половины гейт ловил
 * бы символ, а не имя.
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Каталоги, которых нет в дереве: сборка, установленные пакеты, кэш. */
const SKIP_DIRS = new Set(["node_modules", "dist", "build", "coverage", ".vite", ".turbo", "playwright-report", "test-results"]);

interface Finding {
  file: string;
  line: number;
  column: number;
  name: string;
  /** Первый нелатинский знак — он и объясняет находку. */
  offender: string;
}

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (SKIP_DIRS.has(entry) || entry.startsWith(".")) continue;
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) sourceFiles(full, acc);
    else if (/\.(ts|tsx|mts|cts)$/.test(entry)) acc.push(full);
  }
  return acc;
}

/** Имена, осмотренные в одном файле, и найденные среди них нелатинские. */
function scan(file: string, text: string): { seen: number; findings: Finding[] } {
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, /\.tsx$/.test(file) ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const findings: Finding[] = [];
  let seen = 0;

  const walk = (node: ts.Node): void => {
    if (ts.isIdentifier(node) || ts.isPrivateIdentifier(node)) {
      seen += 1;
      const name = node.text;
      const offender = [...name].find((ch) => ch.codePointAt(0)! > 0x7f);
      if (offender !== undefined) {
        const { line, character } = source.getLineAndCharacterOfPosition(node.getStart(source));
        findings.push({ file, line: line + 1, column: character + 1, name, offender });
      }
    }
    ts.forEachChild(node, walk);
  };
  walk(source);
  return { seen, findings };
}

const files = sourceFiles(consoleRoot);
let identsSeen = 0;
const findings: Finding[] = [];
for (const file of files) {
  const result = scan(file, readFileSync(file, "utf8"));
  identsSeen += result.seen;
  findings.push(...result.findings);
}

// Перепись печатается ПОТОКОМ, а не именем пробы: этот прогон печатает имена
// только у упавших, поэтому объём осмотренного при зелёном был бы невидим. Он же
// утверждается ниже — печать даёт наблюдаемость, а роняет гейт утверждение.
process.stdout.write(
  `\n  латиница в именах: файлов TS прочитано ${files.length}, имён осмотрено ${identsSeen}, находок ${findings.length}\n`,
);

describe("имя в коде консоли — латиницей", () => {
  it(`перепись: файлов TS прочитано ${files.length}, имён осмотрено ${identsSeen}, находок ${findings.length}`, () => {
    // Предпосылка гейта: он что-то прочитал. Пустой перечень при пустом обходе —
    // не «чисто», а «не искали».
    expect(files.length).toBeGreaterThan(500);
    expect(identsSeen).toBeGreaterThan(50_000);
  });

  it("нелатинских имён нет ни одного", () => {
    const report = findings
      .slice(0, 40)
      .map((f) => `  ${path.relative(consoleRoot, f.file)}:${f.line}:${f.column}: ${f.name} — знак ${f.offender} не латинский`)
      .join("\n");
    const tail = findings.length > 40 ? `\n  …и ещё ${findings.length - 40}` : "";
    expect(findings.length === 0 ? "" : `нелатинских имён ${findings.length}:\n${report}${tail}`).toBe("");
  });
});

describe("предпосылка гейта: он различает имя и текст", () => {
  const withDefect = [
    "const перечень = [1, 2];",
    "export function собрать(вход: string) { return вход; }",
  ].join("\n");

  // Законный близнец: та же кириллица, но НЕ в именах. Каждая строка ниже — форма,
  // которая в дереве встречается сотнями и обязана остаться нетронутой.
  const legitimate = [
    "// Разбор по-русски: почему здесь именно так.",
    "/** Документация тоже по-русски. */",
    'const label = "Облачные сети";',
    'const message = `создано ${1} штук`;',
    'const byKey = { "имя": 1 };',
    'it("показывает ресурсы модуля", () => {});',
  ].join("\n");

  it("находит нелатинское имя в синтетике", () => {
    const found = scan("synthetic.ts", withDefect).findings.map((f) => f.name);
    expect(found).toEqual(["перечень", "собрать", "вход", "вход"]);
  });

  it("молчит на кириллице в тексте, ключе-строке и комментарии", () => {
    const result = scan("synthetic-twin.ts", legitimate);
    // Положительный контроль: имена в близнеце ЕСТЬ и осмотрены — значит «ноль
    // находок» означает отсутствие предмета, а не пустой разбор.
    expect(result.seen).toBeGreaterThan(0);
    expect(result.findings).toEqual([]);
  });

  // Формы имени, которые в консоли встречаются чаще прочих и легко ускользают:
  // разбор видит их все как `Identifier`, но утверждать это надо, а не полагать.
  it.each([
    ["атрибут разметки", 'const a = <Foo подпись="x" />;', "подпись"],
    ["поле типа", "type T = { поле: string };", "поле"],
    ["метод класса", "class C { приватный() {} }", "приватный"],
    ["импорт с псевдонимом", 'import { x as псевдоним } from "m";', "псевдоним"],
    ["перечисление", "enum E { Значение = 1 }", "Значение"],
    ["параметр обобщения", "function f<Тип>(x: Тип) { return x; }", "Тип"],
    ["деструктуризация", "const { a: имя } = obj;", "имя"],
  ])("ловит нелатинское имя: %s", (_name, code, expected) => {
    const found = scan("form.tsx", code).findings.map((f) => f.name);
    expect(found).toContain(expected);
  });

  it("находит омоглиф — имя, неотличимое глазом от латинского", () => {
    // Кириллическое `с` вместо латинского `c`: два разных имени выглядят одним.
    const found = scan("homoglyph.ts", "const сount = 1;").findings;
    expect(found).toHaveLength(1);
    expect(found[0].offender).toBe("с");
  });
});
