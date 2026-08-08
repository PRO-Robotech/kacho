import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { AuthUser } from "@shared/api/auth";
import type { AuthContextValue } from "@shared/contexts/AuthContext";
import type { UserMenu as UserMenuExport } from "./UserMenu";

const auth = { user: null, logout: jest.fn() } as unknown as AuthContextValue;

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => auth,
}));

let UserMenu: typeof UserMenuExport;

const user = (over: Partial<AuthUser>) => ({ id: "usr-1", ...over }) as AuthUser;

const renderMenu = () =>
  render(
    <MemoryRouter>
      <UserMenu />
    </MemoryRouter>,
  );

describe("UserMenu", () => {
  beforeAll(async () => {
    ({ UserMenu } = await import("./UserMenu"));
  });

  beforeEach(() => {
    auth.user = null;
    jest.clearAllMocks();
  });

  it("без личности не рисует ничего", () => {
    const { container } = renderMenu();

    expect(container).toBeEmptyDOMElement();
  });

  it("подписывается отображаемым именем, когда оно есть", () => {
    auth.user = user({ display_name: "Иван Петров", email: "ivan@example.test" });

    renderMenu();

    expect(screen.getByText("Иван Петров")).toBeInTheDocument();
    // Инициалы двусоставного имени — первая буква первого и последнего слова.
    expect(screen.getByText("ИП")).toBeInTheDocument();
  });

  it("без имени подписывается почтой, а инициалы берёт из неё", () => {
    auth.user = user({ email: "ivan@example.test" });

    renderMenu();

    expect(screen.getByText("ivan@example.test")).toBeInTheDocument();
    expect(screen.getByText("IV")).toBeInTheDocument();
  });

  it("без имени и почты подписывается идентификатором", () => {
    auth.user = user({ id: "usr-42" });

    renderMenu();

    expect(screen.getByText("usr-42")).toBeInTheDocument();
    // Инициалов выводить не из чего — на их месте generic-иконка, а не "US".
    expect(screen.queryByText("US")).not.toBeInTheDocument();
  });
});
