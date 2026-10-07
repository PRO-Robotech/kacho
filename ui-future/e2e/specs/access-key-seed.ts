// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createHash, createPrivateKey, generateKeyPairSync, randomBytes, sign, type KeyObject } from "node:crypto";
import { expect, type CDPSession, type Page } from "@playwright/test";
import { acceptedOperation, operationSucceeded, readerOf } from "./cloud-admin";
import { SESSION_IDENTITY, lastIssued, type Seed } from "./ceremony-seed";
import { conditionNotCreated } from "./mail-receiver";
import { FIXTURE_UNMET_PREFIX } from "./producer-answers";

/**
 * Посев К — ключ доступа человека П-п в аутентификаторе браузера сценария
 * (приёмка F8-S4, §6.0). Посев К-снят — тот же ключ, снятый глаголом Ф7.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ БЕЗ ЭКРАНА И БЕЗ ЧЕЛОВЕКА
 *
 * Экран заведения ключа — другой предмет (параметры учётной записи), и посев
 * экраном сделал бы каждый сценарий входа заложником чужого экрана. Поэтому ключ
 * заводится глаголами Ф7 через край тем же контекстом посева, что держит сессию
 * П-п, а результат церемонии регистрации собирает КОД ПОСЕВА программным ключом
 * ES256 при аттестации «none». Тот же закрытый ключ, идентификатор
 * удостоверения и рукоятка человека кладутся в виртуальный аутентификатор
 * страницы (домен CDP `WebAuthn`), и экран предъявляет его настоящей церемонией
 * браузера — подмены интерфейса браузера в сквозной пробе нет.
 *
 * ФЛАГИ НАЗВАНЫ ЗНАЧЕНИЕМ, А НЕ УМОЛЧАНИЕМ (N15, N16)
 *
 *   • данные аутентификатора регистрации: UP = 1, UV — как у аутентификатора
 *     страницы, BE = 0, BS = 0, счётчик подписи 0 — хранимое значение ключа
 *     после регистрации; сверка флага резервного копирования между регистрацией
 *     и предъявлением сходится по построению;
 *   • удостоверение в аутентификаторе страницы: `backupEligibility: false`,
 *     `backupState: false`, начальный счётчик `100·k`, где `k` — номер контекста
 *     браузера по порядку входа. Служба принимает утверждение, только если
 *     присланный счётчик строго больше хранимого, и хранит присланное:
 *     `100·k > 100·(k−1) + 1` при любом `k ≥ 1`, поэтому каждый контекст входит
 *     по построению, как бы аутентификатор ни прибавлял единицу.
 *
 * КАЖДЫЙ ШАГ УТВЕРЖДАЕТ СВОЙ ИСХОД — ответом, который получил сам; заведение и
 * снятие — исходом операции (`done` без `error`), а не её приёмом.
 *
 * УСЛОВИЕ П4 (§4). Происхождение браузера набора служба принимает только из
 * объявленного перечня. Отказ регистрации причиной «происхождение не в перечне»
 * — условие стенда, а не исход экрана: посев называет его «условие не создано»,
 * и сценарий не краснеет за чужую причину.
 *
 * ОСЬ ИСТОЧНИКА. Глаголы Ф7 не пишут ни в один счёт источника (N14): посев К
 * ключа оси не тратит. Обращения посева перепись страниц не видит — они идут
 * контекстом запросов.
 */

/** Происхождение браузера сценария — то, что встанет в клиентские данные. */
function originOf(baseURL: string | undefined): string {
  if (!baseURL) throw new Error("посев К: у проекта нет baseURL — происхождение браузера не названо");
  return new URL(baseURL).origin;
}

// ─── CBOR — ровно то подмножество, которое нужно объекту аттестации «none» ───

function cborHead(major: number, n: number): Buffer {
  if (n < 24) return Buffer.from([(major << 5) | n]);
  if (n < 0x100) return Buffer.from([(major << 5) | 24, n]);
  if (n < 0x10000) return Buffer.from([(major << 5) | 25, n >> 8, n & 0xff]);
  const b = Buffer.alloc(5);
  b[0] = (major << 5) | 26;
  b.writeUInt32BE(n, 1);
  return b;
}

type Cbor = number | string | Buffer | Array<[number | string, Cbor]>;

