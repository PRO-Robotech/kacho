// Гейт одной геометрии: наборы значений консоли рисуются ОДНОЙ формой.
//
// Предмет. Блоки CIDR подсети, супернет сети, состав набора префиксов, диапазоны
// пула адресов, статические маршруты (и на карточке, и в форме), метки, списки
// из одного подполя — это ОДИН предмет: набор значений, который правят по
// одному. Рисовать его по-разному значит называть один предмет несколькими
// именами (канон консоли, правило 3). Расхождение здесь не бросается в глаза:
// две формы одного набора стоят на разных страницах и рядом не встречаются, —
// поэтому оно растёт молча. Так и жил пул адресов: пятый по счёту набор блоков
// рисовался чипами в карточке, тогда как четыре соседних — таблицей.
//
// Правило. У семьи два вида участников, и требования к ним ПРОТИВОПОЛОЖНЫ:
//
//   · РИСУЮЩИЙ обязан брать геометрию из общего `editor-surface` — высоту
//     строки, ширину колонки действий, поверхность. Своё число высоты строки в
//     таком файле — находка: пять копий одной высоты расходятся молча;
//     поверхность у него бывает ДВУХ законных форм — своя рамка редактора
//     (`editorSurfaceStyle`, редактор в форме стоит сам по себе) и тело внутри
//     общей поверхности карточки (`editorBodyStyle` внутри `DetailSurface`,
//     решение владельца «таблицы должны быть цельными»). Обе объявлены в общем
//     источнике; находка — когда нет ни одной либо когда рамка выписана
//     литералом, то есть та же своя поверхность под другим именем;
//   · НАСТРАИВАЮЩИЙ (владелец ресурса) не рисует вовсе. Он называет пути,
//     имена полей и тексты, а вид получает от рисующего. Появившаяся у него
//     разметка и есть та самая вторая форма одного предмета.
//
// Перепись. Гейт печатает, скольких участников он осмотрел и сколько прочитал:
// «расхождений не найдено» обязано быть отличимо от «ничего не прочитано» —
// переехавший каталог иначе сделал бы все утверждения вакуумно истинными.
//
// Своя предпосылка. Гейт обоснован тем, что общий источник геометрии
// существует и объявляет те самые имена. Перестанет — всплывёт здесь, а не
// превратит обход в тихое «ноль находок».

import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const sharedSrc = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const GEOMETRY_MODULE = "components/organisms/form/editor-surface.ts";
const GEOMETRY_IMPORT = "@shared/components/organisms/form/editor-surface";

/** Файлы, которые РИСУЮТ набор значений. */
const DRAWERS = [
  {
    file: "components/organisms/CidrTableSection/CidrTableSection.tsx",
    subject: "блоки CIDR — общий вид всех наборов",
  },
  { file: "components/organisms/RoutesPanel/RoutesPanel.tsx", subject: "статические маршруты на карточке" },
  { file: "components/organisms/RoutesEditor/RoutesEditor.tsx", subject: "статические маршруты в форме" },
  { file: "components/molecules/EditableKVTable/EditableKVTable.tsx", subject: "метки и списки из одного подполя" },
  // Рисующий из МОДУЛЯ, а не из общего каталога. Он здесь не для полноты списка:
  // перепись, читающая только `shared`, объявляла бы «расхождений не найдено»,
  // ни разу не заглянув в дерево, где форк как раз и заводится, — модуль правят
  // отдельно от общего, и разошедшаяся копия там не встречается с оригиналом.
  // Путь идёт от `shared/src` вверх, потому что общего корня у гейта нет.
  {
    file: "../../nlb/src/components/organisms/TargetsManager/TargetsManager.tsx",
    subject: "цели балансировщика (модуль nlb)",
  },
];

