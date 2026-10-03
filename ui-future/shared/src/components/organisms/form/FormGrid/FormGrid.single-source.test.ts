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

/**
 * ФОРМЫ ЗАПИСИ геометрии — ВСЕ законные, а не одна (#1274, круг 1 ревью).
 *
 * Прежде гейт знал одну форму — проп `labelCol` в разметке — и читал только
 * `components/`. Две копии прошли мимо него молча: объект свойств формы
 * (`labelCol: { flex: … }` в раскрытии `{...PROPS}`) и геометрия, выписанная
 * руками, без формы вовсе (`flex: "0 0 200px"` у подписи страницы параметров
 * учётной записи в `pages/`). Обе — копии одного числа, ради снятия которых
 * `FormGrid` и заведён.
 *
 * Третья форма узнаётся по ЧИСЛУ, а не по свойству: ширину колонки можно
 * выписать `flex`, `flexBasis`, `width`, и перечень свойств отстал бы от первой
 * же новой записи. Строковый литерал, несущий ширину колонки в пикселях, вне
 * `FormGrid` есть копия by construction — `FormGrid` объявляет её числом.
 */
/**
 * Ширина колонки — из ОБЪЯВЛЕНИЯ `FormGrid`, а не выписанная здесь: выписанное
 * число разошлось бы с каноном молча. Читается текстом, а не импортом: модуль
 * тянет библиотеку компонентов, а гейту нужна одна константа.
 */
const FORM_LABEL_WIDTH = Number(/FORM_LABEL_WIDTH = (\d+)/.exec(stripComments(readFileSync(join(ROOT, HOME), "utf8")))?.[1]);

const DECLARATION_FORMS: ReadonlyArray<{ name: string; re: RegExp; sample: string }> = [
  { name: "проп формы в разметке", re: /labelCol=\{\{\s*flex:/, sample: `<Form labelCol={{ flex: "200px" }}>` },
  { name: "проп формы в объекте", re: /labelCol:\s*\{\s*flex:/, sample: `const P = { labelCol: { flex: "200px" } };` },
  {
    name: "ширина колонки руками",
    re: new RegExp(`["'\`][^"'\`\\n]*\\b${FORM_LABEL_WIDTH}px\\b`),
    sample: `<label style={{ flex: "0 0 200px" }}>`,
  },
];

function declares(src: string): string[] {
  const code = stripComments(src);
  return DECLARATION_FORMS.filter((f) => f.re.test(code)).map((f) => f.name);
}

describe("ширина колонки подписи объявлена одним файлом", () => {
  // Обход — весь `shared/src`, а не `components/`: страница церемонии в `pages/`
  // несла копию, которой гейт не видел по раскладке, а не по форме.
  const files = sources(ROOT);
  const declaring = files.filter((p) => declares(readFileSync(p, "utf8")).length > 0);

  it("перепись непуста — иначе «ноль находок» означало бы «ноль прочитанного»", () => {
    expect(files.length).toBeGreaterThan(100);
  });

  const relative = declaring.map((p) => p.slice(ROOT.length + 1));

  it("объявление ровно одно, и это FormGrid", () => {
    const found = relative
      .filter((p) => !EXEMPTIONS.includes(p))
      .map((p) => `${p} (${declares(readFileSync(join(ROOT, p), "utf8")).join(", ")})`);
    expect(found).toEqual([`${HOME} (проп формы в разметке)`]);
  });

  // Распознаватель обязан знать КАЖДУЮ форму: форма, которой он не знает, даёт
  // не красное и не зелёное, а молчание. Каждая узнаётся на своём образце и
  // замолкает на том же образце в комментарии.
  it.each(DECLARATION_FORMS.map((f) => [f.name, f] as const))("форма «%s» узнаётся и в комментарии молчит", (_n, f) => {
    expect(f.re.test(stripComments(f.sample))).toBe(true);
    expect(f.re.test(stripComments(`// здесь стояло ${f.sample}\nexport const x = 1;`))).toBe(false);
  });

  it("число без пикселей и чужое число копией не считаются", () => {
    expect(declares(`const c = { width: ${FORM_LABEL_WIDTH} };`)).toEqual([]);
    expect(declares(`const s = { flex: "0 0 160px" };`)).toEqual([]);
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
