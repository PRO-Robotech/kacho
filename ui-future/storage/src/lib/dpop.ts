// DPoP (RFC 9449) proof generator + ECDSA P-256 keypair manager (KAC-127 Phase 2).
//
// Контракт:
//   1. На signin SPA генерирует ECDSA P-256 keypair (CryptoKeyPair) с
//      `extractable=false` через WebCrypto. Private key никогда не покидает
//      браузер: ни в localStorage, ни в network, ни в JS-heap raw bytes.
//   2. KeyPair сохраняется в IndexedDB (`kacho-dpop-keys` store, ключ "current")
//      — CryptoKey можно структурно копировать в IDB.
//   3. На каждый API request (`api-client.ts` fetch wrapper) генерируется
//      свежий DPoP-JWT:
//         header = { typ:"dpop+jwt", alg:"ES256", jwk:<public-jwk> }
//         payload = { htm:<method>, htu:<full-url>, iat:<sec>, jti:<rand-uuid>, ath?:<base64url-sha256(access_token)> }
//   4. JWT подписывается private key, прикладывается в header `DPoP: <jwt>`.
//   5. JWK-thumbprint (RFC 7638) — `cnf.jkt` в access-token. Backend верифицирует.
//
// Особенности:
//   * Никаких external libs (типа jose) — pure WebCrypto + base64url + JSON.
//   * Тестируемо: keypair можно подменить через `setKeyPairForTesting`.
//   * SSR-safe: все WebCrypto calls — после явного init из браузера.
//
// Источник: https://www.rfc-editor.org/rfc/rfc9449 §4.

import { appOrigin } from "@/lib/config";

// ─────────────────────────────────────────────────────────────────────────────
// Base64url helpers
// ─────────────────────────────────────────────────────────────────────────────

function bytesToBase64url(bytes: Uint8Array): string {
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin).replace(/=+$/g, "").replace(/\+/g, "-").replace(/\//g, "_");
}

function strToBase64url(s: string): string {
  return bytesToBase64url(new TextEncoder().encode(s));
}

// ─────────────────────────────────────────────────────────────────────────────
// IndexedDB persistence
// ─────────────────────────────────────────────────────────────────────────────

const IDB_NAME = "kacho-dpop";
const IDB_STORE = "keys";
const IDB_KEY = "current";

function openIDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    if (typeof indexedDB === "undefined") {
      reject(new Error("IndexedDB not available"));
      return;
    }
    const req = indexedDB.open(IDB_NAME, 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(IDB_STORE)) {
        db.createObjectStore(IDB_STORE);
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error("IDB open failed"));
  });
}

async function idbPut(value: CryptoKeyPair): Promise<void> {
  const db = await openIDB();
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(IDB_STORE, "readwrite");
    tx.objectStore(IDB_STORE).put(value, IDB_KEY);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error ?? new Error("IDB put failed"));
  });
  db.close();
}

async function idbGet(): Promise<CryptoKeyPair | null> {
  const db = await openIDB();
  const v = await new Promise<CryptoKeyPair | null>((resolve, reject) => {
    const tx = db.transaction(IDB_STORE, "readonly");
    const req = tx.objectStore(IDB_STORE).get(IDB_KEY);
    req.onsuccess = () => resolve((req.result as CryptoKeyPair) ?? null);
    req.onerror = () => reject(req.error ?? new Error("IDB get failed"));
  });
  db.close();
  return v;
}

async function idbDelete(): Promise<void> {
  const db = await openIDB();
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(IDB_STORE, "readwrite");
    tx.objectStore(IDB_STORE).delete(IDB_KEY);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error ?? new Error("IDB delete failed"));
  });
  db.close();
}

// ─────────────────────────────────────────────────────────────────────────────
// KeyPair lifecycle
// ─────────────────────────────────────────────────────────────────────────────

let cachedPair: CryptoKeyPair | null = null;

/** Сгенерировать новую non-extractable ECDSA P-256 пару + сохранить в IDB. */
export async function generateDpopKeyPair(): Promise<CryptoKeyPair> {
  if (typeof crypto === "undefined" || !crypto.subtle) {
    throw new Error("WebCrypto not available");
  }
  const pair = await crypto.subtle.generateKey({ name: "ECDSA", namedCurve: "P-256" }, /* extractable */ false, [
    "sign",
    "verify",
  ]);
  cachedPair = pair as CryptoKeyPair;
  await idbPut(cachedPair);
  return cachedPair;
}

/** Загрузить пару из IDB; вернуть null если не существует. */
export async function loadDpopKeyPair(): Promise<CryptoKeyPair | null> {
  if (cachedPair) return cachedPair;
  try {
    const v = await idbGet();
    if (v) cachedPair = v;
    return cachedPair;
  } catch {
    return null;
  }
}

/** Получить или создать пару. Idempotent. */
export async function ensureDpopKeyPair(): Promise<CryptoKeyPair> {
  const existing = await loadDpopKeyPair();
  if (existing) return existing;
  return generateDpopKeyPair();
}

/** Удалить keypair (logout / key rotation). */
export async function clearDpopKeyPair(): Promise<void> {
  cachedPair = null;
  try {
    await idbDelete();
  } catch {
    // ignore — clean-best-effort на logout
  }
}

