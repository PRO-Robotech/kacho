// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createHash, createPrivateKey, sign } from "node:crypto";
import { expect, type APIResponse, type TestInfo } from "@playwright/test";
import { acceptedOperation, cloudAdminAtLevelTwo, operationSucceeded, readerOf, resetOwnFactor } from "./cloud-admin";
import { dropSeedSecondFactor, raiseSeedToLevelTwo, seedAccessKey, type SeededAccessKey } from "./access-key-seed";
import {
  LANE,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  type Seed,
  type SeededHuman,
} from "./ceremony-seed";
import { runTag, test } from "./fixtures";

/**
 * Вход ключом доступа без пароля и уровень уверенности, который он даёт, —
 * сквозь край платформы (приёмка Ф13 `passwordless-login-with-access-key.md`,
 * APPROVED; приёмка Ф11 `assurance-level-is-declared-by-our-session.md`, APPROVED).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ
 *
 *   Ф13-10 — ключ без проверки пользователя, BE = 0 → уровень «2»;
 *   Ф13-11 — ТОТ ЖЕ ключ с проверкой пользователя → «3» (пара с Ф13-10: без неё
 *            «3» неотличимо от «полоса всегда отвечает «3»»);
 *   Ф13-12 — проверка выполнена, ключ допускает резервное копирование → «2», не «3»;
 *   Ф13-15 — заведённый второй фактор: кода не требуется, уровень по ключу,
 *            состояние фактора — «заведён»;
 *   Ф13-06 «в» — происхождение вне перечня на полосе ВХОДА: единый отказ (ветвь «а»,
 *            подделанная подпись на полосе входа, — `access-key-login.spec.ts`,
 *            F7-07-console; здесь не повторяется);
 *   Ф13-06 «и» — владелец ключа заблокирован: единый отказ; разблокирован — входит.
 *
 * Уровень наблюдается ТРЕМЯ местами, и каждое — своё утверждение: копия в ответе
 * входа, ответ края о сессии (`GET /iam/v1/auth/me`) и ПОЛ КРАЯ — глагол с полом
 * «2» (удаление группы) проходит по носителю ключа. Пол утверждается ПАРОЙ: тот же
 * глагол по носителю пароля того же человека («1») отвергается вызовом повышения.
 * Одно «проходит» зеленело бы на крае, где пола нет вовсе (Ф11 предикат #1280).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ УТВЕРЖДЕНИЕ СОБИРАЕТ ПРОБА, А НЕ БРАУЗЕР
 *
 * Флаги утверждения (UV, BE) — предмет сценариев, а браузер при входе без имени
 * без проверки пользователя церемонию отвергает сам (F8S4-03): уровень «2» по
 * ключу без проверки на экране не наблюдаем. Поэтому глаголы полосы входа
 * (`/iam/v1/auth/access-key/begin` и `/login`) зовутся контекстом посева с
 * признаком формы своего вида, а утверждение подписывает закрытый ключ посева К —
 * тем же путём, каким его подписал бы аутентификатор. Экранный путь несут
 * `access-key-login.spec.ts` и `access-key-lifecycle.spec.ts`.
 *
 * ДОЛЯ S4 ОСИ ИСТОЧНИКА (F8S4-15, `ceremony-budget.ts`). Файл тратит её ДЕСЯТЬЮ
 * попытками: восемь выдач испытания (Ф13-10·11 — 2, Ф13-12 — 1, Ф13-15 — 1,
 * Ф13-06 «в» — 2, Ф13-06 «и» — 2) и два единых отказа входа ключом («в», «и»).
 *
 * ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО: ожидания временем (ждётся условие); общего человека
 * (каждый сценарий сеет своего); подставленных ответов (каждый — ответ службы через
 * край).
 */

const ACCESS_KEY_BEGIN = "/iam/v1/auth/access-key/begin";
const ACCESS_KEY_LOGIN = "/iam/v1/auth/access-key/login";

