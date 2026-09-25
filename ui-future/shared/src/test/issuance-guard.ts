// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Страж ИСПОЛНЕНИЯ мест выпуска — держатель того, что каждое обращение консоли
 * к сети идёт через упорядочивающий транспорт (приёмка F8, Р10, F8-46).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ ИСПОЛНЕНИЕ, А НЕ ПЕРЕПИСЬ ТЕКСТА
 *
 * Перепись мест выпуска (`issuance-census.ts`) распознаёт формы, которыми код
 * добирается до транспорта браузера. Форм у языка больше, чем у любого
 * распознавателя: четыре круга проверки подряд находили новую — ссылку в
 * переменной, привязку, доступ по строке, окно фрейма, результат `open()`,
 * `UIEvent.view`, `MessageEvent.source`, — и каждая была законным кодом, который
 * перепись пропускала. Полноты у распознавания текста не бывает.
 *
 * У исполнения она есть в своих границах: какой бы путь ни привёл к `fetch`,
 * вызов проходит через одно место — функцию, которую окно отдаёт под этим
 * именем. Страж стоит там. Законный выпуск отличается от незаконного не формой
 * записи, а тем, КТО выпускает: упорядочивающий транспорт на время своего
 * синхронного вызова отмечает выпуск в общем состоянии вкладки
 * (`issuing` под ключом `kacho.console.carrier-order`, см. `api/carrier-order.ts`).
 * Вызов без отметки — находка: страж записывает её и отказывает обращению, не
 * пропуская его в сеть.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ГДЕ СТОИТ
 *
 *   • jest — общее окружение проб всех модулей консоли (`test/setup.ts`):
 *     находка, записанная за пробой, роняет её в `afterEach`, даже если код
 *     продукта проглотил отказ;
 *   • playwright — каждый контекст браузера сквозных проб
 *     (`e2e/specs/issuance-guard.ts`): страж ставится сценарием в КАЖДЫЙ кадр
 *     и каждое окно контекста, находка роняет пробу при её разборе.
 *
 * Та же функция служит обоим: браузеру она передаётся текстом, поэтому не
 * ссылается ни на что вне своего тела.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ПОКРЫТО И ГДЕ ГРАНИЦА
 *
 * Покрыт `fetch` окна под любым именем и любой формой доступа: `fetch`,
 * `window`/`globalThis`/`self`/`top`/`parent`/`frames`, `document.defaultView`,
 * ссылка, привязка, `call`/`apply`, ключ строкой и вычисленный, отражение,
 * разбор. Окна, которые отдаёт значением API браузера, — `contentWindow` и
 * `contentDocument` фрейма и объекта, результат `open()` и
 * `document.open(url, имя, свойства)`, `UIEvent.view`, `MessageEvent.source`, —
 * получают стража при первом же обращении к ним; в браузере, сверх того, —
 * сценарием контекста при создании кадра.
 *
 * Не покрыто — и сказано, а не подразумевается:
 *   • ИСПОЛНЕННОЕ, а не написанное: путь, который ни одна проба не исполнила,
 *     страж не судит. Место, до которого исполнение не дошло, держит правило
 *     линта (`ui-future/shared/issuance-ordering.eslint.config.js`) и быстрая
 *     подсказка переписи;
 *   • транспорты, кроме `fetch` (`EventSource`, `XMLHttpRequest`, `WebSocket`,
 *     `sendBeacon`), и переход документа — их держат правило линта и перепись;
 *   • рабочие потоки (`Worker`): сценарий контекста в них не ставится;
 *   • код, исполняемый ВНУТРИ синхронного отрезка выпуска (геттер аргумента,
 *     вызванный самим `fetch`), — отрезок принадлежит транспорту.
 */

/** Окно, которому ставится страж: глобальный объект jsdom или браузера. */
export type GuardScope = typeof globalThis;

/** Куда страж отдаёт находку: текст — метод, адрес и стек вызвавшего. */
export type BreachSink = (breach: string) => void;

/** Ключ состояния упорядочения — тот же, что у `api/carrier-order.ts`. */
export const ORDER_STATE_KEY = "kacho.console.carrier-order";

/**
 * Собственный транспорт ПРОБЫ, а не консоли: обращение, которое выпускает
 * сама проба (подсаженный экран, `page.evaluate`), судом стража не является.
 * В продукте этого ключа нет, поэтому код продукта, взявший его, упадёт в
 * первом же браузере; линт запрещает его в прод-коде.
 */
export const PROBE_TRANSPORT_KEY = "kacho.probe.fetch";

/**
 * Поставить стража на `fetch` окна `scope`. Повторная постановка на то же окно
 * ничего не делает.
 *
 * Свойство `fetch` становится неперенастраиваемым: заменить стража пробой
 * (`jest.spyOn`, `Object.defineProperty`) нельзя — такая замена сняла бы суд
 * со всех вызовов пробы. Присвоение `fetch = заменитель` законно и подставляет
 * СЕТЬ под стражем: заменитель получает только обращения, выпущенные
 * упорядочивающим транспортом. Присвоение взятого прежде стража возвращает
 * сеть, которая была под ним тогда.
 *
 * Чтение члена стража отдаёт член сети под ним: `expect(fetch).toHaveBeenCalled()`
 * и `jest.mocked(fetch).mock.calls` судят подставленную сеть, как и прежде.
 *
 * ТЕЛО САМОДОСТАТОЧНО: браузеру оно передаётся текстом (`toString`).
 */
