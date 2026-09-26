// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Состояние браузера, привязанное к ЧЕЛОВЕКУ, — и что после выхода остаётся
// (приёмка F8, выход; условие C14).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ
//
// Выход гасит носитель у службы, но постоянное хранилище браузера ей не
// принадлежит. Консоль кладёт туда выбранные аккаунт и проект ВМЕСТЕ С ИМЕНАМИ:
// следующий человек, вошедший в этом браузере, увидел бы в шапке имена чужого
// аккаунта и проекта, а каркас попытался бы открыть чужой проект. Поэтому после
// ПОДТВЕРЖДЁННОГО выхода (`200` глагола выхода, и только его) такое состояние
// снимается; на отказе выхода не снимается ничего — экран не делает вид, что
// вышли (F8-19).
//
// ЧТО ОСТАЁТСЯ — НАЗВАНО ПЕРЕЧНЕМ, А НЕ УМОЛЧАНО. Перепись хранилищ консоли
// (`principal-state.census.test.ts`) требует, чтобы каждый прод-файл, пишущий
// в хранилище браузера, стоял ровно в одной из двух граф ниже: хранилище,
// заведённое без решения «чьё оно», краснит перепись.

/** Хранилище браузера, которым консоль пользуется. */
export type BrowserStore = "localStorage" | "sessionStorage" | "indexedDB";

export interface StoredState {
  store: BrowserStore;
  /** Ключ записи; у записей, чей ключ задаёт вызывающий, — описание ключа. */
  key: string;
  /** Прод-файлы консоли, которые пишут и читают эту запись. */
  files: readonly string[];
  /** Что лежит и почему решено так, как решено. */
  what: string;
}

/** Привязанное к человеку: снимается после подтверждённого выхода. */
export const PRINCIPAL_BOUND_STATE: readonly StoredState[] = [
  {
    store: "localStorage",
    key: "kacho.context.v2",
    files: ["shared/src/lib/context-store.ts", "host/src/utils/host-context.ts", "dashboard/src/utils/host-context.ts"],
    what: "выбранные аккаунт и проект с их именами — у другого человека в этом браузере их быть не должно",
  },
];

/** Остаётся после выхода: о человеке не говорит и чужому не применяется. */
export const KEPT_ACROSS_PRINCIPALS: readonly StoredState[] = [
  {
    store: "localStorage",
    key: "kacho-theme",
    files: ["shared/src/lib/theme-context.tsx", "host/src/App.tsx"],
    what: "светлая или тёмная тема — выбор браузера, а не человека",
  },
  {
    store: "localStorage",
    key: "видимость колонок таблицы (ключ задаёт таблица)",
    files: ["shared/src/components/molecules/TableToolbar/TableToolbar.tsx"],
    what: "какие колонки таблицы скрыты — имена колонок, а не данные",
  },
  {
    store: "sessionStorage",
    key: "kacho.subscription.edge-silent-until",
    files: ["shared/src/lib/subscription/hub.ts"],
    what: "до какого момента поток изменений края не переоткрывается — свойство края, а не человека",
  },
  {
    store: "indexedDB",
    key: "kacho-dpop",
    files: ["nlb/src/lib/dpop.ts", "registry/src/lib/dpop.ts", "storage/src/lib/dpop.ts"],
    what:
      "ключевая пара доказательства владения токеном; закрытая половина неизвлекаема и сведений " +
      "о человеке не несёт",
  },
];

/**
 * Снять состояние, привязанное к человеку. Зовёт ТОЛЬКО подтверждённый выход:
 * до `200` глагола выхода снимать нечего — сессия жива.
 */
export function forgetPrincipalState(): void {
  for (const s of PRINCIPAL_BOUND_STATE) {
    try {
      if (s.store === "localStorage") window.localStorage.removeItem(s.key);
      else if (s.store === "sessionStorage") window.sessionStorage.removeItem(s.key);
    } catch {
      // Хранилище недоступно (закрытый режим браузера) — снимать нечего.
    }
  }
}
