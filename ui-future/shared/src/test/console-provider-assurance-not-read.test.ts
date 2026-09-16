import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

/**
 * Гейт: КОНСОЛЬ НЕ ЧИТАЕТ УРОВЕНЬ УВЕРЕННОСТИ ПОСТАВЩИКА ЛИЧНОСТИ (Ф11, Ч8).
 *
 * Уровень уверенности сессии объявляет НАША служба и решает по нему край
 * (приёмка Ф11 «уровень уверенности объявляет наша сессия», Р7). Консоль
 * читала поле сессии поставщика (`authenticator_assurance_level === "aal2"`)
 * и вычисляла из него значение «свежесть подтверждения» (`mfaFreshUntil`), у
 * которого не было ни одного читателя вне своего файла: значение писали и не
 * читали. Ф11 §1.3 (Ч8) снимает читателя вместе со значением — переносить его
 * на нашу сессию было бы нечем оправдать: у переноса не было бы потребителя.
 *
 * ЧТО ТРЕБУЕТ ГЕЙТ. В прод-коде консоли нет ни одного УЗЛА разбора, называющего
 * поле уровня поставщика либо снятое значение свежести: обращение к полю,
 * объявление поля в типе, идентификатор. Судится узел, а не текст — иначе гейт
 * краснел бы на комментарии, объясняющем снятие (в том числе на этом).
 *
 * ЧЕГО ГЕЙТ НЕ ТРЕБУЕТ. Сквозные пробы (`ui-future/e2e`) и пробы модулей вне
 * обхода: там имя поля законно как величина, которую стенд поставщика ещё
 * отдаёт до снятия компонента (Ф10). Граница названа переписью.
 *
 * Перепись печатается: «ноль находок» обязано быть отличимо от «ноль
 * прочитанного», поэтому гейт падает на пустом обходе.
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Каталоги, которых нет в дереве: сборка, установленные пакеты, кэш, сквозные пробы. */
const SKIP_DIRS = new Set([
  "node_modules",
  "dist",
  "build",
  "coverage",
  ".vite",
  ".turbo",
  "playwright-report",
  "test-results",
  "e2e",
]);

/** Имена, которых в прод-коде консоли больше нет. Закрытый перечень: снятый читатель и снятое значение. */
export const RETIRED_PROVIDER_ASSURANCE_NAMES = ["authenticator_assurance_level", "mfaFreshUntil"] as const;

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (SKIP_DIRS.has(entry) || entry.startsWith(".")) continue;
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) sourceFiles(full, acc);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) acc.push(full);
  }
  return acc;
}

export interface ProviderAssuranceFinding {
  line: number;
  name: string;
  kind: string;
}

/**
 * Узлы разбора, называющие снятое имя: обращение к полю (`x.authenticator_assurance_level`),
 * объявление поля в типе, любой идентификатор с этим именем. Комментарии и строковые
 * литералы узлами-именами не являются — на них гейт молчит by construction.
 */
export function findProviderAssuranceReaders(source: ts.SourceFile): { findings: ProviderAssuranceFinding[]; nodes: number } {
  const findings: ProviderAssuranceFinding[] = [];
  const retired = new Set<string>(RETIRED_PROVIDER_ASSURANCE_NAMES);
  let nodes = 0;
  const visit = (node: ts.Node) => {
    nodes++;
    if (ts.isIdentifier(node) && retired.has(node.text)) {
      const { line } = source.getLineAndCharacterOfPosition(node.getStart(source));
      findings.push({ line: line + 1, name: node.text, kind: ts.SyntaxKind[node.parent.kind] });
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return { findings, nodes };
}

function parse(fileName: string, text: string): ts.SourceFile {
  return ts.createSourceFile(fileName, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
}

describe("консоль не читает уровень уверенности поставщика (Ф11 Ч8)", () => {
  const files = sourceFiles(consoleRoot);
  const found: { file: string; finding: ProviderAssuranceFinding }[] = [];
  let nodesJudged = 0;
  for (const file of files) {
    const { findings, nodes } = findProviderAssuranceReaders(parse(file, readFileSync(file, "utf8")));
    nodesJudged += nodes;
    for (const finding of findings) found.push({ file: path.relative(consoleRoot, file), finding });
  }

  it("предпосылка: прод-дерево консоли прочитано", () => {
    // Порог — нижняя граница числа прод-файлов консоли, а не их точное число:
    // ноль означал бы, что обход шёл не по тому корню.
    expect(files.length).toBeGreaterThan(300);
    expect(nodesJudged).toBeGreaterThan(10_000);
    console.log(
      `перепись: прод-файлов консоли осмотрено ${files.length} · узлов разбора ${nodesJudged} · ` +
        `имён под запретом ${RETIRED_PROVIDER_ASSURANCE_NAMES.length} · находок ${found.length}`,
    );
  });

  it("ни один узел прод-кода не называет поле уровня поставщика или снятую свежесть", () => {
    expect(found.map((f) => `${f.file}:${f.finding.line} ${f.finding.name} (${f.finding.kind})`)).toEqual([]);
  });
});

describe("предпосылка гейта: он различает узел-имя и текст о нём", () => {
  it("обращение к полю и объявление поля в типе — находки с координатой", () => {
    const injected = parse(
      "injected.ts",
      [
        "interface S { authenticator_assurance_level: string }",
        "export function f(s: S) {",
        '  if (s.authenticator_assurance_level === "aal2") { return 1; }',
        "  const mfaFreshUntil = 0;",
        "  return mfaFreshUntil;",
        "}",
      ].join("\n"),
    );
    const { findings } = findProviderAssuranceReaders(injected);
    expect(findings.map((f) => `${f.line}:${f.name}`)).toEqual([
      "1:authenticator_assurance_level",
      "3:authenticator_assurance_level",
      "4:mfaFreshUntil",
      "5:mfaFreshUntil",
    ]);
  });

  it("комментарий и строковый литерал о том же — законный близнец, гейт молчит", () => {
    const twin = parse(
      "twin.ts",
      [
        "// authenticator_assurance_level больше не читается; mfaFreshUntil снято (Ф11 Ч8)",
        'export const note = "authenticator_assurance_level mfaFreshUntil";',
        "export const level = 1;",
      ].join("\n"),
    );
    const { findings, nodes } = findProviderAssuranceReaders(twin);
    expect(nodes).toBeGreaterThan(0);
    expect(findings).toEqual([]);
  });
});
