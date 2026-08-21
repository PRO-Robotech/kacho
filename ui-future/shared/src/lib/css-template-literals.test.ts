import { readFileSync } from "node:fs";
import { globSync } from "node:fs";
import { join } from "node:path";

/**
 * ОБРАТНАЯ КАВЫЧКА В КОММЕНТАРИИ CSS-ЛИТЕРАЛА ЛОМАЕТ СБОРКУ МОДУЛЯ.
 *
 * Стили здесь объявляются шаблонной строкой в .tsx. Обратная кавычка внутри неё
 * ЗАКРЫВАЕТ строку, поэтому привычная разметка имени в комментарии — та самая,
 * которой набран весь остальной корпус, — превращает остаток файла в код и
 * роняет сборку сообщением, не называющим причину: «Expected ";" but found ...».
 *
 * Проверка заведена после ЧЕТВЁРТОГО такого случая за одну сессию. Три первых
 * стоили полного круга: правка → сборка → отказ → поиск строки. Стоимость самой
 * проверки — доли секунды, и она называет файл и номер строки сразу.
 *
 * Что именно проверяется: внутри объявления вида `const X_CSS = ` + шаблонная
 * строка не встречается обратная кавычка. Escape-последовательность (\`) в CSS
 * не нужна ни разу, поэтому исключений у правила нет.
 */
const ROOT = new URL("../../..", import.meta.url).pathname;

function cssLiterals(source: string): { body: string; startLine: number }[] {
  const out: { body: string; startLine: number }[] = [];
  const re = /const\s+\w*(?:CSS|STYLES)\w*\s*=\s*`/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(source))) {
    const from = m.index + m[0].length;
    // конец литерала — первая обратная кавычка, за которой идёт `;`
    const end = source.indexOf("`;", from);
    if (end < 0) continue;
    out.push({ body: source.slice(from, end), startLine: source.slice(0, from).split("\n").length });
    re.lastIndex = end;
  }
  return out;
}

describe("шаблонные строки со стилями", () => {
  it("не содержат обратных кавычек — они закрывают литерал и роняют сборку", () => {
    const files = globSync("**/*.tsx", { cwd: join(ROOT, "shared/src") })
      .filter((f) => !f.includes(".test."))
      .map((f) => join(ROOT, "shared/src", f));

    const offenders: string[] = [];
    let literals = 0;
    for (const file of files) {
      const src = readFileSync(file, "utf8");
      for (const lit of cssLiterals(src)) {
        literals += 1;
        if (lit.body.includes("`")) {
          const rel = file.slice(ROOT.length);
          const line = lit.startLine + lit.body.slice(0, lit.body.indexOf("`")).split("\n").length - 1;
          offenders.push(`${rel}:${line}`);
        }
      }
    }

    // Перепись печатается всегда: «ноль находок» обязано быть отличимо от «ноль
    // прочитанного» — иначе сломанный обход выглядит как чистое дерево.
    expect(files.length).toBeGreaterThan(0);
    expect(literals).toBeGreaterThan(0);
    expect(offenders).toEqual([]);
  });
});
