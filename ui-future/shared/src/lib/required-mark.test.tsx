// Звёздочка обязательного поля — СПРАВА от подписи (решение владельца).
//
// Проба держит ДВА разных факта, и ни один без другого не достаточен:
//
//   1. производитель ставит звёздочку ПОСЛЕ текста — это про порядок;
//   2. живая оболочка этого производителя ДЕЙСТВИТЕЛЬНО передаёт форме — это
//      про достижимость.
//
// Предыдущая настройка провалилась ровно на втором: она существовала, была
// написана верно и жила в файле с нулём прод-импортёров, поэтому ни один
// арендатор её не видел. Проба только на порядок (1) осталась бы зелёной и в
// том состоянии — о достижимости она не говорит ничего.
//
// ПОЧЕМУ ОБОЛОЧКА ПОДМЕНЯЕТСЯ ЗДЕСЬ, А НЕ ЧИТАЕТСЯ ИСХОДНИКОМ. Общий стенд
// подменяет `ConfigProvider` фрагментом, который свои свойства выбрасывает: на
// нём провязка не наблюдаема вовсе, и проба зеленела бы при снятой настройке.
// Читать вместо этого файл оболочки текстом — не выход: такая проба зелена,
// пока файл существует, ничего не исполняет и ничего не утверждает (гейт
// `TestUITestsDoNotReadTheirOwnSourceAsText` справедливо считает это находкой).
// Поэтому оболочка ИСПОЛНЯЕТСЯ, а `ConfigProvider` подменён записывающим: проба
// утверждает то, что провайдер реально отдал форме, и прогоняет отданное.
//
// Наблюдаемое на живом стенде (где звёздочку рисует настоящая библиотека)
// утверждает проба браузером — `e2e/specs/findings.spec.ts`, `verifies #562`.

import { jest } from "@jest/globals";
import React from "react";
import { render, screen } from "@testing-library/react";
import { antdStub } from "@shared/test/antd-stub";

interface FormConfig {
  requiredMark?: unknown;
}

/**
 * Что живая оболочка передала форме через ConfigProvider.
 *
 * Держится полем объекта и читается через `captured()`: присваивание идёт
 * ИЗ подменённого модуля, о котором анализ типов не знает, и при чтении прямо
 * из переменной он сузил бы её до «ничего» по последнему видимому присваиванию.
 */
const capture: { form?: FormConfig } = {};

const captured = (): FormConfig | undefined => capture.form;

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  ConfigProvider: ({ children, form }: React.PropsWithChildren<{ form?: FormConfig }>) => {
    capture.form = form;
    return React.createElement(React.Fragment, null, children);
  },
}));

const { requiredMarkAfterLabel } = await import("@shared/lib/required-mark");
const { ThemeProvider } = await import("@shared/lib/theme-context");

type Producer = (label: React.ReactNode, info: { required: boolean }) => React.ReactNode;

/** Рисует подпись ТЕМ производителем, который оболочка отдала форме. */
function liveShellLabel(text: string, required: boolean): string {
  capture.form = undefined;
  render(<ThemeProvider>{null}</ThemeProvider>);

  const form = captured();
  if (typeof form?.requiredMark !== "function") {
    throw new Error(
      "ThemeProvider не передал форме производитель звёздочки (form.requiredMark). Через эту " +
        "оболочку проходит КАЖДАЯ форма, которую заполняет арендатор — страницы всех модулей " +
        "обёрнуты ею, — и без провязки звёздочку рисует библиотека по-своему: СЛЕВА от подписи, " +
        "вопреки решению владельца (#562)",
    );
  }

  render(<div data-testid="подпись">{(form.requiredMark as Producer)(<span>{text}</span>, { required })}</div>);
  return screen.getByTestId("подпись").textContent ?? "";
}

describe("живая оболочка ставит звёздочку ПОСЛЕ текста подписи", () => {
  it("у обязательного поля подпись оканчивается звёздочкой", () => {
    // Утверждается ПОРЯДОК, а не присутствие: `toContain("*")` зеленел бы и на
    // звёздочке слева — то есть ровно на дефекте, который правится.
    const text = liveShellLabel("Имя", true);
    expect(text).toMatch(/Имя\s*\*$/);
    expect(text).not.toMatch(/^\s*\*/);
  });

  it("у необязательного поля звёздочки нет вовсе", () => {
    // Положительный контроль к предыдущему. Без него «звёздочка справа»
    // зеленело бы и на реализации, которая рисует её КАЖДОМУ полю: подпись
    // необязательного поля тоже оканчивалась бы звёздочкой.
    expect(liveShellLabel("Описание", false)).toBe("Описание");
  });

  it("оболочка отдаёт форме единственного производителя, а не свою копию", () => {
    // Второй экземпляр renderer'а разошёлся бы с этим молча, и правка порядка
    // доехала бы не всюду.
    capture.form = undefined;
    render(<ThemeProvider>{null}</ThemeProvider>);
    expect(captured()?.requiredMark).toBe(requiredMarkAfterLabel);
  });
});
