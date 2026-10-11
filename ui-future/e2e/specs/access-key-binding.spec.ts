// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { generateKeyPairSync, randomBytes } from "node:crypto";
import { expect, type TestInfo } from "@playwright/test";
import {
  presentAccessKey,
  revokeAccessKey,
  seedAccessKey,
  submitRegistration,
  type AssertionDeviation,
  type PresentableKey,
  type SeededAccessKey,
  type SentAssertion,
} from "./access-key-seed";
import { newSeed, seedAddress, seedConfirmedHuman, type Seed } from "./ceremony-seed";
import { test } from "./fixtures";

/**
 * Привязка ключа доступа через край (kacho#1273, предикат снятия пп. 2 и 3;
 * приёмка kaname `access-keys-are-ours.md`, Ф7-07, Ф7-09, Ф7-10/11, Ф7-44).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ И ПОЧЕМУ ЗДЕСЬ
 *
 * Служба держит те же свойства своими пробами Ф7 на собственном фронте. Здесь
 * утверждается то, что видит человек консоли: край пропускает глаголы Ф7 и
 * отдаёт их отказ как есть — подделка и чужое происхождение отвергнуты ТЕМ ЖЕ
 * ответом, что неизвестный ключ, а сам ответ не повторяет присланных байтов.
 * Край, который принял бы утверждение сам или переписал отказ в различимый,
 * эти пробы роняют.
 *
 * ОДНО ПОЛЕ ПРОТИВ БЛИЗНЕЦА. Каждое отрицание отличается от честного
 * утверждения (регистрации) ровно одним полем — подписантом, байтом
 * подписанных данных, происхождением (`AssertionDeviation`,
 * `submitRegistration`). Честный близнец исполняется в том же сценарии ДО
 * отрицаний: без него отказ неотличим от неподнятого края.
 *
 * УСЛОВИЕ ПРОГОНА (§4 П4) — только у положительного посева: своё
 * происхождение не принято — «не выполнилось». У отрицаний ветки «условие не
 * создано» нет: их отказ — исход продукта.
 *
 * Ключ, заведённый сценарием, снимается тем же сценарием, в том числе на
 * падении. Ожидания временем нет.
 */

const NOT_ACCEPTED = "access key assertion is not accepted";
const ORIGIN_NOT_IN_LIST = "credential origin is not in the declared list";

/** Тело отказа края — google.rpc.Status. */
interface Refusal {
  code?: unknown;
  message?: unknown;
  details?: unknown;
}

function originOf(testInfo: TestInfo): string {
  const base = testInfo.project.use.baseURL;
  if (!base) throw new Error("у проекта нет baseURL — происхождение браузера не названо");
  return new URL(base).origin;
}

/**
 * Чужие происхождения: тот же узел на соседнем порту (похожее) и заведомо
 * чужой узел. Оба отличимы от происхождения набора по построению.
 */
function foreignOrigins(testInfo: TestInfo): string[] {
  const own = new URL(originOf(testInfo));
  const port = Number(own.port || (own.protocol === "https:" ? 443 : 80));
  const lookalike = new URL(own.toString());
  lookalike.port = String(port === 65535 ? port - 1 : port + 1);
  return [lookalike.origin, "https://origin-not-declared.kacho.invalid"];
}

function otherPrivateKey(): Buffer {
  return generateKeyPairSync("ec", { namedCurve: "P-256" }).privateKey.export({ format: "der", type: "pkcs8" });
}

/** Человек сценария со своим ключом; ключ снимается и на падении. */
async function withKeyHolder(
  testInfo: TestInfo,
  scenario: string,
  body: (seed: Seed, key: SeededAccessKey & PresentableKey) => Promise<void>,
): Promise<void> {
  const seed = await newSeed(testInfo);
  let key: (SeededAccessKey & PresentableKey) | undefined;
  try {
    await seedConfirmedHuman(seed, seedAddress(scenario));
    key = { ...(await seedAccessKey(seed, testInfo.project.use.baseURL, { userVerification: true })), signCount: 0 };
    await body(seed, key);
  } finally {
    try {
      if (key) await revokeAccessKey(seed, key);
    } finally {
      await seed.dispose();
    }
  }
}

async function present(seed: Seed, testInfo: TestInfo, key: PresentableKey, deviation?: AssertionDeviation) {
  return presentAccessKey(seed, testInfo.project.use.baseURL, key, deviation);
}

async function expectAccepted(seed: Seed, testInfo: TestInfo, key: SeededAccessKey & PresentableKey, what: string) {
  const got = await present(seed, testInfo, key);
  expect(got.status, `${what}: край не принял честное утверждение — ${got.status} ${got.text.slice(0, 300)}`).toBe(200);
  expect((JSON.parse(got.text) as { userId?: unknown }).userId, `${what}: ответ не называет человека ключа`).toBe(
    key.userId,
  );
}

