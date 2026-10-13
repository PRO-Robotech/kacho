// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createHash, createPrivateKey, generateKeyPairSync, randomBytes, sign, type KeyObject } from "node:crypto";
import { expect, type APIResponse, type CDPSession, type Page, type TestInfo } from "@playwright/test";
import { acceptedOperation, operationSucceeded, readerOf } from "./cloud-admin";
import { ORIGIN_NOT_ALLOWED } from "./access-key-texts";
import {
  LANE,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedSecondFactor,
  type Seed,
  type SeededHuman,
  type SeededSecondFactor,
} from "./ceremony-seed";
import { ConditionNotCreated, conditionNotCreated } from "./mail-receiver";
import { attachSourceAxisRecords } from "./source-axis-records";
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
 * УРОВЕНЬ «2» (приёмка kaname level-two-is-enrolled-at-level-two, Р2, Р3;
 * kaname#747). У трёх глаголов ключа — обоих шагов регистрации и снятия — пол
 * «2». Посев К поэтому заводит ключ сессией «2» путём, который приёмка оставила
 * законным: человек с паролем заводит код и подтверждает его — подтверждение
 * перевыпускает сессию посева уровнем «2» (`raiseSeedToLevelTwo`). Там, где
 * «Дано» сценария — человек без кода, код снимается запасным кодом после
 * регистрации (`dropSeedSecondFactor`); уровень сессии посева остаётся «2».
 * Отказ по уровню посев «условием не создано» НЕ называет: это исход продукта.
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

/** Ключ, чью регистрацию проба отправила, — до исхода операции. */
export type SubmittedAccessKey = Omit<SeededAccessKey, "accessKeyId">;

/**
 * Отправить результат церемонии регистрации глаголами Ф7 через край — без
 * суждения об ответе: «принят» и «отвергнут» судит вызывающий. Происхождение
 * в клиентских данных — АРГУМЕНТ: положительный посев кладёт своё
 * (`seedAccessKey`), проба привязки — чужое, и только это поле расходится с
 * близнецом. Испытание выдаётся заново на каждый вызов; человек — тот, чью
 * сессию держит посев.
 */
export async function submitRegistration(
  seed: Seed,
  origin: string,
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
): Promise<{ response: APIResponse; key: SubmittedAccessKey }> {
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

  const response = await seed.api.post(`/iam/v1/users/${userId}/accessKeys`, {
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
  return {
    response,
    key: {
      userId,
      rpId,
      credentialId,
      privateKey: privateKey.export({ format: "der", type: "pkcs8" }),
      userHandle,
      userVerification,
    },
  };
}

/**
 * Завести ключ человеку посева глаголами Ф7 через край. Посев держит сессию
 * П-п уровня «2» (`raiseSeedToLevelTwo` — «Дано» вызывающего); регистрация идёт
 * сразу после неё — внутри окна свежести Ф7 Р5.
 */
export async function seedAccessKey(
  seed: Seed,
  baseURL: string | undefined,
  options: {
    userVerification: boolean;
    /** Имя и описание ключа — их задаёт сценарий (посев К′ приёмки F8, группа L); не названы — не посылаются. */
    name?: string;
    description?: string;
  },
): Promise<SeededAccessKey> {
  const origin = originOf(baseURL);
  const { response: finished, key } = await submitRegistration(seed, origin, options);
  if (finished.status() !== 200) {
    const text = await finished.text();
    if (text.includes(ORIGIN_NOT_ALLOWED)) {
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

  return { ...key, accessKeyId };
}

/**
 * Поднять сессию посева до «2» (kaname#747, Р2): завести и подтвердить код
 * (`seedSecondFactor`). Шаг утверждает уровень из ответа подтверждения: без «2»
 * регистрация и снятие ключа упрутся в пол, и падение назвало бы виновником
 * чужой шаг.
 */
export async function raiseSeedToLevelTwo(seed: Seed): Promise<SeededSecondFactor> {
  const factor = await seedSecondFactor(seed);
  expect(factor.assuranceLevel, "посев К: подтверждение второго фактора обязано поднять сессию посева до «2»").toBe(
    "2",
  );
  return factor;
}

/**
 * Снять код посева запасным кодом глаголом полосы (Ф12-28) — «Дано» человека без
 * кода после регистрации ключа. Снятие предъявляет код в той же сессии, поэтому
 * её уровень остаётся «2», и снятие ключа тем же посевом проходит пол. Прочие
 * сессии человека снятие гасит — посев зовёт его до переноса носителя в браузер.
 * Утверждается исход и уровень из ответа края о сессии.
 */
export async function dropSeedSecondFactor(seed: Seed, factor: SeededSecondFactor): Promise<void> {
  const code = factor.backupCodes[0];
  const removed = await seed.submit(LANE.remove, "second-factor", { method: "lookup_secret", code });
  expect(
    removed.status(),
    `посев К: снятие кода запасным кодом отвергнуто — ${removed.status()} ${JSON.stringify(lastIssued(seed, LANE.remove).body)}`,
  ).toBe(200);
  const me = await seed.read(SESSION_IDENTITY);
  const view = lastIssued(seed, SESSION_IDENTITY).body as { session?: { assuranceLevel?: unknown } } | null;
  expect(
    { status: me.status(), level: String(view?.session?.assuranceLevel ?? "") },
    "посев К: после снятия кода сессия посева обязана остаться «2» (запасной код предъявлен в ней)",
  ).toEqual({ status: 200, level: "2" });
}

/**
 * Код человеку — в ОТДЕЛЬНОМ контексте посева тем же адресом и паролем: сессия,
 * уже перенесённая в браузер, остаётся прежней, уровня «1» (подтверждение кода
 * прочих сессий не гасит). Возвращает запасные коды: код по времени истрачен
 * подтверждением, и повышение в браузере идёт запасным.
 */
export async function secondFactorAside(testInfo: TestInfo, human: SeededHuman): Promise<string[]> {
  const aside = await newSeed(testInfo);
  try {
    const res = await aside.submit(LANE.login, "login", { email: human.email, password: human.password });
    expect(
      res.status(),
      `посев 2Ф: вход ${human.email} отвергнут — ${res.status()} ${JSON.stringify(lastIssued(aside, LANE.login).body)}`,
    ).toBe(200);
    return (await seedSecondFactor(aside)).backupCodes;
  } finally {
    await aside.dispose();
  }
}

/**
 * ТЕЛО ПРОБЫ-ПРЕДУСЛОВИЯ П4 (приёмка F8-S4, §4 П4, DoD п. 6): служба принимает
 * происхождение браузера набора для ключей доступа. Зовут его предусловие
 * (`preconditions/access-key-origin.precondition.ts`, «Дано» — посев П-п) и
 * канарейка его самопроверки (`scripts/access-key-origin-canary/`, «Дано» —
 * режим края петли): судится один и тот же код.
 *
 * «Не создано» — ТОЛЬКО отказ, помеченный посевом как несозданное условие
 * (`ConditionNotCreated`: происхождение не в перечне). Любой иной отказ —
 * возможный дефект службы, о нём обязаны сказать пробы ключа красным: условие
 * его пропускает и кладёт текст вложением. Ключ, заведённый условием,
 * снимается тем же заходом. Запись расхода окна источника (регистрации посева,
 * обращения полосы) сдаётся тем же вложением, что у проб сценариев (DoD п. 10).
 */
export async function accessKeyOriginCondition(
  testInfo: TestInfo,
  given: (seed: Seed) => Promise<void>,
): Promise<string> {
  const seed = await newSeed(testInfo);
  let outcome = "";
  try {
    await given(seed);
    const key = await seedAccessKey(seed, testInfo.project.use.baseURL, { userVerification: true });
    outcome = "ключ заведён — происхождение принято";
    await revokeAccessKey(seed, key);
  } catch (err) {
    if (err instanceof ConditionNotCreated) throw err;
    // Отказ не об условии: пробы ключа исполнятся и скажут о нём своим вердиктом.
    outcome = `посев отказал не по условию — судят пробы: ${err instanceof Error ? err.message : String(err)}`;
  } finally {
    await testInfo.attach("исход посева К", { body: outcome || "условие не создано", contentType: "text/plain" });
    await seed.dispose();
    await attachSourceAxisRecords(testInfo);
  }
  return outcome;
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
 * Отклонение утверждения от честного — РОВНО одно поле против близнеца (Ф7-07,
 * Ф7-10/11). Не названо — утверждение честное. Ветки «условие не создано» здесь
 * нет и быть не должно: отказ на отклонённом утверждении — исход продукта,
 * который судит проба.
 */
export interface AssertionDeviation {
  /** Подписать ДРУГИМ закрытым ключом (PKCS#8 DER, P-256), а не ключом удостоверения. */
  signer?: Buffer;
  /** Изменить один байт данных аутентификатора ПОСЛЕ подписи (старший байт счётчика). */
  alterSignedData?: boolean;
  /** Происхождение в клиентских данных вместо происхождения браузера набора. */
  origin?: string;
}

/** Что проба отправила — чтобы утверждать, что ответ этих байтов не повторяет. */
export interface SentAssertion {
  credentialId: string;
  clientDataJson: string;
  authenticatorData: string;
  signature: string;
}

/**
 * Предъявить ключ службе утверждением, собранным ПРОБОЙ (Ф7 §3.0): контекст
 * посева зовёт `accessKeys:beginAssertion` и `accessKeys:finishAssertion`, а
 * подпись ставит закрытый ключ удостоверения — посева К или аутентификатора
 * страницы (`WebAuthn.getCredentials`). Счётчик — строго больше хранимого
 * (F8-S4 N16): `signCount` плюс один; принятый счётчик запоминается. Исход
 * отдаётся как есть — «принят» и «не принят» судит вызывающий. Ось источника
 * глаголы Ф7 не тратят (N29), носителя не ставят. `deviation` — отклонение
 * пробы привязки (Ф7-07, Ф7-10/11); умолчание — честное утверждение.
 */
export async function presentAccessKey(
  seed: Seed,
  baseURL: string | undefined,
  key: PresentableKey,
  deviation: AssertionDeviation = {},
): Promise<{ status: number; text: string; sent: SentAssertion }> {
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
      origin: deviation.origin ?? originOf(baseURL),
      crossOrigin: false,
    }),
    "utf8",
  );
  const signature = sign(
    "sha256",
    Buffer.concat([authData, createHash("sha256").update(clientDataJSON).digest()]),
    createPrivateKey({ key: deviation.signer ?? key.privateKey, format: "der", type: "pkcs8" }),
  );
  // Старший байт счётчика: подписанные данные меняются, а счётчик остаётся
  // строго больше хранимого — отказ может дать только сверка подписи.
  if (deviation.alterSignedData) authData[33] ^= 0x80;
  const sent: SentAssertion = {
    credentialId: key.credentialId.toString("base64"),
    clientDataJson: clientDataJSON.toString("base64"),
    authenticatorData: authData.toString("base64"),
    signature: signature.toString("base64"),
  };
  const res = await seed.api.post("/iam/v1/accessKeys:finishAssertion", {
    data: {
      credential: {
        id: sent.credentialId,
        clientDataJson: sent.clientDataJson,
        authenticatorData: sent.authenticatorData,
        signature: sent.signature,
        userHandle: key.userHandle.toString("base64"),
      },
    },
  });
  const text = await res.text();
  if (res.status() === 200) key.signCount += 1;
  return { status: res.status(), text, sent };
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
