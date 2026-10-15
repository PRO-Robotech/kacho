// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { api, ApiError } from "@shared/api/client";
import type { Operation } from "@shared/api/types";
import { operationIdOf } from "@shared/lib/operation-outcome";

// Клиент удаления своей личности — транспорт, как `access-keys.ts`; исход
// человеку называет экран параметров. `DELETE /iam/v1/users/{user_id}` (#3186; kaname
// `docs/content/api/user.mdx`, «Delete — удалить личность»).
//
// Что держит служба и чего НЕ делает консоль:
//   • решает служба: удалить себя вправе сам человек, уровень сессии — `2`;
//     консоль своей проверки «можно ли» не заводит — пол уровня поднимает окно
//     повышения клиента платформы (`StepUpModal`), а отказ называет служба;
//   • `user_id` — только из ответа «кто я» сессии: ни адрес страницы, ни форма его
//     не задают;
//   • порядок — аккаунт, затем личность: владеющему аккаунтом служба отвечает
//     `FAILED_PRECONDITION`, и экран называет этот шаг;
//   • глагол асинхронный: исход — `done` операции, прочитанный опросом.

/** Опросов операции не больше этого (раз в секунду) — дальше исход назван неизвестным. */
const MAX_POLLS = 120;
const POLL_PAUSE_MS = 1000;
/** `google.rpc.Code` условия, которое служба называет «сперва удалите аккаунт». */
const FAILED_PRECONDITION = 9;
const UNAUTHENTICATED_STATUS = 401;

/** Исход удаления, который служба назвала отказом. */
export class SelfRemovalRefused extends Error {
  constructor(
    /** `google.rpc.Code` отказа; `null` — не назван. */
    readonly code: number | null,
    message: string,
  ) {
    super(message);
    this.name = "SelfRemovalRefused";
  }

  /** Отказ «человек ещё владеет аккаунтом» — шаг к нему один: сперва аккаунты. */
  get ownsAccounts(): boolean {
    return this.code === FAILED_PRECONDITION;
  }
}

/** Исход не прочитан: служба приняла глагол, а `done` не дошёл. */
export class SelfRemovalUnknown extends Error {
  constructor() {
    super("Исход удаления не получен: служба приняла запрос, но не сообщила, завершено ли удаление.");
    this.name = "SelfRemovalUnknown";
  }
}

function pause(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function codeOf(e: ApiError): number | null {
  return typeof e.code === "number" ? e.code : null;
}

/**
 * Удалить свою личность и дождаться исхода. Успех — `done` без `error` либо
 * отказ «учётные данные не приняты» на опросе: удостоверения удалённой личности
 * служба отвергает — это и есть следствие удаления, и сессии больше нет.
 */
export async function removeSelf(userId: string): Promise<void> {
  let answer: unknown;
  try {
    answer = await api.delete(`/iam/v1/users/${encodeURIComponent(userId)}`);
  } catch (e) {
    if (e instanceof ApiError) throw new SelfRemovalRefused(codeOf(e), e.message);
    throw e;
  }
  const id = operationIdOf(answer);
  if (!id) throw new SelfRemovalUnknown();
  for (let polled = 0; polled < MAX_POLLS; polled++) {
    if (polled > 0) await pause(POLL_PAUSE_MS);
    let op: Operation | null;
    try {
      op = await api.get<Operation | null>(`/operations/${encodeURIComponent(id)}`);
    } catch (e) {
      if (e instanceof ApiError && e.status === UNAUTHENTICATED_STATUS) return;
      throw e;
    }
    if (!op || typeof op !== "object") throw new SelfRemovalUnknown();
    if (op.done) {
      if (op.error) throw new SelfRemovalRefused(op.error.code ?? null, op.error.message || "удаление не выполнено");
      return;
    }
  }
  throw new SelfRemovalUnknown();
}
