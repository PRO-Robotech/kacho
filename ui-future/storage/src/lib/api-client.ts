// API client — DPoP-bound fetch wrapper (KAC-127 Phase 2).
//
// Контракт:
//   1. Каждый запрос подписывает DPoP-proof (createDpopProof) и
//      прикладывает в `DPoP: <jwt>` header.
//   2. Если есть access-token — прикладывает Authorization: `DPoP <token>`
//      (NOT `Bearer` — RFC 9449 §7 требует scheme=DPoP когда proof bound).
//   3. На 401 со `WWW-Authenticate: DPoP error="use_dpop_nonce", nonce=...`
//      — выполняет один retry с включённым nonce в payload.
//   4. На 401 от protected API (access-token истёк) — вызывает
//      `onTokenExpired` callback (StepUpModal / refresh-flow).
//   5. На 403 со `WWW-Authenticate: Bearer error="insufficient_user_authentication"`
//      — выбрасывает StepUpRequiredError → StepUpModal перехватывает.
//
// Все state-store-side-effects (refresh, replay) — через injected callbacks
// (никаких глобальных синглтонов state-вне-AuthContext).

import { createDpopProof } from "@/lib/dpop";

export interface ApiClientOptions {
  /** Текущий access-token (in-memory). null = unauthenticated request. */
  getAccessToken?: () => string | null;
  /** Колбэк при 401 protected API — обычно refresh + replay. Возвращает новый token или null. */
  onTokenExpired?: () => Promise<string | null>;
  /** Колбэк при 403 step-up — UI открывает модалку и резолвит после success. */
  onStepUpRequired?: (acrRequired?: string) => Promise<void>;
  /** Base URL — обычно "" (same-origin). */
  baseUrl?: string;
}

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

export class StepUpRequiredError extends ApiError {
  acrRequired?: string;
  amrRequired?: string[];
  constructor(status: number, acrRequired?: string, amrRequired?: string[]) {
    super(status, "insufficient_user_authentication", null, "step-up required");
    this.name = "StepUpRequiredError";
    this.acrRequired = acrRequired;
    this.amrRequired = amrRequired;
  }
}

interface ParsedChallenge {
  scheme: string;
  params: Record<string, string>;
}

/** Парсит первый challenge из WWW-Authenticate (минимально, без RFC 9110 полноты). */
export function parseWwwAuthenticate(value: string | null): ParsedChallenge[] {
  if (!value) return [];
  // Разбиваем по запятой-вне-кавычек; достаточно для DPoP / Bearer.
  const challenges: ParsedChallenge[] = [];
  const re = /([A-Za-z]+)\s+((?:[A-Za-z_]+=(?:"[^"]*"|[^,]+)(?:,\s*)?)+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(value)) !== null) {
    const scheme = m[1];
    const rest = m[2];
    const params: Record<string, string> = {};
    const paramRe = /([A-Za-z_]+)=("([^"]*)"|([^,]+))/g;
    let pm: RegExpExecArray | null;
    while ((pm = paramRe.exec(rest)) !== null) {
      params[pm[1]] = (pm[3] ?? pm[4] ?? "").trim();
    }
    challenges.push({ scheme, params });
  }
  return challenges;
}

interface FetchOpts {
  method?: string;
  body?: unknown;
  /** Override token (для случаев когда нужен fresh после refresh). */
  accessToken?: string | null;
  /** Headers (extra). */
  headers?: Record<string, string>;
  /** Skip step-up handling (для тестов / fresh-call). */
  skipStepUp?: boolean;
  /** Skip token-refresh (для refresh-call самого, избегаем рекурсии). */
  skipRefresh?: boolean;
  /** DPoP nonce (для retry). */
  nonce?: string;
}

export class ApiClient {
  private opts: ApiClientOptions;

  constructor(opts: ApiClientOptions = {}) {
    this.opts = opts;
  }

  /** Update opts (например, после установки/обновления accessToken-getter). */
  configure(opts: Partial<ApiClientOptions>): void {
    this.opts = { ...this.opts, ...opts };
  }

