// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { orderedTransport } from "@shared/api/carrier-order";
import { requestUrl } from "./fetch-capture";
import { PROBE_TRANSPORT_KEY, failOnIssuanceBreaches, stubNetworkOnce, takeIssuanceBreaches } from "./issuance-guard";

/**
 * F8-46 · страж исполнения мест выпуска способен упасть и способен смолчать.
 *
 * Каждая форма, которой код добирается до `fetch` окна, — из возвратов проверки
 * кругов 2–4 (ссылка, привязка, доступ по строке, окно фрейма и его документа,
 * `UIEvent.view`, `MessageEvent.source`, вычисленный ключ, отражение), —
 * ИСПОЛНЯЕТСЯ здесь в окружении проб консоли. Находка: обращение не дошло до
 * сети, отказ назван, запись за пробой есть. Близнец — то же обращение,
 * выпущенное упорядочивающим транспортом: сеть его получила, записей ноль.
 * Изменён ровно один факт: через что выпущено обращение.
 *
 * Формы, которых jsdom не исполняет (результат `window.open()`, окно объекта),
 * доказываются настоящим браузером — `e2e/scripts/issuance-guard-selftest.ts`.
 */

const PATH = "/vpc/v1/networks";

function ok(): Response {
  return {
    ok: true,
    status: 200,
    headers: { get: () => null },
    text: () => Promise.resolve("{}"),
  } as unknown as Response;
}

