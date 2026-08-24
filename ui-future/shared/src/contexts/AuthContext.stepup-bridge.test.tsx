// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ШОВ между окном подтверждения и клиентом API обязан быть СОМКНУТ (#1213).
//
// ПРЕДМЕТ. Окно регистрирует свой обработчик в контексте личности; отказ края
// «поднимите уровень» приходит в клиент API. Если контекст обработчик только
// ЗАПОМИНАЕТ, шов разомкнут: обе стороны исправны по отдельности, каждая покрыта
// своими пробами, а окно не открывается никогда — и уровень из консоли поднять
// нечем. Ровно так и было: у записи не было ни одного читателя во всём дереве.
//
// Проверяется поэтому не «ссылка выставлена», а СКВОЗНОЕ свойство: попросил
// клиент — дошло до обработчика окна.

import React from "react";
import { jest } from "@jest/globals";
import { act, render } from "@testing-library/react";
import { requestStepUp } from "@shared/api/step-up";

jest.unstable_mockModule("@shared/api/auth", () => ({
  authApi: {
    me: jest.fn(() => Promise.resolve({ user: null })),
    whoami: jest.fn(() => Promise.resolve(null)),
    logout: jest.fn(() => Promise.resolve()),
  },
  hasPermission: () => false,
}));

jest.unstable_mockModule("@shared/lib/kratos", () => ({
  kratos: {
    whoami: jest.fn(() => Promise.resolve(null)),
    initLogout: jest.fn(() => Promise.resolve({ logout_token: "", logout_url: "" })),
    submitLogout: jest.fn(() => Promise.resolve()),
    loginUrl: () => "#idp",
  },
}));

const { AuthProvider, useAuth } = await import("./AuthContext");

/** Дитя, которое ведёт себя как окно подтверждения: подписывается и отписывается. */
function FakeWindow({ handler }: { handler: (acr?: string) => Promise<void> }) {
  const { setStepUpHandler } = useAuth();
  React.useEffect(() => {
    setStepUpHandler(handler);
    return () => setStepUpHandler(null);
  }, [setStepUpHandler, handler]);
  return null;
}

describe("шов «отказ по полу → окно подтверждения»", () => {
  it("просьба клиента доходит до смонтированного окна", async () => {
    const asked: Array<string | undefined> = [];
    const handler = (acr?: string) => {
      asked.push(acr);
      return Promise.resolve();
    };

    render(
      <AuthProvider>
        <FakeWindow handler={handler} />
      </AuthProvider>,
    );

    let ok = false;
    await act(async () => {
      ok = await requestStepUp("2");
    });

    expect(ok).toBe(true);
    expect(asked).toEqual(["2"]);
  });

  it("окна нет — просьба отвечает ОТКАЗОМ, а не молчаливым согласием", async () => {
    const { unmount } = render(
      <AuthProvider>
        <FakeWindow handler={() => Promise.resolve()} />
      </AuthProvider>,
    );
    unmount();

    let ok = true;
    await act(async () => {
      ok = await requestStepUp("2");
    });

    // Fail-closed: без окна подтверждать нечем, и повторять запрос нельзя.
    expect(ok).toBe(false);
  });
});