/** Тело отказа входа — одно на все причины (Ф13 Р7, Ф3-02). */
const AUTHENTICATION_FAILED = { code: 16, message: "authentication failed", details: [] };

const FLAG_UP = 0x01;
const FLAG_UV = 0x04;
const FLAG_BE = 0x08;

function originOf(testInfo: TestInfo): string {
  const base = testInfo.project.use.baseURL;
  if (!base) throw new Error("проба: у проекта нет baseURL — происхождение браузера не названо");
  return new URL(base).origin;
}

/** Байты из base64 либо base64url — форма поля испытания на проводе. */
function bytesOfWire(text: string): Buffer {
  return Buffer.from(text.replace(/-/g, "+").replace(/_/g, "/"), "base64");
}

interface KeyHolder {
  human: SeededHuman;
  key: SeededAccessKey;
  /** Последний счётчик подписи, который служба приняла либо хранит. */
  signCount: number;
  /** Аккаунт человека — область глагола с полом «2». */
  accountId: string;
}

/**
 * «Дано»: человек с паролем и ключом. Ключ заводится сессией «2» (пол kaname#747);
 * код после этого снимается, если сценарий его не держит (`secondFactor`).
 */
async function given(
  testInfo: TestInfo,
  scenario: string,
  opts: { userVerification: boolean; backupEligible?: boolean; secondFactor?: boolean },
): Promise<KeyHolder> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedConfirmedHuman(seed, seedAddress(scenario));
    const factor = await raiseSeedToLevelTwo(seed);
    const key = await seedAccessKey(seed, testInfo.project.use.baseURL, {
      userVerification: opts.userVerification,
      backupEligible: opts.backupEligible ?? false,
    });
    if (!opts.secondFactor) await dropSeedSecondFactor(seed, factor);
    const accountId = await ownAccount(seed);
    return { human, key, signCount: 0, accountId };
  } finally {
    await seed.dispose();
  }
}

/** Аккаунт человека: заводится сам; ждётся условие, значение берётся отдельным чтением. */
async function ownAccount(seed: Seed): Promise<string> {
  const read = async () => {
    const res = await seed.api.get("/iam/v1/accounts?pageSize=1000");
    if (!res.ok()) return "";
    const body = (await res.json()) as { accounts?: Array<{ id: string }> };
    return body.accounts?.[0]?.id ?? "";
  };
  await expect.poll(read, { message: "аккаунт человека не появился — «Дано» не собрано", timeout: 45_000 }).not.toBe("");
  const accountId = await read();
  expect(accountId, "аккаунт прочитан опросом, но повторное чтение его не отдало — «Дано» не собрано").not.toBe("");
  return accountId;
}

interface KeyLogin {
  seed: Seed;
  res: APIResponse;
  text: string;
}

/**
 * Вход ключом глаголами полосы: выдача испытания, утверждение, предъявление.
 * Флаги и происхождение — АРГУМЕНТ: сценарий меняет ровно одно против близнеца.
 * Исход НЕ утверждается — его судит вызывающий; утверждается лишь выдача
 * испытания (шаг «Дано» самого входа).
 */
