// Раздел «Система» на посадке без admin-плоскости говорит СЛОВАМИ, что раздел
// недоступен здесь, и не предлагает действий, которые кончатся отказом
// каталога, — вместо «нет прав» администратору (#2692, наблюдение 1).
//
// Подменён ТРАНСПОРТ (ответ пробы) и страницы (метками, печатающими то, что
// им передали); таблица маршрутов и вывод посадки — настоящие.

import React from "react";
import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import type { ResourceSpec } from "@shared/lib/resource-registry";

const list = jest.fn<(path: string, query?: Record<string, string>) => Promise<unknown>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, get: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
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

const unavailable = () => screen.queryByText(/недоступен на этой посадке/);

beforeEach(() => {
  jest.clearAllMocks();
});

describe("SystemRoutes — посадка admin-плоскости", () => {
  it("плоскости на этой посадке нет: раздел говорит это словами, а действия сняты", async () => {
    list.mockRejectedValue(new ApiError(404, 5, [], "Not Found"));

    openAt("/system/regions");

    expect(await screen.findByText(/недоступен на этой посадке/)).toBeInTheDocument();
    expect(await screen.findByText("список regions create=false update=false delete=false")).toBeInTheDocument();
    expect(list).toHaveBeenCalledWith("/vpc/v1/addressPools", expect.objectContaining({ pageSize: "1" }));
  });

  it("плоскость обслуживается: слов о недоступности нет, действия на месте — контроль в обратную сторону", async () => {
    list.mockResolvedValue({ addressPools: [] });

    openAt("/system/regions");

    expect(await screen.findByText("список regions create=true update=true delete=true")).toBeInTheDocument();
    expect(unavailable()).not.toBeInTheDocument();
  });

  it("отказ в правах — не отсутствие раздела: слова не показываются, отказ остаётся отказом", async () => {
    list.mockRejectedValue(new ApiError(403, 7, [{ reason: "AUTHZ_DENIED" }], "permission denied"));

    openAt("/system/zones");

    expect(await screen.findByText("список zones create=true update=true delete=true")).toBeInTheDocument();
    expect(unavailable()).not.toBeInTheDocument();
  });

  it("пока посадка не известна, действия не предлагаются — иначе кнопка успела бы отказать раньше слов", () => {
    list.mockReturnValue(new Promise(() => {}));

    openAt("/system/address-pools");

    expect(screen.getByText("список address-pools create=false update=false delete=false")).toBeInTheDocument();
    expect(unavailable()).not.toBeInTheDocument();
  });
});
