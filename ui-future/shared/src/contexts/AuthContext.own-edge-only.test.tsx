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
import { render, screen } from "@testing-library/react";

const { AuthProvider, useAuth } = await import("./AuthContext");

/** Признак адреса ЧУЖОЙ службы личности: её публичный край и её потоки. */
const FOREIGN_IDENTITY = [/\/\.ory\//, /\/self-service\//, /\/sessions\/whoami/];

function isForeign(url: string): boolean {
  return FOREIGN_IDENTITY.some((re) => re.test(url));
}

/** Метка конца подъёма: провайдер снимает `loading`, разобрав ответы ВСЕХ ручек подъёма. */
function MountDone() {
  const { loading } = useAuth();
  return loading ? null : <div data-testid="shell-mount-done" />;
}

/**
 * Поднять оболочку и дождаться КОНЦА подъёма, а не одного оборота очереди.
 * Множество запрошенных адресов судится только после того, как провайдер разобрал
 * все ответы: иначе запрос, ушедший позже первого оборота, не попал бы в обход.
 */
async function mountShell(): Promise<void> {
  render(
    <AuthProvider>
      <MountDone />
    </AuthProvider>,
  );
  await screen.findByTestId("shell-mount-done");
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
      // ветку не глушит: провайдер разбирает отказ каждой из своих ручек.
      //
      // Ответ собран вручную: у jsdom нет глобального `Response`, и `new Response`
      // здесь бросал `ReferenceError` — клиент глотал его как отказ сети, и 401
      // не наступал вовсе. Заменитель отдаёт всё, что читает клиент края.
      return Promise.resolve({
        ok: false,
        status: 401,
        statusText: "Unauthorized",
        headers: { get: () => null },
        text: () => Promise.resolve(""),
      });
    }) as unknown as typeof globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it("не спрашивает чужую службу личности о печенье сессии", async () => {
    await mountShell();

    // Находка печатается САМИМ РАЗЛИЧИЕМ: слева — адреса чужой службы, которые
    // подъём запросил, справа — пусто. Имя упавшего шага при этом не нужно
    // разбирать: в выводе стоит сам адрес.
    const foreign = asked.filter(isForeign);
    expect(foreign).toEqual([]);
  });

  it("спрашивает свой край — положительный контроль того же обхода", async () => {
    await mountShell();

    expect(asked.length).toBeGreaterThan(0);
    expect(asked.some((u) => u.includes("/iam/v1/auth/me"))).toBe(true);
    expect(asked.some((u) => u.includes("/iam/v1/me"))).toBe(true);
  });
});