/** Файлы, которые набор НАСТРАИВАЮТ: пути, поля, тексты — и ничего не рисуют. */
const OWNERS = [
  { file: "components/organisms/SubnetCidrManager/SubnetCidrManager.tsx", subject: "CIDR подсети" },
  { file: "components/organisms/SubnetCidrPanel/SubnetCidrPanel.tsx", subject: "оба семейства подсети" },
  { file: "components/organisms/NetworkCidrManager/NetworkCidrManager.tsx", subject: "супернет сети" },
  {
    file: "components/organisms/CidrGroupBlocksManager/CidrGroupBlocksManager.tsx",
    subject: "состав набора префиксов",
  },
  { file: "components/organisms/AddressPoolCidrManager/AddressPoolCidrManager.tsx", subject: "диапазоны пула адресов" },
  { file: "components/organisms/LabelsEditor/LabelsEditor.tsx", subject: "метки ресурса" },
];

const read = (rel: string) => readFileSync(path.join(sharedSrc, rel), "utf8");

/** Исполняемая часть файла: гейт судит по коду, а не по прозе о коде —
 *  комментарий, объясняющий рамку, не является рамкой. */
const codeOf = (source: string) => source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");

/** Своё число высоты строки — ровно та форма, которой расходились копии. */
const OWN_ROW_HEIGHT = /const\s+[A-Za-z_]*ROW_H[A-Za-z_]*\s*=\s*\d/;

/** Своя рамка или радиус, выписанные литералом, — вторая форма поверхности под
 *  другим именем. `borderTop` строки-разделителя сюда не попадает: за `border`
 *  там идёт `Top`, а не двоеточие. */
