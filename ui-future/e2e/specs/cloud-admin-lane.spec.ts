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
  seedSecondFactor,
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

/** Состояние второго фактора своей сессией — тело, как отдал край. */
async function factorStatus(seed: Seed, who: string): Promise<Record<string, unknown>> {
  const res = await seed.read(LANE.secondFactor);
  const body = lastIssued(seed, LANE.secondFactor).body as Record<string, unknown> | null;
  expect(res.status(), `${who}: состояние второго фактора — ${JSON.stringify(body)}`).toBe(200);
  return body ?? {};
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

/** Предел ожидания несвежести: окно профиля (`authn.self-service-freshness`) плюс запас на шаги «Дано». */
const STALE_BUDGET_MS = 22 * 60_000;
/** Шаг опроса: окно — минуты, частый опрос условие не приблизит. */
const STALE_POLL_MS = 30_000;

/**
 * Ждать УСЛОВИЕ «сессия несвежа», а не время: заведение из сессии человека с
 * заведённым фактором отвечает отказом состояния, пока сессия свежа, и
 * `403 SESSION_NOT_FRESH`, когда окно прошло — свежесть судится раньше состояния
 * строки (Ф12 Р4). Это же контроль «Дано» (а): самосброс пройдёт не потому, что
 * окно было открыто. Ни один из этих отказов в счёт попыток не идёт (Ф12-32).
 */
async function awaitStale(seed: Seed, who: string): Promise<void> {
  await expect
    .poll(
      async () => {
        const res = await seed.submit(LANE.enroll, "second-factor", {});
        const body = lastIssued(seed, LANE.enroll).body;
        const text = JSON.stringify(body);
        if (res.status() === 403 && text.includes("SESSION_NOT_FRESH")) return "несвежа";
        if (res.status() === 200) return `заведение прошло — фактор не был заведён: ${text}`;
        return `свежа: ${res.status()}`;
      },
      { message: `${who}: сессия не стала несвежей за окно профиля`, timeout: STALE_BUDGET_MS, intervals: [STALE_POLL_MS] },
    )
    .toBe("несвежа");
}

test("Ф12-45 · самосброс кодом из несвежей сессии; административный сброс своей записи — только администратору облака", async ({ browserName: _browserName }, testInfo) => {
  // verifies #1281 — Ф12-45 (Ф12 c5e535f0…) через край на посадке own; близнец «б» — администратор облака (#2878).
  test.setTimeout(30 * 60_000);
  await withCloudAdmin(testInfo, async (admin) => {
    // Дано U: фактор заведён, устройство утрачено — годны только запасные коды; S1 — вход паролем, «1».
    const uSeed = await newSeed(testInfo);
    const u = await seedConfirmedHuman(uSeed, seedAddress("f12-45-u"));
    const uFactor = await seedSecondFactor(uSeed);
    const s1 = await signedIn(testInfo, u.email, u.password, "S1");
    // Дано L: фактор заведён, все десять кодов потреблены (Ф12-26); его сессия «1» несвежа так же, как S1.
    const lSeed = await newSeed(testInfo);
    const l = await seedConfirmedHuman(lSeed, seedAddress("f12-45-l"));
    const lFactor = await seedSecondFactor(lSeed);
    const lSession = await signedIn(testInfo, l.email, l.password, "L");
    // Дано W: то же, что U, и сессия поднята до «2» запасным кодом (Ф12-18).
    const wSeed = await newSeed(testInfo);
    const w = await seedConfirmedHuman(wSeed, seedAddress("f12-45-w"));
    const wFactor = await seedSecondFactor(wSeed);
    const wSession = await signedIn(testInfo, w.email, w.password, "W");
    const seeds = [uSeed, s1, lSeed, lSession, wSeed, wSession];
    try {
      expect(await levelOf(s1, "S1"), "S1 — вход паролем без кода (Ф12-14)").toBe("1");
      expect(await levelOf(lSession, "L"), "сессия L — вход паролем без кода").toBe("1");

      for (const [i, code] of lFactor.backupCodes.entries()) {
        const res = await lSeed.submit(LANE.stepUp, "step-up", { method: "lookup_secret", code });
        expect(
          res.status(),
          `Дано L: потребление запасного кода №${i + 1} — ${JSON.stringify(lastIssued(lSeed, LANE.stepUp).body)}`,
        ).toBe(200);
      }

      const raised = await wSession.submit(LANE.stepUp, "step-up", { method: "lookup_secret", code: wFactor.backupCodes[0] });
      expect(raised.status(), `Дано W: повышение запасным кодом — ${JSON.stringify(lastIssued(wSession, LANE.stepUp).body)}`).toBe(200);
      expect(await levelOf(wSession, "W"), "Дано W: сессия поднята до «2»").toBe("2");
      const wId = await userIdOf(wSession, "W");

      // (б) W через край зовёт ResetSecondFactor со СВОИМ user_id — 403 · code 7 · AUTHZ_DENIED; у W ничего не изменено.
      const wBefore = await factorStatus(wSession, "W до вызова");
      const denied = await answerOf(await wSession.api.post(userVerb(wId, "resetSecondFactor"), { data: {} }));
      expect(denied.status, `(б): административный сброс своей записи не администратором — ${denied.text}`).toBe(403);
      const deniedBody = JSON.parse(denied.text) as { code?: unknown };
      expect(deniedBody.code, `(б): код отказа — PERMISSION_DENIED (7): ${denied.text}`).toBe(7);
      expect(denied.text, "(б): причина отказа — AUTHZ_DENIED, той же формы, что у распорядителя в Ф12-30").toContain(
        "AUTHZ_DENIED",
      );
      expect(await factorStatus(wSession, "W после вызова"), "(б): состояние фактора W не изменено, сессия W годна").toEqual(
        wBefore,
      );
      expect(wBefore.totp, "(б): фактор W заведён").toEqual(expect.objectContaining({ enrolled: true }));
      // Одно-фактный близнец (б) — тот же вызов на своей записи администратором облака:
      // `200`, фактор снят. Его исполняет уборка `withCloudAdmin` (resetOwnFactor) с теми
      // же утверждениями; здесь он идёт ДО ожидания, чтобы сессия администратора «2»
      // не пережидала окно.
      await resetOwnFactor(admin, testInfo);

      // Окно Р8 плюс ε: S1 и сессия L несвежи — условие, а не время.
      await awaitStale(s1, "S1");
      await awaitStale(lSession, "L");

      // (а) U под S1: remove кодом №4 набора — 200; свежести сверх кода не требуется.
      const s1Before = (await carrierOf(s1, "S1")).value;
      const removed = await s1.submit(LANE.remove, "second-factor", { method: "lookup_secret", code: uFactor.backupCodes[3] });
      const removedBody = lastIssued(s1, LANE.remove).body as { backupCodesRemaining?: unknown } | null;
      expect(removed.status(), `(а): самосброс кодом из несвежей сессии — ${JSON.stringify(removedBody)}`).toBe(200);
      expect(removedBody?.backupCodesRemaining, "(а): набора после снятия нет — остаток 0 (Р4, kaname#275)").toBe(0);
      expect((await carrierOf(s1, "S1 после снятия")).value, "(а): снятие перевыпускает носитель").not.toBe(s1Before);
      expect(await levelOf(s1, "S1 после снятия"), "(а): S1 на новом носителе — «2» (запасной код предъявлен)").toBe("2");
      const uAfter = await factorStatus(s1, "U после снятия");
      expect(uAfter.totp, `(а): строк фактора у U нет — ${JSON.stringify(uAfter)}`).toEqual(
        expect.objectContaining({ enrolled: false }),
      );
      expect(Object.keys(uAfter), `(а): ключа backupCodes после снятия нет — ${JSON.stringify(uAfter)}`).not.toContain(
        "backupCodes",
      );
      const reEnroll = await s1.submit(LANE.enroll, "second-factor", {});
      expect(
        reEnroll.status(),
        `(а): enroll после самосброса — без нового предъявления (код снятия освежил окно): ${JSON.stringify(lastIssued(s1, LANE.enroll).body)}`,
      ).toBe(200);

      // (в) L под своей сессией: remove потреблённым кодом — 401 authentication failed; строки L не тронуты.
      const lBefore = await factorStatus(lSession, "L до снятия");
      const refused = await answerOf(
        await lSession.submit(LANE.remove, "second-factor", { method: "lookup_secret", code: lFactor.backupCodes[0] }),
      );
      expect(refused.status, `(в): снятие потреблённым кодом — ${refused.text}`).toBe(401);
      expect((JSON.parse(refused.text) as { code?: unknown; message?: unknown }).message, "(в): текст отказа").toBe(
        "authentication failed",
      );
      expect(await factorStatus(lSession, "L после отказа"), "(в): строки L не тронуты").toEqual(lBefore);
      expect(lBefore.totp, "(в): фактор L заведён").toEqual(expect.objectContaining({ enrolled: true }));
    } finally {
      await Promise.all(seeds.map((s) => s.dispose()));
    }
  });
});