async function keyLogin(
  testInfo: TestInfo,
  holder: KeyHolder,
  flags: { userVerification: boolean },
  deviation: { origin?: string } = {},
): Promise<KeyLogin> {
  const seed = await newSeed(testInfo);
  const begun = await seed.submit(ACCESS_KEY_BEGIN, "access-key-begin", {});
  const beginText = await begun.text();
  expect(begun.status(), `вход ключом: выдача испытания — ${begun.status()} ${beginText.slice(0, 300)}`).toBe(200);
  const pk = (JSON.parse(beginText) as { publicKey?: { challenge?: string; rpId?: string } }).publicKey ?? {};
  const challenge = bytesOfWire(pk.challenge ?? "");
  const rpId = pk.rpId ?? "";
  expect(
    { challenge: challenge.length > 0, rpId: rpId !== "" },
    "вход ключом: испытание без испытания или имени доверяющей стороны",
  ).toEqual({ challenge: true, rpId: true });

  const counter = Buffer.alloc(4);
  counter.writeUInt32BE(holder.signCount + 1, 0);
  const flagByte =
    FLAG_UP | (flags.userVerification ? FLAG_UV : 0) | (holder.key.backupEligible ? FLAG_BE : 0);
  const authData = Buffer.concat([createHash("sha256").update(rpId).digest(), Buffer.from([flagByte]), counter]);
  const clientDataJSON = Buffer.from(
    JSON.stringify({
      type: "webauthn.get",
      challenge: challenge.toString("base64url"),
      origin: deviation.origin ?? originOf(testInfo),
      crossOrigin: false,
    }),
    "utf8",
  );
  const signature = sign(
    "sha256",
    Buffer.concat([authData, createHash("sha256").update(clientDataJSON).digest()]),
    createPrivateKey({ key: holder.key.privateKey, format: "der", type: "pkcs8" }),
  );
  const id = holder.key.credentialId.toString("base64url");
  const res = await seed.submit(ACCESS_KEY_LOGIN, "access-key-login", {
    credential: {
      id,
      rawId: id,
      type: "public-key",
      response: {
        clientDataJSON: clientDataJSON.toString("base64url"),
        authenticatorData: authData.toString("base64url"),
        signature: signature.toString("base64url"),
        userHandle: holder.key.userHandle.toString("base64url"),
      },
    },
  });
  const text = await res.text();
  if (res.status() === 200) holder.signCount += 1;
  return { seed, res, text };
}

/** Вход принят; возвращается уровень из копии в ответе входа. */
function acceptedLevel(login: KeyLogin, what: string): string {
  expect(login.res.status(), `${what}: вход ключом отвергнут — ${login.res.status()} ${login.text.slice(0, 300)}`).toBe(
    200,
  );
  const body = JSON.parse(login.text) as { session?: { assuranceLevel?: unknown } };
  return String(body.session?.assuranceLevel ?? "");
}

/** Уровень из ответа края о сессии — по носителю этого входа. */
async function edgeLevel(seed: Seed): Promise<string> {
  const me = await seed.read(SESSION_IDENTITY);
  const view = lastIssued(seed, SESSION_IDENTITY).body as { session?: { assuranceLevel?: unknown } } | null;
  expect(me.status(), `ответ края о сессии — ${me.status()} ${JSON.stringify(view)}`).toBe(200);
  return String(view?.session?.assuranceLevel ?? "");
}

/** Группа человека, заведённая на доступном уровне (пол «1»): предмет глагола с полом «2». */
async function ownGroup(seed: Seed, holder: KeyHolder, scenario: string): Promise<string> {
  const created = await seed.api.post("/iam/v1/groups", {
    data: { accountId: holder.accountId, name: `e2e-${scenario.toLowerCase()}-${runTag()}`, description: "пол «2»" },
  });
  expect(created.status(), `заведение группы не удалось: ${await created.text()}`).toBe(200);
  const op = (await created.json()) as { metadata?: { groupId?: string } };
  const groupId = op.metadata?.groupId ?? "";
  expect(groupId, "операция не назвала идентификатор группы").not.toBe("");
  // Своя свежая группа читается: право материализовано, идентификатор — не фантом.
  await expect
    .poll(async () => (await seed.api.get(`/iam/v1/groups/${groupId}`)).status(), {
      message: "своя свежая группа не читается: право не материализовалось либо id — фантом",
      timeout: 45_000,
    })
    .toBe(200);
  return groupId;
}

/** Вход паролем того же человека — носитель уровня «1». */
async function passwordSession(testInfo: TestInfo, holder: KeyHolder): Promise<Seed> {
  const seed = await newSeed(testInfo);
  const res = await seed.submit(LANE.login, "login", { email: holder.human.email, password: holder.human.password });
  expect(res.status(), `вход паролем — ${res.status()} ${JSON.stringify(lastIssued(seed, LANE.login).body)}`).toBe(200);
  expect(await edgeLevel(seed), "вход паролем обязан дать «1»").toBe("1");
  return seed;
}

