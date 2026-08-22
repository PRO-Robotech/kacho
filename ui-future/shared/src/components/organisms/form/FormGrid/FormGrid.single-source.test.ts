// Геометрия формы объявлена ОДИН раз.
//
// # Класс
//
// Правило продукта требует от тела формы пары «имя слева, ввод справа» с
// колонкой подписи в 200. Требование исполнялось ВОСЕМЬЮ дословными копиями
// одного набора свойств `<Form …>` — в общем теле формы и в каждой рукописной
// форме сети. Копии одного числа расходятся молча: расхождение видно только
// когда две формы стоят рядом на одном экране, то есть почти никогда.
//
// # Что утверждает проба
//
// Ширину колонки подписи объявляет ровно один файл. Проба читает ИСПОЛНЯЕМУЮ
// часть (комментарии сняты общим разборщиком): объяснение, называющее число,
// разбором, а не объявлением, и падать на нём значило бы запретить объяснять.
//
// # Объём осмотренного
//
// Печатается числом: «ноль находок» обязано быть отличимо от «ноль прочитанных
// файлов» — перечень собирается обходом дерева, и пустой обход дал бы зелёное
// на любом дереве.

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { stripComments } from "@shared/test/strip-comments";

/**
 * Корень консоли ищется ВВЕРХ от рабочего каталога: суита исполняется как ESM,
 * где `__dirname` не определён, и проба падала бы поломкой разбора — то есть
 * «не выполнилось», а не вердиктом.
 */
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
/** Единственный законный дом геометрии. */
const HOME = join("components", "organisms", "form", "FormGrid", "FormGrid.tsx");

/**
 * Послабление — ровно одно, названное поимённо и с причиной.
 *
 * Окно выдачи прав администратора — не форма ресурса: оно не проходит через
 * `FormShell`, не несёт общего подвала и объявляет СВОЮ ширину колонки подписи
 * (160, а не 200) — то есть это не копия канона, а другая геометрия у другого
 * предмета. Свести её к общей стоит отдельной работы и отдельного вердикта,
 * поэтому здесь она названа, а не молча пропущена.
 *
 * Послабление ИСТЕКАЕТ САМО: запись, которой больше нечего исключать, — находка
 * (проверка ниже), иначе перечень переживёт свой предмет и унаследует следующую
 * слепую зону.
 */
const EXEMPTIONS = [join("components", "organisms", "system", "GrantAdminModal", "GrantAdminModal.tsx")];

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

describe("ширина колонки подписи объявлена одним файлом", () => {
  const files = sources(join(ROOT, "components"));
  const declaring = files.filter((p) => /labelCol=\{\{\s*flex:/.test(stripComments(readFileSync(p, "utf8"))));

  it("перепись непуста — иначе «ноль находок» означало бы «ноль прочитанного»", () => {
    expect(files.length).toBeGreaterThan(100);
  });

  const relative = declaring.map((p) => p.slice(ROOT.length + 1));

  it("объявление ровно одно, и это FormGrid", () => {
    expect(relative.filter((p) => !EXEMPTIONS.includes(p))).toEqual([HOME]);
  });

  it("послаблению есть что исключать — иначе оно переживёт свой предмет", () => {
    for (const p of EXEMPTIONS) expect(relative).toContain(p);
  });

  // Контроль в обратную сторону: гейт читает ИСПОЛНЯЕМУЮ часть. Без этого он
  // краснел бы на собственном объяснении и на любом разборе, называющем свойство.
  it("упоминание в комментарии объявлением не считается", () => {
    const comment = `// здесь стояло labelCol={{ flex: "200px" }} — снято\nexport const x = 1;`;

    expect(/labelCol=\{\{\s*flex:/.test(stripComments(comment))).toBe(false);
    expect(/labelCol=\{\{\s*flex:/.test(comment)).toBe(true);
  });

  it("объявленное число — то самое, которого требует канон формы", () => {
    const src = readFileSync(join(ROOT, HOME), "utf8");
    expect(stripComments(src)).toContain("FORM_LABEL_WIDTH = 200");
  });
});
