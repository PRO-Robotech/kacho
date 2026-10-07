// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Кодек церемонии ключа доступа — ЕДИНСТВЕННОЕ место консоли, где байты
// браузерного интерфейса ключей переводятся в форму провода службы и обратно
// (приёмка F8-S4, Р4; служба — приёмка Ф13, Р1). Его берут экран входа ключом
// (`pages/auth/access-key/`) и экран заведения ключа — второй кодек тех же
// полей разошёлся бы с первым молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ И ЧЕГО ЗДЕСЬ НЕТ
//
//   • ни сети, ни состояния, ни журнала: глаголы живут в клиенте полосы
//     (`login-lane.ts`), попытка — в экране; кодек — чистые функции;
//   • base64url без дополнения — единственная форма в обе стороны: служба
//     принимает только её (`loginlanehttp/access_key_login.go`), и второе
//     написание того же значения кодек не принимает и не производит;
//   • параметры церемонии берутся из ответа службы ДОСЛОВНО и только названные:
//     имя доверяющей стороны не выводится из адреса страницы, срок и требование
//     проверки пользователя не выдумываются, перечень удостоверений не
//     расширяется (ручек ключа у консоли нет — шапка `lib/config.ts`);
//   • тело предъявления — ЗАКРЫТАЯ проекция ответа браузера: стандартная
//     сериализация (`toJSON`) несёт `clientExtensionResults` и
//     `authenticatorAttachment`, и служба отвечает на них отказом формы (Р4);
//   • отказ кодека НЕ несёт входа: ни испытания, ни идентификатора
//     удостоверения, ни рукоятки человека — текст называет только часть,
//     которая не той формы. Экран этот текст не показывает вовсе, но и в
//     журнал, и в отчёт об ошибке он уйти не вправе с материалом церемонии.

/** Отказ кодека: часть ответа не той формы. Текст — без входа (К3). */
export class AccessKeyEncodingError extends Error {
  constructor(readonly part: string) {
    super(`access key: ${part} is malformed`);
    this.name = "AccessKeyEncodingError";
  }
}

const BASE64URL = /^[A-Za-z0-9_-]+$/;

/** Байты буфера или вида над ним — ровно его собственные, без чужого хвоста. */
function bytesOfBuffer(value: unknown, part: string): Uint8Array {
  if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  if (Object.prototype.toString.call(value) === "[object ArrayBuffer]") return new Uint8Array(value as ArrayBuffer);
  throw new AccessKeyEncodingError(part);
}

/** Байты → base64url без дополнения. */
export function base64urlOf(bytes: ArrayBuffer | ArrayBufferView): string {
  const view = bytesOfBuffer(bytes, "bytes");
  let binary = "";
  for (const b of view) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/**
 * base64url без дополнения → байты. Пустая строка, дополнение, стандартный
 * алфавит и обрезок (длина ≡ 1 по модулю 4) — не та форма.
 */
export function bytesOf(text: unknown, part: string): ArrayBuffer {
  if (typeof text !== "string" || !BASE64URL.test(text) || text.length % 4 === 1) {
    throw new AccessKeyEncodingError(part);
  }
  const padded = text.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (text.length % 4)) % 4);
  let binary: string;
  try {
    binary = atob(padded);
  } catch {
    throw new AccessKeyEncodingError(part);
  }
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out.buffer;
}

function objectOf(value: unknown, part: string): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw new AccessKeyEncodingError(part);
  return value as Record<string, unknown>;
}

function stringOf(value: unknown, part: string): string {
  if (typeof value !== "string" || value === "") throw new AccessKeyEncodingError(part);
  return value;
}

/**
 * Ответ испытания (`POST /iam/v1/auth/access-key/begin` → `{publicKey: …}`) —
 * параметры церемонии браузера. Названное службой переносится дословно, не
 * названное — отсутствует: умолчание браузера лучше выдуманного консолью.
 */
export function assertionRequestOf(answer: unknown): PublicKeyCredentialRequestOptions {
  const pk = objectOf(objectOf(answer, "answer").publicKey, "publicKey");
  const options: PublicKeyCredentialRequestOptions = {
    challenge: bytesOf(pk.challenge, "publicKey.challenge"),
    rpId: stringOf(pk.rpId, "publicKey.rpId"),
  };
  if (pk.timeout !== undefined) {
    if (typeof pk.timeout !== "number" || !Number.isFinite(pk.timeout) || pk.timeout <= 0) {
      throw new AccessKeyEncodingError("publicKey.timeout");
    }
    options.timeout = pk.timeout;
  }
  if (pk.userVerification !== undefined) {
    options.userVerification = stringOf(pk.userVerification, "publicKey.userVerification") as UserVerificationRequirement;
  }
  if (pk.allowCredentials !== undefined) {
    if (!Array.isArray(pk.allowCredentials)) throw new AccessKeyEncodingError("publicKey.allowCredentials");
    options.allowCredentials = pk.allowCredentials.map((raw) => {
      const d = objectOf(raw, "publicKey.allowCredentials");
      const descriptor: PublicKeyCredentialDescriptor = {
        type: stringOf(d.type, "publicKey.allowCredentials.type") as PublicKeyCredentialType,
        id: bytesOf(d.id, "publicKey.allowCredentials.id"),
      };
      if (d.transports !== undefined) {
        if (!Array.isArray(d.transports) || !d.transports.every((t) => typeof t === "string")) {
          throw new AccessKeyEncodingError("publicKey.allowCredentials.transports");
        }
        descriptor.transports = d.transports as AuthenticatorTransport[];
      }
      return descriptor;
    });
  }
  return options;
}

/** Тело предъявления утверждения — ровно поля формы службы (Ф13 Р1). */
export interface AccessKeyAssertionBody {
  [field: string]: unknown;
  id: string;
  rawId: string;
  type: string;
  response: {
    clientDataJSON: string;
    authenticatorData: string;
    signature: string;
    /** Пусто — браузер рукоятки не назвал; суждение о ней — дело службы. */
    userHandle: string;
  };
}

/**
 * Ответ браузера (`navigator.credentials.get`) → тело предъявления. Значения
 * переносятся без суждения о содержимом (F8 Р2): `id` — как назвал браузер,
 * `rawId` — кодированием его байтов; их расхождение судит служба.
 */
export function assertionBodyOf(credential: unknown): AccessKeyAssertionBody {
  const c = objectOf(credential, "credential");
  const r = objectOf(c.response, "credential.response");
  const encode = (value: unknown, part: string) => base64urlOf(bytesOfBuffer(value, part));
  return {
    id: stringOf(c.id, "credential.id"),
    rawId: encode(c.rawId, "credential.rawId"),
    type: stringOf(c.type, "credential.type"),
    response: {
      clientDataJSON: encode(r.clientDataJSON, "credential.response.clientDataJSON"),
      authenticatorData: encode(r.authenticatorData, "credential.response.authenticatorData"),
      signature: encode(r.signature, "credential.response.signature"),
      userHandle: r.userHandle === null || r.userHandle === undefined ? "" : encode(r.userHandle, "credential.response.userHandle"),
    },
  };
}