/**
 * ПОЛ КРАЯ ПАРОЙ: глагол с полом «2» (удаление своей группы) по носителю пароля —
 * вызов повышения (401, `insufficient_user_authentication`), по носителю ключа —
 * проходит. Глагол один, носитель — единственное различие.
 */
async function floorTwoPair(testInfo: TestInfo, holder: KeyHolder, keySeed: Seed, scenario: string) {
  const pw = await passwordSession(testInfo, holder);
  try {
    const groupId = await ownGroup(pw, holder, scenario);
    const denied = await pw.api.delete(`/iam/v1/groups/${groupId}`);
    expect(denied.status(), "глагол с полом «2» по носителю пароля («1») обязан быть отвергнут краем").toBe(401);
    expect(
      denied.headers()["www-authenticate"] ?? "",
      "отказ по полу обязан назвать причину вызовом повышения",
    ).toContain("insufficient_user_authentication");
    // Край держит вердикт о сессии недолгим кэшем — ждётся условие, не время.
    await expect
      .poll(async () => (await keySeed.api.delete(`/iam/v1/groups/${groupId}`)).status(), {
        message: "глагол с полом «2» не прошёл по носителю ключа — уровень ключа край не принял",
        timeout: 60_000,
      })
      .toBe(200);
  } finally {
    await pw.dispose();
  }
}

/** Единый отказ входа: 401, тело побайтово Ф3-02, без носителя. */
function expectSingleRefusal(login: KeyLogin, what: string) {
  expect(login.res.status(), `${what}: отказ входа ключом обязан быть 401`).toBe(401);
  expect(login.text, `${what}: тело отказа — побайтово одно на все причины`).toBe(JSON.stringify(AUTHENTICATION_FAILED));
  expect(login.res.headers()["set-cookie"] ?? "", `${what}: отказ выдал носитель сессии`).toBe("");
}

// ═══ Полоса «уровень» — Ф11-03…05 под номерами Ф13 ═══════════════════════════

test("Ф13-10 · Ф13-11 · один ключ: без проверки пользователя «2», с проверкой «3»; пол «2» пройден обоими", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #1282 — Ф13-10 (= Ф11-03) и Ф13-11 (= Ф11-04): уровень — свойство
  // утверждения, а не способа; единственное различие двух входов — бит UV.
  // Пол края парой — предикат #1280.
  test.setTimeout(300_000);
  const holder = await given(testInfo, "Ф13-10", { userVerification: true });
  const two = await keyLogin(testInfo, holder, { userVerification: false });
  try {
    expect(acceptedLevel(two, "Ф13-10"), "Ф13-10: копия уровня в ответе входа").toBe("2");
    expect(await edgeLevel(two.seed), "Ф13-10: ответ края о сессии").toBe("2");
    await floorTwoPair(testInfo, holder, two.seed, "Ф13-10");
  } finally {
    await two.seed.dispose();
  }
  const three = await keyLogin(testInfo, holder, { userVerification: true });
  try {
    expect(acceptedLevel(three, "Ф13-11"), "Ф13-11: копия уровня в ответе входа").toBe("3");
    expect(await edgeLevel(three.seed), "Ф13-11: ответ края о сессии").toBe("3");
    await floorTwoPair(testInfo, holder, three.seed, "Ф13-11");
  } finally {
    await three.seed.dispose();
  }
});

test("Ф13-12 · проверка выполнена, ключ допускает резервное копирование: уровень «2», не «3»", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #1282 — Ф13-12 (= Ф11-05): единственное отличие от Ф13-11 — BE = 1.
  test.setTimeout(240_000);
  const holder = await given(testInfo, "Ф13-12", { userVerification: true, backupEligible: true });
  const login = await keyLogin(testInfo, holder, { userVerification: true });
  try {
    expect(acceptedLevel(login, "Ф13-12"), "копия уровня в ответе входа").toBe("2");
    expect(await edgeLevel(login.seed), "ответ края о сессии").toBe("2");
    await floorTwoPair(testInfo, holder, login.seed, "Ф13-12");
  } finally {
    await login.seed.dispose();
  }
});

