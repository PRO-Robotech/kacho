// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type TestInfo } from "@playwright/test";
import {
  LANE,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  seedSecondFactor,
  totpCode,
  type Seed,
} from "./ceremony-seed";
import { acceptedOperation, operationSucceeded, readerOf } from "./cloud-admin";
import { runTag, test } from "./fixtures";
import { answerOf } from "./session-lane";

/**
 * Стадия S3 интерактивного входа — сценарии 21 и 22 на живом стенде (kacho#2571).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО СУДИТСЯ
 *
 *   IAM-INT-1 — `docs/specs/sub-phase-IAM-INT-1-interactive-login-capability-acceptance.md`
 *   (воркспейс), вердикт ✅ APPROVED относится к редакции 1 (коммит `433b47f2`,
 *   sha256 053ce2bdc7fb94afb1e0cd3c00000fe7615f0daeca1f6930e83dee62c8cc8712).
 *   Тексты сценариев 21 и 22 в редакции 6 (ждёт вердикта) побайтово те же.
 *
 * Сценарии перенацелены на НАШУ личность решением задачи 2026-09-15: «публичный
 * API провайдера личности» сценария 21 — глаголы полосы формы службы доступа
 * через край (`/iam/v1/auth/*`), носитель — наша сессия.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЗАПРОСОМ К КРАЮ, А НЕ ЭКРАНОМ
 *
 * Сценарий 21 прямо требует заведения «без браузера», а 22 утверждает ответ
 * края по полу глагола. Экран консоли здесь лишний свидетель. Каждый носитель
 * живёт в СВОЁМ контексте запросов (`newSeed`): общей банки печенья нет, и
 * сессия одного входа не подменит сессию другого.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ОТРИЦАНИЕ — ТОЛЬКО В ПАРЕ, И ФАКТ МЕЖДУ НИМИ ОДИН
 *
 *   21: вход паролем и кодом фактора — уровень «2»; вход ТЕМ ЖЕ паролем без
 *       кода — проходит, но уровень «1». Изменён один факт — предъявлен ли код.
 *   22: глагол с полом «2» под `T1` — отказ с вызовом повышения; под `T2` —
 *       проходит. Глагол с полом «1» проходит под обоими. Человек, глагол и
 *       ресурс одни; изменён один факт — вход со вторым фактором или без.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ЗДЕСЬ НЕТ — СЦЕНАРИИ 24 И 25, И ЭТО ИСХОД ВХОДНОГО ГЕЙТА, А НЕ ПРОПУСК
 *
 * Приёмка запрещает писать 24–25, пока посев не предъявит одного человека краю
 * вторым носителем — подписанным предъявителем. Под посадкой `own` его выдаёт
 * только церемония авторизации (`GET /iam/v1/authorize` → `POST /iam/v1/token`)
 * заведённому интерактивному клиенту, а клиент заводится лишь на внутреннем
 * слушателе края (mTLS, лист оператора) служебной учёткой администратора
 * кластера. У набора консоли нет ни того, ни другого: гейт в нём не пройден,
 * и по правилу гейта сценарии в набор не пишутся. Открытый долг назван числом
 * в kacho#2571.
 */

/** Глагол платформы, через который находится аккаунт человека (каталог — без пола). */
const ACCOUNTS = "/iam/v1/accounts?pageSize=1000";

/**
 * Тело отказа по полу — ответ края ПОСЛЕ решения kaname#511 (R36 п. 1): текст
 * называет следующий шаг, машинный признак `insufficient_user_authentication`
 * живёт в вызове `WWW-Authenticate`. Одобренная редакция сценария 22 выписывает
 * тело `{"code":16,"message":"insufficient_user_authentication"}` — это текст до
 * того решения; расхождение возвращено вопросом к приёмке (kacho#2571).
 * Производитель — `stepUpDenyMessage`, gateway/internal/middleware/auth_stepup.go.
 */
const STEP_UP_REFUSAL = {
  code: 16,
  message: "authentication level is insufficient: step up with a second factor, or present a credential of another kind",
  details: [],
};

const stepOf = (ms: number) => Math.floor(ms / 30_000);

/**
 * Код фактора ШАГА НОВЕЕ того, на котором фактор подтверждён: код своего шага
 * принимается один раз, и повтор подтверждающего был бы отказом по повтору, а
 * не входом. Ждётся смена шага — условие, а не время.
 */
