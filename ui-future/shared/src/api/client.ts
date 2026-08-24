// Базовый клиент: REST JSON на api-gateway endpoints.
// В dev: vite.config.ts проксирует /<domain>/v1/* на http://localhost:8080.
// В prod: same-origin, ingress рулит на api-gateway:8080.
//
// URL-ы verbatim из proto google.api.http annotations:
//   iam: /iam/v1/accounts, /iam/v1/projects
//   vpc:                  /vpc/v1/networks, /vpc/v1/subnets, /vpc/v1/addresses, /vpc/v1/routeTables
//   operations:           /operations/{id}
//
// API mapping:
//   GET    /<domain>/v1/<plural>          → List
//   GET    /<domain>/v1/<plural>/{id}     → Get
//   POST   /<domain>/v1/<plural>          → Create  → Operation
//   PATCH  /<domain>/v1/<plural>/{id}     → Update  → Operation
//   DELETE /<domain>/v1/<plural>/{id}     → Delete  → Operation
//   POST   /<domain>/v1/<plural>/{id}:verb → Custom verb → Operation

import { snakeToCamel, camelToSnake } from "@shared/lib/case";
import { acrFromChallenge, challengeOf, isStepUpDenial, requestStepUp } from "./step-up";
import type { Operation } from "./types";

const API_BASE = ""; // относительный путь, ingress/proxy сделают остальное

/**
 * Код ошибки в том виде, в каком его прислал отправитель.
 *
 * Край собирает тело из `google.rpc.Status`, где `code` объявлен `int32`, —
 * в JSON это ЧИСЛО (`{"code":5,"message":"Not Found"}`). Строка остаётся
 * законной формой для отправителей, пишущих имя кода, и для запасного значения
 * `String(status)`, когда тела нет вовсе.
 *
 * Объявлять это поле одной лишь строкой значило солгать компилятору: тип он
 * принимает на веру, а тело приходит из `JSON.parse`, то есть как `unknown`, —
 * и сравнение `code === "7"` было ложным ВСЕГДА, оставаясь на вид исправным.
 * Разбор кода — `@shared/lib/grpc-status`.
 */
export type ApiErrorCode = number | string;

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: ApiErrorCode,
    public details: unknown,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/** Max raw (non-JSON) error body length preserved on an ApiError, in chars. */
const MAX_RAW_ERROR_BODY = 2048;

/**
 * Build an ApiError from a response status/statusText and its raw body text.
 *
 * A JSON error envelope ({code,message,details}) is unwrapped; a non-JSON body
 * (e.g. an nginx/gateway 5xx HTML/plaintext page) is preserved into
 * message/details (truncated) instead of being silently dropped, so the real
 * backend detail survives for on-call debugging.
 */
export function apiErrorFromBody(status: number, statusText: string, text: string): ApiError {
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      // Non-JSON body — preserved below via rawBody, not discarded.
    }
  }
  const e = (parsed ?? {}) as { code?: string; message?: string; details?: unknown };
  const rawBody = parsed === null && text ? text.slice(0, MAX_RAW_ERROR_BODY) : undefined;
  return new ApiError(status, e.code ?? String(status), e.details ?? rawBody, e.message ?? rawBody ?? statusText);
}

// crypto.randomUUID требует secure context (HTTPS или localhost). При работе
// через http://console.kacho.local оно недоступно — fallback на Math.random.
function makeRequestId(): string {
  try {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return crypto.randomUUID();
    }
  } catch {
    // ignore
  }
  return (
    Math.random().toString(36).slice(2, 10) +
    "-" +
    Math.random().toString(36).slice(2, 10) +
    "-" +
    Date.now().toString(36)
  );
}

/**
 * `replayed` — этот запрос уже повторяли после подтверждения уровня.
 *
 * Повтор РОВНО ОДИН. Отказ, который подтверждением не лечится (пол выше, чем
 * даёт любой доступный способ), иначе открывал бы окно бесконечно — состояние
 * хуже исходного отказа, потому что из него нет выхода вовсе.
 */
async function fetchJson<T>(method: string, path: string, body?: unknown, replayed = false): Promise<T> {
  const url = `${API_BASE}${path}`;
  const init: RequestInit = {
    method,
    headers: {
      "Content-Type": "application/json",
      "X-Request-ID": makeRequestId(),
    },
  };
  if (body !== undefined) {
    // UI работает в snake_case; Kachō REST contract = camelCase. Convert на отправке.
    init.body = JSON.stringify(snakeToCamel(body));
  }
  const res = await fetch(url, init);
  const text = await res.text();
  if (!res.ok) {
    // Край объявляет «поднимите уровень» вызовом RFC 9470, и это ЕДИНСТВЕННОЕ
    // место консоли, где такой отказ доходит до окна подтверждения. Без него
    // окно регистрировалось и не открывалось никогда (#1213).
    const challenge = challengeOf(res);
    if (!replayed && isStepUpDenial(res.status, challenge)) {
      if (await requestStepUp(acrFromChallenge(challenge))) {
        return fetchJson<T>(method, path, body, true);
      }
    }
    throw apiErrorFromBody(res.status, res.statusText, text);
  }
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      // Successful response with a non-JSON body → treat as null payload.
    }
  }
  // На приёме: camelCase → snake_case (UI ожидает proto-style ключи).
  return camelToSnake(parsed);
}

export const api = {
  /** GET <path> → данные */
  get<T>(path: string): Promise<T> {
    return fetchJson<T>("GET", path);
  },

  /** GET <path>?k=v&… → список */
  list<T>(path: string, query?: Record<string, string>): Promise<T> {
    const qs = query && Object.keys(query).length > 0 ? "?" + new URLSearchParams(query).toString() : "";
    return fetchJson<T>("GET", `${path}${qs}`);
  },

  /** POST <path>  body=resource → Operation */
  create(path: string, body: unknown): Promise<{ operation: Operation }> {
    return fetchJson("POST", path, body);
  },

  /** POST <path>  body, raw return — для custom RPC (e.g. :invite, :listBySubject). */
  post<T>(path: string, body: unknown): Promise<T> {
    return fetchJson<T>("POST", path, body);
  },

  /** PATCH <path>/{id}  body=resource → Operation */
  update(path: string, body: unknown): Promise<{ operation: Operation }> {
    return fetchJson("PATCH", path, body);
  },

  /** DELETE <path>/{id} → Operation */
  delete(path: string): Promise<{ operation: Operation }> {
    return fetchJson("DELETE", path);
  },

  /** POST <path>/{id}:verb  body → Operation */
  action(path: string, body?: unknown): Promise<{ operation: Operation }> {
    return fetchJson("POST", path, body ?? {});
  },
};