test("Ф13-15 · заведённый второй фактор: вход ключом без кода, уровень по ключу, фактор «заведён»", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #1282 — Ф13-15: состояние фактора и множество предъявленного — разные факты.
  test.setTimeout(240_000);
  const holder = await given(testInfo, "Ф13-15", { userVerification: true, secondFactor: true });
  const login = await keyLogin(testInfo, holder, { userVerification: true });
  try {
    expect(acceptedLevel(login, "Ф13-15"), "уровень по флагам ключа — как без фактора").toBe("3");
    const sf = await login.seed.read(LANE.secondFactor);
    const state = lastIssued(login.seed, LANE.secondFactor).body as { totp?: { enrolled?: unknown } } | null;
    expect(sf.status(), `состояние второго фактора — ${JSON.stringify(state)}`).toBe(200);
    expect(state?.totp?.enrolled, "из сессии ключа фактор обязан читаться «заведён»").toBe(true);
    await floorTwoPair(testInfo, holder, login.seed, "Ф13-15");
    const codePaths = [LANE.stepUp, LANE.confirm];
    expect(
      login.seed.issued.filter((c) => (codePaths as readonly string[]).includes(c.path)).map((c) => c.path),
      "пол «2» пройден без единого предъявления кода",
    ).toEqual([]);
  } finally {
    await login.seed.dispose();
  }
});

// ═══ Единый отказ на полосе входа: происхождение и блокировка ════════════════

test("Ф13-06 «в» · происхождение вне перечня на полосе входа: единый отказ; своё происхождение входит", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #1282 — Ф13-06 ветвь «в»; близнец отличается одним полем — origin клиентских данных.
  test.setTimeout(240_000);
  const holder = await given(testInfo, "Ф13-06в", { userVerification: true });
  const foreign = await keyLogin(testInfo, holder, { userVerification: true }, { origin: "https://foreign.invalid" });
  try {
    expectSingleRefusal(foreign, "чужое происхождение");
  } finally {
    await foreign.seed.dispose();
  }
  const own = await keyLogin(testInfo, holder, { userVerification: true });
  try {
    expect(acceptedLevel(own, "своё происхождение"), "близнец своего происхождения входит").toBe("3");
  } finally {
    await own.seed.dispose();
  }
});

test("Ф13-06 «и» · владелец ключа заблокирован: единый отказ; разблокирован — входит", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #1282 — Ф13-06 ветвь «и»: блокировку зовёт распорядитель уровня «2».
  test.setTimeout(300_000);
  const holder = await given(testInfo, "Ф13-06и", { userVerification: true });
  const admin = await cloudAdminAtLevelTwo(testInfo);
  try {
    const block = async (verb: "block" | "unblock") => {
      const res = await admin.seed.api.post(`/iam/v1/users/${holder.key.userId}:${verb}`, { data: {} });
      const op = await acceptedOperation(res, `распорядитель: ${verb}`);
      await operationSucceeded(readerOf(admin.seed), op, `распорядитель: ${verb}`);
    };
    await block("block");
    const blocked = await keyLogin(testInfo, holder, { userVerification: true });
    try {
      expectSingleRefusal(blocked, "владелец заблокирован");
    } finally {
      await blocked.seed.dispose();
    }
    await block("unblock");
    const back = await keyLogin(testInfo, holder, { userVerification: true });
    try {
      expect(acceptedLevel(back, "разблокирован"), "разблокированный человек входит тем же ключом").toBe("3");
    } finally {
      await back.seed.dispose();
    }
  } finally {
    // Фактор администратора, заведённый ради пола «2», снимается тем же прогоном.
    await resetOwnFactor(admin, testInfo);
  }
});
