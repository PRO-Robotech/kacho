// Инъекция к пробе «показ примонтирован»: распознаватель судит УЗЕЛ, а не написание.
//
// # Зачем отдельная проба
//
// Проба дерева рядом отвечает «все семь модулей несут показ». Это утверждение
// выполняется и предикатом, который отвечает «да» на что угодно похожее, —
// поэтому здесь распознавателю подаётся синтетический вход, и по каждой оси
// спрашивается ОБА направления: настоящий показ обязан быть найден, его двойник
// — нет.
//
// # Что было
//
// Предикат сверял вхождение подстроки `<Toaster` и потому засчитывал соседа с
// похожим именем. Обнаружено тем, что инъекция `<ToasterDISABLED` НЕ покраснела:
// показ был снят, а проба молчала. Слепых зон у подстроки оказалось три, и все
// перечислены ниже своими случаями.

import { mountsDisplay } from "../../../test/toaster-mount";

const IMPORT_LINE =
  'import { Toaster } from "@/components/molecules/Toaster";\n';

/** Модуль, монтирующий показ ровно так, как это делают все семь сегодня. */
const LAWFUL = `${IMPORT_LINE}export function Page() {\n  return <div><Toaster /></div>;\n}\n`;

describe("распознаватель показа: настоящий найден", () => {
  it("самозакрывающийся тег — форма всех семи модулей дерева", () => {
    expect(mountsDisplay("Page.tsx", LAWFUL)).toBe(true);
  });

  it("тег с содержимым", () => {
    const code = `${IMPORT_LINE}export function Page() {\n  return <Toaster>тише</Toaster>;\n}\n`;
    expect(mountsDisplay("Page.tsx", code)).toBe(true);
  });

  it("переименование при ввозе — показ остаётся показом", () => {
    const code =
      'import { Toaster as Notifications } from "@/components/molecules/Toaster";\n' +
      "export function Page() {\n  return <Notifications />;\n}\n";
    expect(mountsDisplay("Page.tsx", code)).toBe(true);
  });

  it("обращение через пространство имён", () => {
    const code =
      'import * as UI from "@/components/molecules/Toaster";\n' +
      "export function Page() {\n  return <UI.Toaster />;\n}\n";
    expect(mountsDisplay("Page.tsx", code)).toBe(true);
  });
});

describe("распознаватель показа: двойник молчит", () => {
  it("сосед с похожим именем показом не является", () => {
    // Ровно тот вход, на котором прежний предикат не покраснел: имя начинается
    // так же, а компонент другой.
    const code =
      'import { ToasterPlaceholder } from "@/components/molecules/Toaster";\n' +
      "export function Page() {\n  return <ToasterPlaceholder />;\n}\n";
    expect(mountsDisplay("Page.tsx", code)).toBe(false);
  });

  it("отключённый показ — имя с суффиксом", () => {
    const code =
      'import { ToasterDISABLED } from "@/components/molecules/Toaster";\n' +
      "export function Page() {\n  return <ToasterDISABLED />;\n}\n";
    expect(mountsDisplay("Page.tsx", code)).toBe(false);
  });

  it("упоминание в комментарии показом не является", () => {
    const code = `${IMPORT_LINE}export function Page() {\n  // здесь стоял <Toaster />, его сняли\n  return <div />;\n}\n`;
    expect(mountsDisplay("Page.tsx", code)).toBe(false);
  });

  it("строковый литерал показом не является", () => {
    const code = `${IMPORT_LINE}export const hint = "<Toaster />";\n`;
    expect(mountsDisplay("Page.tsx", code)).toBe(false);
  });

  it("тег без ввоза показа не собирается вовсе", () => {
    const code = "export function Page() {\n  return <Toaster />;\n}\n";
    expect(mountsDisplay("Page.tsx", code)).toBe(false);
  });

  it("разметки не бывает в .ts — показ там невозможен by construction", () => {
    expect(
      mountsDisplay(
        "toast.ts",
        `${IMPORT_LINE}export const s = "<Toaster />";\n`,
      ),
    ).toBe(false);
  });
});

describe("распознаватель показа: собственная предпосылка", () => {
  it("законный вход остаётся законным — контроль к каждому отрицанию выше", () => {
    // Без него все утверждения об отсутствии выполнял бы распознаватель,
    // отвечающий «нет» всегда, — и суита была бы зелена при мёртвом предикате.
    expect(mountsDisplay("Page.tsx", LAWFUL)).toBe(true);
  });
});