/** Кодирование CBOR: целое, байты, текст и отображение (в порядке, как подано). */
function cbor(value: Cbor): Buffer {
  if (typeof value === "number") return value >= 0 ? cborHead(0, value) : cborHead(1, -1 - value);
  if (typeof value === "string") {
    const text = Buffer.from(value, "utf8");
    return Buffer.concat([cborHead(3, text.length), text]);
  }
  if (Buffer.isBuffer(value)) return Buffer.concat([cborHead(2, value.length), value]);
  return Buffer.concat([cborHead(5, value.length), ...value.flatMap(([k, v]) => [cbor(k), cbor(v)])]);
}

/** Открытый ключ ES256 как COSE_Key: kty EC2, alg −7, кривая P-256, x, y. */
function coseKeyOf(publicKey: KeyObject): Buffer {
  const jwk = publicKey.export({ format: "jwk" });
  return cbor([
    [1, 2],
    [3, -7],
    [-1, 1],
    [-2, Buffer.from(jwk.x ?? "", "base64url")],
    [-3, Buffer.from(jwk.y ?? "", "base64url")],
  ]);
}

const FLAG_UP = 0x01;
const FLAG_UV = 0x04;
const FLAG_AT = 0x40;

// ─── посев К ─────────────────────────────────────────────────────────────────

export interface SeededAccessKey {
  userId: string;
  accessKeyId: string;
  /** Имя доверяющей стороны — из ответа выдачи испытания регистрации. */
  rpId: string;
  credentialId: Buffer;
  /** Закрытый ключ PKCS#8 DER — форма, которую принимает аутентификатор страницы. */
  privateKey: Buffer;
  /** Рукоятка человека — `user.id` испытания регистрации. */
  userHandle: Buffer;
  /** Проверяет ли аутентификатор пользователя (UV данных регистрации). */
  userVerification: boolean;
}

interface RegistrationChallenge {
  challenge?: string;
  rp?: { id?: string };
  user?: { id?: string };
}

/**
 * Завести ключ человеку посева глаголами Ф7 через край. Посев держит сессию
 * П-п; регистрация идёт сразу после неё — внутри окна свежести Ф7 Р5.
 */
