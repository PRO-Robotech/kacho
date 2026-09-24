// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ПОДЪЁМ ОБОЛОЧКИ СПРАШИВАЕТ ТОЛЬКО СВОЙ КРАЙ (#2733).
//
// ПРЕДМЕТ — наблюдаемое: какие запросы уходят с адреса консоли, когда оболочка
// поднимается. Утверждение не о разметке и не о том, «какой модуль импортирован»:
// перехватывается САМ вызов сети, и судится множество адресов, которое он даёт.
//
// Почему это не косметика. Подъём оболочки спрашивал печенье сессии У ЧУЖОЙ
// СЛУЖБЫ ЛИЧНОСТИ (`/.ory/kratos/public/sessions/whoami`) — и ответ не читал
// НИКТО: поле `session` контекста не разбиралось ни одним потребителем во всём
// `ui-future/**` (предикат: деструктуризация `session` из `useAuth` — ноль
// совпадений). То есть на каждый подъём страницы уходил запрос к службе, которой
// у продукта нет в посадке `own`, и его исход не влиял ни на что видимое.
//
// Обратная сторона утверждения — положительный контроль: свой край оболочка
// спрашивать ОБЯЗАНА. Без него «ноль чужих адресов» была бы зелена и на
// оболочке, которая не спрашивает вообще ничего.

import { jest } from "@jest/globals";
import { act, render } from "@testing-library/react";

const { AuthProvider } = await import("./AuthContext");

/** Признак адреса ЧУЖОЙ службы личности: её публичный край и её потоки. */
const FOREIGN_IDENTITY = [/\/\.ory\//, /\/self-service\//, /\/sessions\/whoami/];

function isForeign(url: string): boolean {
  return FOREIGN_IDENTITY.some((re) => re.test(url));
}

describe("подъём оболочки консоли", () => {
  let asked: string[];
  let realFetch: typeof globalThis.fetch;

  beforeEach(() => {
    asked = [];
    realFetch = globalThis.fetch;
    globalThis.fetch = jest.fn((input: unknown) => {
      const url = typeof input === "string" ? input : String((input as { url?: string })?.url ?? input);
      asked.push(url);
      // 401 — «не залогинен»: обычный исход подъёма без сессии, и он ни одну
      // ветку не глушит: провайдер разбирает отказ каждой из трёх ручек.
      return Promise.resolve(new Response("", { status: 401, statusText: "Unauthorized" }));
    }) as unknown as typeof globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it("не спрашивает чужую службу личности о печенье сессии", async () => {
    await act(async () => {
      render(
        <AuthProvider>
          <div />
        </AuthProvider>,
      );
    });

    // Находка печатается САМИМ РАЗЛИЧИЕМ: слева — адреса чужой службы, которые
    // подъём запросил, справа — пусто. Имя упавшего шага при этом не нужно
    // разбирать: в выводе стоит сам адрес.
    const foreign = asked.filter(isForeign);
    expect(foreign).toEqual([]);
  });

  it("спрашивает свой край — положительный контроль того же обхода", async () => {
    await act(async () => {
      render(
        <AuthProvider>
          <div />
        </AuthProvider>,
      );
    });

    expect(asked.length).toBeGreaterThan(0);
    expect(asked.some((u) => u.includes("/iam/v1/auth/me"))).toBe(true);
    expect(asked.some((u) => u.includes("/iam/v1/me"))).toBe(true);
  });
});
