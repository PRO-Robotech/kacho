// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ВОПРОС О ПРАВАХ — ТОЛЬКО ПОСЛЕ «АДРЕС ПОДТВЕРЖДЁН» (приёмка F6b, Р7).
//
// Предмет — наблюдаемое: какие запросы уходят при подъёме контекста личности.
// Прежде вопрос о сессии и `GET /iam/v1/me` уходили ВМЕСТЕ, не дожидаясь ответа
// о сессии, — и учётная запись с неподтверждённым адресом получала вопрос о
// правах, ответ на который ей не положен. Теперь `GET /iam/v1/me` уходит ровно
// тогда, когда край ответил «сессия есть, адрес подтверждён», и после этого
// ответа.

import { render, screen } from "@testing-library/react";
import { installLane, SESSION, type LaneAnswer } from "@shared/test/lane-fake";

const { AuthProvider, useAuth } = await import("./AuthContext");

function MountDone() {
  const { loading } = useAuth();
  return loading ? null : <div data-testid="auth-mount-done" />;
}

async function mount() {
  render(
    <AuthProvider>
      <MountDone />
    </AuthProvider>,
  );
  await screen.findByTestId("auth-mount-done");
}

const USER = { id: "usr-1", email: "a@kacho.local", displayName: "a", subjectType: "user", permissions: [] };
const me = (emailVerified: boolean): LaneAnswer => ({
  status: 200,
  body: { user: USER, session: { ...SESSION, emailVerified } },
});
const WHOAMI: LaneAnswer = { status: 200, body: { subject: "user:usr-1", userId: "usr-1", accounts: [] } };

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

describe("F6b · контекст личности спрашивает о правах только подтверждённую сессию", () => {
  it("неподтверждённая сессия: вопроса о правах нет", async () => {
    lane = installLane({ "GET /iam/v1/auth/me": me(false), "GET /iam/v1/me": WHOAMI });
    await mount();
    expect(lane.of("GET", "/iam/v1/auth/me")).toHaveLength(1);
    expect(lane.of("GET", "/iam/v1/me")).toEqual([]);
  });

  it("сессии нет и край не ответил: вопроса о правах нет", async () => {
    for (const answer of [
      { status: 200, body: { user: null } },
      { status: 503, body: { code: 14, message: "unavailable", details: [] } },
    ] as LaneAnswer[]) {
      lane = installLane({ "GET /iam/v1/auth/me": answer, "GET /iam/v1/me": WHOAMI });
      const { unmount } = render(
        <AuthProvider>
          <MountDone />
        </AuthProvider>,
      );
      await screen.findByTestId("auth-mount-done");
      expect([answer.status, lane.of("GET", "/iam/v1/me")]).toEqual([answer.status, []]);
      unmount();
      lane.restore();
    }
  });

  it("близнец: подтверждённая сессия — вопрос о правах уходит, и уходит ПОСЛЕ ответа о сессии", async () => {
    lane = installLane({ "GET /iam/v1/auth/me": me(true), "GET /iam/v1/me": WHOAMI });
    await mount();
    const order = lane.calls.map((c) => c.path).filter((p) => p === "/iam/v1/auth/me" || p === "/iam/v1/me");
    expect(order).toEqual(["/iam/v1/auth/me", "/iam/v1/me"]);
  });
});
