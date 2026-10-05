// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type TestInfo } from "@playwright/test";
import {
  LANE,
  SEED_PASSWORD,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  type Seed,
} from "./ceremony-seed";
import {
  acceptedOperation,
  cloudAdminAtLevelTwo,
  operationSucceeded,
  readerOf,
  resetOwnFactor,
  type CloudAdmin,
} from "./cloud-admin";
import { test } from "./fixtures";
import { answerOf, carrierOf, expectSessionLaneRefusal, signedIn } from "./session-lane";

/**
 * Сквозные позиции, чьё «Дано» — администратор облака стенда с сессией «2»
 * (kacho#2878): распорядитель над записью человека через край на посадке own.
 * Держатель обеих в ведомости долга набора службы — эта платформа
 * (`.github/scripts/newman-suite-debt.py` службы: `_HOLDER_F3_PLATFORM`,
 * `_HOLDER_F12_PHASE`); набор службы их не исполняет, потому что края у его
 * стенда нет, а такого предъявителя его стенд не куёт.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО СУДИТСЯ — ПРИЁМКИ И ИХ ОТПЕЧАТКИ (sha256 содержимого; то же содержимое на
 * пине службы в `go.mod` и на ветке `296` PRO-Robotech/kaname)
 *
 *   Ф3 — `docs/engineering/acceptance/login-lane-issues-our-session-and-logout-ends-it-server-side.md`
 *        20ccad56e52ff778ed598c997834b5369e47508694178f2f363f2dd89795dfa5 — APPROVED;
 *   Ф12 — `docs/engineering/acceptance/second-factor-totp-and-recovery-codes.md`
 *        c5e535f0573e5ed86a229af1688b05ffad5c179b41b4f99b643cc1c06298486d — APPROVED.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ОТРИЦАНИЕ — ТОЛЬКО В ПАРЕ С БЛИЗНЕЦОМ, И ФАКТ МЕЖДУ НИМИ ОДИН
 *
 *   Ф3-24: вход верным паролем заблокированного — отказ, побайтово равный отказу
 *          НЕВЕРНОМУ паролю того же человека до блокировки (Ф3-02); после
 *          `Unblock` тот же вход проходит. Изменён один факт — состояние записи.
 *   Ф12-45 «б»: `ResetSecondFactor` на своей записи — отказ человеку без
 *          отношения и `200` администратору облака. Изменён один факт —
 *          держит ли вызывающий отношение записи каталога.
 *   Ф12-45 «а»/«в»: снятие кодом из несвежей сессии — проходит с годным кодом и
 *          отвергается с потреблённым. Изменён один факт — годен ли код.
 *
 * УБОРКА. Фактор администратора снимается его же `ResetSecondFactor` в конце
 * каждой пробы — это и есть одно-фактный близнец «б»; провал уборки не прячет
 * провал пробы и не прячется за ним (`withCloudAdmin`).
 */

/** Глагол отношения распорядителя записи человека (`identity_suspender`, пол «2»). */
const userVerb = (userId: string, verb: "block" | "unblock" | "resetSecondFactor") => `/iam/v1/users/${userId}:${verb}`;

/** Идентификатор человека из «кто я» его же сессии. */
async function userIdOf(seed: Seed, who: string): Promise<string> {
  const res = await seed.read(SESSION_IDENTITY);
  const view = lastIssued(seed, SESSION_IDENTITY).body as { user?: { id?: unknown } } | null;
  expect(res.status(), `${who}: «кто я» — ${JSON.stringify(view)}`).toBe(200);
  const id = String(view?.user?.id ?? "");
  expect(id, `${who}: «кто я» не назвал идентификатор`).not.toBe("");
  return id;
}

/** Уровень сессии из «кто я». */
async function levelOf(seed: Seed, who: string): Promise<string> {
  const res = await seed.read(SESSION_IDENTITY);
  const view = lastIssued(seed, SESSION_IDENTITY).body as { session?: { assuranceLevel?: unknown } } | null;
  expect(res.status(), `${who}: «кто я» — ${JSON.stringify(view)}`).toBe(200);
  return String(view?.session?.assuranceLevel ?? "");
}

/**
 * Проба с администратором облака: уборка (снятие его фактора) идёт ВСЕГДА, и её
 * провал не теряется — при зелёном теле он и есть отказ пробы, при красном
 * записан пометкой рядом с первопричиной.
 */
