import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import type { AuthUser } from "@shared/api/auth";
import type { AuthContextValue } from "@shared/contexts/AuthContext";
import type { RequireAuth as RequireAuthExport } from "./RequireAuth";

const auth = { user: null, loading: false } as unknown as AuthContextValue;

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => auth,
}));

let RequireAuth: typeof RequireAuthExport;

/** Экран входа: печатает строку запроса, чтобы адрес возврата был наблюдаем. */
function LoginScreen() {
  const { search } = useLocation();
  return <div data-testid="login-screen">{search}</div>;
}

const renderAt = (path: string, guard: React.ReactNode) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/auth/login" element={<LoginScreen />} />
        <Route path="/secret" element={guard} />
      </Routes>
    </MemoryRouter>,
  );

describe("RequireAuth", () => {
  beforeAll(async () => {
    ({ RequireAuth } = await import("./RequireAuth"));
  });

  beforeEach(() => {
    auth.user = null;
    auth.loading = false;
  });

  it("пока личность грузится — держит место, не пуская и не выгоняя", () => {
    auth.loading = true;

    renderAt("/secret", <RequireAuth>{<div data-testid="protected" />}</RequireAuth>);

    expect(screen.getByTestId("require-auth-loading")).toBeInTheDocument();
    expect(screen.queryByTestId("protected")).not.toBeInTheDocument();
    expect(screen.queryByTestId("login-screen")).not.toBeInTheDocument();
  });

  it("на время загрузки показывает переданный заполнитель вместо своего", () => {
    auth.loading = true;

    renderAt("/secret", <RequireAuth fallback={<div data-testid="my-spinner" />}>{<div />}</RequireAuth>);

    expect(screen.getByTestId("my-spinner")).toBeInTheDocument();
    expect(screen.queryByTestId("require-auth-loading")).not.toBeInTheDocument();
  });

  it("незалогиненного уводит на вход и сохраняет адрес возврата", () => {
    renderAt("/secret?tab=json", <RequireAuth>{<div data-testid="protected" />}</RequireAuth>);

    expect(screen.getByTestId("login-screen")).toHaveTextContent(
      `?return_to=${encodeURIComponent("/secret?tab=json")}`,
    );
    expect(screen.queryByTestId("protected")).not.toBeInTheDocument();
  });

  it("уводит по указанному адресу, если он задан", () => {
    renderAt(
      "/secret",
      <RequireAuth redirectTo="/auth/login">
        <div />
      </RequireAuth>,
    );

    expect(screen.getByTestId("login-screen")).toBeInTheDocument();
  });

  it("залогиненного пускает к защищённому содержимому", () => {
    auth.user = { id: "usr-1" } as AuthUser;

    renderAt("/secret", <RequireAuth>{<div data-testid="protected" />}</RequireAuth>);

    expect(screen.getByTestId("protected")).toBeInTheDocument();
    expect(screen.queryByTestId("login-screen")).not.toBeInTheDocument();
  });
});
