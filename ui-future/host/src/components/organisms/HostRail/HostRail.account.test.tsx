import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { LaneRefusal, type SessionAnswer, type SessionIdentity } from "@shared/api/login-lane";
import { installLane, refusal } from "@shared/test/lane-fake";
import { HostRail } from ".";

// Учётная запись в каркасе (приёмка F8, F8-17…F8-19): вход — экраном консоли,
// выход — глаголом службы на месте; ни то, ни другое не уходит к чужому
// поставщику.

const WHO: SessionIdentity = {
  kind: "present",
  user: { id: "usr-1", email: "a@kacho.local", displayName: "a", subjectType: "user", permissions: [] },
  session: { expiresAt: "2026-09-24T00:00:00Z", assuranceLevel: "1", emailVerified: false },
};

const UNKNOWN_SESSION: SessionAnswer = {
  kind: "unknown",
  refusal: new LaneRefusal(503, 14, "unavailable", null, null, null),
};

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => {
  lane?.restore();
  jest.restoreAllMocks();
  // Подтверждённый выход держит метку вкладки до ухода документа, и второй
  // выход при ней не начинается (`tab-exit.ts`). Здесь документ остаётся, и
  // следующая проба начинает вкладку заново.
  delete (globalThis as unknown as Record<symbol, unknown>)[Symbol.for("kacho.console.tab-exit")];
});