export function installIssuanceGuard(scope: GuardScope, sink: BreachSink): void {
  "use strict";
  const ORDER = Symbol.for("kacho.console.carrier-order");
  const MARK = Symbol.for("kacho.console.issuance-guard");
  const PROBE = Symbol.for("kacho.probe.fetch");
  const g = scope as unknown as Record<PropertyKey, unknown>;
  if (Object.prototype.hasOwnProperty.call(g, MARK)) return;

  let baseline: unknown = g.fetch;
  let network: unknown = baseline;
  let stubbed = false;

  /** Идёт ли синхронный выпуск упорядочивающего транспорта этого окна. */
  const issuing = (): boolean => {
    const o = g[ORDER] as { issuing?: unknown } | null | undefined;
    return !!o && typeof o.issuing === "number" && o.issuing > 0;
  };

  const describe = (args: unknown[]): string => {
    let method = "GET";
    let url = "?";
    try {
      const input = args[0];
      if (typeof input === "string") url = input;
      else if (input && typeof input === "object") {
        const r = input as { url?: unknown; href?: unknown; method?: unknown };
        url = typeof r.url === "string" ? r.url : typeof r.href === "string" ? r.href : "?";
        if (typeof r.method === "string") method = r.method;
      }
      const init = args[1] as { method?: unknown } | null | undefined;
      if (init && typeof init.method === "string") method = init.method;
    } catch {
      // Аргумент, который не читается, — всё равно находка; адрес тогда «?».
    }
    return `${method.toUpperCase()} ${url}`;
  };

  /** Кто вызвал: три кадра стека выше стража. */
  const caller = (): string => {
    const frames = (new Error().stack ?? "")
      .split("\n")
      .map((l) => l.trim())
      .filter((l) => l.startsWith("at ") || /@/.test(l));
    // [0] — сам `caller`, [1] — ловушка вызова; дальше — вызвавший.
    const own = frames.slice(2, 5);
    return own.length > 0 ? ` · вызвал: ${own.join(" ← ")}` : "";
  };

  /**
   * Страж ОДНОЙ сети: у каждой сети, бывшей под стражем, свой страж, и чтение
   * `fetch` отдаёт стража текущей. Так ссылка, взятая пробой до подстановки
   * (`const original = globalThis.fetch`), возвращает присвоением ту сеть, что
   * была тогда, а заменитель, зовущий взятую ссылку, зовёт прежнюю сеть, а не
   * самого себя.
   */
  const ABSENT = {};
  const guards = new WeakMap<object, unknown>();
  const networkOf = new WeakMap<object, unknown>();
  const guardOf = (net: unknown): unknown => {
    const key = typeof net === "function" || (typeof net === "object" && net !== null) ? net : ABSENT;
    const known = guards.get(key);
    if (known) return known;
    const held = key === ABSENT ? undefined : net;
    const reachable = typeof held === "function" || (typeof held === "object" && held !== null);
    const target = (): undefined => undefined;
    const guard = new Proxy(target, {
      apply(_t, _this, args: unknown[]) {
        if (!issuing()) {
          const breach = `${describe(args)} — выпуск мимо упорядочивающего транспорта${caller()}`;
          try {
            sink(breach);
          } catch {
            // Приёмник находки не вправе отменить отказ обращению.
          }
          return Promise.reject(new TypeError(`[F8-46] ${breach}`));
        }
        if (typeof held !== "function") throw new TypeError("fetch is not a function");
        return Reflect.apply(held as (...a: unknown[]) => unknown, scope, args);
      },
      get(_t, prop) {
        return reachable ? (Reflect.get(held, prop) as unknown) : undefined;
      },
      set(_t, prop, value) {
        return reachable ? Reflect.set(held, prop, value) : false;
      },
    });
    guards.set(key, guard);
    networkOf.set(guard, held);
    return guard;
  };
  /** Присвоенное значение — сеть; присвоенный страж — его сеть. */
  const netOf = (value: unknown): unknown =>
    (typeof value === "function" || (typeof value === "object" && value !== null)) && networkOf.has(value)
      ? networkOf.get(value)
      : value;

  Object.defineProperty(g, MARK, {
    value: {
      /** Сеть под стражем по умолчанию — ставит окружение, а не проба. */
      setBaseline(fn: unknown) {
        baseline = netOf(fn);
        network = baseline;
        stubbed = false;
      },
      /** Сеть на одну пробу; снимается `releaseStub`. */
      stub(fn: unknown) {
        network = netOf(fn);
        stubbed = true;
      },
      releaseStub() {
        if (stubbed) network = baseline;
        stubbed = false;
      },
    },
  });
  Object.defineProperty(g, PROBE, {
    value: function probeFetch(...args: unknown[]) {
      if (typeof network !== "function") throw new TypeError("fetch is not a function");
      return Reflect.apply(network as (...a: unknown[]) => unknown, scope, args);
    },
  });
  Object.defineProperty(g, "fetch", {
    configurable: false,
    enumerable: true,
    get: () => guardOf(network),
    set: (value: unknown) => {
      network = netOf(value);
    },
  });

  /** Окно, отданное значением API браузера, получает того же стража. */
  const guardWindow = (w: unknown): void => {
    try {
      if (w && typeof w === "object" && (w as { window?: unknown }).window === w && w !== scope) {
        installIssuanceGuard(w as GuardScope, sink);
      }
    } catch {
      // Окно чужого происхождения: его членов не прочесть, и выпустить его
      // `fetch` отсюда нельзя — судить нечего.
    }
  };
  const guardDocumentWindow = (d: unknown): void => {
    try {
      if (d && typeof d === "object") guardWindow((d as { defaultView?: unknown }).defaultView);
    } catch {
      // Документ чужого происхождения — см. выше.
    }
  };
  const wrapGetter = (owner: unknown, key: string, after: (value: unknown) => void): void => {
    const proto = (owner as { prototype?: object } | undefined)?.prototype;
    if (!proto) return;
    const d = Object.getOwnPropertyDescriptor(proto, key);
    if (!d || typeof d.get !== "function" || !d.configurable) return;
    Object.defineProperty(proto, key, {
      ...d,
      get(this: unknown) {
        const value: unknown = d.get?.call(this);
        after(value);
        return value;
      },
    });
  };
  for (const name of ["HTMLIFrameElement", "HTMLFrameElement", "HTMLObjectElement"]) {
    wrapGetter(g[name], "contentWindow", guardWindow);
    wrapGetter(g[name], "contentDocument", guardDocumentWindow);
  }
  wrapGetter(g.Document, "defaultView", guardWindow);
  wrapGetter(g.UIEvent, "view", guardWindow);
  wrapGetter(g.MessageEvent, "source", guardWindow);

  const wrapOpener = (holder: Record<PropertyKey, unknown>, key: string): void => {
    const d = Object.getOwnPropertyDescriptor(holder, key);
    if (!d || typeof d.value !== "function" || !d.configurable) return;
    const original = d.value as (...a: unknown[]) => unknown;
    Object.defineProperty(holder, key, {
      ...d,
      value: function guardedOpen(this: unknown, ...args: unknown[]): unknown {
        const opened: unknown = Reflect.apply(original, this, args);
        guardWindow(opened);
        return opened;
      },
    });
  };
  wrapOpener(g, "open");
  const documentProto = (g.Document as { prototype?: Record<PropertyKey, unknown> } | undefined)?.prototype;
  if (documentProto) wrapOpener(documentProto, "open");
}

