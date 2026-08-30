import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

/**
 * Гейт: ОТБОР, СПРЯТАВШИЙ СТРОКУ, ОБЯЗАН СЧИТАТЬСЯ СУЖЕНИЕМ.
 *
 * Список ресурса решает две вещи РАЗНЫМИ выражениями: какие строки показать
 * (`filteredItems`) и считать ли список суженным (`anyFilterActive`). Второе
 * выбирает, что увидит пользователь на пустом результате — «ничего не найдено»
 * или приглашение «создайте первый».
 *
 * Пока эти два выражения могут разойтись, продукт вправе солгать: строки
 * отброшены, сужение не объявлено, и страница говорит арендатору «ресурсов нет»
 * ровно там, где край ответил «есть». Тихо — у арендатора, чьи ресурсы попали
 * под отбор частично, список просто короче настоящего, и по экрану этого не
 * понять.
 *
 * Так и было (#927): отбор адресов по наличию внешнего выбрасывал внутренние, а
 * `anyFilterActive` о нём не знал. Дефект прожил до сквозной пробы и был найден
 * не чтением кода, а браузером.
 *
 * ЧТО ТРЕБУЕТ ГЕЙТ. Каждая ветка тела `filteredItems`, отбрасывающая строку
 * (`return false`), обязана опираться хотя бы на одну ручку, перечисленную в
 * `anyFilterActive`. Ветка, не опирающаяся ни на одну, — отбор, о котором
 * пользователю не сказано.
 *
 * ЧЕГО ГЕЙТ НЕ ТРЕБУЕТ. Он не судит о том, ЧТО именно отбирается: ручки бывают
 * разные, и заводить их — обычная работа. Он требует лишь, чтобы отбор и
 * объявление сужения происходили от ОДНОГО набора имён.
 *
 * Перепись печатается потоком: этот прогон печатает имена проб только у
 * упавших, и объём осмотренного при зелёном был бы невидим.
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Каталоги, которых нет в дереве: сборка, установленные пакеты, кэш. */
const SKIP_DIRS = new Set(["node_modules", "dist", "build", "coverage", ".vite", ".turbo", "playwright-report", "test-results"]);

/**
 * Обход дерева, а НЕ чтение одного файла по своей же координате.
 *
 * Гейт утверждает СОСТАВ дерева: сколько в консоли мест, решающих об отборе
 * списка, и все ли они объявляют сужение. Один выписанный путь этого не даёт —
 * второй такой компонент завтра появится и останется невидимым; заодно проба,
 * читающая модуль по названному ею пути, сама подпадает под запрет дерева
 * (`internal/repohygiene` `TestUITestsDoNotReadTheirOwnSourceAsText`), и
 * справедливо: там предмет — проба, подтверждающая себя своим же текстом.
 */
function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (SKIP_DIRS.has(entry) || entry.startsWith(".")) continue;
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) sourceFiles(full, acc);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) acc.push(full);
  }
  return acc;
}

interface Analysis {
  /** Имена, из которых собран признак «список сужен». */
  declared: string[];
  /** Ветки отбрасывания: имена, на которые каждая опирается. */
  drops: { line: number; names: string[] }[];
}

