// Оболочка карточки ресурса: какой таб показан, чем он выбирается и что
// происходит с адресом страницы.
//
// Предмет — четыре свойства, каждое из которых ломается молча:
//
//  1. таб берётся из адреса (`?tab=`), а неизвестный идентификатор откатывается
//     к первому. Без отката оболочка показала бы пустую зону — «страница
//     сломалась» вместо «такого таба нет»;
//  2. выбор таба ПО УМОЛЧАНИЮ убирает `?tab=` из адреса, а не пишет его: иначе
//     каждый заход плодил бы две разные ссылки на одну и ту же страницу;
//  3. в управляемом режиме оболочка адрес НЕ трогает вовсе — навигацией
//     распоряжается вызывающий (у него на таб приходится свой путь);
//  4. `HeaderSlotPortal` вне оболочки не рисует НИЧЕГО. Это заявленная мягкая
//     деградация: связанные таблицы поднимают свой тулбар на строку имени и
//     обязаны переживать использование вне карточки.
//
// `Menu` общего стенда-заменителя — пустой `<div>`: пункты он не рисует, поэтому
// на нём «клик по табу» был бы недостижим, а утверждение о выбранном табе —
// истинным при любом. Здесь он переопределён так, чтобы пункты были кнопками.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { antdStub } from "@shared/test/antd-stub";

interface MenuItem {
  key: string;
  label?: React.ReactNode;
}

interface MenuProps {
  items?: MenuItem[];
  selectedKeys?: string[];
  onClick?: (info: { key: string }) => void;
}

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Menu: ({ items, selectedKeys, onClick }: MenuProps) =>
    React.createElement(
      "nav",
      { "data-testid": "rail" },
      (items ?? []).map((item) =>
        React.createElement(
          "button",
          {
            key: item.key,
            type: "button",
            "aria-current": (selectedKeys ?? []).includes(item.key) ? "page" : undefined,
            onClick: () => onClick?.({ key: item.key }),
          },
          item.label as React.ReactNode,
        ),
      ),
    ),
}));

const { DetailShell, HeaderSlotPortal } = await import("./DetailShell");

const tabs = [
  { id: "overview", label: "Обзор", render: () => <div>содержимое обзора</div> },
  { id: "json", label: "JSON", render: () => <div>содержимое json</div> },
];

/** Печатает текущий адрес: свойство «что стало со ссылкой» иначе ненаблюдаемо. */
function Address() {
  const loc = useLocation();
  return <div data-testid="address">{loc.search || "(без параметров)"}</div>;
}

function show(initial: string, props: Record<string, unknown> = {}) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Address />
      <DetailShell resourceLabel="Сеть" resourceName="web" tabs={tabs} {...props} />
    </MemoryRouter>,
  );
}

const railButtons = () => [...screen.getByTestId("rail").querySelectorAll("button")];
const selectedTab = () => railButtons().find((b) => b.getAttribute("aria-current") === "page")?.textContent;
const address = () => screen.getByTestId("address").textContent;

describe("DetailShell — выбор таба", () => {
  it("без параметра показывает первый таб", () => {
    show("/networks/net-1");

    expect(screen.getByText("содержимое обзора")).toBeInTheDocument();
    expect(screen.queryByText("содержимое json")).not.toBeInTheDocument();
    expect(selectedTab()).toBe("Обзор");
  });

  it("таб берётся из адреса", () => {
    show("/networks/net-1?tab=json");

    expect(screen.getByText("содержимое json")).toBeInTheDocument();
    expect(screen.queryByText("содержимое обзора")).not.toBeInTheDocument();
    expect(selectedTab()).toBe("JSON");
  });

  it("неизвестный таб откатывается к первому, а не показывает пустоту", () => {
    show("/networks/net-1?tab=такого-нет");

    expect(screen.getByText("содержимое обзора")).toBeInTheDocument();
    expect(selectedTab()).toBe("Обзор");
  });

  it("выбор неосновного таба записывается в адрес", () => {
    show("/networks/net-1");

    fireEvent.click(railButtons()[1]);

    expect(address()).toBe("?tab=json");
    expect(screen.getByText("содержимое json")).toBeInTheDocument();
  });

  it("возврат к табу по умолчанию УБИРАЕТ параметр, а не пишет его", () => {
    // Иначе на одну страницу приходилось бы две разные ссылки.
    show("/networks/net-1?tab=json");

    fireEvent.click(railButtons()[0]);

    expect(address()).toBe("(без параметров)");
    expect(screen.getByText("содержимое обзора")).toBeInTheDocument();
  });

  it("в управляемом режиме адрес не трогается — навигирует вызывающий", () => {
    const onTabSelect = jest.fn();
    show("/networks/net-1", { activeTabId: "json", onTabSelect });

    expect(screen.getByText("содержимое json")).toBeInTheDocument();

    fireEvent.click(railButtons()[0]);

    expect(onTabSelect).toHaveBeenCalledWith("overview");
    expect(address()).toBe("(без параметров)");
    // Активный таб задаёт вызывающий: сама оболочка его не переключала.
    expect(screen.getByText("содержимое json")).toBeInTheDocument();
  });
});

describe("DetailShell — содержимое главной зоны", () => {
  it("mainOverride заменяет содержимое активного таба, оставляя рейл", () => {
    show("/networks/net-1", { mainOverride: <div>форма правки</div> });

    expect(screen.getByText("форма правки")).toBeInTheDocument();
    expect(screen.queryByText("содержимое обзора")).not.toBeInTheDocument();
    expect(railButtons()).toHaveLength(2);
  });

  it("ресурс без имени подписан явно, а не пустотой", () => {
    show("/networks/net-1", { resourceName: "" });

    expect(screen.getByText("(без имени)")).toBeInTheDocument();
  });
});

describe("HeaderSlotPortal", () => {
  it("внутри оболочки показывает содержимое слота", () => {
    render(
      <MemoryRouter initialEntries={["/networks/net-1"]}>
        <DetailShell
          resourceLabel="Сеть"
          resourceName="web"
          tabs={[
            {
              id: "overview",
              label: "Обзор",
              render: () => <HeaderSlotPortal>поиск по списку</HeaderSlotPortal>,
            },
          ]}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("поиск по списку")).toBeInTheDocument();
  });

  it("вне оболочки не рисует ничего и не падает", () => {
    // Заявленная мягкая деградация: связанные таблицы переиспользуются вне
    // карточки, и отсутствие слота не должно ронять страницу.
    const { container } = render(<HeaderSlotPortal>поиск по списку</HeaderSlotPortal>);

    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByText("поиск по списку")).not.toBeInTheDocument();
  });
});
