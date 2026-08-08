// Меню действий строки таблицы. Оно ОБЕЩАЕТ операции: пункт, которого API не
// подаёт, — обещание без исполнителя, а недостающий пункт делает действие
// недостижимым из консоли вовсе. Плюс два свойства, которые ломаются молча:
// адрес правки берётся из плоскости мутаций (у каталожных ресурсов публичный
// путь на запись не смаршрутизирован), а клик по пункту не должен всплывать до
// строки — иначе таблица уводит на карточку раньше, чем откроется диалог.
//
// antd переопределён локально: общий стенд подменяет `Dropdown` пустым div'ом и
// пункты меню вообще не рисует — на нём проба зеленела бы при любом составе.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

interface MockItem {
  key?: string;
  label?: React.ReactNode;
  type?: string;
  onClick?: (info: { domEvent: { stopPropagation: () => void } }) => void;
}

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  // Пункты меню рисуются кнопками: их состав и обработчики — предмет пробы.
  Dropdown: ({ children, menu }: React.PropsWithChildren<{ menu?: { items?: MockItem[] } }>) =>
    React.createElement(
      "div",
      null,
      children,
      React.createElement(
        "div",
        { "data-testid": "menu" },
        (menu?.items ?? []).map((item, i) =>
          item.type === "divider"
            ? React.createElement("hr", { key: `d${i}` })
            : React.createElement(
                "button",
                {
                  key: item.key ?? i,
                  type: "button",
                  onClick: (e: React.MouseEvent) =>
                    item.onClick?.({ domEvent: { stopPropagation: () => e.stopPropagation() } }),
                },
                item.label,
              ),
        ),
      ),
    ),
}));

const { REGISTRY } = await import("@shared/lib/resource-registry");
const { RowActionsMenu, resourceHasRowActions } = await import("./RowActionsMenu");

function menuLabels(): string[] {
  return [...screen.getByTestId("menu").querySelectorAll("button")].map((b) => b.textContent ?? "");
}

/** Диалог удаления внутри меню зовёт инвалидацию списка — ей нужен клиент. */
function Harness({ children }: React.PropsWithChildren) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/subnets"]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

function renderMenu(specId: string, row: Record<string, unknown>, opts?: { editAsPanel?: boolean }) {
  return render(
    <Harness>
      <RowActionsMenu
        spec={REGISTRY[specId]}
        row={row}
        basePath="/projects/prj-1/vpc/subnets"
        projectId="prj-1"
        editAsPanel={opts?.editAsPanel}
      />
    </Harness>,
  );
}

describe("RowActionsMenu — состав меню", () => {
  it("у изменяемого проектного ресурса есть просмотр, правка, перемещение и удаление", () => {
    renderMenu("subnets", { id: "sub-1", name: "frontend" });
    expect(menuLabels()).toEqual(["Просмотр", "Редактировать", "Переместить", "Удалить"]);
  });

  it("ресурсу без семантики перемещения пункт «Переместить» не обещается", () => {
    // Диалог перемещения печатает REST-вызов; предлагать его там, где глагола
    // нет, значит обещать операцию, которой не существует.
    renderMenu("regions", { id: "ru-central1", name: "ru-central1" });
    expect(menuLabels()).not.toContain("Переместить");
  });

  it("группе безопасности по умолчанию удаление не предлагается", () => {
    // Её удалить нельзя — сеть без группы по умолчанию не бывает; кнопка вела
    // бы в гарантированный отказ.
    renderMenu("security-groups", { id: "sg-1", name: "default", default_for_network: true });
    expect(menuLabels()).not.toContain("Удалить");
  });

  it("обычной группе безопасности удаление предлагается", () => {
    // Положительный контроль к предыдущему: без него «Удалить нет» означало бы
    // лишь, что меню не разобрано.
    renderMenu("security-groups", { id: "sg-2", name: "web" });
    expect(menuLabels()).toContain("Удалить");
  });

  it("у сети есть быстрое создание подсети", () => {
    renderMenu("networks", { id: "net-1", name: "core" });
    expect(menuLabels()).toContain("Создать подсеть");
  });
});

describe("RowActionsMenu — клик не всплывает до строки", () => {
  it("открытие диалога удаления не уводит на карточку", () => {
    // Строка таблицы кликабельна. Всплывший клик увёл бы на карточку до того,
    // как диалог успел бы открыться, — операция стала бы недостижимой.
    const onRowClick = jest.fn();
    render(
      <Harness>
        {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions, jsx-a11y/click-events-have-key-events */}
        <div onClick={onRowClick}>
          <RowActionsMenu
            spec={REGISTRY["subnets"]}
            row={{ id: "sub-1", name: "frontend" }}
            basePath="/projects/prj-1/vpc/subnets"
            projectId="prj-1"
          />
        </div>
      </Harness>,
    );

    fireEvent.click(screen.getByText("Удалить"));

    expect(onRowClick).not.toHaveBeenCalled();
  });

  it("клик по самой кнопке меню строку тоже не задевает", () => {
    const onRowClick = jest.fn();
    render(
      <Harness>
        {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions, jsx-a11y/click-events-have-key-events */}
        <div onClick={onRowClick}>
          <RowActionsMenu
            spec={REGISTRY["subnets"]}
            row={{ id: "sub-1", name: "frontend" }}
            basePath="/projects/prj-1/vpc/subnets"
            projectId="prj-1"
          />
        </div>
      </Harness>,
    );

    fireEvent.click(screen.getByLabelText("Действия"));

    expect(onRowClick).not.toHaveBeenCalled();
  });
});

describe("resourceHasRowActions", () => {
  it("у ресурса с действиями колонка нужна", () => {
    expect(resourceHasRowActions(REGISTRY["subnets"])).toBe(true);
    expect(resourceHasRowActions(REGISTRY["networks"])).toBe(true);
  });

  it("у каталожного ресурса без действий колонки быть не должно", () => {
    // Колонка с кнопкой, открывающей пустое меню, — форма без содержания.
    expect(resourceHasRowActions(REGISTRY["disk-types"])).toBe(false);
  });
});