/** Отказ утверждения: тот же статус и то же тело, что у неизвестного ключа, без эха присланного. */
function expectSingleRefusal(
  got: { status: number; text: string; sent: SentAssertion },
  unknown: { status: number; body: Refusal },
  what: string,
) {
  expect(got.status, `${what}: край ответил ${got.status} ${got.text.slice(0, 300)}`).toBe(unknown.status);
  expect(JSON.parse(got.text) as Refusal, `${what}: отказ различим с отказом неизвестному ключу`).toEqual(unknown.body);
  for (const [field, value] of Object.entries(got.sent)) {
    expect(got.text.includes(value), `${what}: ответ повторяет присланное поле ${field}`).toBe(false);
  }
}

/** Отказ неизвестному ключу — эталон единого отказа (Ф7-09), снятый тем же стендом. */
async function unknownKeyRefusal(seed: Seed, testInfo: TestInfo, key: SeededAccessKey) {
  const stranger: PresentableKey = {
    credentialId: randomBytes(16),
    privateKey: otherPrivateKey(),
    userHandle: key.userHandle,
    signCount: 0,
  };
  const got = await present(seed, testInfo, stranger);
  expect(got.status, `неизвестный ключ: край ответил ${got.status} ${got.text.slice(0, 300)}`).toBe(400);
  const body = JSON.parse(got.text) as Refusal;
  expect({ code: body.code, message: body.message }, "неизвестный ключ: отказ не единый отказ утверждения").toEqual({
    code: 3,
    message: NOT_ACCEPTED,
  });
  return { status: got.status, body };
}

test("F7-07-edge · подделанная подпись отвергается краем тем же отказом, что неизвестный ключ; честная подпись того же ключа принята", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #1273 — предикат п.2: подпись другим закрытым ключом и изменённые
  // после подписи данные аутентификатора; близнец — честное утверждение.
  await withKeyHolder(testInfo, "F7-07-edge", async (seed, key) => {
    await expectAccepted(seed, testInfo, key, "честный близнец");
    const unknown = await unknownKeyRefusal(seed, testInfo, key);
    expectSingleRefusal(
      await present(seed, testInfo, key, { signer: otherPrivateKey() }),
      unknown,
      "подпись другим закрытым ключом",
    );
    expectSingleRefusal(
      await present(seed, testInfo, key, { alterSignedData: true }),
      unknown,
      "данные аутентификатора изменены после подписи",
    );
    await expectAccepted(seed, testInfo, key, "честный близнец после отказов");
  });
});

test("F7-10-edge · утверждение с чужим происхождением отвергается краем тем же отказом, что неизвестный ключ", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #1273 — предикат п.3 (утверждение): изменено только происхождение
  // в клиентских данных; близнец — то же утверждение со своим происхождением.
  await withKeyHolder(testInfo, "F7-10-edge", async (seed, key) => {
    await expectAccepted(seed, testInfo, key, "честный близнец");
    const unknown = await unknownKeyRefusal(seed, testInfo, key);
    for (const origin of foreignOrigins(testInfo)) {
      expectSingleRefusal(await present(seed, testInfo, key, { origin }), unknown, `происхождение ${origin}`);
    }
    await expectAccepted(seed, testInfo, key, "честный близнец после отказов");
  });
});

test("F7-44-edge · регистрация с чужим происхождением отвергается краем, ключ в перечне не появляется; своё происхождение принято", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #1273 — предикат п.3 (церемония): изменено только происхождение
  // в клиентских данных регистрации; близнец — посев со своим происхождением.
  await withKeyHolder(testInfo, "F7-44-edge", async (seed, key) => {
    const listPath = `/iam/v1/users/${key.userId}/accessKeys`;
    for (const origin of foreignOrigins(testInfo)) {
      const { response, key: refused } = await submitRegistration(seed, origin, { userVerification: true });
      const text = await response.text();
      expect(response.status(), `регистрация с происхождением ${origin}: край ответил ${text.slice(0, 300)}`).toBe(400);
      const body = JSON.parse(text) as Refusal & { details?: Array<{ reason?: unknown }> };
      expect(
        { code: body.code, message: body.message, reasons: (body.details ?? []).map((d) => d.reason) },
        `регистрация с происхождением ${origin}: отказ не назвал причину`,
      ).toEqual({ code: 3, message: ORIGIN_NOT_IN_LIST, reasons: ["ORIGIN_NOT_ALLOWED"] });
      expect(text.includes(origin), `регистрация с происхождением ${origin}: ответ повторяет присланное происхождение`).toBe(
        false,
      );
      expect(
        text.includes(refused.credentialId.toString("base64")),
        `регистрация с происхождением ${origin}: ответ повторяет присланный идентификатор удостоверения`,
      ).toBe(false);
    }
    const listed = await seed.read(listPath);
    const listText = await listed.text();
    expect(listed.status(), `перечень ключей: ${listText.slice(0, 300)}`).toBe(200);
    const ids = ((JSON.parse(listText) as { accessKeys?: Array<{ id?: unknown }> }).accessKeys ?? []).map((k) => k.id);
    expect(ids, "после отвергнутых регистраций в перечне не только ключ близнеца").toEqual([key.accessKeyId]);
  });
});
