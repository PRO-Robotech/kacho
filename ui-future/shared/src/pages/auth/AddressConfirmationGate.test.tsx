// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useEffect } from "react";
import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { api } from "@shared/api/client";
import { UNKNOWN_SESSION_TEXT } from "@shared/api/login-lane";
import { SESSION, installLane, refusal, type LaneAnswer } from "@shared/test/lane-fake";

// Страж над каркасом консоли (приёмка F6b, Р7). Каркас здесь — дублёр, который
// при монтировании выпускает ТО ЖЕ чтение, что настоящий (`GET /iam/v1/accounts`,
// §1.5 приёмки): по нему, а не по разметке, проба видит, смонтирован ли каркас.
// Сквозная проба стража — `address-confirmation.spec.ts` (F6b-15).

const { AddressConfirmationGate } = await import("./AddressConfirmationGate");
const { VerificationPage } = await import("./VerificationPage");

const ME = "GET /iam/v1/auth/me";
const ACCOUNTS = "/iam/v1/accounts";
const CONFIRM = "/iam/v1/auth/verify-email/confirm";
const NOT_NAMED_TEXT = "Не удалось узнать, подтверждён ли адрес почты: служба доступа не сообщила этого в ответе о сессии.";

const USER = { id: "usr-1", email: "a@kacho.local", displayName: "a", subjectType: "user", permissions: [] };

function me(session: Record<string, unknown> | null): LaneAnswer {
  return { status: 200, body: session === null ? { user: USER } : { user: USER, session } };
}
const VERIFIED = me({ ...SESSION, emailVerified: true });
const UNVERIFIED = me({ ...SESSION, emailVerified: false });

function ShellDouble() {
  useEffect(() => {
    void api.list(ACCOUNTS).catch(() => undefined);
  }, []);
  return <div>каркас консоли</div>;
}

function WhereAmI() {
  const { pathname, search } = useLocation();
  return <output aria-label="адрес страницы">{`${pathname}${search}`}</output>;
}

function renderGateAt(url: string) {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <WhereAmI />
      <Routes>
        <Route path="/verification" element={<div>экран подтверждения</div>} />
        <Route path="/login" element={<div>экран входа</div>} />
        <Route
          path="*"
          element={
            <AddressConfirmationGate>
              <ShellDouble />
            </AddressConfirmationGate>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

const address = () => screen.getByRole("status", { name: "адрес страницы" }).textContent;

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

/** Обращения к API сверх перечня Р6 — перепись дублёра, с числом осмотренных. */
function beyondAllowed(calls: ReturnType<typeof installLane>["calls"]): string[] {
  const allowed = new Set(["GET /iam/v1/auth/me", "GET /iam/v1/auth/csrf", "POST /iam/v1/auth/logout"]);
  expect(calls.length).toBeGreaterThan(0);
  return calls.map((c) => `${c.method} ${c.path}`).filter((k) => !allowed.has(k));
}

describe("страж над каркасом (Р7)", () => {
  it("F6b-15 · неподтверждённая сессия: каркас не монтируется, адрес — экран подтверждения с адресом возврата", async () => {
    lane = installLane({ [ME]: UNVERIFIED });
    renderGateAt("/projects/prj-1/vpc/networks?x=1");
    expect(await screen.findByText("экран подтверждения")).toBeInTheDocument();
    expect(address()).toBe("/verification?returnTo=%2Fprojects%2Fprj-1%2Fvpc%2Fnetworks%3Fx%3D1");
    expect(screen.queryByText("каркас консоли")).toBeNull();
    expect(beyondAllowed(lane.calls)).toEqual([]);
  });

  it("F6b-15 · корень консоли уходит на экран подтверждения без адреса возврата", async () => {
    lane = installLane({ [ME]: UNVERIFIED });
    renderGateAt("/");
    expect(await screen.findByText("экран подтверждения")).toBeInTheDocument();
    expect(address()).toBe("/verification");
  });

  it("F6b-16 · близнец: подтверждённая сессия получает каркас и его чтения", async () => {
    lane = installLane({ [ME]: VERIFIED, [`GET ${ACCOUNTS}`]: { status: 200, body: { accounts: [] } } });
    renderGateAt("/dashboard");
    expect(await screen.findByText("каркас консоли")).toBeInTheDocument();
    await waitFor(() => expect(lane!.of("GET", ACCOUNTS)).toHaveLength(1));
    expect(address()).toBe("/dashboard");
  });

  it("Р7 · сессии нет — как до этой под-фазы: каркас анонимного вызова", async () => {
    lane = installLane({ [ME]: { status: 200, body: { user: null } }, [`GET ${ACCOUNTS}`]: { status: 200, body: {} } });
    renderGateAt("/dashboard");
    expect(await screen.findByText("каркас консоли")).toBeInTheDocument();
  });

  const UNKNOWN: Array<[string, LaneAnswer, string]> = [
    ["а — человек без объекта session", me(null), NOT_NAMED_TEXT],
    ["б — session без поля emailVerified", me({ expiresAt: SESSION.expiresAt, assuranceLevel: "1" }), NOT_NAMED_TEXT],
    ["в — ответ не 2xx", refusal(503, 14, "unavailable"), UNKNOWN_SESSION_TEXT],
  ];
  for (const [label, answer, text] of UNKNOWN) {
    it(`F6b-31 · ${label}: названная страница, «Проверить снова» и «Выйти», каркаса и входа нет`, async () => {
      lane = installLane({ [ME]: answer });
      renderGateAt("/dashboard");
      expect(await screen.findByText(text)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Проверить снова" })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Выйти" })).toBeInTheDocument();
      expect(screen.queryByText("каркас консоли")).toBeNull();
      expect(screen.queryByText("экран входа")).toBeNull();
      expect(address()).toBe("/dashboard");
      expect(beyondAllowed(lane.calls)).toEqual([]);
      expect(document.body.textContent).not.toMatch(/вы вышли/i);
    });
  }

  it("F6b-31 · «Проверить снова» спрашивает край заново и открывает каркас на «подтверждён»", async () => {
    lane = installLane({
      [ME]: (_c, nth) => (nth === 1 ? refusal(503, 14, "unavailable") : VERIFIED),
      [`GET ${ACCOUNTS}`]: { status: 200, body: { accounts: [] } },
    });
    renderGateAt("/dashboard");
    fireEvent.click(await screen.findByRole("button", { name: "Проверить снова" }));
    expect(await screen.findByText("каркас консоли")).toBeInTheDocument();
    expect(lane.of("GET", "/iam/v1/auth/me")).toHaveLength(2);
  });

  it("Р7 · на одну загрузку документа страж спрашивает край о сессии один раз", async () => {
    lane = installLane({ [ME]: VERIFIED, [`GET ${ACCOUNTS}`]: { status: 200, body: {} } });
    renderGateAt("/dashboard");
    expect(await screen.findByText("каркас консоли")).toBeInTheDocument();
    await waitFor(() => expect(lane!.of("GET", ACCOUNTS)).toHaveLength(1));
    expect(lane.of("GET", "/iam/v1/auth/me")).toHaveLength(1);
  });
});

