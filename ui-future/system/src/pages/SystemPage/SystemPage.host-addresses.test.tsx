// Каждый адрес под `/system/…`, который рекламирует оболочка, обязан ВЕСТИ
// куда-то. Модуль ловит всё остальное правилом «увести на регионы», поэтому
// адрес без маршрута не падает и ничего не печатает: человек нажимает «Поиск» и
// получает список регионов — отказ, неотличимый от «так и задумано».
//
// Предмет пробы — ИСХОД перехода: модуль открывается на каждом рекламируемом
// адресе, и утверждается, что он на нём остался. Прежняя редакция читала
// SystemPage.tsx, navigation.ts и рейл оболочки как ТЕКСТ и сравнивала строки;
// она пережила бы любую смену формы записи маршрута, а на сработавшем
// правиле-ловушке осталась бы зелёной — про переход она не спрашивала вовсе.
//
// Страницы подменены метками: предмет — куда привёл адрес, а не что нарисовала
// страница. Подменён ТРАНСПОРТ отрисовки, таблица маршрутов — настоящая.

import React from "react";
import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes, useLocation } from "react-router";

const marker = (name: string) => () => React.createElement("div", null, name);

jest.unstable_mockModule("@shared/components/organisms/ResourceListPage", () => ({
  ResourceListPage: marker("список"),
}));
jest.unstable_mockModule("@shared/components/organisms/ResourceCreatePage", () => ({
  ResourceCreatePage: marker("создание"),
}));
jest.unstable_mockModule("@shared/components/organisms/ResourceDetailPage", () => ({
  ResourceDetailPage: marker("карточка"),
}));
jest.unstable_mockModule("@shared/components/organisms/ResourceEditPage", () => ({
  ResourceEditPage: marker("правка"),
}));
jest.unstable_mockModule("@shared/pages/AddressPoolDetailPage", () => ({ AddressPoolDetailPage: marker("пул") }));
jest.unstable_mockModule("@shared/pages/SystemSearchPage", () => ({
  SystemSearchPage: marker("поиск"),
  SEARCH_DOMAINS: [],
}));
jest.unstable_mockModule("@shared/pages/system/ClusterAdminsPage", () => ({ default: marker("администраторы") }));
jest.unstable_mockModule("@/components/organisms/AdminLayout", () => ({
  AdminLayout: () => React.createElement(Outlet, null),
}));
// Оболочка и раздел токенов к таблице маршрутов отношения не имеют, но
// импортируются модулем на верхнем уровне и тянут за собой весь граф консоли.
jest.unstable_mockModule("@/pages/RemoteShell", () => ({ RemoteShell: marker("оболочка") }));
jest.unstable_mockModule("@/pages/TokensPage", () => ({ TokensRoutes: marker("токены") }));

const { SYSTEM_NAVIGATION } = await import("@/navigation");
const { SystemRoutes } = await import("./SystemPage");

function Address() {
  const loc = useLocation();
  return <div data-testid="address">{loc.pathname}</div>;
}

function openAt(address: string) {
  render(
    <MemoryRouter initialEntries={[address]}>
      <Address />
      <Routes>
        <Route path="/system/*" element={<SystemRoutes />} />
      </Routes>
    </MemoryRouter>,
  );
  // Перенаправление правила-ловушки происходит В ХОДЕ отрисовки, поэтому адрес
  // здесь уже окончательный — ожидание не нужно и было бы лишним источником
  // недетерминизма.
  return screen.getByTestId("address").textContent ?? "";
}

/**
 * Адреса под `/system/…`, которые рекламирует навигация модуля, плюс «Поиск» —
 * его рейл оболочки держит постоянным пунктом. Раздел токенов обслуживается
 * ДРУГИМ блоком маршрутов (`/system/tokens/*`) и здесь не проверяется.
 */
const advertised = [
  ...new Set(
    SYSTEM_NAVIGATION.flatMap((s) => [s.landingPath, ...s.items.map((i) => i.path)]).filter(
      (p) => p.startsWith("/system/") && !p.startsWith("/system/tokens"),
    ),
  ),
  "/system/search",
];

describe("SystemPage — адреса, рекламируемые оболочкой", () => {
  it("объём осмотренного назван: сколько адресов проверяется", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: опустей
    // навигация — перечисление ниже не выполнилось бы ни разу.
    expect(advertised.length).toBeGreaterThanOrEqual(4);
    expect(advertised).toContain("/system/search");
  });

  it.each(advertised.map((a) => [a]))("%s остаётся на своём адресе", (address) => {
    expect(openAt(address)).toBe(address);
  });

  it("адрес без маршрута уводит на регионы — контроль в обратную сторону", () => {
    // Без него перечисление выше зеленело бы и на модуле без правила-ловушки,
    // то есть проверяло бы не то, ради чего написано.
    expect(openAt("/system/nothing-like-this")).toBe("/system/regions");
  });
});
