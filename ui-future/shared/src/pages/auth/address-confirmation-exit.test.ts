// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { leaveToAddressConfirmation } from "./address-confirmation-exit";

// Уход вкладки на экран подтверждения по отказу края `EMAIL_NOT_VERIFIED`
// (приёмка F6b, F6b-25): документом, с адресом возврата — страницей, чьё
// обращение получило отказ. Сквозная проба того же предмета —
// `address-confirmation.spec.ts`.

const TAB_EXIT_KEY = Symbol.for("kacho.console.tab-exit");

afterEach(() => {
  delete (globalThis as unknown as Record<symbol, unknown>)[TAB_EXIT_KEY];
});

describe("F6b-25 · уход на экран подтверждения по отказу адреса", () => {
  it("уводит документ на экран подтверждения с адресом текущей страницы", () => {
    window.history.pushState(null, "", "/projects/prj-1/vpc/networks?page=2#top");
    const go = jest.fn<(to: string) => void>();
    leaveToAddressConfirmation(go);
    expect(go.mock.calls).toEqual([["/verification?returnTo=%2Fprojects%2Fprj-1%2Fvpc%2Fnetworks%3Fpage%3D2%23top"]]);
  });

  it("с корня консоли — экран подтверждения без адреса возврата", () => {
    window.history.pushState(null, "", "/");
    const go = jest.fn<(to: string) => void>();
    leaveToAddressConfirmation(go);
    expect(go.mock.calls).toEqual([["/verification"]]);
  });

  it("на экранах церемонии вне каркаса не уводит: они и есть разрешённое до подтверждения", () => {
    for (const path of ["/verification", "/login", "/registration", "/logout"]) {
      window.history.pushState(null, "", path);
      const go = jest.fn<(to: string) => void>();
      leaveToAddressConfirmation(go);
      expect([path, go.mock.calls]).toEqual([path, []]);
    }
  });

  it("параметры учётной записи в каркасе уводятся, как любая страница каркаса", () => {
    window.history.pushState(null, "", "/settings");
    const go = jest.fn<(to: string) => void>();
    leaveToAddressConfirmation(go);
    expect(go.mock.calls).toEqual([["/verification?returnTo=%2Fsettings"]]);
  });

  it("пока вкладка уходит выходом, не уводит: переход выхода не отменяется", async () => {
    const { beginTabExit } = await import("./tab-exit");
    window.history.pushState(null, "", "/dashboard");
    expect(beginTabExit()).not.toBeNull();
    const go = jest.fn<(to: string) => void>();
    leaveToAddressConfirmation(go);
    expect(go.mock.calls).toEqual([]);
  });
});