describe("F6b-37 · после подтверждения край не ответил о сессии", () => {
  it("F6b-37 · страница «неизвестно» на адресе возврата, второго предъявления нет; «Проверить снова» — каркас", async () => {
    lane = installLane({
      [ME]: (_c, nth) => (nth === 1 ? UNVERIFIED : nth === 2 ? refusal(503, 14, "unavailable") : VERIFIED),
      [`POST ${CONFIRM}`]: { status: 200, body: { user: USER, session: { ...SESSION, emailVerified: true } } },
      [`GET ${ACCOUNTS}`]: { status: 200, body: { accounts: [] } },
    });
    // Экран подтверждения уходит ДОКУМЕНТОМ: дальше решает страж новой загрузки.
    const leave = jest.fn<(to: string) => void>();
    const first = render(
      <MemoryRouter initialEntries={["/verification?returnTo=%2Fdashboard"]}>
        <VerificationPage leave={leave} />
      </MemoryRouter>,
    );
    await screen.findByRole("heading", { name: "Подтвердите адрес почты" });
    fireEvent.change(screen.getByLabelText("Код из письма"), { target: { value: "ABCDEFGH23" } });
    fireEvent.click(screen.getByRole("button", { name: "Подтвердить" }));
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    first.unmount();

    renderGateAt(leave.mock.calls[0][0]);
    expect(await screen.findByText(UNKNOWN_SESSION_TEXT)).toBeInTheDocument();
    expect(screen.queryByText("экран входа")).toBeNull();
    expect(lane.of("POST", CONFIRM)).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "Проверить снова" }));
    expect(await screen.findByText("каркас консоли")).toBeInTheDocument();
    expect(lane.calls.filter((c) => c.method === "POST")).toHaveLength(1);
  });

  it("F6b-37 · близнец: первый же ответ «подтверждён» — каркас без страницы «неизвестно»", async () => {
    lane = installLane({ [ME]: VERIFIED, [`GET ${ACCOUNTS}`]: { status: 200, body: {} } });
    renderGateAt("/dashboard");
    expect(await screen.findByText("каркас консоли")).toBeInTheDocument();
    expect(screen.queryByText(UNKNOWN_SESSION_TEXT)).toBeNull();
  });
});
