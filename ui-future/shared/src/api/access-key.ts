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
//   • ни сети, ни состояния, ни журнала: глаголы входа живут в клиенте полосы
//     (`login-lane.ts`), глаголы заведения и снятия — в клиенте раздела ключей
//     (`api/access-keys.ts`), попытка — в экране;
//     кодек — чистые функции;
//   • у ВХОДА ключом base64url без дополнения — единственная форма в обе
//     стороны: полоса службы принимает только её
//     (`loginlanehttp/access_key_login.go`), и второе написание того же
//     значения кодек входа не принимает и не производит. Заведение идёт
//     поверхностью платформы, у которой форма `bytes` другая, — раздел ниже;
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
    options.userVerification = stringOf(
      pk.userVerification,
      "publicKey.userVerification",
    ) as UserVerificationRequirement;
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
      userHandle:
        r.userHandle === null || r.userHandle === undefined
          ? ""
          : encode(r.userHandle, "credential.response.userHandle"),
    },
  };
}

// ─── заведение ключа из сессии (Ф7; экран — приёмка F8, ред. 12, Р11) ────────
//
// Глаголы заведения — поверхность ПЛАТФОРМЫ (`/iam/v1/users/{userId}/accessKeys…`),
// а не полоса формы: их отвечает край разбором контракта службы, и `bytes` в
// таком ответе — строка base64 СТАНДАРТНОГО алфавита с дополнением (`protojson`).
// Разбор края принимает оба алфавита, с дополнением и без, поэтому кодек
// заведения читает так же; пишет он каноническую форму — стандартный алфавит с
// дополнением. Сравнение «без изменения» — по байтам, а не по строкам.
//
// Ответ испытания регистрации кодек получает в той форме, в какой его отдаёт
// клиент платформы (`api/client.ts` переводит ключи провода в snake_case). Поле,
// которого ответ не нёс, у `protojson` означает значение по умолчанию (пустую
// строку, пустой перечень), и кодек переносит его этим значением, а не
// выдумывает своё; испытание и рукоятка человека пустыми не бывают.

const WIRE_BASE64 = /^[A-Za-z0-9+/_-]+={0,2}$/;

/** `bytes` провода платформы → байты: оба алфавита, с дополнением и без. */
export function bytesOfWire(text: unknown, part: string): ArrayBuffer {
  if (typeof text !== "string" || !WIRE_BASE64.test(text)) throw new AccessKeyEncodingError(part);
  const bare = text.replace(/=+$/, "");
  if (bare.length % 4 === 1) throw new AccessKeyEncodingError(part);
  const std = bare.replace(/-/g, "+").replace(/_/g, "/");
  let binary: string;
  try {
    binary = atob(std + "=".repeat((4 - (std.length % 4)) % 4));
  } catch {
    throw new AccessKeyEncodingError(part);
  }
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out.buffer;
}

/** Байты → `bytes` провода платформы: стандартный алфавит с дополнением. */
export function base64Of(bytes: unknown, part: string): string {
  const view = bytesOfBuffer(bytes, part);
  let binary = "";
  for (const b of view) binary += String.fromCharCode(b);
  return btoa(binary);
}

/** Строка ответа, которую `protojson` опускает пустой: нет поля — `""`. */
function textOrDefault(value: unknown, part: string): string {
  if (value === undefined) return "";
  if (typeof value !== "string") throw new AccessKeyEncodingError(part);
  return value;
}

/** Целое `int64` провода: `protojson` пишет его строкой; число принимается тоже. */
function integerOf(value: unknown, part: string): number {
  if (typeof value === "number" && Number.isInteger(value)) return value;
  if (typeof value === "string" && /^-?\d+$/.test(value)) return Number(value);
  throw new AccessKeyEncodingError(part);
}

/**
 * Ответ выдачи испытания регистрации (`AccessKeyRegistrationChallenge`, ключи
 * клиента платформы) → параметры `navigator.credentials.create`. Названное
 * службой переносится без изменения; срок испытания (`expires_at`) браузеру не
 * передаётся — свой срок церемонии консоль не выдумывает, судит срок служба.
 */