async function withCloudAdmin(testInfo: TestInfo, body: (admin: CloudAdmin) => Promise<void>): Promise<void> {
  const admin = await cloudAdminAtLevelTwo(testInfo);
  let failure: unknown = null;
  try {
    await body(admin);
  } catch (e) {
    failure = e;
  }
  try {
    if (!admin.released) await resetOwnFactor(admin, testInfo);
  } catch (cleanup) {
    if (failure === null) throw cleanup;
    testInfo.annotations.push({ type: "уборка не прошла", description: String(cleanup) });
  } finally {
    await admin.seed.dispose();
  }
  if (failure !== null) throw failure;
}

test("Ф3-24 · блокировка: живые сессии — отказ на предъявлении, вход — как неверный пароль, Unblock возвращает вход", async ({ browserName: _browserName }, testInfo) => {
  // verifies #1269 — Ф3-24 (Ф3 20ccad56…): отказ живым сессиям — полоса личности края; распорядитель — администратор облака (#2878).
  test.setTimeout(240_000);
  await withCloudAdmin(testInfo, async (admin) => {
    // Дано: человек с двумя живыми сессиями — S1 (посев) и S2 (вход паролем).
    const first = await newSeed(testInfo);
    const human = await seedConfirmedHuman(first, seedAddress("f3-24"));
    const second = await signedIn(testInfo, human.email, human.password, "S2");
    const userId = await userIdOf(first, "S1");
    await carrierOf(first, "S1");
    let blocked = false;
    try {
      expect(await levelOf(first, "S1 до блокировки"), "S1 до блокировки обязана быть живой").not.toBe("");
      expect(await levelOf(second, "S2 до блокировки"), "S2 до блокировки обязана быть живой").not.toBe("");

      // Образец Ф3-02 — неверный пароль того же человека ДО блокировки.
      const probe = await newSeed(testInfo);
      const wrong = await answerOf(
        await probe.submit(LANE.login, "login", { email: human.email, password: `${SEED_PASSWORD}-wrong` }),
      );
      await probe.dispose();
      expect(wrong.status, `образец Ф3-02: неверный пароль — ${wrong.text}`).toBe(401);

      // Когда: администратор зовёт Block; операция завершена без отказа.
      const op = await acceptedOperation(
        await admin.seed.api.post(userVerb(userId, "block"), { data: {} }),
        "Block",
      );
      await operationSucceeded(readerOf(admin.seed), op, "Block");
      blocked = true;

      // Тогда: обе сессии — отказ F4d-22 НЕМЕДЛЕННО (край ответ Resolve не кэширует), тела равны.
      const refusedOne = await answerOf(await first.read(SESSION_IDENTITY));
      const refusedTwo = await answerOf(await second.read(SESSION_IDENTITY));
      expectSessionLaneRefusal(refusedOne, "S1 после блокировки");
      expectSessionLaneRefusal(refusedTwo, "S2 после блокировки");
      expect(refusedTwo.text, "отказы двум сессиям заблокированного обязаны быть одним ответом").toBe(refusedOne.text);

      // Вход ВЕРНЫМ паролем — побайтово как неверный (Ф1-16, Ф1-05).
      const lockedOut = await newSeed(testInfo);
      const refusedLogin = await answerOf(
        await lockedOut.submit(LANE.login, "login", { email: human.email, password: human.password }),
      );
      await lockedOut.dispose();
      expect(
        { status: refusedLogin.status, text: refusedLogin.text },
        "вход заблокированного верным паролем обязан быть неотличим от неверного пароля (Ф3-02)",
      ).toEqual({ status: wrong.status, text: wrong.text });

      // Положительный контроль: Unblock — и вход тем же паролем проходит.
      const un = await acceptedOperation(
        await admin.seed.api.post(userVerb(userId, "unblock"), { data: {} }),
        "Unblock",
      );
      await operationSucceeded(readerOf(admin.seed), un, "Unblock");
      blocked = false;
      const back = await signedIn(testInfo, human.email, human.password, "вход после Unblock");
      expect(await levelOf(back, "вход после Unblock"), "сессия после Unblock — уровень пароля").toBe("1");
      await back.dispose();
    } finally {
      if (blocked) {
        // Уборка своей фикстуры: человек пробы не остаётся заблокированным.
        const un = await acceptedOperation(
          await admin.seed.api.post(userVerb(userId, "unblock"), { data: {} }),
          "уборка: Unblock",
        );
        await operationSucceeded(readerOf(admin.seed), un, "уборка: Unblock");
      }
      await Promise.all([first.dispose(), second.dispose()]);
    }
  });
});