function analyse(source: ts.SourceFile): Analysis {
  const declared = new Set<string>();
  const drops: { line: number; names: string[] }[] = [];

  /**
   * Имена, на которые опирается выражение, — ИСТОЧНИКИ значений, а не всякий
   * идентификатор в тексте.
   *
   * Имя ПОЛЯ в обращении именем не является: `row.addresses.length` говорит о
   * полях `addresses` и `length` объекта `row`, а опирается ровно на `row`.
   * Пока оба клались в набор наравне, признак сужения этого же компонента
   * (`Object.keys(serverFilters).length > 0`) вносил в объявленные слово
   * `length` — и ЛЮБАЯ ветка отбрасывания, где оно встречается, считалась
   * объявленной. А `.length` в коде отбора встречается постоянно.
   *
   * Гейт становился вакуумным на этой полосе: форма проверки есть, содержания
   * нет — ровно тот класс, который он и стережёт (#1496). Ловил он при этом
   * только ветки БЕЗ `.length`, то есть работал, и потому слепота была тихой.
   *
   * Ключ объекта-литерала снимается по той же причине и с той же оговоркой:
   * сокращённая запись (`{ zone }`) — настоящая ссылка на значение, и она
   * остаётся.
   */
  const namesOf = (node: ts.Node): string[] => {
    const found: string[] = [];
    const walk = (n: ts.Node): void => {
      if (ts.isPropertyAccessExpression(n)) {
        walk(n.expression); // `.name` — имя поля, а не источник значения
        return;
      }
      if (ts.isPropertyAssignment(n) && ts.isIdentifier(n.name)) {
        walk(n.initializer); // ключ — имя поля; значение остаётся
        return;
      }
      if (ts.isIdentifier(n)) found.push(n.text);
      ts.forEachChild(n, walk);
    };
    walk(node);
    return found;
  };

  const visit = (node: ts.Node): void => {
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) {
      if (node.name.text === "anyFilterActive") {
        for (const n of namesOf(node.initializer)) declared.add(n);
      }
      if (node.name.text === "filteredItems") {
        // Ветки отбрасывания ищем ВНУТРИ этого объявления: `return false` в
        // предикате отбора и есть «строку не показываем».
        const inspectNode = (n: ts.Node, guards: ts.Node[]): void => {
          if (ts.isIfStatement(n)) {
            const inner = [...guards, n.expression];
            inspectNode(n.thenStatement, inner);
            if (n.elseStatement) inspectNode(n.elseStatement, guards);
            return;
          }
          if (ts.isReturnStatement(n) && n.expression && n.expression.kind === ts.SyntaxKind.FalseKeyword) {
            const names = guards.flatMap(namesOf);
            drops.push({ line: source.getLineAndCharacterOfPosition(n.getStart(source)).line + 1, names });
            return;
          }
          ts.forEachChild(n, (c) => inspectNode(c, guards));
        };
        inspectNode(node.initializer, []);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return { declared: [...declared], drops };
}

const inspected: { file: string; declared: string[]; drops: { line: number; names: string[] }[] }[] = [];
let filesRead = 0;
for (const file of sourceFiles(consoleRoot)) {
  const raw = readFileSync(file, "utf8");
  filesRead += 1;
  // Предмет — место, где ОБА решения принимаются: какие строки показать и
  // считать ли список суженным. Файл без них к делу не относится.
  if (!raw.includes("anyFilterActive") || !raw.includes("filteredItems")) continue;
  const src = ts.createSourceFile(file, raw, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const { declared, drops } = analyse(src);
  inspected.push({ file: path.relative(consoleRoot, file), declared, drops });
}

const undeclared = inspected.flatMap((i) =>
  i.drops.filter((d) => !d.names.some((n) => i.declared.includes(n))).map((d) => ({ ...d, file: i.file, declared: i.declared })),
);

process.stdout.write(
  `\n  отбор списка: файлов прочитано ${filesRead}, решают об отборе ${inspected.length}, ` +
    `ручек сужения объявлено ${inspected.reduce((n, i) => n + i.declared.length, 0)}, ` +
    `веток отбрасывания ${inspected.reduce((n, i) => n + i.drops.length, 0)}, ` +
    `не опирающихся на признак сужения ${undeclared.length}\n`,
);

describe("отбор, спрятавший строку, считается сужением", () => {
  it("предпосылка: дерево прочитано и место отбора найдено", () => {
    // Без этого «ноль находок» означало бы «ноль прочитанного»: переименуют
    // `anyFilterActive` — и гейт станет молча зелёным на любом отборе.
    expect(filesRead).toBeGreaterThan(300);
    expect(inspected.length).toBeGreaterThan(0);
    for (const i of inspected) {
      expect(i.declared.length).toBeGreaterThan(0);
      expect(i.drops.length).toBeGreaterThan(0);
    }
  });

  it("каждая ветка отбрасывания опирается на объявленную ручку", () => {
    const report = undeclared
      .map((d) => `  ${d.file}:${d.line}: отбор по [${d.names.join(", ")}] — ни одно имя не входит в признак сужения`)
      .join("\n");
    expect(
      undeclared.length === 0
        ? ""
        : `веток отбрасывания вне признака сужения ${undeclared.length}:\n${report}\n\n` +
          `Такой отбор прячет строки, а страница объявляет список НЕсуженным — то есть на ` +
          `пустом результате показывает «создайте первый» вместо «ничего не найдено». ` +
          `Ручки сужения этого файла: [${undeclared[0]?.declared.join(", ") ?? ""}]`,
    ).toBe("");
  });
});

describe("предпосылка гейта: он различает объявленный отбор и необъявленный", () => {
  const parse = (code: string) =>
    analyse(ts.createSourceFile("synthetic.tsx", code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX));

  const legitimate = `
    const anyFilterActive = query.trim() !== "" || (hasZoneFilter && zone !== "all");
    const filteredItems = items.filter((row) => {
      if (hasZoneFilter && zone !== "all" && rowZone(row) !== zone) return false;
      return true;
    });
  `;
  const withDefect = `
    const anyFilterActive = query.trim() !== "" || (hasZoneFilter && zone !== "all");
    const filteredItems = items.filter((row) => {
      if (!row.external) return false;
      return true;
    });
  `;

  it("молчит на отборе по объявленной ручке", () => {
    const a = parse(legitimate);
    expect(a.drops.length).toBe(1);
    expect(a.drops.filter((d) => !d.names.some((n) => a.declared.includes(n)))).toEqual([]);
  });

  it("находит отбор, о котором признак сужения не знает", () => {
    const a = parse(withDefect);
    const bad = a.drops.filter((d) => !d.names.some((n) => a.declared.includes(n)));
    expect(bad).toHaveLength(1);
  });

  /*
   * ИМЯ ПОЛЯ В ОБРАЩЕНИИ — НЕ ОБЪЯВЛЕННОЕ ИМЯ (#1496).
   *
   * Набор объявленных имён собирается из идентификаторов выражения. Пока в него
   * попадало имя ПОЛЯ (`x.length` давало и `x`, и `length`), любая ветка со
   * словом `length` считалась объявленной — а `Object.keys(serverFilters).length`
   * стоит в признаке сужения этого самого компонента. Гейт становился вакуумным
   * ровно на том слове, которое в коде отбора встречается постоянно.
   *
   * Это тот класс, который гейт и стережёт, этажом выше: форма проверки есть,
   * содержания нет.
   *
   * Пара обязательна. Одна сторона зеленела бы на распознавателе, отвергающем
   * всё: вторая фикстура несёт ТО ЖЕ `.length`, но опирается ещё и на настоящее
   * объявленное имя — и обязана молчать.
   */
  const forgivenByPropertyName = `
    const anyFilterActive = query.trim() !== "" || Object.keys(serverFilters).length > 0;
    const filteredItems = items.filter((row) => {
      if (row.addresses.length === 0) return false;
      return true;
    });
  `;
  const propertyNamePlusDeclaredHandle = `
    const anyFilterActive = query.trim() !== "" || Object.keys(serverFilters).length > 0;
    const filteredItems = items.filter((row) => {
      if (query.trim() !== "" && row.tags.length === 0) return false;
      return true;
    });
  `;

  it("имя поля в обращении не считается объявленной ручкой", () => {
    const a = parse(forgivenByPropertyName);
    expect(a.declared).not.toContain("length");
    const bad = a.drops.filter((d) => !d.names.some((n) => a.declared.includes(n)));
    expect(bad).toHaveLength(1);
  });

  it("молчит, когда ветка опирается на НАСТОЯЩУЮ объявленную ручку рядом с полем", () => {
    const a = parse(propertyNamePlusDeclaredHandle);
    const bad = a.drops.filter((d) => !d.names.some((n) => a.declared.includes(n)));
    expect(bad).toEqual([]);
  });
});
