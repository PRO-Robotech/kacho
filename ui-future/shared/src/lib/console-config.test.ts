// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { act, render, screen } from "@testing-library/react";
import React from "react";
import {
  CONFIG_DEADLINE_MS,
  consoleNotificationsState,
  loadConsoleConfig,
  retryConsoleConfig,
  subscribeConsoleConfig,
  useConsoleConfig,
} from "./console-config";
import type * as ConsoleConfigModule from "./console-config";

/**
 * Признак подключения уведомлений (приёмка NTF-6 Р2, замысел З2): одно чтение
 * `/console-config.json` на окно, четыре состояния, выход из «не прочитано»
 * только фокусом и «Повторить», дедлайн чтения `CONFIG_DEADLINE_MS` (B1, З20).
 *
 * Сеть подставляется под стражем выпуска (`test/setup.ts`): обращение уходит
 * через упорядочивающий транспорт вкладки, как всякое обращение консоли, и
 * заменитель `fetch` видит только его. Часы — управляемые (`jest.useFakeTimers`):
 * дедлайн обязан идти по тем же часам, что проба (З2 п.3), поэтому каждая проба
 * дедлайна сдвигает их ровно до границы и на шаг за неё.
 */

const KEY = Symbol.for("kacho.console-config");
const registry = globalThis as unknown as Record<symbol, unknown>;

type Reply = { status: number; body: string };
type Pending = {
  url: string;
  init: RequestInit;
  reply: (r: Reply) => void;
  fail: (e: unknown) => void;
};

/** Сеть, чьими ответами проба управляет: каждое обращение ждёт `reply`/`fail`. */
function heldNetwork(opts: { honoursAbort: boolean } = { honoursAbort: true }): Pending[] {
  const calls: Pending[] = [];
  globalThis.fetch = ((url: string, init: RequestInit = {}) =>
    new Promise<Response>((resolve, reject) => {
      const reply = (r: Reply) =>
        resolve({
          ok: r.status >= 200 && r.status < 300,
          status: r.status,
          text: () => Promise.resolve(r.body),
        } as unknown as Response);
      if (opts.honoursAbort) {
        init.signal?.addEventListener("abort", () =>
          reject(Object.assign(new Error("aborted"), { name: "AbortError" })),
        );
      }
      calls.push({ url: String(url), init, reply, fail: reject });
    })) as unknown as typeof fetch;
  return calls;
}

/** Сеть, отвечающая сразу одним и тем же ответом. */
function answeringNetwork(r: Reply | Error): Pending[] {
  const calls = heldNetwork();
  const original = globalThis.fetch;
  globalThis.fetch = ((url: string, init?: RequestInit) => {
    const p = original(url, init);
    const call = calls[calls.length - 1];
    if (r instanceof Error) call.fail(r);
    else call.reply(r);
    return p;
  }) as unknown as typeof fetch;
  return calls;
}

const settle = () => jest.advanceTimersByTimeAsync(0);

function focusWindow(): void {
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "visible" });
  document.dispatchEvent(new Event("visibilitychange"));
}

function hideWindow(): void {
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "hidden" });
  document.dispatchEvent(new Event("visibilitychange"));
}

async function otherCopy(): Promise<typeof ConsoleConfigModule> {
  let other: typeof ConsoleConfigModule | null = null;
  await jest.isolateModulesAsync(async () => {
    other = await import("./console-config");
  });
  if (other === null) throw new Error("вторая копия модуля не загрузилась");
  return other;
}

beforeEach(() => {
  jest.useFakeTimers();
});

afterEach(() => {
  delete registry[KEY];
  jest.useRealTimers();
});

describe("три исхода признака Р2 (DoD 2)", () => {
  test("ответ 200 с true — enabled; обращение одно, без кэша, с печеньем своего источника", async () => {
    const calls = answeringNetwork({ status: 200, body: '{"notificationsEnabled":true}' });
    loadConsoleConfig();
    await settle();
    expect(consoleNotificationsState()).toBe("enabled");
    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe("/console-config.json");
    expect(calls[0].init.cache).toBe("no-store");
    expect(calls[0].init.credentials).toBe("same-origin");
  });

  test("ответ 200 с false — disabled", async () => {
    answeringNetwork({ status: 200, body: '{"notificationsEnabled":false}' });
    loadConsoleConfig();
    await settle();
    expect(consoleNotificationsState()).toBe("disabled");
  });

  const unreadableForms: ReadonlyArray<readonly [string, Reply | Error]> = [
    ["ответ не 200 при верном теле", { status: 404, body: '{"notificationsEnabled":true}' }],
    ["тело не JSON (документ вместо признака)", { status: 200, body: "<!doctype html><html></html>" }],
    ["поля нет", { status: 200, body: '{"enabled":true}' }],
    ["поле не логическое", { status: 200, body: '{"notificationsEnabled":"true"}' }],
    ["тело — null", { status: 200, body: "null" }],
    ["сетевой отказ", new TypeError("Failed to fetch")],
  ];

  test.each(unreadableForms)("%s — unreadable", async (_why, reply) => {
    answeringNetwork(reply);
    loadConsoleConfig();
    await settle();
    expect(consoleNotificationsState()).toBe("unreadable");
  });
});

