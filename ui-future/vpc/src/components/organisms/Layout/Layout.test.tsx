import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import type { Layout as LayoutExport } from "./Layout";

const toggle = jest.fn();
let mode: "light" | "dark" = "light";

jest.unstable_mockModule("@shared/lib/theme-context", () => ({
  useThemeMode: () => ({ mode, setMode: jest.fn(), toggle }),
}));

// Дети подменены маркерами: предмет пробы — СОСТАВ страницы и её собственная
// шапка, а не поведение чужих узлов (у каждого своя проба).
jest.unstable_mockModule("@/components/organisms/ContextUrlSync", () => ({
  ContextUrlSync: () => <div data-testid="context-url-sync" />,
}));

jest.unstable_mockModule("@/components/molecules/ContextBreadcrumb", () => ({
  ContextBreadcrumb: () => <div data-testid="context-breadcrumb" />,
}));

jest.unstable_mockModule("@/components/organisms/ServiceSidebar", () => ({
  ServiceSidebar: () => <div data-testid="service-sidebar" />,
}));

jest.unstable_mockModule("@shared/components/organisms/GlobalResourceFormModal", () => ({
  GlobalResourceFormModal: () => <div data-testid="global-form-modal" />,
}));

jest.unstable_mockModule("@shared/components/molecules/OperationBanner", () => ({
  OperationBanner: () => <div data-testid="operation-banner" />,
}));

let Layout: typeof LayoutExport;

const renderLayout = () =>
  render(
    <MemoryRouter initialEntries={["/dashboard"]}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/dashboard" element={<div data-testid="page" />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

describe("Layout", () => {
  beforeAll(async () => {
    ({ Layout } = await import("./Layout"));
  });

  beforeEach(() => {
    jest.clearAllMocks();
    mode = "light";
  });

  it("показывает страницу маршрута внутри себя", () => {
    renderLayout();

    expect(screen.getByTestId("page")).toBeInTheDocument();
  });

  it("держит смонтированными узлы, без которых страницы молча ломаются", () => {
    renderLayout();

    // Каждый из них однажды забывали смонтировать: без синхронизатора контекст
    // не следует за адресом, без плашки операций изменения не подтягиваются,
    // без общей модалки кнопкам «Создать» некуда открыться.
    expect(screen.getByTestId("context-url-sync")).toBeInTheDocument();
    expect(screen.getByTestId("operation-banner")).toBeInTheDocument();
    expect(screen.getByTestId("global-form-modal")).toBeInTheDocument();
    expect(screen.getByTestId("service-sidebar")).toBeInTheDocument();
    expect(screen.getByTestId("context-breadcrumb")).toBeInTheDocument();
  });

  it("даёт странице правый слот шапки", async () => {
    const { useHeaderRight } = await import("@shared/components/molecules/PageHeaderSlot");
    const right = <span>Создать сеть</span>;
    const Page = () => {
      useHeaderRight(right);
      return null;
    };

    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Routes>
          <Route element={<Layout />}>
            <Route path="/dashboard" element={<Page />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText("Создать сеть")).toBeInTheDocument();
  });

  it("переключатель темы подписан противоположной темой и переключает по клику", () => {
    renderLayout();

    const btn = screen.getByRole("button", { name: "Включить тёмную тему" });
    fireEvent.click(btn);

    expect(toggle).toHaveBeenCalledTimes(1);
  });

  it("в тёмной теме предлагает вернуться к светлой", () => {
    mode = "dark";

    renderLayout();

    expect(screen.getByRole("button", { name: "Включить светлую тему" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Включить тёмную тему" })).not.toBeInTheDocument();
  });

  it("в подвале год считается, а не вписан", () => {
    renderLayout();

    expect(screen.getByText(`PRO Robotech © ${new Date().getFullYear()}`)).toBeInTheDocument();
  });
});