export function registrationRequestOf(answer: unknown): PublicKeyCredentialCreationOptions {
  const a = objectOf(answer, "answer");
  const rp = objectOf(a.rp, "rp");
  const user = objectOf(a.user, "user");
  const params = a.pub_key_cred_params === undefined ? [] : a.pub_key_cred_params;
  if (!Array.isArray(params)) throw new AccessKeyEncodingError("pub_key_cred_params");
  const userId = bytesOfWire(user.id, "user.id");
  if (userId.byteLength === 0) throw new AccessKeyEncodingError("user.id");
  const challenge = bytesOfWire(a.challenge, "challenge");
  if (challenge.byteLength === 0) throw new AccessKeyEncodingError("challenge");
  const options: PublicKeyCredentialCreationOptions = {
    challenge,
    rp: { id: stringOf(rp.id, "rp.id"), name: textOrDefault(rp.name, "rp.name") },
    user: {
      id: userId,
      name: textOrDefault(user.name, "user.name"),
      displayName: textOrDefault(user.display_name, "user.display_name"),
    },
    pubKeyCredParams: params.map((raw) => {
      const p = objectOf(raw, "pub_key_cred_params");
      return {
        type: stringOf(p.type, "pub_key_cred_params.type") as PublicKeyCredentialType,
        alg: integerOf(p.alg, "pub_key_cred_params.alg"),
      };
    }),
  };
  if (a.authenticator_selection !== undefined) {
    const s = objectOf(a.authenticator_selection, "authenticator_selection");
    const selection: AuthenticatorSelectionCriteria = {};
    if (s.resident_key !== undefined) {
      selection.residentKey = stringOf(
        s.resident_key,
        "authenticator_selection.resident_key",
      ) as ResidentKeyRequirement;
    }
    if (s.require_resident_key !== undefined) {
      if (typeof s.require_resident_key !== "boolean") {
        throw new AccessKeyEncodingError("authenticator_selection.require_resident_key");
      }
      selection.requireResidentKey = s.require_resident_key;
    }
    if (s.user_verification !== undefined) {
      selection.userVerification = stringOf(
        s.user_verification,
        "authenticator_selection.user_verification",
      ) as UserVerificationRequirement;
    }
    options.authenticatorSelection = selection;
  }
  if (a.attestation !== undefined) {
    options.attestation = stringOf(a.attestation, "attestation") as AttestationConveyancePreference;
  }
  if (a.extensions !== undefined) {
    const e = objectOf(a.extensions, "extensions");
    if (e.cred_props !== undefined && typeof e.cred_props !== "boolean") throw new AccessKeyEncodingError("extensions");
    // `credProps: false` — значение по умолчанию: `protojson` его не пишет, и
    // просить браузер о нём незачем; переносится только просьба.
    if (e.cred_props === true) options.extensions = { credProps: true };
  }
  return options;
}

/**
 * Результат регистрации в теле приёма (`RegistrationCredential`) — ровно поля
 * контракта. `discoverable` — три состояния Ф7-41: `true`, `false` и «нет поля»,
 * как браузер сообщил `credProps.rk`; консоль обнаружимость не судит.
 */
export interface AccessKeyRegistrationCredential {
  [field: string]: unknown;
  id: string;
  clientDataJson: string;
  attestationObject: string;
  discoverable?: boolean;
}

/**
 * Ответ браузера (`navigator.credentials.create`) → результат регистрации.
 * Закрытая проекция: `transports`, `publicKeyAlgorithm`, прочие расширения и
 * `authenticatorAttachment` в тело не идут — их в контракте нет.
 */
export function registrationCredentialOf(credential: unknown): AccessKeyRegistrationCredential {
  const c = objectOf(credential, "credential");
  const r = objectOf(c.response, "credential.response");
  const out: AccessKeyRegistrationCredential = {
    id: base64Of(c.rawId, "credential.rawId"),
    clientDataJson: base64Of(r.clientDataJSON, "credential.response.clientDataJSON"),
    attestationObject: base64Of(r.attestationObject, "credential.response.attestationObject"),
  };
  const results =
    typeof c.getClientExtensionResults === "function"
      ? (c.getClientExtensionResults as () => unknown).call(credential)
      : undefined;
  const props = results && typeof results === "object" ? (results as { credProps?: unknown }).credProps : undefined;
  const rk = props && typeof props === "object" ? (props as { rk?: unknown }).rk : undefined;
  if (typeof rk === "boolean") out.discoverable = rk;
  return out;
}
