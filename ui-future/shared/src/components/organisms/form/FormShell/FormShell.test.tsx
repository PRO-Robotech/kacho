// Оболочка формы. Её решение — чью шапку показывать, и оно ТРОЙНОЕ, потому что
// одна и та же форма живёт в трёх местах: отдельной страницей, внутри модалки и
// панелью внутри карточки ресурса. В двух последних шапку уже нарисовал кто-то
// другой; продублировать её значит показать «Создать подсеть» дважды и сдвинуть
// поля относительно соседней вкладки.
//
// Второе её решение — ЧТО в этой шапке написано. Прежде действие и предмет
// стояли двумя строками: надзаголовок «Создание» прописными и заголовок
// «Подсеть» под ним. Надзаголовки сняты решением владельца (канон консоли,
// правило 1), и осталась одна строка «Создать подсеть» — действие с предметом в
// винительном падеже. Пробы ниже утверждают именно её, и каждая несёт отрицание
// прежней формы: без него они зеленели бы на шапке, которая называет предмет
// дважды.

import { render, screen } from "@testing-library/react";
import { DetailHeaderProvider } from "@shared/components/molecules/PanelHeader";
import { FORM_WIDTH, FormBareProvider, FormShell } from "./FormShell";

function body() {
  return <div data-testid="body">поля</div>;
}

describe("FormShell — отдельная страница", () => {
  it("шапка называет действие и предмет ОДНОЙ строкой", () => {
    render(
      <FormShell specId="subnets" mode="create" singular="Подсеть">
        {body()}
      </FormShell>,
    );

    expect(screen.getByRole("heading", { name: "Создать подсеть" })).toBeInTheDocument();
    // Прежняя двухчастная форма: надзаголовок-действие над заголовком-типом.
    // Оба узла обязаны отсутствовать — иначе предмет назван дважды.
    expect(screen.queryByText("Создание")).not.toBeInTheDocument();
    expect(screen.queryByText("Подсеть")).not.toBeInTheDocument();
    expect(screen.getByTestId("body")).toBeInTheDocument();
  });

  it("предмет назван ОБЪЯВЛЕННЫМ падежом, а не именительным", () => {
    // Падеж объявляет ресурс (`spec.accusative`); вывод по хвосту слова
    // ошибается молча. Пара «объявленный есть — именительного нет» падает и
    // тогда, когда падеж перестали читать, и тогда, когда его собрали по месту.
    render(
      <FormShell specId="route-tables" mode="create" singular="Таблица маршрутов" accusative="Таблицу маршрутов">
        {body()}
      </FormShell>,
    );

    expect(screen.getByRole("heading", { name: "Создать таблицу маршрутов" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Создать таблица маршрутов" })).not.toBeInTheDocument();
  });

  it("падеж не объявлен — шапка молчит именительным, но предмет всё равно называет", () => {
    // Положительный близнец к предыдущей пробе: без него отрицание «именительного
    // нет» зеленело бы на шапке, которая не называет предмет вовсе.
    render(
      <FormShell specId="subnets" mode="create" singular="Подсеть">
        {body()}
      </FormShell>,
    );

    expect(screen.getByRole("heading", { name: "Создать подсеть" })).toBeInTheDocument();
  });

  it("правка называется правкой, а не созданием", () => {
    render(
      <FormShell specId="subnets" mode="edit" singular="Подсеть">
        {body()}
      </FormShell>,
    );

    expect(screen.getByRole("heading", { name: "Изменить подсеть" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Создать подсеть" })).not.toBeInTheDocument();
  });

  it("шапка стоит на ширине СТРАНИЦЫ, поля — в колонке единой ширины", () => {
    // Прежде колонка `FORM_WIDTH` была корнем и несла в себе заголовок: из-за
    // этого заголовок формы стоял на другой вертикали, чем заголовок списка,
    // откуда на форму пришли, и текст дёргался в переходе. Ширину ограничивают
    // ПОЛЯ — длинная строка ввода не читается, а заголовок от ширины не страдает.
    const { container } = render(
      <FormShell specId="subnets" mode="create" singular="Подсеть">
        {body()}
      </FormShell>,
    );

    const root = container.firstElementChild as HTMLElement;
    expect(root.className).toContain("kc-surface");
    expect(root.style.maxWidth).toBe("");

    const column = [...container.querySelectorAll<HTMLElement>("div")].find(
      (el) => el.style.maxWidth === `${FORM_WIDTH}px`,
    );
    expect(column).toBeDefined();
    // Пара: поля ВНУТРИ колонки, шапка ВНЕ её. Одно без другого выполнимо
    // формой, у которой колонки нет вовсе, либо формой, где в колонку заехало всё.
    expect(column!.contains(screen.getByTestId("body"))).toBe(true);
    expect(column!.contains(screen.getByRole("heading", { name: "Создать подсеть" }))).toBe(false);
  });

  it("заголовок и подзаголовок можно переопределить", () => {
    render(
      <FormShell specId="subnets" mode="create" singular="Подсеть" title="Новая подсеть" subtitle="в сети core">
        {body()}
      </FormShell>,
    );
    expect(screen.getByText("Новая подсеть")).toBeInTheDocument();
    expect(screen.getByText("в сети core")).toBeInTheDocument();
  });
});

describe("FormShell — внутри модалки", () => {
  it("шапку рисует, а свою поверхность — нет", () => {
    // Поверхность даёт сама модалка; карточка внутри карточки читается как
    // двойная рамка.
    const { container } = render(
      <FormBareProvider>
        <FormShell specId="subnets" mode="create" singular="Подсеть">
          {body()}
        </FormShell>
      </FormBareProvider>,
    );

    expect(screen.getByRole("heading", { name: "Создать подсеть" })).toBeInTheDocument();
    expect(container.querySelector(".kc-surface")).toBeNull();
  });
});

describe("FormShell — панелью внутри карточки ресурса", () => {
  it("шапку НЕ дублирует: её уже показала зона карточки", () => {
    render(
      <DetailHeaderProvider value={{ icon: <span data-testid="ctx-icon">S</span> }}>
        <FormShell specId="subnets" mode="create" singular="Подсеть">
          {body()}
        </FormShell>
      </DetailHeaderProvider>,
    );

    // Положительный рядом с отрицанием: без него «шапки нет» зеленело бы и на
    // форме, которая не отрисовалась вовсе.
    expect(screen.getByTestId("body")).toBeInTheDocument();
    // Отрицание названо ТЕКУЩЕЙ формой шапки. Прежняя редакция искала здесь
    // «Создание» и «Подсеть» — узлы, которых продукт больше не производит нигде:
    // проба оставалась зелёной при любом поведении.
    expect(screen.queryByRole("heading")).toBeNull();
    expect(screen.queryByText("Создать подсеть")).not.toBeInTheDocument();
  });
});