async function codeOfNextStep(secret: string, after: number): Promise<string> {
  await expect
    .poll(() => stepOf(Date.now()), { message: "шаг кода не сменился", timeout: 45_000, intervals: [500] })
    .toBeGreaterThan(stepOf(after));
  return totpCode(secret, Date.now());
}

/** Вход своим контекстом с названным телом; шаг утверждает исход `200` и носитель. */
async function signIn(testInfo: TestInfo, body: Record<string, unknown>, who: string): Promise<Seed> {
  const seed = await newSeed(testInfo);
  const res = await seed.submit(LANE.login, "login", body);
  expect(res.status(), `${who}: вход отвергнут — ${res.status()} ${JSON.stringify(lastIssued(seed, LANE.login).body)}`).toBe(
    200,
  );
  expect(await seed.sessionBearer(), `${who}: вход прошёл, а носителя сессии у контекста нет`).not.toBe("");
  return seed;
}

/** Уровень сессии носителя — из ответа края «кто я», а не из ответа входа. */
async function levelOf(seed: Seed, who: string): Promise<string> {
  const res = await seed.read(SESSION_IDENTITY);
  const text = await res.text();
  expect(res.status(), `${who}: «кто я» — ${text}`).toBe(200);
  return String((JSON.parse(text) as { session?: { assuranceLevel?: unknown } }).session?.assuranceLevel);
}

test("IAM-INT-1-21 · второй фактор заводится без браузера и предъявляется входом; вход одним паролем даёт другой уровень", async ({
  browserName: _browserName,
}, testInfo) => {
  // verifies #2571 — IAM-INT-1-21 (редакция 1, 053ce2bd…), на нашей личности (решение 2026-09-15).
  test.setTimeout(240_000);
  const seeds: Seed[] = [];
  try {
    // Дано: личность с паролем и подтверждённым адресом.
    const owner = await newSeed(testInfo);
    seeds.push(owner);
    const human = await seedConfirmedHuman(owner, seedAddress("iam-int-1-21"));

    // Когда: посев заводит второй фактор глаголами службы через край — без браузера.
    const factor = await seedSecondFactor(owner);
    const confirmedAt = Date.now();
    expect(factor.assuranceLevel, "заведение завершилось, а сессия посева не поднялась до «2»").toBe("2");

    // Тогда: посев ПРЕДЪЯВЛЯЕТ фактор — вход паролем и кодом проходит, уровень «2».
    const code = await codeOfNextStep(factor.secret, confirmedAt);
    const withFactor = await signIn(
      testInfo,
      { email: human.email, password: human.password, secondFactor: { method: "totp", code } },
      "вход паролем и кодом фактора",
    );
    seeds.push(withFactor);
    expect(await levelOf(withFactor, "вход с фактором"), "вход с предъявленным фактором обязан дать уровень «2»").toBe("2");

    // И (отрицательная пара): вход ТЕМ ЖЕ паролем без кода проходит, но уровень другой.
    const passwordOnly = await signIn(testInfo, { email: human.email, password: human.password }, "вход одним паролем");
    seeds.push(passwordOnly);
    expect(
      await levelOf(passwordOnly, "вход одним паролем"),
      "вход одним паролем при заведённом факторе обязан проходить уровнем «1», а не «2»",
    ).toBe("1");
  } finally {
    await Promise.all(seeds.map((s) => s.dispose()));
  }
});