export async function seedAccessKey(
  seed: Seed,
  baseURL: string | undefined,
  {
    userVerification,
    name,
    description,
  }: {
    userVerification: boolean;
    /** Имя и описание ключа — их задаёт сценарий (посев К′ приёмки F8, группа L); не названы — не посылаются. */
    name?: string;
    description?: string;
  },
): Promise<SeededAccessKey> {
  const origin = originOf(baseURL);
  const me = await seed.read(SESSION_IDENTITY);
  const who = lastIssued(seed, SESSION_IDENTITY).body as { user?: { id?: unknown } } | null;
  expect(me.status(), `посев К: ответ края о сессии посева — ${me.status()}`).toBe(200);
  const userId = typeof who?.user?.id === "string" ? who.user.id : "";
  expect(userId, "посев К: край не назвал человека посева").not.toBe("");

  const beginPath = `/iam/v1/users/${userId}/accessKeys:beginRegistration`;
  const begun = await seed.api.post(beginPath, { data: {} });
  const beginText = await begun.text();
  expect(begun.status(), `посев К: выдача испытания регистрации — ${begun.status()} ${beginText.slice(0, 300)}`).toBe(200);
  const ch = JSON.parse(beginText) as RegistrationChallenge;
  const challenge = Buffer.from(ch.challenge ?? "", "base64");
  const rpId = ch.rp?.id ?? "";
  const userHandle = Buffer.from(ch.user?.id ?? "", "base64");
  expect(
    { challenge: challenge.length > 0, rpId: rpId !== "", userHandle: userHandle.length > 0 },
    "посев К: испытание регистрации без испытания, имени доверяющей стороны или рукоятки",
  ).toEqual({ challenge: true, rpId: true, userHandle: true });

  const { publicKey, privateKey } = generateKeyPairSync("ec", { namedCurve: "P-256" });
  const credentialId = randomBytes(16);
  const counter = Buffer.alloc(4); // счётчик подписи регистрации — 0 (N16)
  const idLength = Buffer.alloc(2);
  idLength.writeUInt16BE(credentialId.length, 0);
  const authData = Buffer.concat([
    createHash("sha256").update(rpId).digest(),
    // UP = 1, UV — как у аутентификатора страницы, BE = 0, BS = 0, AT = 1.
    Buffer.from([FLAG_UP | (userVerification ? FLAG_UV : 0) | FLAG_AT]),
    counter,
    Buffer.alloc(16), // AAGUID: аттестация «none»
    idLength,
    credentialId,
    coseKeyOf(publicKey),
  ]);
  const attestationObject = cbor([
    ["fmt", "none"],
    ["attStmt", []],
    ["authData", authData],
  ]);
  const clientDataJSON = Buffer.from(
    JSON.stringify({ type: "webauthn.create", challenge: challenge.toString("base64url"), origin, crossOrigin: false }),
    "utf8",
  );

  const finishPath = `/iam/v1/users/${userId}/accessKeys`;
  const finished = await seed.api.post(finishPath, {
    data: {
      ...(name === undefined ? {} : { name }),
      ...(description === undefined ? {} : { description }),
      credential: {
        id: credentialId.toString("base64"),
        clientDataJson: clientDataJSON.toString("base64"),
        attestationObject: attestationObject.toString("base64"),
        discoverable: true,
      },
    },
  });
  if (finished.status() !== 200) {
    const text = await finished.text();
    if (text.includes("ORIGIN_NOT_ALLOWED")) {
      conditionNotCreated(
        `${FIXTURE_UNMET_PREFIX} (приёмка F8-S4, §4 П4) происхождение браузера набора ${origin} ` +
          "не входит в перечень происхождений ключа доступа на стенде — церемония ключа на нём не выполнима",
      );
    }
    expect(finished.status(), `посев К: регистрация ключа отвергнута — ${finished.status()} ${text.slice(0, 300)}`).toBe(200);
  }
  const op = await acceptedOperation(finished, "посев К: регистрация ключа");
  const done = await operationSucceeded(readerOf(seed), op, "посев К: регистрация ключа");
  // Ключ называет ответ операции (`RegisterAccessKeyResponse`) и её метаданные
  // (`RegisterAccessKeyMetadata`) — одно значение двумя полями контракта.
  const meta = ((done as { metadata?: unknown }).metadata ?? {}) as { accessKeyId?: unknown };
  const response = (done.response ?? {}) as { accessKey?: { id?: unknown } };
  const accessKeyId = String(response.accessKey?.id ?? meta.accessKeyId ?? "");
  expect(accessKeyId, "посев К: операция регистрации не назвала ключ").not.toBe("");

  return {
    userId,
    accessKeyId,
    rpId,
    credentialId,
    privateKey: privateKey.export({ format: "der", type: "pkcs8" }),
    userHandle,
    userVerification,
  };
}

/**
 * Посев К-снят: снять ключ глаголом Ф7 и дождаться исхода операции. Удостоверение
 * в аутентификаторе страницы при этом ОСТАЁТСЯ — служба о нём больше не знает.
 */
export async function revokeAccessKey(seed: Seed, key: SeededAccessKey): Promise<void> {
  const res = await seed.api.delete(`/iam/v1/users/${key.userId}/accessKeys/${key.accessKeyId}`);
  const op = await acceptedOperation(res, "посев К-снят: снятие ключа");
  await operationSucceeded(readerOf(seed), op, "посев К-снят: снятие ключа");
}

// ─── «ключ принят / не принят» — запись выпускающего (приёмка F8, группа L) ──

/** Удостоверение, которым проба собирает утверждение сама. */
export interface PresentableKey {
  credentialId: Buffer;
  /** PKCS#8 DER, P-256. */
  privateKey: Buffer;
  userHandle: Buffer;
  /** Последний счётчик подписи, который служба приняла либо хранит. */
  signCount: number;
}

/**
 * Предъявить ключ службе утверждением, собранным ПРОБОЙ (Ф7 §3.0): контекст
 * посева зовёт `accessKeys:beginAssertion` и `accessKeys:finishAssertion`, а
 * подпись ставит закрытый ключ удостоверения — посева К или аутентификатора
 * страницы (`WebAuthn.getCredentials`). Счётчик — строго больше хранимого
 * (F8-S4 N16): `signCount` плюс один; принятый счётчик запоминается. Исход
 * отдаётся как есть — «принят» и «не принят» судит вызывающий. Ось источника
 * глаголы Ф7 не тратят (N29), носителя не ставят.
 */
