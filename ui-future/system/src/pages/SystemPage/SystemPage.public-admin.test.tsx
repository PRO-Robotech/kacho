// Экраны регионов, зон и администраторов кластера раздела «Система» стоят на
// ПУБЛИЧНЫХ службах (kacho#3094): их действия не зависят от посадки края, и
// консоль не спрашивает край ни о какой «admin-плоскости» — ни внутренним
// путём, ни каким-либо ещё. Права решает край, отказ приходит словами.
//
// Подменён ТРАНСПОРТ и страницы (метками, печатающими то, что им передали);
// таблица маршрутов — настоящая.

import React from "react";
import { jest } from "@jest/globals";
import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import type { ResourceSpec } from "@shared/lib/resource-registry";

const get = jest.fn<(path: string, query?: Record<string, string>) => Promise<unknown>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list: jest.fn(), get, create: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

const opsMarker = (name: string) => (p: { spec: ResourceSpec }) =>
  React.createElement(
    "div",
    null,
    `${name} ${p.spec.id} create=${String(p.spec.ops.create)} update=${String(p.spec.ops.update)} delete=${String(p.spec.ops.delete)}`,
  );

jest.unstable_mockModule("@shared/components/organisms/ResourceListPage", () => ({
  ResourceListPage: opsMarker("список"),
}));
jest.unstable_mockModule("@shared/components/organisms/ResourceCreatePage", () => ({
  ResourceCreatePage: opsMarker("создание"),
}));
jest.unstable_mockModule("@shared/components/organisms/ResourceDetailPage", () => ({
  ResourceDetailPage: opsMarker("карточка"),
}));
jest.unstable_mockModule("@shared/components/organisms/ResourceEditPage", () => ({
  ResourceEditPage: opsMarker("правка"),
}));
jest.unstable_mockModule("@shared/pages/AddressPoolDetailPage", () => ({
  AddressPoolDetailPage: () => React.createElement("div", null, "пул"),
}));
jest.unstable_mockModule("@shared/pages/SystemSearchPage", () => ({
  SystemSearchPage: () => React.createElement("div", null, "поиск"),
  SEARCH_DOMAINS: [],
}));
jest.unstable_mockModule("@shared/pages/system/ClusterAdminsPage", () => ({
  default: () => React.createElement("div", null, "администраторы"),
}));
jest.unstable_mockModule("@/components/organisms/AdminLayout", () => ({
  AdminLayout: () => React.createElement(Outlet, null),
}));
jest.unstable_mockModule("@/pages/RemoteShell", () => ({
  RemoteShell: () => React.createElement("div", null, "оболочка"),
}));
jest.unstable_mockModule("@/pages/TokensPage", () => ({
  TokensRoutes: () => React.createElement("div", null, "токены"),
}));

const { SystemRoutes } = await import("./SystemPage");

function openAt(address: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <MemoryRouter initialEntries={[address]}>
      <QueryClientProvider client={client}>
        <Routes>
          <Route path="/system/*" element={<SystemRoutes />} />
        </Routes>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  jest.clearAllMocks();
});

describe("SystemRoutes — экраны на публичных службах (#3094)", () => {
  it.each([
    ["/system/regions", "список regions create=true update=true delete=true"],
    ["/system/zones", "список zones create=true update=true delete=true"],
    ["/system/regions/create", "создание regions create=true update=true delete=true"],
    ["/system/zones/z-1/edit", "правка zones create=true update=true delete=true"],
    ["/system/address-pools", "список address-pools create=true update=true delete=true"],
  ])("%s предлагает мутации сразу — без пробы посадки края", async (address, marker) => {
    openAt(address);

    expect(await screen.findByText(marker)).toBeInTheDocument();
    expect(screen.queryByText(/недоступен на этой посадке/)).not.toBeInTheDocument();
  });

  it("администраторы кластера открываются без слов о посадке", async () => {
    openAt("/system/cluster/admins");

    expect(await screen.findByText("администраторы")).toBeInTheDocument();
    expect(screen.queryByText(/недоступен на этой посадке/)).not.toBeInTheDocument();
  });

  it("раздел не обращается к краю сам — во внутреннее пространство путей тем более", async () => {
    openAt("/system/regions");

    expect(await screen.findByText(/^список regions/)).toBeInTheDocument();
    await act(async () => {});
    expect(get.mock.calls.filter(([p]) => p.includes("/internal"))).toEqual([]);
    expect(get).not.toHaveBeenCalled();
  });
});