/** Для тестов: вручную поставить пару (mock). */
export function setKeyPairForTesting(pair: CryptoKeyPair | null): void {
  cachedPair = pair;
}

// ─────────────────────────────────────────────────────────────────────────────
// JWK extraction (public part only)
// ─────────────────────────────────────────────────────────────────────────────

interface PublicJwk {
  kty: "EC";
  crv: "P-256";
  x: string;
  y: string;
}

/** Экспорт public JWK из пары (private key остаётся в WebCrypto). */
export async function publicJwk(pair: CryptoKeyPair): Promise<PublicJwk> {
  const jwk = (await crypto.subtle.exportKey("jwk", pair.publicKey)) as JsonWebKey;
  if (jwk.kty !== "EC" || jwk.crv !== "P-256" || !jwk.x || !jwk.y) {
    throw new Error("Unexpected JWK shape");
  }
  return { kty: "EC", crv: "P-256", x: jwk.x, y: jwk.y };
}

/** JWK Thumbprint (RFC 7638) — `jkt` для cnf claim. */
export async function jwkThumbprint(jwk: PublicJwk): Promise<string> {
  // Canonical JSON: keys в lex-порядке, без whitespace.
  const canon = JSON.stringify({ crv: jwk.crv, kty: jwk.kty, x: jwk.x, y: jwk.y });
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(canon));
  return bytesToBase64url(new Uint8Array(digest));
}

// ─────────────────────────────────────────────────────────────────────────────
// DPoP proof JWT
// ─────────────────────────────────────────────────────────────────────────────

interface DpopProofOptions {
  /** HTTP method (uppercase). */
  htm: string;
  /** Full target URI (scheme://host[:port]/path). Query/fragment не включать. */
  htu: string;
  /** Optional access-token (для ath claim — sha256 binding). */
  accessToken?: string;
  /** Optional nonce от server WWW-Authenticate: DPoP-Nonce. */
  nonce?: string;
  /** Override iat (для тестов). */
  iat?: number;
  /** Override jti (для тестов). */
  jti?: string;
}

function randomJti(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    try {
      return crypto.randomUUID();
    } catch {
      // fallthrough
    }
  }
  const buf = new Uint8Array(16);
  if (typeof crypto !== "undefined" && crypto.getRandomValues) {
    crypto.getRandomValues(buf);
  } else {
    for (let i = 0; i < 16; i++) buf[i] = Math.floor(Math.random() * 256);
  }
  return bytesToBase64url(buf);
}

async function sha256Base64url(s: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
  return bytesToBase64url(new Uint8Array(digest));
}

/**
 * Нормализовать htu: убрать query / fragment, нормализовать host.
 * RFC 9449 §4.2: «htu MUST be the HTTP target URI».
 */
export function normaliseHtu(url: string): string {
  try {
    const u = new URL(url, appOrigin());
    return `${u.origin}${u.pathname}`;
  } catch {
    return url;
  }
}

/** Сгенерировать DPoP-proof JWT. Возвращает compact-form `header.payload.signature`. */
export async function createDpopProof(opts: DpopProofOptions): Promise<string> {
  const pair = await ensureDpopKeyPair();
  const jwk = await publicJwk(pair);

  const header = { typ: "dpop+jwt", alg: "ES256", jwk };
  const payload: Record<string, unknown> = {
    htm: opts.htm.toUpperCase(),
    htu: normaliseHtu(opts.htu),
    iat: opts.iat ?? Math.floor(Date.now() / 1000),
    jti: opts.jti ?? randomJti(),
  };
  if (opts.accessToken) {
    payload.ath = await sha256Base64url(opts.accessToken);
  }
  if (opts.nonce) {
    payload.nonce = opts.nonce;
  }

  const enc = `${strToBase64url(JSON.stringify(header))}.${strToBase64url(JSON.stringify(payload))}`;
  const sigBuf = await crypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    pair.privateKey,
    new TextEncoder().encode(enc),
  );
  // WebCrypto ECDSA возвращает raw r||s (64 bytes для P-256) — это уже корректный
  // JOSE-format. Никакого DER → raw переконвертирования не нужно.
  return `${enc}.${bytesToBase64url(new Uint8Array(sigBuf))}`;
}

// ─────────────────────────────────────────────────────────────────────────────
// Helper для тестов / отладки
// ─────────────────────────────────────────────────────────────────────────────

/** Декодировать payload DPoP-proof (для отладки / тестов). НЕ верифицирует подпись. */
export function decodeDpopPayload(token: string): Record<string, unknown> {
  const parts = token.split(".");
  if (parts.length !== 3) throw new Error("Malformed JWT");
  const padded = parts[1].replace(/-/g, "+").replace(/_/g, "/");
  const decoded = atob(padded + "==".slice(0, (4 - (padded.length % 4)) % 4));
  return JSON.parse(decoded) as Record<string, unknown>;
}

/** Декодировать header (для тестов). */
export function decodeDpopHeader(token: string): Record<string, unknown> {
  const parts = token.split(".");
  if (parts.length !== 3) throw new Error("Malformed JWT");
  const padded = parts[0].replace(/-/g, "+").replace(/_/g, "/");
  const decoded = atob(padded + "==".slice(0, (4 - (padded.length % 4)) % 4));
  return JSON.parse(decoded) as Record<string, unknown>;
}
