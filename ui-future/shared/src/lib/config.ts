// Centralised config for kacho-ui (KAC-127 Phase 2).
//
// Все runtime-параметры — env (`VITE_*` экспозится в SPA-bundle через
// Vite). Любое значение, которое отличается между dev / staging / prod —
// читается ОТСЮДА, не хардкодится в компонентах. Если не задано — используем
// document-time defaults (домен `api.kacho.cloud`).
//
// Запрет: НЕ хардкодить `api.kacho.cloud` / `app.kacho.cloud` в компонентах.
// Только через `config.apiDomain` / `config.appDomain` / `config.webauthnRpId`.

interface AppConfig {
  /** Базовый origin для api-gateway REST. Пусто = same-origin (prod через ingress). */
  apiBase: string;
  /** Apex домен (для UI badges / sentry tags). */
  apiDomain: string;
  /** Application origin (используется как audience для DPoP htu — full URL). */
  appDomain: string;
  /** Kratos public base path (browser-flows). Default `/.ory/kratos/public`. */
  kratosUrl: string;
  /** Hydra base (OAuth2 endpoints). Default `/oauth2`. */
  hydraUrl: string;
  /** Hydra client_id для kacho-ui (Public client, PKCE). */
  hydraClientId: string;
  /** Redirect URI после OAuth-callback. Default `/auth/callback`. */
  hydraRedirectUri: string;
  /** OAuth scopes для access-token. */
  hydraScopes: string;
  /** WebAuthn RP-ID (eTLD+1 от app-домена). Kratos config обязан совпадать. */
  webauthnRpId: string;
  /** WebAuthn RP display-name. */
  webauthnRpName: string;
  /** Step-up MFA TTL (минуты). */
  mfaFreshTtlMin: number;
  /** Допустимый clock-skew для DPoP nonce/iat (секунды). */
  dpopClockSkewSec: number;
  /** Recovery magic-link TTL (минуты) — для UI hint. */
  recoveryLinkTtlMin: number;
}

function envStr(key: string, fallback: string): string {
  // import.meta.env типизируется vite-env.d.ts; в тестах jsdom env пустой.
  const meta = (import.meta as ImportMeta & { env?: Record<string, string | undefined> }).env ?? {};
  const v = meta[key];
  if (typeof v === "string" && v.length > 0) return v;
  return fallback;
}

function envNum(key: string, fallback: number): number {
  const raw = envStr(key, "");
  if (!raw) return fallback;
  const n = Number(raw);
  return Number.isFinite(n) ? n : fallback;
}

// Default домены — `api.kacho.cloud` (apex) / `app.kacho.cloud` (UI).
// В prod kacho-deploy подменяет на client-specific домены через env.
const DEFAULT_API_DOMAIN = "api.kacho.cloud";
const DEFAULT_APP_DOMAIN = "app.kacho.cloud";

export const config: AppConfig = {
  apiBase: envStr("VITE_KACHO_API_BASE", ""),
  apiDomain: envStr("VITE_KACHO_API_DOMAIN", DEFAULT_API_DOMAIN),
  appDomain: envStr("VITE_APP_DOMAIN", DEFAULT_APP_DOMAIN),
  kratosUrl: envStr("VITE_KRATOS_URL", "/.ory/kratos/public"),
  hydraUrl: envStr("VITE_HYDRA_URL", "/oauth2"),
  hydraClientId: envStr("VITE_HYDRA_CLIENT_ID", "kacho-ui"),
  hydraRedirectUri: envStr("VITE_HYDRA_REDIRECT_URI", "/auth/callback"),
  hydraScopes: envStr("VITE_HYDRA_SCOPES", "openid profile email offline_access"),
  webauthnRpId: envStr("VITE_WEBAUTHN_RP_ID", "kacho.cloud"),
  webauthnRpName: envStr("VITE_WEBAUTHN_RP_NAME", "Kachō Cloud"),
  mfaFreshTtlMin: envNum("VITE_MFA_FRESH_TTL_MIN", 15),
  dpopClockSkewSec: envNum("VITE_DPOP_CLOCK_SKEW_SEC", 30),
  recoveryLinkTtlMin: envNum("VITE_RECOVERY_LINK_TTL_MIN", 5),
};

/** Полный URL для Kratos endpoint. */
export function kratosUrl(path: string): string {
  const base = config.kratosUrl.replace(/\/$/, "");
  const p = path.startsWith("/") ? path : `/${path}`;
  return `${base}${p}`;
}

/** Полный URL для Hydra endpoint. */
export function hydraUrl(path: string): string {
  const base = config.hydraUrl.replace(/\/$/, "");
  const p = path.startsWith("/") ? path : `/${path}`;
  return `${base}${p}`;
}

/** Origin для DPoP htu (full URL: scheme + host + path). */
export function appOrigin(): string {
  if (typeof window !== "undefined" && window.location) {
    return `${window.location.protocol}//${window.location.host}`;
  }
  return `https://${config.appDomain}`;
}
