// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Признак «уведомления подключены» (приёмка NTF-6 Р2, замысел З2) — ЕДИНСТВЕННЫЙ
// читатель поля признака в ответе раздачи `/console-config.json`.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕТЫРЕ СОСТОЯНИЯ, И УМОЛЧАНИЯ НЕТ
//
// Исходов у признака три: `enabled`, `disabled`, `unreadable` («не прочитано»).
// Четвёртое состояние `loading` — не исход, а «чтение ещё не завершилось»: пока
// раздача не сказала, ни один читатель не говорит «не подключены» (CX6-21).
// Запасного значения нет, и переменных сборки признак не читает (И2): признак
// различается между установками и приходит только из ответа раздачи. Имя
// `unreadable` выбрано, чтобы не совпасть со словом счётчика «непрочитанное».
//
// Каждый читатель ветвится `switch (state)` по четырём значениям с
// `assertNever(state)` в ветке по умолчанию: свести `loading` к `disabled` так
// нельзя, а пятое значение не скомпилируется у читателя, который его не знает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОДНО ОКНО — ОДНО ЧТЕНИЕ И ОДИН ИСХОД (И3)
//
// `@shared` собирается в каждый модуль своей копией, а признак у окна один.
// Состояние лежит под ключом глобального реестра символов — он общий у всех
// копий одной вкладки. Первая копия, вызвавшая `loadConsoleConfig()`, заводит
// объект и начинает чтение; прочие получают тот же объект и тот же исход.
//
// Выход из `unreadable` — только фокус окна и «Повторить»; из `enabled` и
// `disabled` выхода нет: смена флага — перекатка раздачи, и новый исход приходит
// с перезагрузкой окна. В полёте не больше одного чтения: фокус и «Повторить» во
// время чтения схлопываются в него, и на время повторного чтения состояние
// остаётся `unreadable` — страница «Повторить» не мигает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДЕДЛАЙН `CONFIG_DEADLINE_MS` (B1, замысел З20, И18)
//
// Раз триггеры схлопываются в чтение в полёте, зависшее чтение остановило бы
// выход навсегда. Поэтому у каждого чтения свой `AbortController` и таймер окна:
// по истечении таймер САМ завершает чтение исходом `unreadable` и отменяет
// запрос, не дожидаясь, отзовётся ли транспорт на отмену. Исход — ответ, отказ
// или дедлайн — применяется один раз и только если хранимое `inFlight` — это
// обещание ЭТОГО чтения; применение снимает `inFlight` и таймер. Опоздавший
// ответ себя в `inFlight` не находит и отбрасывается. Таймер — `setTimeout`
// окна, а не `AbortSignal.timeout`: срок идёт по тем же часам, что пробы.
//
// Обращение выпускает упорядочивающий транспорт вкладки, как всякое обращение
// консоли (`api/carrier-order.ts`), а не клиент края: путь отдаёт раздача, и
// перевод регистра клиента телу признака не нужен.

import { useSyncExternalStore } from "react";
import { orderedTransport } from "../api/carrier-order";

export type ConsoleNotificationsState = "loading" | "enabled" | "disabled" | "unreadable";

/** Предел одного чтения признака: «не ответило» отделено от «медленно ответило». */
export const CONFIG_DEADLINE_MS = 10_000;

const CONSOLE_CONFIG_PATH = "/console-config.json";

interface ConsoleConfigStore {
  state: ConsoleNotificationsState;
  inFlight: Promise<void> | null;
  listeners: Set<() => void>;
}

const KEY = Symbol.for("kacho.console-config");
const registry = globalThis as unknown as Record<symbol, ConsoleConfigStore | undefined>;

/** Ветка по умолчанию исчерпывающего `switch` читателя признака. */
export function assertNever(value: never): never {
  throw new Error(`unexpected console notifications state: ${String(value)}`);
}

function setState(store: ConsoleConfigStore, next: ConsoleNotificationsState): void {
  if (store.state === next) return;
  store.state = next;
  for (const listener of [...store.listeners]) listener();
}

function outcomeOf(status: number, text: string): ConsoleNotificationsState {
  if (status !== 200) return "unreadable";
  let body: unknown;
  try {
    body = JSON.parse(text);
  } catch {
    return "unreadable";
  }
  if (typeof body !== "object" || body === null) return "unreadable";
  const flag = (body as Record<string, unknown>).notificationsEnabled;
  if (typeof flag !== "boolean") return "unreadable";
  return flag ? "enabled" : "disabled";
}

function startRead(store: ConsoleConfigStore): void {
  const controller = new AbortController();
  let finish!: () => void;
  const read = new Promise<void>((resolve) => {
    finish = resolve;
  });
  // `inFlight` и таймер кладутся одним синхронным отрезком: между ними нет
  // точки, в которой чтение уже в полёте, а срока у него ещё нет.
  store.inFlight = read;
  const deadline = setTimeout(() => {
    apply("unreadable");
    controller.abort();
  }, CONFIG_DEADLINE_MS);

  function apply(outcome: ConsoleNotificationsState): void {
    if (store.inFlight !== read) return;
    store.inFlight = null;
    clearTimeout(deadline);
    setState(store, outcome);
    finish();
  }

  orderedTransport
    .fetch(CONSOLE_CONFIG_PATH, {
      method: "GET",
      cache: "no-store",
      credentials: "same-origin",
      signal: controller.signal,
    })
    .then(async (res) => outcomeOf(res.status, await res.text()))
    .then(apply, () => apply("unreadable"));
}

function retry(store: ConsoleConfigStore): void {
  if (store.state !== "unreadable" || store.inFlight !== null) return;
  startRead(store);
}

function ensureStore(): ConsoleConfigStore {
  const existing = registry[KEY];
  if (existing) return existing;
  const store: ConsoleConfigStore = { state: "loading", inFlight: null, listeners: new Set() };
  registry[KEY] = store;
  // Слушатель фокуса живёт столько же, сколько объект окна; объект, снятый с
  // окна, повторного чтения не начинает.
  document.addEventListener("visibilitychange", () => {
    if (registry[KEY] !== store || document.visibilityState !== "visible") return;
    retry(store);
  });
  startRead(store);
  return store;
}

/**
 * Начать чтение признака, если окно его ещё не начинало. Повторный вызов — из
 * этой копии или из другой — нового чтения не начинает и отдаёт текущее состояние.
 */
export function loadConsoleConfig(): ConsoleNotificationsState {
  return ensureStore().state;
}

/** «Повторить» на странице «Не удалось прочитать конфигурацию консоли». */
export function retryConsoleConfig(): void {
  retry(ensureStore());
}

/** Текущее состояние признака окна. */
export function consoleNotificationsState(): ConsoleNotificationsState {
  return ensureStore().state;
}

/** Подписка на смену состояния, сделанную любой копией. Возвращает отписку. */
export function subscribeConsoleConfig(listener: () => void): () => void {
  const store = ensureStore();
  store.listeners.add(listener);
  return () => {
    store.listeners.delete(listener);
  };
}

/** Состояние признака для компонента: перерисовка на смену, сделанную любой копией. */
export function useConsoleConfig(): ConsoleNotificationsState {
  return useSyncExternalStore(subscribeConsoleConfig, consoleNotificationsState);
}
