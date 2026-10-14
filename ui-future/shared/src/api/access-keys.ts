// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { ApiError, api } from "./client";
import type { Operation } from "./types";
import { operationIdOf } from "@shared/lib/operation-outcome";

// Глаголы ключей доступа человека из сессии (Ф7; экран — приёмка F8, ред. 12,
// Р11) — ЕДИНСТВЕННОЕ место консоли, где они зовутся. Это транспорт: исход
// человеку называет вызывающий раздел (`pages/auth/access-key/AccessKeysSection.tsx`)
// единым механизмом сигнала — и текстом у раздела, и уведомлением.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕРЕЗ КЛИЕНТ ПЛАТФОРМЫ, А НЕ ПОЛОСУ ФОРМЫ
//
// Перечень, выдача испытания регистрации, приём результата и снятие — маршруты
// края `kaname.cloud.iam.v1.AccessKeyService` (приёмка F8, N25), то есть
// поверхность платформы. Поэтому они идут клиентом платформы (`api/client.ts`):
// упорядочивающим транспортом вкладки без признака «ставит носитель» (F8-67) и
// с ОДНИМ решением консоли на отказ — свежесть (`SESSION_NOT_FRESH`) открывает
// окно повышения и повторяет то же обращение один раз (F8-51, F8-57). Признака
// формы у этих глаголов нет: это не полоса формы службы.
//
// ОПЕРАЦИЯ. Приём результата и снятие отвечают `Operation` (ban #9), и исход —
// это `done` БЕЗ `error`, прочитанный опросом `GET /operations/{id}`, а не приём
// мутации. Синхронный отказ (до заведения операции — N28, N36) приходит ответом
// глагола, и тогда операции нет и опроса нет.

/** Ключ доступа — в форме клиента платформы (ключи snake_case). */
export interface AccessKeyRecord {
  id: string;
  user_id?: string;
  name?: string;
  description?: string;
  created_at?: string;
  /** Не заполнен до первого использования (Ф7). */
  last_used_at?: string;
}

interface AccessKeysPage {
  access_keys?: AccessKeyRecord[];
  next_page_token?: string;
}

/** Операция кончилась отказом: `error.message` — дословно. */
export class AccessKeyOperationFailed extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AccessKeyOperationFailed";
  }
}

/** Ответ мутации или операция не той формы — исхода нет, и он не выдумывается. */
export class AccessKeyOperationUnknown extends Error {
  constructor(readonly step: "приём результата" | "снятие" | "опрос") {
    super(`access keys: ${step} answered without an operation outcome`);
    this.name = "AccessKeyOperationUnknown";
  }
}

const keysPath = (userId: string) => `/iam/v1/users/${encodeURIComponent(userId)}/accessKeys`;

/** Страниц перечня не больше этого: дальше — отказ, а не бесконечное чтение. */
const MAX_PAGES = 50;
/** Опросов операции не больше этого (раз в секунду) — дальше исход назван неизвестным. */
const MAX_POLLS = 120;
const POLL_PAUSE_MS = 1000;

function pause(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** Дождаться исхода операции опросом; первый опрос — сразу. */
async function settled(answer: unknown, step: "приём результата" | "снятие"): Promise<void> {
  const id = operationIdOf(answer);
  if (!id) throw new AccessKeyOperationUnknown(step);
  for (let polled = 0; polled < MAX_POLLS; polled++) {
    if (polled > 0) await pause(POLL_PAUSE_MS);
    const op = await api.get<Operation | null>(`/operations/${encodeURIComponent(id)}`);
    if (!op || typeof op !== "object") throw new AccessKeyOperationUnknown("опрос");
    if (op.done) {
      if (op.error) throw new AccessKeyOperationFailed(op.error.message || "операция завершилась с ошибкой");
      return;
    }
  }
  throw new AccessKeyOperationUnknown("опрос");
}

export const accessKeysClient = {
  /** Перечень дочитывается до конца: каждая следующая страница — `pageToken` предыдущей (F8-61). */
  async list(userId: string): Promise<AccessKeyRecord[]> {
    const out: AccessKeyRecord[] = [];
    const seen = new Set<string>();
    let token = "";
    for (let page = 0; page < MAX_PAGES; page++) {
      const answer = await api.list<AccessKeysPage | null>(keysPath(userId), token ? { pageToken: token } : undefined);
      out.push(...(answer?.access_keys ?? []));
      token = answer?.next_page_token ?? "";
      if (token === "") return out;
      // Тот же токен второй раз — перечень не движется; читать его снова бессмысленно.
      if (seen.has(token)) break;
      seen.add(token);
    }
    throw new ApiError(0, "", undefined, "Перечень ключей не дочитан: служба не назвала последнюю страницу");
  },

  /** Выдача испытания регистрации — ответ как прислан; разбор — кодек `registrationRequestOf`. */
  beginRegistration(userId: string): Promise<unknown> {
    return api.post<unknown>(`${keysPath(userId)}:beginRegistration`, {});
  },

  /** Приём результата: тело — ровно `name`, `description`, `credential`; исход — `done` без `error`. */
  async finishRegistration(
    userId: string,
    form: { name: string; description: string; credential: Record<string, unknown> },
  ): Promise<void> {
    const answer = await api.post<unknown>(keysPath(userId), {
      name: form.name,
      description: form.description,
      credential: form.credential,
    });
    await settled(answer, "приём результата");
  },

  /** Снятие по платформенному `id` ключа (ban #15); исход — `done` без `error`. */
  async revoke(userId: string, accessKeyId: string): Promise<void> {
    const answer = await api.delete(`${keysPath(userId)}/${encodeURIComponent(accessKeyId)}`);
    await settled(answer, "снятие");
  },
};
