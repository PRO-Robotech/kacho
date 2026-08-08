import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import type { AuthContextValue } from "@shared/contexts/AuthContext";
import type { LoginButton as LoginButtonExport } from "./LoginButton";

// Личность приходит из контекста, поэтому контекст — единственное, что подменяется:
// сам компонент монтируется настоящий.
const auth = { loading: false, login: jest.fn() } as unknown as AuthContextValue;

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => auth,
}));

let LoginButton: typeof LoginButtonExport;

describe("LoginButton", () => {
  beforeAll(async () => {
    ({ LoginButton } = await import("./LoginButton"));
  });

  beforeEach(() => {
    auth.loading = false;
    jest.clearAllMocks();
  });

  it("предлагает вход подписанной кнопкой", () => {
    render(<LoginButton />);

    expect(screen.getByRole("button", { name: "Войти" })).toBeInTheDocument();
  });

  it("по клику зовёт login() ровно один раз и без аргументов", () => {
    render(<LoginButton />);

    fireEvent.click(screen.getByRole("button", { name: "Войти" }));

    expect(auth.login).toHaveBeenCalledTimes(1);
    // Вход идёт в self-service поток провайдера личности; своего адреса возврата
    // кнопка не передаёт — если начнёт, утверждение обязано покраснеть.
    expect(auth.login).toHaveBeenCalledWith();
  });

  it("сам вход не начинает — только по клику пользователя", () => {
    // Кнопка, зовущая login() на монтировании, дала бы петлю перенаправлений:
    // страница уходит к провайдеру личности до того, как её кто-то попросил.
    auth.loading = true;
    const { rerender } = render(<LoginButton />);
    auth.loading = false;
    rerender(<LoginButton />);

    expect(auth.login).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Войти" })).toBeInTheDocument();
  });
});
