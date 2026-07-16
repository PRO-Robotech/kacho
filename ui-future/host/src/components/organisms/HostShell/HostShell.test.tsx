import { render, screen } from "@testing-library/react";
import { jest } from "@jest/globals";
import { BrowserRouter } from "react-router-dom";
import { HostShell } from ".";

const jsonResponse = (body: unknown) => {
  return Promise.resolve({
    ok: true,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
};

describe("HostShell", () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.history.pushState(null, "", "/");
    jest.spyOn(global, "fetch").mockImplementation(() => jsonResponse({ accounts: [] }));
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("renders rail, breadcrumb, theme action, and content", () => {
    render(
      <BrowserRouter>
        <HostShell dark={false} setDark={jest.fn()} showReachability={false}>
          <div>Shell content</div>
        </HostShell>
      </BrowserRouter>,
    );

    expect(screen.getByRole("button", { name: "Все сервисы" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /выберите аккаунт/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Включить тёмную тему" })).toBeInTheDocument();
    expect(screen.getByText("Shell content")).toBeInTheDocument();
  });

  it("clears persisted context on root before first render", () => {
    window.localStorage.setItem(
      "kacho.context.v2",
      JSON.stringify({
        account: { id: "acc-1", name: "Account" },
        project: { id: "project-1", name: "Project", accountId: "acc-1" },
      }),
    );

    render(
      <BrowserRouter>
        <HostShell dark={false} setDark={jest.fn()} showReachability={false}>
          <div>Shell content</div>
        </HostShell>
      </BrowserRouter>,
    );

    expect(screen.getByRole("button", { name: /выберите аккаунт/i })).toBeInTheDocument();
    expect(window.localStorage.getItem("kacho.context.v2")).toBe(JSON.stringify({ account: null, project: null }));
  });
});
