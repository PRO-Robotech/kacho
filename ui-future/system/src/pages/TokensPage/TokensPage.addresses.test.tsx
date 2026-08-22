// Каждый адрес части «Токены и ключи», который рекламируют меню модуля и рейл
// раздела, обязан ВЕСТИ куда-то.
//
// Предмет. Подписи и адреса пунктов части сведены к одному источнику
// (`@/labels`), и меню с рейлом берут их оттуда. Маршруты — нет: они выписаны
// в `TokensRoutes` отдельно. Значит пункт, добавленный в источник и забытый в
// маршрутах, получил бы рекламу в двух местах и правило-ловушку вместо
// страницы: человек нажимает пункт и попадает на ключи сервисных аккаунтов —
// отказ, неотличимый от «так и задумано».
//
// Соседняя проба (`SystemPage.host-addresses.test.tsx`) этот класс не ловит и
// говорит об этом прямо: «Раздел токенов обслуживается ДРУГИМ блоком маршрутов
// и здесь не проверяется». Здесь он и проверяется.
//
// Утверждается ИСХОД перехода, а не текст файла с маршрутами: чтение исходника
// пережило бы любую смену формы записи и осталось бы зелёным на сработавшем
// правиле-ловушке. Страницы подменены метками — подменён транспорт отрисовки,
// таблица маршрутов настоящая.

import React from "react";
import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes, useLocation } from "react-router";

const marker = (name: string) => () => React.createElement("div", null, name);

jest.unstable_mockModule("@shared/pages/system/ServiceAccountKeysPage", () => ({ default: marker("ключи") }));
jest.unstable_mockModule("@shared/pages/system/UserTokensPage", () => ({ default: marker("токены") }));
// Оболочка части к таблице маршрутов отношения не имеет, но тянет за собой
// общий рейл и весь его граф.
jest.unstable_mockModule("@/components/organisms/TokensLayout", () => ({
  TokensLayout: () => React.createElement(Outlet, null),
}));
jest.unstable_mockModule("@/pages/RemoteShell", () => ({ RemoteShell: marker("оболочка") }));

const { TOKENS_SECTIONS, TOKENS_LANDING_PATH } = await import("@/labels");
const { TokensRoutes } = await import("./TokensPage");

function Address() {
  const loc = useLocation();
  return <div data-testid="address">{loc.pathname}</div>;
}

function openAt(address: string) {
  render(
    <MemoryRouter initialEntries={[address]}>
      <Address />
      <Routes>
        <Route path="/system/tokens/*" element={<TokensRoutes />} />
      </Routes>
    </MemoryRouter>,
  );
  // Перенаправление правила-ловушки происходит В ХОДЕ отрисовки, поэтому адрес
  // здесь уже окончательный — ожидание было бы лишним источником недетерминизма.
  return screen.getByTestId("address").textContent ?? "";
}

const advertised = TOKENS_SECTIONS.map((s) => s.path);

describe("«Токены и ключи» — адреса, рекламируемые меню и рейлом", () => {
  it("объём осмотренного назван: сколько адресов проверяется", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: опустей
    // источник подписей — перечисление ниже не выполнилось бы ни разу.
    expect(advertised.length).toBeGreaterThanOrEqual(2);
    expect(advertised).toContain(TOKENS_LANDING_PATH);
  });

  it.each(advertised.map((a) => [a]))("%s остаётся на своём адресе", (address) => {
    expect(openAt(address)).toBe(address);
  });

  it("адрес без маршрута уводит на первый пункт — контроль в обратную сторону", () => {
    // Без него перечисление выше зеленело бы и на части без правила-ловушки,
    // то есть проверяло бы не то, ради чего написано.
    expect(openAt("/system/tokens/nothing-like-this")).toBe(TOKENS_LANDING_PATH);
  });
});
