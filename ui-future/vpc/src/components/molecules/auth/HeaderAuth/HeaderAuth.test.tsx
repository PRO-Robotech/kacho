import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import type { AuthUser } from "@shared/api/auth";
import type { AuthContextValue } from "@shared/contexts/AuthContext";
import type { HeaderAuth as HeaderAuthExport } from "./HeaderAuth";

// Предмет пробы — САМ ВЫБОР HeaderAuth между тремя состояниями, поэтому обе ветви
// подменены различимыми заглушками: иначе утверждение зеленело бы на любой из них
// (обе настоящие ветви рисуют кнопку).
const auth = { user: null, loading: false } as unknown as AuthContextValue;

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => auth,
}));

jest.unstable_mockModule("../LoginButton", () => ({
  LoginButton: () => <div data-testid="login-button" />,
}));

jest.unstable_mockModule("../UserMenu", () => ({
  UserMenu: () => <div data-testid="user-menu" />,
}));

let HeaderAuth: typeof HeaderAuthExport;

const someUser = { id: "usr-1", email: "a@b.c" } as unknown as AuthUser;

describe("HeaderAuth", () => {
  beforeAll(async () => {
    ({ HeaderAuth } = await import("./HeaderAuth"));
  });

  it("пока личность грузится — не предлагает ни входа, ни меню", () => {
    auth.loading = true;
    auth.user = null;

    render(<HeaderAuth />);

    expect(screen.queryByTestId("login-button")).not.toBeInTheDocument();
    expect(screen.queryByTestId("user-menu")).not.toBeInTheDocument();
  });

  it("незалогиненному предлагает вход", () => {
    auth.loading = false;
    auth.user = null;

    render(<HeaderAuth />);

    expect(screen.getByTestId("login-button")).toBeInTheDocument();
    expect(screen.queryByTestId("user-menu")).not.toBeInTheDocument();
  });

  it("залогиненному показывает меню пользователя", () => {
    auth.loading = false;
    auth.user = someUser;

    render(<HeaderAuth />);

    expect(screen.getByTestId("user-menu")).toBeInTheDocument();
    expect(screen.queryByTestId("login-button")).not.toBeInTheDocument();
  });

  it("загрузка сильнее личности: пользователь уже есть, но состояние ещё не устоялось", () => {
    // Порядок веток — часть поведения: иначе на долгом whoami успевает мигнуть
    // меню с личностью, которую ответ ещё не подтвердил.
    auth.loading = true;
    auth.user = someUser;

    render(<HeaderAuth />);

    expect(screen.queryByTestId("user-menu")).not.toBeInTheDocument();
  });
});