describe("одно окно — одно чтение и один исход (CX6-16 (а), И3)", () => {
  test("первое чтение не завершено — состояние loading (CX6-21)", async () => {
    heldNetwork();
    loadConsoleConfig();
    await settle();
    expect(consoleNotificationsState()).toBe("loading");
  });

  test("вторая копия модуля — действительно другой экземпляр (условие проб ниже)", async () => {
    const other = await otherCopy();
    expect(other.loadConsoleConfig).not.toBe(loadConsoleConfig);
  });

  test("два вызова загрузчика из двух «бандлов» — одно обращение и один исход у обоих", async () => {
    const calls = heldNetwork();
    const other = await otherCopy();
    const seenByOther: string[] = [];
    other.subscribeConsoleConfig(() => seenByOther.push(other.consoleNotificationsState()));

    loadConsoleConfig();
    other.loadConsoleConfig();
    await settle();
    expect(calls).toHaveLength(1);

    calls[0].reply({ status: 200, body: '{"notificationsEnabled":true}' });
    await settle();
    expect(consoleNotificationsState()).toBe("enabled");
    expect(other.consoleNotificationsState()).toBe("enabled");
    expect(seenByOther).toEqual(["enabled"]);
  });
});

describe("выход из unreadable — фокус и «Повторить» (CX6-16 (б))", () => {
  test("unreadable → фокус → enabled; на время повторного чтения остаётся unreadable", async () => {
    const calls = heldNetwork();
    const states: string[] = [];
    subscribeConsoleConfig(() => states.push(consoleNotificationsState()));
    loadConsoleConfig();
    await settle();
    calls[0].reply({ status: 503, body: "" });
    await settle();
    expect(consoleNotificationsState()).toBe("unreadable");

    focusWindow();
    await settle();
    expect(calls).toHaveLength(2);
    expect(consoleNotificationsState()).toBe("unreadable");

    calls[1].reply({ status: 200, body: '{"notificationsEnabled":true}' });
    await settle();
    expect(consoleNotificationsState()).toBe("enabled");
    expect(states).toEqual(["unreadable", "enabled"]);
  });

  test("«Повторить» — то же чтение", async () => {
    const calls = heldNetwork();
    loadConsoleConfig();
    await settle();
    calls[0].reply({ status: 200, body: "{}" });
    await settle();
    retryConsoleConfig();
    await settle();
    expect(calls).toHaveLength(2);
    calls[1].reply({ status: 200, body: '{"notificationsEnabled":false}' });
    await settle();
    expect(consoleNotificationsState()).toBe("disabled");
  });

  test("скрытие окна чтения не начинает", async () => {
    const calls = heldNetwork();
    loadConsoleConfig();
    await settle();
    calls[0].reply({ status: 500, body: "" });
    await settle();
    hideWindow();
    await settle();
    expect(calls).toHaveLength(1);
  });

  test("фокус и «Повторить» во время чтения в полёте схлопываются в него", async () => {
    const calls = heldNetwork();
    loadConsoleConfig();
    await settle();
    calls[0].reply({ status: 500, body: "" });
    await settle();
    focusWindow();
    retryConsoleConfig();
    focusWindow();
    await settle();
    expect(calls).toHaveLength(2);
  });

  test.each([
    ["enabled", '{"notificationsEnabled":true}'],
    ["disabled", '{"notificationsEnabled":false}'],
  ])("%s → фокус и «Повторить» — обращений нет: исход окончателен", async (state, body) => {
    const calls = heldNetwork();
    loadConsoleConfig();
    await settle();
    calls[0].reply({ status: 200, body });
    await settle();
    expect(consoleNotificationsState()).toBe(state);
    focusWindow();
    retryConsoleConfig();
    await settle();
    expect(calls).toHaveLength(1);
    expect(consoleNotificationsState()).toBe(state);
  });

  test("повторный вызов загрузчика после исхода нового чтения не начинает", async () => {
    const calls = answeringNetwork({ status: 500, body: "" });
    loadConsoleConfig();
    await settle();
    loadConsoleConfig();
    await settle();
    expect(calls).toHaveLength(1);
    expect(consoleNotificationsState()).toBe("unreadable");
  });
});

