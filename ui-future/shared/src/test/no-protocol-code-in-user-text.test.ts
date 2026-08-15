import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { stripComments } from "@shared/test/strip-comments";

/**
 * Гейт: числовой код протокола не попадает в текст, который читает пользователь.
 *
 * # Класс
 *
 * Край собирает тело ошибки из `google.rpc.Status`, где `code` объявлен `int32`.
 * В JSON это число, и шаблон `` `${err.code}: ${err.message}` `` давал на экране
 * «5: Region not found» и «9: network is not empty». Величина протокола занимала
 * начало строки — место, куда смотрят первым, — и не сообщала арендатору ничего.
 *
 * Находка тихая: код собирается, текст непуст, ни один тест не краснеет. Она и
 * прожила в **52** местах именно поэтому.
 *
 * # Что утверждается
 *
 * Ни один исходник консоли, кроме двух названных ниже, не подставляет `.code`
 * (ошибки или операции) внутрь шаблонной строки. Разбор кода живёт в
 * `lib/grpc-status`, показ — в `lib/error-presentation` (поле `devDetail`,
 * приглушённая строка «для того, кто чинит»). Текст для пользователя строит
 * `errorText`.
 *
 * Проверка читает ИСПОЛНЯЕМУЮ часть: комментарии снимаются, иначе гейт падал бы
 * на объяснении самого запрета — на этом файле в первую очередь.
 *
 * # Чего гейт НЕ видит — названо, а не умолчано
 *
 * Конкатенацию через `+` и `String(err.code)` он не ловит: на момент заведения
 * таких мест в дереве ноль (предикат в самой пробе), а предикат, ловящий любое
 * упоминание `.code`, покраснел бы на законном разборе и был бы снят первым же
 * ложным срабатыванием. Форма запрета — ровно та, которой класс и жил.
 *
 * # Объём осмотренного
 *
 * Число прочитанных файлов утверждается непустым и сверяется с нижней границей:
 * «ноль находок» обязано быть отличимо от «ноль прочитанного».
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Пакеты консоли — выводятся из дерева, а не выписываются. */
function consolePackages(): string[] {
  return readdirSync(consoleRoot)
    .filter((d) => {
      const p = path.join(consoleRoot, d, "src");
      try {
        return statSync(p).isDirectory();
      } catch {
        return false;
      }
    })
    .sort();
}

/**
 * Два места, где код протокола читать и показывать МОЖНО, — и оба названы
 * по существу, а не «исторически»:
 *   - `lib/grpc-status` — единственный разбор кода;
 *   - `lib/error-presentation` — единственный показ, и только в `devDetail`.
 */
const ALLOWED = ["shared/src/lib/grpc-status.ts", "shared/src/lib/error-presentation.ts"];

/**
 * `${…code}` внутри шаблонной строки — форма, которой класс и жил.
 *
 * Один уровень вложенных фигурных скобок разрешён намеренно: первая редакция
 * писалась как `[^}]*` и МОЛЧАЛА на `` `${(e as {code:unknown}).code}` `` —
 * закрывающая скобка типа обрывала класс раньше предмета. Поймано инъекцией,
 * а не чтением; поэтому обе формы стоят ниже как проверка предиката.
 */
const CODE_IN_TEMPLATE = /\$\{(?:[^{}]|\{[^{}]*\})*\.code\b(?:[^{}]|\{[^{}]*\})*\}/;

function walk(dir: string, out: string[]): string[] {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === "node_modules" || e.name === "dist" || e.name === ".vite") continue;
      walk(full, out);
      continue;
    }
    if (/\.(ts|tsx)$/.test(e.name) && !/\.test\.tsx?$/.test(e.name)) out.push(full);
  }
  return out;
}

function sourceFiles(): string[] {
  const out: string[] = [];
  for (const pkg of consolePackages()) walk(path.join(consoleRoot, pkg, "src"), out);
  return out;
}

/** Строки файла, подставляющие `.code` в шаблон, — по исполняемой части. */
export function codeInTemplateHits(source: string): number[] {
  const lines = stripComments(source, { keepLines: true }).split("\n");
  const hits: number[] = [];
  lines.forEach((l, i) => {
    if (CODE_IN_TEMPLATE.test(l)) hits.push(i + 1);
  });
  return hits;
}

describe("код протокола не показывается пользователю", () => {
  const files = sourceFiles();

  it("прочитано непустое дерево (иначе «ноль находок» ничего не значит)", () => {
    expect(consolePackages().length).toBeGreaterThanOrEqual(9);
    expect(files.length).toBeGreaterThan(500);
  });

  it("ни один исходник вне двух названных мест не подставляет код в шаблон", () => {
    const findings: string[] = [];
    for (const f of files) {
      const rel = path.relative(consoleRoot, f);
      if (ALLOWED.includes(rel)) continue;
      for (const line of codeInTemplateHits(readFileSync(f, "utf8"))) {
        findings.push(`${rel}:${line}`);
      }
    }
    expect(findings).toEqual([]);
  });

  // Инъекция в обе стороны — на синтетике, чтобы доказательство не зависело от
  // фикстуры, которая истекает вместе со своим предметом.
  it("предикат краснеет на самой форме запрета", () => {
    expect(codeInTemplateHits("const m = `${err.code}: ${err.message}`;")).toEqual([1]);
    expect(codeInTemplateHits("toast.error(`X: ${e.error.code}`);")).toEqual([1]);
    // Форма с приведением типа: на ней первая редакция предиката молчала.
    expect(codeInTemplateHits("const m = `${(err as { code?: unknown }).code}: ${err.message}`;")).toEqual([1]);
  });

  it("и молчит на законной форме той же длины", () => {
    expect(codeInTemplateHits("const m = `${err.message}`;")).toEqual([]);
    expect(codeInTemplateHits("const label = grpcCodeLabel(err.code);")).toEqual([]);
    // Разбор в комментарии — объяснение, а не вызов: запрещать его значило бы
    // запретить объяснять запрет.
    expect(codeInTemplateHits("// прежде здесь стояло `${err.code}: ${err.message}`")).toEqual([]);
  });

  // Координата — часть находки: гейт, называющий не ту строку, посылает искать
  // не туда. Многострочная шапка файла сдвигала номер, пока снятие комментариев
  // не стало сохранять нумерацию.
  it("координата не сдвигается многострочным комментарием выше", () => {
    const src = ["/*", " * шапка", " * из четырёх строк", " */", "const m = `${err.code}`;"].join("\n");
    expect(codeInTemplateHits(src)).toEqual([5]);
  });
});