export async function presentAccessKey(
  seed: Seed,
  baseURL: string | undefined,
  key: PresentableKey,
): Promise<{ status: number; text: string }> {
  const begun = await seed.api.post("/iam/v1/accessKeys:beginAssertion", { data: {} });
  const beginText = await begun.text();
  expect(begun.status(), `проба: выдача испытания утверждения — ${begun.status()} ${beginText.slice(0, 300)}`).toBe(200);
  const ch = JSON.parse(beginText) as { challenge?: string; rpId?: string };
  const challenge = Buffer.from(ch.challenge ?? "", "base64");
  const rpId = ch.rpId ?? "";
  expect(
    { challenge: challenge.length > 0, rpId: rpId !== "" },
    "проба: испытание утверждения без испытания или имени доверяющей стороны",
  ).toEqual({ challenge: true, rpId: true });

  const counter = Buffer.alloc(4);
  counter.writeUInt32BE(key.signCount + 1, 0);
  // UP = 1, UV = 1 — как у аутентификатора страницы; BE = 0, BS = 0.
  const authData = Buffer.concat([createHash("sha256").update(rpId).digest(), Buffer.from([FLAG_UP | FLAG_UV]), counter]);
  const clientDataJSON = Buffer.from(
    JSON.stringify({
      type: "webauthn.get",
      challenge: challenge.toString("base64url"),
      origin: originOf(baseURL),
      crossOrigin: false,
    }),
    "utf8",
  );
  const signature = sign(
    "sha256",
    Buffer.concat([authData, createHash("sha256").update(clientDataJSON).digest()]),
    createPrivateKey({ key: key.privateKey, format: "der", type: "pkcs8" }),
  );
  const res = await seed.api.post("/iam/v1/accessKeys:finishAssertion", {
    data: {
      credential: {
        id: key.credentialId.toString("base64"),
        clientDataJson: clientDataJSON.toString("base64"),
        authenticatorData: authData.toString("base64"),
        signature: signature.toString("base64"),
        userHandle: key.userHandle.toString("base64"),
      },
    },
  });
  const text = await res.text();
  if (res.status() === 200) key.signCount += 1;
  return { status: res.status(), text };
}

// ─── аутентификатор страницы ─────────────────────────────────────────────────

export interface PageAuthenticator {
  cdp: CDPSession;
  authenticatorId: string;
}

/**
 * Виртуальный аутентификатор браузера страницы БЕЗ удостоверений — шаг 1 посева К
 * дословно (§6.0): `ctap2`, `internal`, резидентный ключ, проверка пользователя
 * по `userVerification`, флаги резервного копирования `false`. Удостоверение в
 * нём заводит либо посев (`authenticatorWithKey`), либо экран страницы.
 */
export async function pageAuthenticator(page: Page, userVerification: boolean): Promise<PageAuthenticator> {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("WebAuthn.enable", { enableUI: false });
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2",
      transport: "internal",
      hasResidentKey: true,
      hasUserVerification: userVerification,
      isUserVerified: userVerification,
      automaticPresenceSimulation: true,
      defaultBackupEligibility: false,
      defaultBackupState: false,
    },
  });
  return { cdp, authenticatorId };
}

/**
 * Виртуальный аутентификатор браузера страницы с удостоверением посева К —
 * ДО открытия страницы сценария; обращений страницы не порождает. `k` — номер
 * контекста браузера в сценарии по порядку входа (§6.0 шаг 3).
 */
export async function authenticatorWithKey(page: Page, key: SeededAccessKey, k = 1): Promise<PageAuthenticator> {
  const { cdp, authenticatorId } = await pageAuthenticator(page, key.userVerification);
  await cdp.send("WebAuthn.addCredential", {
    authenticatorId,
    credential: {
      credentialId: key.credentialId.toString("base64"),
      isResidentCredential: true,
      rpId: key.rpId,
      privateKey: key.privateKey.toString("base64"),
      userHandle: key.userHandle.toString("base64"),
      signCount: 100 * k,
      backupEligibility: false,
      backupState: false,
    },
  });
  return { cdp, authenticatorId };
}
