// Отказ у поля рисует ОДИН компонент.
//
// # Класс
//
// Правило 1 и правило 8 канона консоли: отказ, названный у поля, — общий
// `FieldError`, а его связь с вводом (`aria-describedby`) — общий
// `fieldErrorId`. Экраны церемоний (#1274, круг 1 ревью) выписали свою
// разметку отказа — свой блок, свой цвет, свой идентификатор — в пяти файлах,
// при том что общий компонент в дереве уже был. Копии расходятся молча: одна
// объявляла отказ предупреждением для читающего с экрана, другие — нет.
//
// # Что утверждает проба
//
// Идентификатор сообщения об отказе поля строит ровно один файл — `FieldError`.
// Рукописная разметка отказа без своего идентификатора не живёт: ввод обязан
// ссылаться на сообщение, и ссылаться ему не на что, кроме этого имени.
// Проба читает ИСПОЛНЯЕМУЮ часть: объяснение, называющее форму, находкой не
// является.
//
// # Объём осмотренного
//
// Печатается числом — пустой обход дал бы зелёное на любом дереве.

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { stripComments } from "@shared/test/strip-comments";

function root(): string {
  let dir = process.cwd();
  for (let i = 0; i < 6; i++) {
    if (existsSync(join(dir, "shared", "src")) && existsSync(join(dir, "vpc", "src"))) return dir;
    const up = dirname(dir);
    if (up === dir) break;
    dir = up;
  }
  throw new Error(`корень консоли не найден вверх от ${process.cwd()} — проба не знает, что читать`);
}

const ROOT = join(root(), "shared", "src");
/** Единственный законный дом имени сообщения об отказе поля. */
const HOME = join("components", "organisms", "form", "FieldError", "FieldError.tsx");

/**
 * Формы, которыми идентификатор сообщения об отказе выписывают руками. Каждая
 * узнаётся на своём образце: форма, которой распознаватель не знает, даёт не
 * красное, а молчание.
 */
const HAND_BUILT_FORMS: ReadonlyArray<{ name: string; re: RegExp; sample: string }> = [
  { name: "шаблонная строка", re: /`[^`]*\$\{[^}]*\}[^`]*-error`/, sample: "const e = `${id}-error`;" },
  { name: "сложение строк", re: /\+\s*["'][\w-]*-error["']/, sample: 'const e = id + "-error";' },
  { name: "литерал атрибута", re: /(?:id|aria-describedby)=["'][\w-]+-error["']/, sample: '<div id="email-error" />' },
];

function handBuilt(src: string): string[] {
  const code = stripComments(src);
  return HAND_BUILT_FORMS.filter((f) => f.re.test(code)).map((f) => f.name);
}

function sources(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) {
      sources(p, out);
      continue;
    }
    if (!/\.tsx?$/.test(entry)) continue;
    if (/\.test\.tsx?$/.test(entry)) continue;
    out.push(p);
  }
  return out;
}

describe("имя сообщения об отказе поля строит один файл", () => {
  const files = sources(ROOT);
  const found = files
    .map((p) => ({ file: p.slice(ROOT.length + 1), forms: handBuilt(readFileSync(p, "utf8")) }))
    .filter((f) => f.forms.length > 0);

  it("перепись непуста — иначе «ноль находок» означало бы «ноль прочитанного»", () => {
    expect(files.length).toBeGreaterThan(100);
  });

  it("строит его ровно FieldError — остальные берут fieldErrorId", () => {
    expect(found.map((f) => `${f.file} (${f.forms.join(", ")})`)).toEqual([`${HOME} (шаблонная строка)`]);
  });

  it.each(HAND_BUILT_FORMS.map((f) => [f.name, f] as const))("форма «%s» узнаётся и в комментарии молчит", (_n, f) => {
    expect(f.re.test(stripComments(f.sample))).toBe(true);
    expect(f.re.test(stripComments(`// здесь стояло ${f.sample}\nexport const x = 1;`))).toBe(false);
  });

  it("вызов fieldErrorId находкой не считается", () => {
    expect(handBuilt(`const e = fieldErrorId(\`\${id}-code\`);`)).toEqual([]);
  });
});
