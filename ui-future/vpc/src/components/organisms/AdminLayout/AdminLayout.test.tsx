import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import type { PermissionSnapshot } from "@shared/lib/permissions";
import type { AdminLayout as AdminLayoutExport } from "./AdminLayout";

let isSystemAdmin = false;

jest.unstable_mockModule("@shared/lib/permissions", () => ({
  usePermissions: (): Partial<PermissionSnapshot> => ({ isSystemAdmin, loaded: true }),
}));

jest.unstable_mockModule("@shared/components/organisms/GlobalResourceFormModal", () => ({
  GlobalResourceFormModal: () => <div data-testid="global-form-modal" />,
}));

let AdminLayout: typeof AdminLayoutExport;

const renderAt = (path: string) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/system/*" element={<AdminLayout />}>
          <Route path="*" element={<div data-testid="admin-page" />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

/** Активная вкладка — единственное, что навигатор объявляет наружу. */
const activeTab = () => screen.getByTestId("admin-tabs").getAttribute("activekey");

describe("AdminLayout", () => {
  beforeAll(async () => {
    ({ AdminLayout } = await import("./AdminLayout"));
  });

  beforeEach(() => {
    isSystemAdmin = false;
  });

  it("подписывает раздел и показывает вложенную страницу", () => {
    renderAt("/system/regions");

    expect(screen.getByText("Администрирование")).toBeInTheDocument();
    expect(screen.getByTestId("admin-page")).toBeInTheDocument();
  });

  it("монтирует общую модалку создания — иначе кнопки «Создать» некуда открыть", () => {
    renderAt("/system/regions");

    expect(screen.getByTestId("global-form-modal")).toBeInTheDocument();
  });

  it("активной становится вкладка текущего адреса", () => {
    renderAt("/system/zones");

    expect(activeTab()).toBe("/system/zones");
  });

  it("админ попадает на админскую вкладку по её адресу", () => {
    isSystemAdmin = true;

    renderAt("/system/address-pools");

    expect(activeTab()).toBe("/system/address-pools");
  });

  it("не-админу скрытая вкладка не становится активной — навигатор откатывается к первой видимой", () => {
    // Гейт настоящий на сервере; здесь проверяется, что консоль не подсвечивает
    // раздел, которого у этого пользователя в навигаторе нет.
    renderAt("/system/address-pools");

    expect(activeTab()).toBe("/system/regions");
  });

  it("то же для раздела администраторов кластера", () => {
    renderAt("/system/cluster/admins");
    expect(activeTab()).toBe("/system/regions");

    isSystemAdmin = true;
    renderAt("/system/cluster/admins");
    expect(screen.getAllByTestId("admin-tabs")[1].getAttribute("activekey")).toBe("/system/cluster/admins");
  });

  it("на неизвестном адресе раздела активна первая видимая вкладка", () => {
    renderAt("/system/whatever");

    expect(activeTab()).toBe("/system/regions");
  });
});
