import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import type { AuthUser } from "@shared/api/auth";
import type { AuthContextValue } from "@shared/contexts/AuthContext";
import { contextApi } from "@shared/lib/context-store";
import type { ServiceSidebar as ServiceSidebarExport, VPC_APP_MODULES as VpcAppModulesExport } from "./ServiceSidebar";

const login = jest.fn<(returnTo?: string) => void>();
const auth = { user: null, loading: false, login, logout: jest.fn() } as unknown as AuthContextValue;

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => auth,
}));

let ServiceSidebar: typeof ServiceSidebarExport;
let VPC_APP_MODULES: typeof VpcAppModulesExport;

/** Печатает текущий адрес — навигация сайдбара иначе ненаблюдаема. */
function Where() {
  const { pathname } = useLocation();
  return <div data-testid="where">{pathname}</div>;
}

const renderAt = (path: string) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <ServiceSidebar />
      <Routes>
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  );

const where = () => screen.getByTestId("where").textContent;

describe("ServiceSidebar", () => {
  beforeAll(async () => {
    ({ ServiceSidebar, VPC_APP_MODULES } = await import("./ServiceSidebar"));
  });

  beforeEach(() => {
    jest.clearAllMocks();
    auth.user = null;
    auth.loading = false;
    window.localStorage.clear();
    contextApi.setAccount(null);
  });

  it("объявляет себя навигацией и несёт бренд-кнопку на главную", () => {
    renderAt("/dashboard");

    expect(screen.getByRole("navigation", { name: "Навигация сервиса" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Kachō Console" }));

    expect(where()).toBe("/");
  });

  it("не предлагает раздел управления доступом — этого маршрута у приложения нет", () => {
    // Кнопка, за которой ничего не откроется, — тот же дефект, что мёртвая ссылка.
    expect(VPC_APP_MODULES.map((m) => m.key)).not.toContain("iam");
    expect(VPC_APP_MODULES.length).toBeGreaterThan(0);
  });

  it("вне проекта пункты, которым проект нужен, отключены", () => {
    renderAt("/dashboard");

    expect(screen.getByRole("button", { name: "Virtual Private Cloud" })).toBeDisabled();
  });

  it("с выбранным проектом те же пункты открываются и ведут внутрь проекта", () => {
    contextApi.setProject({ id: "prj-7", name: "Седьмой", accountId: "acc-1" });

    renderAt("/dashboard");

    const vpc = screen.getByRole("button", { name: "Virtual Private Cloud" });
    expect(vpc).not.toBeDisabled();

    fireEvent.click(vpc);

    expect(where()).toContain("prj-7");
  });

  it("внутри модуля показывает его собственные разделы и отмечает текущий", () => {
    contextApi.setProject({ id: "prj-7", name: "Седьмой", accountId: "acc-1" });

    renderAt("/projects/prj-7/vpc/subnets");

    const subnets = screen.getByRole("button", { name: "Подсети" });
    expect(subnets).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("button", { name: "Облачные сети" })).not.toHaveAttribute("aria-current");
  });

  it("неавторизованному не показывает администрирование и предлагает вход", () => {
    renderAt("/dashboard");

    expect(screen.queryByRole("button", { name: "Администрирование" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Войти" })).toBeInTheDocument();
  });

  it("вход начинается только по нажатию и запоминает, откуда ушли", () => {
    renderAt("/dashboard");

    expect(login).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Войти" }));

    expect(login).toHaveBeenCalledTimes(1);
    expect(String(login.mock.calls[0][0])).toContain(window.location.pathname);
  });

  it("пока личность грузится — ни входа, ни администрирования", () => {
    auth.loading = true;

    renderAt("/dashboard");

    expect(screen.queryByRole("button", { name: "Войти" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Администрирование" })).not.toBeInTheDocument();
  });

  it("авторизованному открывает администрирование и подписывает его личностью", () => {
    auth.user = { id: "usr-1", display_name: "Иван Петров" } as AuthUser;

    renderAt("/dashboard");

    expect(screen.getByRole("button", { name: "Администрирование" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Иван Петров" })).toBeInTheDocument();
    expect(screen.getByText("ИП")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Войти" })).not.toBeInTheDocument();
  });

  it("администрирование ведёт в раздел регионов", () => {
    auth.user = { id: "usr-1" } as AuthUser;

    renderAt("/dashboard");

    fireEvent.click(screen.getByRole("button", { name: "Администрирование" }));

    expect(where()).toBe("/system/regions");
  });
});