describe("учётная запись в рейле", () => {
  it("без сессии — «Войти», и ведёт она на экран входа консоли с адресом возврата", () => {
    render(<HostRail showReachability={false} identity={{ kind: "absent" }} currentPath="/iam/users" />);
    expect(screen.getByRole("button", { name: "Войти" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Учётная запись" })).toBeNull();
  });

  it("пока край не ответил, рейл не обещает ни входа, ни учётной записи", () => {
    render(<HostRail showReachability={false} identity={undefined} />);
    expect(screen.queryByRole("button", { name: "Войти" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Учётная запись" })).toBeNull();
  });

  it("C6 · край не ответил о сессии — рейл не говорит «вы вышли»: ни «Войти», ни учётной записи", () => {
    render(<HostRail showReachability={false} identity={UNKNOWN_SESSION} />);
    expect(screen.queryByRole("button", { name: "Войти" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Учётная запись" })).toBeNull();
  });

  it("C8 · поля подтверждённости в ответе нет — признака нет, «не подтверждён» не выдумывается", () => {
    const withoutFlag: SessionIdentity = { ...WHO, session: { expiresAt: "t", assuranceLevel: "1" } };
    render(<HostRail showReachability={false} identity={withoutFlag} />);
    fireEvent.click(screen.getByRole("button", { name: "Учётная запись" }));
    const panel = screen.getByRole("dialog", { name: "Учётная запись" });
    expect(panel).toHaveTextContent("a@kacho.local");
    expect(panel).not.toHaveTextContent("Адрес не подтверждён");
    expect(panel).not.toHaveTextContent("Адрес подтверждён");
  });

  it("F8-17 · учётная запись: адрес и признак подтверждённости, действия «подтвердить» нет", () => {
    render(<HostRail showReachability={false} identity={WHO} />);
    fireEvent.click(screen.getByRole("button", { name: "Учётная запись" }));
    const panel = screen.getByRole("dialog", { name: "Учётная запись" });
    expect(panel).toHaveTextContent("a@kacho.local");
    expect(panel).toHaveTextContent("Адрес не подтверждён");
    expect(within(panel).queryByRole("button", { name: /подтвердить/i })).toBeNull();
  });

  it("второе нажатие на пункт рейла закрывает панель, а третье открывает снова", () => {
    render(<HostRail showReachability={false} identity={WHO} />);
    const item = screen.getByRole("button", { name: "Учётная запись" });
    // Нажатие — как у человека: сначала «кнопка мыши опущена», потом щелчок.
    const press = () => {
      fireEvent.mouseDown(item);
      fireEvent.click(item);
    };
    press();
    expect(screen.getByRole("dialog", { name: "Учётная запись" })).toBeInTheDocument();
    press();
    expect(screen.queryByRole("dialog", { name: "Учётная запись" })).toBeNull();
    press();
    expect(screen.getByRole("dialog", { name: "Учётная запись" })).toBeInTheDocument();
  });

  it("нажатие вне панели и вне пункта рейла закрывает панель", () => {
    render(<HostRail showReachability={false} identity={WHO} />);
    fireEvent.click(screen.getByRole("button", { name: "Учётная запись" }));
    expect(screen.getByRole("dialog", { name: "Учётная запись" })).toBeInTheDocument();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole("dialog", { name: "Учётная запись" })).toBeNull();
  });

  it("F8-18 · «Выйти» зовёт глагол выхода с признаком своего вида и уводит на экран входа", async () => {
    lane = installLane({ "POST /iam/v1/auth/logout": { status: 200, body: {} } });
    const leave = jest.fn<(to: string) => void>();
    const { AccountPanel } = await import("../AccountPanel");
    render(<AccountPanel identity={WHO} onClose={() => undefined} navigate={() => undefined} leave={leave} />);
    fireEvent.click(screen.getByRole("button", { name: "Выйти" }));
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/login"));
    expect(lane.of("POST", "/iam/v1/auth/logout")[0].body).toEqual({ csrfToken: "tok-logout-1" });
    expect(lane.calls.every((c) => c.path.startsWith("/iam/v1/auth/"))).toBe(true);
  });

  it("F8-19 · служба не подтвердила выход: текст отказа на месте, ухода нет", async () => {
    lane = installLane({ "POST /iam/v1/auth/logout": refusal(503, 14, "logout not performed; try again later") });
    const leave = jest.fn<(to: string) => void>();
    const { AccountPanel } = await import("../AccountPanel");
    render(<AccountPanel identity={WHO} onClose={() => undefined} navigate={() => undefined} leave={leave} />);
    fireEvent.click(screen.getByRole("button", { name: "Выйти" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("logout not performed; try again later");
    expect(leave).not.toHaveBeenCalled();
  });

  it("C13 · C14 · после ПОДТВЕРЖДЁННОГО выхода чужие аккаунт и проект с именами сняты, тема осталась", async () => {
    window.localStorage.setItem("kacho.context.v2", JSON.stringify({ account: { id: "acc-1", name: "Чужой" } }));
    window.localStorage.setItem("kacho-theme", "light");
    lane = installLane({ "POST /iam/v1/auth/logout": { status: 200, body: {} } });
    const leave = jest.fn<(to: string) => void>();
    const { AccountPanel } = await import("../AccountPanel");
    render(<AccountPanel identity={WHO} onClose={() => undefined} navigate={() => undefined} leave={leave} />);
    fireEvent.click(screen.getByRole("button", { name: "Выйти" }));
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/login"));
    expect(window.localStorage.getItem("kacho.context.v2")).toBeNull();
    expect(window.localStorage.getItem("kacho-theme")).toBe("light");
  });

  it("C13 · отказ выхода не снимает ничего: состояние и адрес на месте", async () => {
    const context = JSON.stringify({ account: { id: "acc-1", name: "Свой" } });
    window.localStorage.setItem("kacho.context.v2", context);
    lane = installLane({ "POST /iam/v1/auth/logout": refusal(503, 14, "logout not performed; try again later") });
    const leave = jest.fn<(to: string) => void>();
    const { AccountPanel } = await import("../AccountPanel");
    render(<AccountPanel identity={WHO} onClose={() => undefined} navigate={() => undefined} leave={leave} />);
    fireEvent.click(screen.getByRole("button", { name: "Выйти" }));
    await screen.findByRole("alert");
    expect(window.localStorage.getItem("kacho.context.v2")).toBe(context);
    expect(leave).not.toHaveBeenCalled();
  });
});