test("IAM-INT-1-22 · пол «2» отвергает вход паролем и пропускает вход со вторым фактором; пол «1» проходит под обоими", async ({
  browserName: _browserName,
}, testInfo) => {
  // verifies #2571 — IAM-INT-1-22 (редакция 1, 053ce2bd…), на нашей личности (решение 2026-09-15).
  test.setTimeout(240_000);
  const seeds: Seed[] = [];
  let groupPath = "";
  let removed = false;
  let t2: Seed | undefined;
  try {
    // Дано: один человек с заведённым фактором и два его предъявителя.
    const owner = await newSeed(testInfo);
    seeds.push(owner);
    const human = await seedConfirmedHuman(owner, seedAddress("iam-int-1-22"));
    const factor = await seedSecondFactor(owner);
    const confirmedAt = Date.now();

    // T1 — вход паролем.
    const t1 = await signIn(testInfo, { email: human.email, password: human.password }, "T1");
    seeds.push(t1);
    expect(await levelOf(t1, "T1"), "T1 — вход паролем, уровень «1»").toBe("1");
    // T2 — вход со вторым фактором (код шага новее подтверждения).
    const code = await codeOfNextStep(factor.secret, confirmedAt);
    t2 = await signIn(
      testInfo,
      { email: human.email, password: human.password, secondFactor: { method: "totp", code } },
      "T2",
    );
    seeds.push(t2);
    expect(await levelOf(t2, "T2"), "T2 — вход со вторым фактором, уровень «2»").toBe("2");

    // Предмет: группа аккаунта человека — заведение и чтение полом «1», удаление полом «2».
    // Аккаунт заводится сам и не сразу: ждётся УСЛОВИЕ — его появление.
    await expect
      .poll(
        async () => {
          const res = await t1.read(ACCOUNTS);
          return res.ok() ? (((await res.json()) as { accounts?: unknown[] }).accounts ?? []).length : -res.status();
        },
        { message: "аккаунт человека не появился — предмет пробы не собран", timeout: 45_000 },
      )
      .toBeGreaterThan(0);
    const accounts = (await (await t1.read(ACCOUNTS)).json()) as { accounts: Array<{ id: string }> };
    const created = await acceptedOperation(
      await t1.api.post("/iam/v1/groups", {
        data: { accountId: accounts.accounts[0].id, name: `e2e-iam-int-1-22-${runTag()}`, description: "IAM-INT-1-22" },
      }),
      "заведение группы под T1 (пол «1»)",
    );
    const done = await operationSucceeded(readerOf(t1), created, "заведение группы под T1");
    const groupId = String((created as { metadata?: { groupId?: unknown } }).metadata?.groupId ?? "");
    expect(groupId, `операция не назвала идентификатор группы — ${JSON.stringify(done)}`).not.toBe("");
    groupPath = `/iam/v1/groups/${groupId}`;
    // Первое чтение своего свежего ресурса по его СОБСТВЕННОМУ адресу — окно
    // материализации права и подтверждение, что id не фантом. Чтение — контекстом
    // T1 напрямую (`request.get`): эту форму узнаёт гейт дерева
    // `TestProbeIdentifiersFromOperationMetadataAreConfirmedByReading`.
    const request = t1.api;
    await expect
      .poll(async () => (await request.get(`/iam/v1/groups/${groupId}`)).status(), {
        message: "своя свежая группа не читается под T1 — право не материализовалось либо id фантомен",
        timeout: 45_000,
      })
      .toBe(200);

    // Когда/Тогда: глагол с полом «1» — оба → 200 (ступень не плата за рутину).
    expect((await t1.read(groupPath)).status(), "пол «1» под T1 обязан проходить").toBe(200);
    expect((await t2.read(groupPath)).status(), "пол «1» под T2 обязан проходить").toBe(200);

    // Когда/Тогда: глагол с полом «2» — T1 → 401 с вызовом повышения.
    const denied = await answerOf(await t1.api.delete(groupPath));
    expect(denied.status, `пол «2» под T1 обязан отвергаться 401: ${denied.text}`).toBe(401);
    expect(JSON.parse(denied.text), "тело отказа по полу").toEqual(STEP_UP_REFUSAL);
    expect(denied.challenge, "вызов повышения — признак RFC 9470").toContain('error="insufficient_user_authentication"');
    expect(denied.challenge, "вызов повышения обязан назвать требуемый уровень").toContain('acr_values="2"');

    // И: тот же глагол под T2 → 200, и операция завершается без ошибки.
    const passedRes = await t2.api.delete(groupPath);
    const passed = await acceptedOperation(passedRes, "удаление группы под T2 (пол «2»)");
    await operationSucceeded(readerOf(t2), passed, "удаление группы под T2");
    removed = true;
  } finally {
    // Снятие и на падении: группа, не снятая сценарием, снимается носителем «2».
    if (groupPath && !removed && t2) {
      // Исход уборки не утверждается: утверждение в `finally` подменило бы
      // отказ сценария своим. Неуспех печатается — остаток виден переписи стенда.
      const res = await t2.api.delete(groupPath);
      if (!res.ok()) console.log(`[IAM-INT-1-22] уборка группы ${groupPath}: ${res.status()} ${await res.text()}`);
    }
    await Promise.all(seeds.map((s) => s.dispose()));
  }
});