/** Сеть на пробу: пишет, какие адреса до неё дошли. */
function network() {
  const reached: string[] = [];
  const fn = jest.fn((input: RequestInfo | URL) => {
    reached.push(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
    return Promise.resolve(ok());
  });
  stubNetworkOnce(globalThis, fn);
  return { reached, fn };
}

type Form = { name: string; issue: () => Promise<unknown> };

function frameWindow(): Window {
  const frame = document.createElement("iframe");
  document.body.appendChild(frame);
  const w = frame.contentWindow;
  if (!w) throw new Error("jsdom не отдал окно фрейма — предпосылка пробы не выполнена");
  return w;
}

const FORMS: readonly Form[] = [
  { name: "голый fetch(…)", issue: () => fetch(PATH) },
  { name: "window.fetch(…)", issue: () => window.fetch(PATH) },
  { name: "globalThis.fetch(…)", issue: () => globalThis.fetch(PATH) },
  { name: "self.fetch(…)", issue: () => self.fetch(PATH) },
  { name: "top.fetch(…)", issue: () => (window.top as Window).fetch(PATH) },
  { name: "parent.fetch(…)", issue: () => window.parent.fetch(PATH) },
  { name: "frames.fetch(…)", issue: () => window.frames.fetch(PATH) },
  { name: "document.defaultView.fetch(…)", issue: () => (document.defaultView as Window).fetch(PATH) },
  {
    name: "ссылка в переменной: const send = fetch",
    issue: () => {
      const send = fetch;
      return send(PATH);
    },
  },
  { name: "привязка: globalThis.fetch.bind(globalThis)", issue: () => globalThis.fetch.bind(globalThis)(PATH) },
  { name: "fetch.call(window, …)", issue: () => fetch.call(window, PATH) },
  { name: "Reflect.apply(fetch, …)", issue: () => Reflect.apply(fetch, window, [PATH]) },
  { name: 'ключ строкой: window["fetch"]', issue: () => window["fetch"](PATH) },
  {
    name: "вычисленный ключ: window[k]",
    issue: () => {
      const k = ["fe", "tch"].join("") as "fetch";
      return window[k](PATH);
    },
  },
  {
    name: 'отражение: Reflect.get(window, "fetch")',
    issue: () => Reflect.get(window, "fetch")(PATH),
  },
  {
    name: "разбор: const { fetch: f } = window",
    issue: () => {
      // Разбор метода окна — сама форма, которую проба исполняет.
      // eslint-disable-next-line @typescript-eslint/unbound-method -- предмет пробы: ссылка на fetch, взятая разбором
      const { fetch: f } = window;
      return f(PATH);
    },
  },
  {
    name: "псевдоним глобального объекта с приведением",
    issue: () => {
      const g = globalThis as unknown as { fetch: typeof fetch };
      return g.fetch(PATH);
    },
  },
  { name: "окно фрейма: frame.contentWindow.fetch", issue: () => frameWindow().fetch(PATH) },
  {
    name: "псевдоним окна фрейма",
    issue: () => {
      const w = frameWindow();
      const alias = w;
      return alias.fetch(PATH);
    },
  },
  {
    name: "документ фрейма: frame.contentDocument.defaultView.fetch",
    issue: () => {
      const frame = document.createElement("iframe");
      document.body.appendChild(frame);
      const view = frame.contentDocument?.defaultView;
      if (!view) throw new Error("jsdom не отдал документ фрейма — предпосылка пробы не выполнена");
      return view.fetch(PATH);
    },
  },
  { name: "UIEvent.view", issue: () => (new UIEvent("focus", { view: window }).view as Window).fetch(PATH) },
  {
    name: "UIEvent.view окна фрейма",
    issue: () => (new UIEvent("focus", { view: frameWindow() }).view as Window).fetch(PATH),
  },
  {
    name: "MessageEvent.source",
    issue: () => (new MessageEvent("message", { source: window }).source as Window).fetch(PATH),
  },
  {
    name: "вычисленный ключ на UIEvent.view (N3)",
    issue: () => {
      const view = new UIEvent("focus", { view: window }).view as unknown as Record<string, typeof fetch>;
      return view[["fe", "tch"].join("")](PATH);
    },
  },
];

afterEach(() => {
  document.body.replaceChildren();
});

describe("F8-46 · страж исполнения: обращение мимо упорядочивающего транспорта не доходит до сети и записано", () => {
  for (const { name, issue } of FORMS) {
    it(`F8-46 · ${name} — отказ, сеть не получила, находка записана`, async () => {
      const net = network();
      await expect(issue()).rejects.toThrow(
        /\[F8-46\] GET \/vpc\/v1\/networks — выпуск мимо упорядочивающего транспорта/,
      );
      expect(net.reached).toEqual([]);
      const breaches = takeIssuanceBreaches();
      expect(breaches).toHaveLength(1);
      expect(breaches[0]).toMatch(/^GET \/vpc\/v1\/networks — выпуск мимо упорядочивающего транспорта · вызвал: /);
    });
  }

  it("F8-46 · проглоченный отказ всё равно роняет пробу: запись остаётся за ней, отказ называет адрес", async () => {
    network();
    await fetch("/iam/v1/me").catch(() => undefined);
    expect(() => failOnIssuanceBreaches()).toThrow(
      /обращений мимо упорядочивающего транспорта: 1[\s\S]*GET \/iam\/v1\/me/,
    );
    // Забрано — следующая проверка судит пустую запись.
    expect(() => failOnIssuanceBreaches()).not.toThrow();
  });

  it("F8-46 · метод и адрес называются и у объекта запроса", async () => {
    network();
    const request = { url: "http://console.test/iam/v1/auth/password", method: "post" } as unknown as Request;
    await expect(fetch(request)).rejects.toThrow(/POST http:\/\/console\.test\/iam\/v1\/auth\/password/);
    expect(takeIssuanceBreaches()).toHaveLength(1);
  });
});

describe("F8-46 · близнецы: законный выпуск страж пропускает", () => {
  it("F8-46 · чтение упорядочивающим транспортом доходит до сети, находок ноль", async () => {
    const net = network();
    await expect(orderedTransport.fetch(PATH)).resolves.toBeDefined();
    expect(net.reached).toEqual([PATH]);
    expect(takeIssuanceBreaches()).toEqual([]);
  });

  it("F8-46 · мутация и глагол, ставящий носитель, — доходят до сети, находок ноль", async () => {
    const net = network();
    await orderedTransport.fetch("/vpc/v1/subnets", { method: "POST", body: "{}" });
    await orderedTransport.fetch("/iam/v1/auth/password", { method: "POST", body: "{}" }, { setsCarrier: true });
    expect(net.reached).toEqual(["/vpc/v1/subnets", "/iam/v1/auth/password"]);
    expect(takeIssuanceBreaches()).toEqual([]);
  });

  it("F8-46 · собственный транспорт пробы — не консоль: доходит до сети, находок ноль", async () => {
    const net = network();
    const probeFetch = (globalThis as unknown as Record<symbol, typeof fetch>)[Symbol.for(PROBE_TRANSPORT_KEY)];
    await probeFetch(PATH);
    expect(net.reached).toEqual([PATH]);
    expect(takeIssuanceBreaches()).toEqual([]);
  });
});

describe("F8-46 · стража нельзя снять пробой, а сеть под ним подставляется как прежде", () => {
  it("F8-46 · свойство fetch неперенастраиваемо: spyOn и defineProperty отказывают", () => {
    expect(Object.getOwnPropertyDescriptor(globalThis, "fetch")?.configurable).toBe(false);
    expect(() => jest.spyOn(globalThis, "fetch")).toThrow();
    expect(() => Object.defineProperty(globalThis, "fetch", { value: () => undefined })).toThrow(TypeError);
  });

  it("F8-46 · ссылка, взятая до подстановки, возвращает присвоением ту сеть, что была тогда", async () => {
    const first = network();
    const prev = globalThis.fetch;
    const second = jest.fn(() => Promise.resolve(ok()));
    globalThis.fetch = second;
    await orderedTransport.fetch("/vpc/v1/a");
    globalThis.fetch = prev;
    await orderedTransport.fetch("/vpc/v1/b");
    expect(second).toHaveBeenCalledTimes(1);
    expect(first.reached).toEqual(["/vpc/v1/b"]);
    expect(takeIssuanceBreaches()).toEqual([]);
  });

  it("F8-46 · заменитель, зовущий взятую ссылку, зовёт прежнюю сеть, а не себя; мимо упорядочения — всё равно находка", async () => {
    const inner = network();
    const wrapped: string[] = [];
    const prev = globalThis.fetch;
    globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
      wrapped.push(requestUrl(input));
      return prev(input, init);
    };
    try {
      await orderedTransport.fetch(PATH);
      expect(wrapped).toEqual([PATH]);
      expect(inner.reached).toEqual([PATH]);
      expect(takeIssuanceBreaches()).toEqual([]);
      await expect(prev("/iam/v1/me")).rejects.toThrow(/\[F8-46\] GET \/iam\/v1\/me/);
      expect(inner.reached).toEqual([PATH]);
      expect(takeIssuanceBreaches()).toHaveLength(1);
    } finally {
      globalThis.fetch = prev;
    }
  });

  it("F8-46 · присвоение подставляет сеть ПОД стражем: мимо упорядочения заменитель не зовётся", async () => {
    const stub = jest.fn(() => Promise.resolve(ok()));
    const before = globalThis.fetch;
    globalThis.fetch = stub;
    try {
      await expect(globalThis.fetch(PATH)).rejects.toThrow(/\[F8-46\]/);
      expect(stub).not.toHaveBeenCalled();
      await orderedTransport.fetch(PATH);
      expect(stub).toHaveBeenCalledTimes(1);
      // Член стража — член сети под ним: суждения о заменителе прежние.
      expect(globalThis.fetch).toHaveBeenCalledTimes(1);
      expect(jest.mocked(globalThis.fetch).mock.calls[0]?.[0]).toBe(PATH);
    } finally {
      globalThis.fetch = before;
    }
    expect(takeIssuanceBreaches()).toHaveLength(1);
  });
});
