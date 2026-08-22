// Счётчик на вкладке рейла — то, ЧТО ВИДИТ ОПЕРАТОР, и до #625 он был
// ненаблюдаем.
//
// ПРЕДМЕТ. Число у вкладки приходит `Badge`-ем ПРОПОМ (`count`), а не детьми.
// Пока общий заменитель подменял это имя пустым `<div>{children}</div>`, число
// уезжало в АТРИБУТ DOM — настоящий antd таких атрибутов не производит ни
// одного, — и всякое утверждение о счётчике было бы прибито к форме дублёра, а
// не к тому, что видит оператор. Поэтому пробы на счётчик до приведения
// заменителя к настоящей форме писать было НЕЛЬЗЯ (#625): они пережили бы
// продукт.
//
// Утверждается наблюдаемое, а не разметка: текст рядом с подписью вкладки.
// Правила показа — часть того, что видит оператор, и все три несущие: ноль
// не показывается вовсе (иначе рейл пестрит нулями), перебор порога рисуется
// как «N+», а обычное число — как есть.

import { jest } from "@jest/globals";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const { DetailShell } = await import("./DetailShell");

function show(tabs: { id: string; label: string; count?: number }[]) {
  return render(
    <MemoryRouter initialEntries={["/networks/net-1"]}>
      <DetailShell
        resourceName="web"
        tabs={tabs.map((t) => ({ ...t, render: () => <div>содержимое {t.id}</div> }))}
      />
    </MemoryRouter>,
  );
}

/** Вкладка по подписи — по РОЛИ, а не по классу: класс говорит о том, как
 *  пункт выглядит, и о его составе не утверждает ничего. */
const tab = (label: string) => screen.getAllByRole("tab").find((t) => within(t).queryByText(label));

describe("DetailShell — счётчик на вкладке", () => {
  it("вкладка с количеством показывает ЧИСЛО рядом со своей подписью", () => {
    show([
      { id: "overview", label: "Обзор" },
      { id: "rules", label: "Правила", count: 3 },
    ]);

    expect(tab("Правила")).toHaveTextContent("3");
    // Парный контроль: без него утверждение зеленело бы на рейле, который
    // печатает «3» у каждой вкладки.
    expect(tab("Обзор")).not.toHaveTextContent("3");
  });

  it("ноль НЕ показывается — пустая вкладка не подписывается нулём", () => {
    show([
      { id: "overview", label: "Обзор" },
      { id: "rules", label: "Правила", count: 0 },
    ]);

    expect(tab("Правила")).toHaveTextContent("Правила");
    expect(tab("Правила")).not.toHaveTextContent("0");
  });

  it("перебор порога показан как «N+», а не сырым числом", () => {
    // Порог объявлен самой оболочкой (`overflowCount`), поэтому утверждение
    // ловит и его смену: 12345 при пороге 9999 оператор видит как «9999+».
    show([{ id: "rules", label: "Правила", count: 12_345 }]);

    expect(tab("Правила")).toHaveTextContent("9999+");
    expect(tab("Правила")).not.toHaveTextContent("12345");
  });
});