  private resolveUrl(path: string): string {
    const base = this.opts.baseUrl ?? "";
    if (/^https?:\/\//.test(path)) return path;
    return `${base}${path.startsWith("/") ? "" : "/"}${path}`;
  }

  private currentToken(override?: string | null): string | null {
    if (override !== undefined) return override;
    return this.opts.getAccessToken ? this.opts.getAccessToken() : null;
  }

  /** Низкоуровневый fetch с DPoP-bound авторизацией. */
  async fetch(path: string, opts: FetchOpts = {}): Promise<Response> {
    const url = this.resolveUrl(path);
    const method = (opts.method ?? "GET").toUpperCase();
    const token = this.currentToken(opts.accessToken);

    const dpop = await createDpopProof({
      htm: method,
      htu: url,
      accessToken: token ?? undefined,
      nonce: opts.nonce,
    });

    const headers: Record<string, string> = {
      Accept: "application/json",
      DPoP: dpop,
      ...opts.headers,
    };
    if (token) {
      headers.Authorization = `DPoP ${token}`;
    }
    if (opts.body !== undefined) {
      headers["Content-Type"] = headers["Content-Type"] ?? "application/json";
    }

    const res = await fetch(url, {
      method,
      headers,
      credentials: "include",
      body:
        opts.body === undefined || opts.body === null
          ? undefined
          : typeof opts.body === "string"
            ? opts.body
            : JSON.stringify(opts.body),
    });

    // 401 → DPoP nonce retry / token-refresh
    if (res.status === 401) {
      const challenges = parseWwwAuthenticate(res.headers.get("WWW-Authenticate"));
      const dpopChal = challenges.find((c) => c.scheme === "DPoP");
      const nonce = res.headers.get("DPoP-Nonce") ?? dpopChal?.params.nonce;
      // DPoP-Nonce → retry один раз
      if (nonce && !opts.nonce) {
        return this.fetch(path, { ...opts, nonce });
      }
      // Access-token истёк → refresh + retry
      if (token && !opts.skipRefresh && this.opts.onTokenExpired) {
        const newToken = await this.opts.onTokenExpired();
        if (newToken) {
          return this.fetch(path, { ...opts, accessToken: newToken, skipRefresh: true });
        }
      }
    }

    // 403 insufficient_user_authentication → step-up
    if (res.status === 403 && !opts.skipStepUp) {
      const challenges = parseWwwAuthenticate(res.headers.get("WWW-Authenticate"));
      const insuf = challenges.find((c) => c.params.error === "insufficient_user_authentication");
      if (insuf) {
        const acr = insuf.params.acr_values;
        const amr = insuf.params.amr_values?.split(/\s+/);
        if (this.opts.onStepUpRequired) {
          await this.opts.onStepUpRequired(acr);
          // Step-up completed → replay original request.
          return this.fetch(path, { ...opts, skipStepUp: true });
        }
        throw new StepUpRequiredError(403, acr, amr);
      }
    }

    return res;
  }

  /** JSON-удобный wrapper. */
  async json<T = unknown>(path: string, opts: FetchOpts = {}): Promise<T> {
    const res = await this.fetch(path, opts);
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
      const e = (parsed ?? {}) as { code?: string; message?: string; details?: unknown };
      throw new ApiError(res.status, e.code ?? String(res.status), e.details, e.message ?? res.statusText);
    }
    return parsed as T;
  }

  get<T = unknown>(path: string, opts: Omit<FetchOpts, "method"> = {}): Promise<T> {
    return this.json<T>(path, { ...opts, method: "GET" });
  }
  post<T = unknown>(path: string, body?: unknown, opts: Omit<FetchOpts, "method"> = {}): Promise<T> {
    return this.json<T>(path, { ...opts, method: "POST", body });
  }
  patch<T = unknown>(path: string, body?: unknown, opts: Omit<FetchOpts, "method"> = {}): Promise<T> {
    return this.json<T>(path, { ...opts, method: "PATCH", body });
  }
  put<T = unknown>(path: string, body?: unknown, opts: Omit<FetchOpts, "method"> = {}): Promise<T> {
    return this.json<T>(path, { ...opts, method: "PUT", body });
  }
  delete<T = unknown>(path: string, opts: Omit<FetchOpts, "method"> = {}): Promise<T> {
    return this.json<T>(path, { ...opts, method: "DELETE" });
  }
}

/** Singleton — configure'ится из AuthProvider при mount. */
export const apiClient = new ApiClient();
