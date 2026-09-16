import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

/**
 * Гейт: ПОЛЕ ССЫЛКИ ПРИГЛАШЕНИЯ СНЯТО С КОНТРАКТА — И В КОНСОЛИ НЕТ НИ ОДНОГО
 * ЕГО ЧТЕНИЯ (приёмка ID-MAIL-1, Р10; MAIL-06, MAIL-34; задача продукта #1774).
 *
 * `InviteUserMetadata.magic_link_url` снято с контракта службы доступа с
 * резервированием номера и имени: ссылка выдавала бы приглашающему
 * предъявителя приглашённого. Консоль, читающая это поле, обещала бы ссылку,
 * которой продукт не производит, — и показывала бы пустой блок либо ветвилась
 * на значении, которого не бывает.
 *
 * ЧТО СУДИТСЯ. Обход дерева консоли, разбор синтаксисом: имя снятого поля как
 * ИДЕНТИФИКАТОР (обращение `.magic_link_url`, ключ типа, деструктуризация) и
 * как СТРОКОВЫЙ ЛИТЕРАЛ (ключ `"magic_link_url"`, путь `"metadata.magicLinkUrl"`).
 * Комментарии, объясняющие снятие, чтением не являются и не считаются — иначе
 * гейт краснел бы на собственном объяснении.
 *
 * ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ, без которого «чтений ноль» зеленело бы на пустом
 * обходе: то, что пришло взамен, — глагол повторной отправки — в дереве ЕСТЬ,
 * и его находит тот же разбор (идентификатор `userResendInvitePath`).
 */

const RETIRED = ["magic_link_url", "magicLinkUrl", "MagicLinkUrl"];
const REPLACEMENT = "userResendInvitePath";

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const SKIP_DIRS = new Set(["node_modules", "dist", "build", "coverage", ".vite", ".turbo", "playwright-report", "test-results"]);

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (SKIP_DIRS.has(entry) || entry.startsWith(".")) continue;
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) {
      sourceFiles(full, acc);
      continue;
    }
    if (/\.(ts|tsx)$/.test(entry) && !/\.(test|spec)\.tsx?$/.test(entry) && !/\.d\.ts$/.test(entry)) acc.push(full);
  }
  return acc;
}

interface Hit {
  file: string;
  line: number;
  what: string;
}

/** Чтения снятого поля и наличие замены — по УЗЛАМ разбора, не по тексту. */
export function scanForRetiredField(files: string[], root: string): { hits: Hit[]; replacementSeen: number; nodes: number } {
  const hits: Hit[] = [];
  let replacementSeen = 0;
  let nodes = 0;
  for (const file of files) {
    const text = readFileSync(file, "utf8");
    const sf = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
    const rel = path.relative(root, file);
    const visit = (n: ts.Node): void => {
      nodes++;
      if (ts.isIdentifier(n)) {
        if (RETIRED.includes(n.text)) {
          hits.push({ file: rel, line: sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1, what: `идентификатор ${n.text}` });
        }
        if (n.text === REPLACEMENT) replacementSeen++;
      } else if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) {
        const found = RETIRED.find((r) => n.text.includes(r));
        if (found) {
          hits.push({ file: rel, line: sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1, what: `строка «${n.text}»` });
        }
      }
      ts.forEachChild(n, visit);
    };
    visit(sf);
  }
  return { hits, replacementSeen, nodes };
}

describe("поле ссылки приглашения снято с контракта — консоль его не читает", () => {
  const files = sourceFiles(consoleRoot);
  const { hits, replacementSeen, nodes } = scanForRetiredField(files, consoleRoot);

  test("перепись: обход непуст", () => {
    console.info(`перепись: файлов консоли прочитано ${files.length} · узлов разбора ${nodes} · чтений снятого поля ${hits.length} · чтений замены (${REPLACEMENT}) ${replacementSeen}`);
    expect(files.length).toBeGreaterThan(50);
    expect(nodes).toBeGreaterThan(1000);
  });

  test("ни одного чтения снятого поля (MAIL-06, MAIL-34)", () => {
    expect(hits.map((h) => `${h.file}:${h.line} — ${h.what}`)).toEqual([]);
  });

  test("положительный контроль: замена — глагол повторной отправки — в дереве есть", () => {
    // Объявление плюс хотя бы одно употребление: одного объявления мало —
    // объявленный и никем не читаемый помощник есть та же форма без содержания.
    expect(replacementSeen).toBeGreaterThanOrEqual(2);
  });
});

describe("предпосылка гейта: разбор находит чтение и не считает комментарий", () => {
  function scanText(text: string, ext = ".ts"): number {
    const sf = ts.createSourceFile(`x${ext}`, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
    let n = 0;
    const visit = (node: ts.Node): void => {
      if (ts.isIdentifier(node) && RETIRED.includes(node.text)) n++;
      if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && RETIRED.some((r) => node.text.includes(r))) n++;
      ts.forEachChild(node, visit);
    };
    visit(sf);
    return n;
  }

  test("обращение к полю — чтение", () => {
    expect(scanText("const link = resp?.metadata?.magic_link_url;")).toBe(1);
  });
  test("ключ типа — чтение", () => {
    expect(scanText("type M = { magic_link_url?: string };")).toBe(1);
  });
  test("строковый путь — чтение", () => {
    expect(scanText('const p = "metadata.magicLinkUrl";')).toBe(1);
  });
  test("комментарий, объясняющий снятие, чтением НЕ является", () => {
    expect(scanText("// поле magic_link_url снято с контракта\nconst a = 1;")).toBe(0);
  });
});