const OWN_SURFACE_LITERAL = /borderRadius\s*:|border\s*:\s*["'`]/;

/** Что не так у РИСУЮЩЕГО. Пустой список — расхождений нет. */
export function drawerFindings(source: string): string[] {
  const out: string[] = [];
  const code = codeOf(source);
  if (!code.includes(GEOMETRY_IMPORT)) out.push("геометрия берётся не из общего источника");
  // Поверхность — из общего источника, в одной из двух законных форм.
  if (!code.includes("editorSurfaceStyle") && !code.includes("editorBodyStyle")) {
    out.push("поверхность (рамка, радиус, фон) объявлена своя");
  }
  // Тело без рамки законно ТОЛЬКО внутри общей поверхности карточки: само по
  // себе оно висит на пустоте, и секция снова перестаёт быть одним блоком.
  if (code.includes("editorBodyStyle") && !code.includes("DetailSurface")) {
    out.push("тело без рамки стоит вне общей поверхности");
  }
  if (OWN_SURFACE_LITERAL.test(code)) out.push("поверхность выписана своим литералом рамки или радиуса");
  if (!code.includes("EDITOR_ROW_HEIGHT") && !code.includes("editorRowStyle")) {
    out.push("высота строки взята не из общего источника");
  }
  if (!code.includes("EDITOR_ACTIONS_WIDTH")) out.push("ширина колонки действий объявлена своя");
  if (OWN_ROW_HEIGHT.test(code)) out.push("в файле объявлено своё число высоты строки");
  return out;
}

/** Что не так у НАСТРАИВАЮЩЕГО. Пустой список — он действительно не рисует. */
export function ownerFindings(source: string): string[] {
  const out: string[] = [];
  const body = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
  if (/\bstyle=/.test(body)) out.push("владелец ресурса рисует сам: в файле есть разметка со стилем");
  if (/borderRadius|border:\s*"|height:\s*\d/.test(body)) out.push("владелец ресурса объявляет свою геометрию");
  return out;
}

describe("наборы значений рисуются одной формой", () => {
  it("общий источник геометрии существует и объявляет те самые имена", () => {
    // Предпосылка гейта. Без неё все утверждения ниже стали бы про имена,
    // которых больше нет, и молчали бы на любом расхождении.
    const geometry = read(GEOMETRY_MODULE);
    // `editorBodyStyle` в перечне обязателен: на нём стоит вторая законная форма
    // поверхности, и исчезни он — предикат молча принял бы любое тело.
    // `editorMissingFieldStyle` — по той же причине с другой стороны: решение
    // владельца «незаполненное поле помечается на самом поле» относится ко всем
    // наборам, и пока пометка объявлена здесь, рисующему незачем выписывать свою
    // рамку. Исчезни имя — рамка вернулась бы в файлы рисующих, а запрет ниже
    // читался бы как придирка.
    for (const name of [
      "EDITOR_ROW_HEIGHT",
      "EDITOR_ACTIONS_WIDTH",
      "editorSurfaceStyle",
      "editorBodyStyle",
      "editorRowStyle",
      "editorMissingFieldStyle",
    ]) {
      expect(geometry).toContain(`export const ${name}`);
    }
  });

  it("перепись: осмотрены все участники семьи, и каждый файл прочитан", () => {
    expect(DRAWERS.length).toBeGreaterThan(0);
    expect(OWNERS.length).toBeGreaterThan(0);

    let bytes = 0;
    for (const { file } of [...DRAWERS, ...OWNERS]) {
      const full = path.join(sharedSrc, file);
      // Исчезнувший файл обязан ронять гейт, а не тихо уменьшать охват:
      // перечень, из которого выпал участник, зеленеет ровно про остаток.
      expect(existsSync(full)).toBe(true);
      bytes += read(file).length;
    }
    expect(bytes).toBeGreaterThan(0);
    // eslint-disable-next-line no-console
    console.log(
      `[одна геометрия] рисующих ${DRAWERS.length} · настраивающих ${OWNERS.length} · прочитано ${bytes} байт`,
    );
  });

  it.each(DRAWERS)("рисующий «$subject» берёт геометрию из общего источника", ({ file }) => {
    expect(drawerFindings(read(file))).toEqual([]);
  });

  it.each(OWNERS)("владелец «$subject» набор не рисует, а настраивает", ({ file }) => {
    expect(ownerFindings(read(file))).toEqual([]);
  });

  it("предикат способен найти расхождение — инъекция в обе стороны", () => {
    // Отрицание без положительного контроля зеленеет на предикате, который не
    // находит ничего и никогда; положительный без отрицания — на предикате,
    // который находит всё и всегда. Поэтому обе стороны здесь.
    const forkedDrawer = `import { Button } from "antd";\nconst ROW_H = 38;\nexport function X() { return null; }\n`;
    expect(drawerFindings(forkedDrawer)).toContain("геометрия берётся не из общего источника");
    expect(drawerFindings(forkedDrawer)).toContain("поверхность (рамка, радиус, фон) объявлена своя");
    expect(drawerFindings(forkedDrawer)).toContain("в файле объявлено своё число высоты строки");

    // Своя рамка литералом — та же вторая форма поверхности под другим именем.
    // Без этой инъекции расширение предиката до двух законных форм было бы
    // неотличимо от его снятия: файл прошёл бы, выписав рамку руками.
    const literalSurface = `import { editorBodyStyle, editorRowStyle, EDITOR_ACTIONS_WIDTH } from "${GEOMETRY_IMPORT}";
      import { DetailSurface } from "@shared/components/organisms/DetailShell";
      const own = { border: "1px solid #000", borderRadius: 11 };
      export function Z() { return null; }
    `;
    expect(drawerFindings(literalSurface)).toContain("поверхность выписана своим литералом рамки или радиуса");

    // Тело без рамки вне общей поверхности — тоже находка.
    const bodyWithoutSurface = `import { editorBodyStyle, editorRowStyle, EDITOR_ACTIONS_WIDTH } from "${GEOMETRY_IMPORT}";
      export function W() { return null; }
    `;
    expect(drawerFindings(bodyWithoutSurface)).toContain("тело без рамки стоит вне общей поверхности");

    // Законные близнецы — ОБЕ формы поверхности, на живых файлах дерева:
    // тело внутри поверхности карточки и своя рамка редактора в форме.
    // Отрицания без них зеленели бы на предикате, отвергающем всё.
    expect(drawerFindings(read("components/organisms/CidrTableSection/CidrTableSection.tsx"))).toEqual([]);
    expect(drawerFindings(read("components/organisms/RoutesEditor/RoutesEditor.tsx"))).toEqual([]);

    const drawingOwner = `export function Y() { return <div style={{ borderRadius: 8 }} />; }\n`;
    expect(ownerFindings(drawingOwner)).toHaveLength(2);
    expect(ownerFindings(read(OWNERS[0].file))).toEqual([]);
  });
});
