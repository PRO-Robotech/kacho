// Базовый клиент: REST JSON на api-gateway endpoints.
// В dev: vite.config.ts проксирует /<domain>/v1/* на http://localhost:8080.
// В prod: same-origin, ingress рулит на api-gateway:8080.
//
// URL-ы verbatim из proto google.api.http annotations:
//   iam: /iam/v1/accounts, /iam/v1/projects
//   vpc:                  /vpc/v1/networks, /vpc/v1/subnets, /vpc/v1/addresses, /vpc/v1/route-tables
//   operations:           /operations/{id}
//
// API mapping:
//   GET    /<domain>/v1/<plural>          → List
//   GET    /<domain>/v1/<plural>/{id}     → Get
//   POST   /<domain>/v1/<plural>          → Create  → Operation
//   PATCH  /<domain>/v1/<plural>/{id}     → Update  → Operation
//   DELETE /<domain>/v1/<plural>/{id}     → Delete  → Operation
//   POST   /<domain>/v1/<plural>/{id}:verb → Custom verb → Operation

import { snakeToCamel, camelToSnake } from "@/lib/case";
import type { Operation } from "./types";

const API_BASE = ""; // относительный путь, ingress/proxy сделают остальное

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    public details: unknown,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
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

async function fetchJson<T>(method: string, path: string, body?: unknown): Promise<T> {
  const url = `${API_BASE}${path}`;
  const init: RequestInit = {
    method,
    headers: {
      "Content-Type": "application/json",
      "X-Request-ID": makeRequestId(),
    },
  };
  if (body !== undefined) {
    // UI работает в snake_case; YC contract = camelCase. Convert на отправке.
    init.body = JSON.stringify(snakeToCamel(body));
  }
  const res = await fetch(url, init);
  const text = await res.text();
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      // not json
    }
  }
  if (!res.ok) {
    const err = (parsed ?? {}) as { code?: string; message?: string; details?: unknown };
    throw new ApiError(res.status, err.code ?? String(res.status), err.details, err.message ?? res.statusText);
  }
  // На приёме: camelCase → snake_case (UI ожидает proto-style ключи).
  return camelToSnake(parsed) as T;
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
