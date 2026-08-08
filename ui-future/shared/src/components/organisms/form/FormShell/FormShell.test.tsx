// Оболочка формы. Её решение — чью шапку показывать, и оно ТРОЙНОЕ, потому что
// одна и та же форма живёт в трёх местах: отдельной страницей, внутри модалки и
// панелью внутри карточки ресурса. В двух последних шапку уже нарисовал кто-то
// другой; продублировать её значит показать «Создание · Подсеть» дважды и
// сдвинуть поля относительно соседней вкладки.

import { render, screen } from "@testing-library/react";
import { DetailHeaderProvider } from "@shared/components/molecules/PanelHeader";
import { FORM_WIDTH, FormBareProvider, FormShell } from "./FormShell";

function body() {
  return <div data-testid="body">поля</div>;
}

describe("FormShell — отдельная страница", () => {
  it("рисует свою шапку с действием и типом ресурса", () => {
    render(
      <FormShell specId="subnets" mode="create" singular="Подсеть">
        {body()}
      </FormShell>,
    );

    expect(screen.getByText("Создание")).toBeInTheDocument();
    expect(screen.getByText("Подсеть")).toBeInTheDocument();
    expect(screen.getByTestId("body")).toBeInTheDocument();
  });

  it("правка называется правкой, а не созданием", () => {
    render(
      <FormShell specId="subnets" mode="edit" singular="Подсеть">
        {body()}
      </FormShell>,
    );
    expect(screen.getByText("Редактирование")).toBeInTheDocument();
    expect(screen.queryByText("Создание")).not.toBeInTheDocument();
  });

  it("кладёт форму на свою поверхность единой ширины", () => {
    const { container } = render(
      <FormShell specId="subnets" mode="create" singular="Подсеть">
        {body()}
      </FormShell>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.maxWidth).toBe(`${FORM_WIDTH}px`);
    expect(container.querySelector(".kc-surface")).not.toBeNull();
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

    expect(screen.getByText("Создание")).toBeInTheDocument();
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

    expect(screen.getByTestId("body")).toBeInTheDocument();
    expect(screen.queryByText("Создание")).not.toBeInTheDocument();
    expect(screen.queryByText("Подсеть")).not.toBeInTheDocument();
  });
});