describe("дедлайн чтения CONFIG_DEADLINE_MS (B1, CX6-26, И18)", () => {
  test("чтение не отвечает → по CONFIG_DEADLINE_MS unreadable, фокус начинает новое чтение (запросов 2)", async () => {
    const calls = heldNetwork();
    loadConsoleConfig();
    await settle();
    await jest.advanceTimersByTimeAsync(CONFIG_DEADLINE_MS - 1);
    expect(consoleNotificationsState()).toBe("loading");
    // До дедлайна фокус и «Повторить» нового чтения не начинают: из loading выхода нет.
    focusWindow();
    retryConsoleConfig();
    await settle();
    expect(calls).toHaveLength(1);

    await jest.advanceTimersByTimeAsync(1);
    expect(consoleNotificationsState()).toBe("unreadable");
    expect(calls[0].init.signal?.aborted).toBe(true);

    focusWindow();
    await settle();
    expect(calls).toHaveLength(2);
  });

  test("fetch не отзывается на отмену — по CONFIG_DEADLINE_MS всё равно unreadable", async () => {
    const calls = heldNetwork({ honoursAbort: false });
    loadConsoleConfig();
    await settle();
    await jest.advanceTimersByTimeAsync(CONFIG_DEADLINE_MS);
    expect(consoleNotificationsState()).toBe("unreadable");
    expect(calls).toHaveLength(1);
  });

  test("ответ enabled после дедлайна состояние не меняет, и слушатели его не слышат", async () => {
    const calls = heldNetwork({ honoursAbort: false });
    const states: string[] = [];
    subscribeConsoleConfig(() => states.push(consoleNotificationsState()));
    loadConsoleConfig();
    await settle();
    await jest.advanceTimersByTimeAsync(CONFIG_DEADLINE_MS);
    calls[0].reply({ status: 200, body: '{"notificationsEnabled":true}' });
    await settle();
    expect(consoleNotificationsState()).toBe("unreadable");
    expect(states).toEqual(["unreadable"]);
  });

  test("отказ по отмене после дедлайна ничего не применяет повторно", async () => {
    heldNetwork({ honoursAbort: true });
    const states: string[] = [];
    subscribeConsoleConfig(() => states.push(consoleNotificationsState()));
    loadConsoleConfig();
    await settle();
    await jest.advanceTimersByTimeAsync(CONFIG_DEADLINE_MS);
    await settle();
    expect(states).toEqual(["unreadable"]);
  });

  test("повторное чтение не отвечает — по дедлайну следующий фокус начинает новое (запросов 3)", async () => {
    const calls = heldNetwork();
    loadConsoleConfig();
    await settle();
    calls[0].reply({ status: 500, body: "" });
    await settle();
    focusWindow();
    await settle();
    expect(calls).toHaveLength(2);
    focusWindow();
    await settle();
    expect(calls).toHaveLength(2);
    await jest.advanceTimersByTimeAsync(CONFIG_DEADLINE_MS);
    expect(consoleNotificationsState()).toBe("unreadable");
    focusWindow();
    await settle();
    expect(calls).toHaveLength(3);
  });

  test("исход до дедлайна снимает таймер: сдвиг часов за дедлайн состояние не меняет", async () => {
    const calls = heldNetwork();
    loadConsoleConfig();
    await settle();
    calls[0].reply({ status: 200, body: '{"notificationsEnabled":true}' });
    await settle();
    expect(jest.getTimerCount()).toBe(0);
    await jest.advanceTimersByTimeAsync(CONFIG_DEADLINE_MS * 2);
    expect(consoleNotificationsState()).toBe("enabled");
  });
});

describe("useConsoleConfig — подписка компонента на состояние окна", () => {
  function Probe() {
    return React.createElement("span", null, `признак: ${useConsoleConfig()}`);
  }

  test("компонент видит loading, затем исход, сделанный чтением", async () => {
    const calls = heldNetwork();
    render(React.createElement(Probe));
    await act(settle);
    expect(screen.getByText("признак: loading")).toBeInTheDocument();
    await act(async () => {
      calls[0].reply({ status: 200, body: '{"notificationsEnabled":false}' });
      await settle();
    });
    expect(screen.getByText("признак: disabled")).toBeInTheDocument();
    expect(calls).toHaveLength(1);
  });
});