const BREACHES = Symbol.for("kacho.console.issuance-breaches");
const MARK_KEY = Symbol.for("kacho.console.issuance-guard");

interface GuardControl {
  setBaseline(fn: unknown): void;
  stub(fn: unknown): void;
  releaseStub(): void;
}

function control(scope: GuardScope): GuardControl {
  const c = (scope as unknown as Record<symbol, GuardControl | undefined>)[MARK_KEY];
  if (!c) throw new Error("страж мест выпуска не поставлен на это окно (installIssuanceGuard)");
  return c;
}

/** Сеть под стражем по умолчанию: заглушка окружения проб модуля. */
export function setBaselineNetwork(scope: GuardScope, fn: unknown): void {
  control(scope).setBaseline(fn);
}

/** Сеть на одну пробу: снимается `releaseStubbedNetwork` в `afterEach` окружения. */
export function stubNetworkOnce(scope: GuardScope, fn: unknown): void {
  control(scope).stub(fn);
}

export function releaseStubbedNetwork(scope: GuardScope): void {
  control(scope).releaseStub();
}

function ledger(): string[] {
  const g = globalThis as unknown as Record<symbol, string[] | undefined>;
  return (g[BREACHES] ??= []);
}

/** Приёмник находок jest-окружения: общий на все копии модуля в процессе пробы. */
export const recordIssuanceBreach: BreachSink = (breach) => {
  ledger().push(breach);
};

/** Забрать записанные находки — проба стража, которая выпускает мимо намеренно. */
export function takeIssuanceBreaches(): string[] {
  return ledger().splice(0);
}

/**
 * Уронить пробу, за которой записаны находки: общее окружение зовёт это в
 * `afterEach` и `afterAll`. Отказ, который код продукта проглотил, запись не
 * стирает — проба падает и тогда.
 */
export function failOnIssuanceBreaches(): void {
  const breaches = takeIssuanceBreaches();
  if (breaches.length > 0) throw new Error(formatIssuanceBreaches(breaches));
}

/** Текст отказа пробы, за которой записаны находки. */
export function formatIssuanceBreaches(breaches: readonly string[]): string {
  return (
    `[F8-46] обращений мимо упорядочивающего транспорта: ${breaches.length}. ` +
    "Каждое обращение консоли к сети выпускается через `orderedTransport` (`@shared/api/carrier-order`):\n" +
    breaches.map((b) => `  • ${b}`).join("\n")
  );
}
