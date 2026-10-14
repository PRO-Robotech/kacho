// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type TestInfo } from "@playwright/test";
import {
  LANE,
  SEED_PASSWORD,
  SESSION_COOKIE,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  seedHuman,
  seedSecondFactor,
  type Cookie,
  type Seed,
} from "./ceremony-seed";
import { runTag, test } from "./fixtures";
import { RECOVERY_NEXT_STEP } from "./lane-texts";
import { RECOVERY_CODE_LINE, awaitLetter, stationMailbox } from "./mail-receiver";
import { answerOf, bareSeed, carrierOf, expectSessionLaneRefusal, signedIn } from "./session-lane";

/**
 * Полоса нашей сессии ЧЕРЕЗ КРАЙ на живом стенде — позиции приёмок Ф3 и Ф5,
 * которые набор службы не исполняет: края у его стенда нет (`chart-own`), а
 * держателем сквозной половины ведомость долга службы называет kacho#1269
 * (`.github/scripts/newman-suite-debt.py` службы, `_HOLDER_F3_PLATFORM`,
 * `_HOLDER_F5_EDGE_HALF`).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО СУДИТСЯ — ПРИЁМКИ И ИХ ОТПЕЧАТКИ (sha256 содержимого на ветке PRO-Robotech/kaname)
 *
 *   Ф3 — `docs/engineering/acceptance/login-lane-issues-our-session-and-logout-ends-it-server-side.md`
 *        20ccad56e52ff778ed598c997834b5369e47508694178f2f363f2dd89795dfa5 — APPROVED
 *        (тот же отпечаток на ветках 296 и 537);
 *   Ф12 — `docs/engineering/acceptance/second-factor-totp-and-recovery-codes.md`
 *        c5e535f0573e5ed86a229af1688b05ffad5c179b41b4f99b643cc1c06298486d — APPROVED
 *        (тот же отпечаток на ветках 296 и 537);
 *   Ф5 — `docs/engineering/acceptance/recovery-of-access.md`
 *        17b2d02c8bff38378135467750b21e026118c62fd9d8828538c6315b6d4644e3 — APPROVED
 *        (ветка 537, PR kaname#593); текст Ф5-24 побайтово тот же, что в редакции
 *        2e441e45ba4adae493d0e3263f629d6b6e29669c9562bb724c70f8e1438de341 на 296.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЗАПРОСОМ К КРАЮ, А НЕ ЭКРАНОМ
 *
 * Все три позиции уровня E утверждают ОТВЕТ КРАЯ: состав «кто я», отказ полосы
 * личности с гашением носителя, исход глагола платформы. Экран консоли здесь
 * свидетель лишний — он читает тот же ответ и добавляет к причине отказа свою.
 * Каждый носитель живёт в СВОЁМ контексте запросов посева (`newSeed`): общей
 * банки печенья нет, и сессия одного шага не подменит сессию другого.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ОТРИЦАНИЕ — ТОЛЬКО В ПАРЕ С БЛИЗНЕЦОМ, И ФАКТ МЕЖДУ НИМИ ОДИН
 *
 *   Ф3-14: «кто я» с нашим носителем — человек; то же ЗНАЧЕНИЕ носителя под
 *          чужим именем печенья — ответ без личности, побайтово равный ответу
 *          без печенья вовсе. Изменён один факт — имя печенья.
 *   Ф3-26: прежние сессии `S1`, `S2` после завершения восстановления — отказ;
 *          сессия `R`, выданная этим завершением, — проходит. Изменён один факт —
 *          момент аутентификации относительно отсечки.
 *   Ф5-24: под `R` и под `L` (вход новым паролем) — тот же человек в «кто я» и
 *          тот же исход глагола платформы. Изменён один факт — множество
 *          предъявленного (код восстановления против пароля).
 *   Ф12-20: глагол с полом «2» под сессией входа паролем — отказ с вызовом
 *          повышения; тот же глагол после повышения запасным кодом — проходит.
 *          Изменён один факт — уровень сессии; глагол с полом «1» проходит под
 *          обеими (ступень не плата за рутину).
 */

/** Ответ «кто я» без личности — побайтово (Ф3-14, обработчик края). */
const NO_IDENTITY = `{"user":null}`;

/** Глагол платформы, на котором сравниваются две сессии (Ф5-24): каталог — `<exempt>` без пола уровня. */
const PLATFORM_VERB = "/iam/v1/accounts?pageSize=1000";

/** Ключи состава «кто я» — дословно Ф3-14 «Тогда». */
const USER_KEYS = ["displayName", "email", "id", "permissions", "subjectType"];
const SESSION_KEYS = ["assuranceLevel", "emailVerified", "expiresAt"];

/** Новый пароль восстановления — отличим от пароля посева, иначе вход под `L` не различал бы два пароля. */
const RECOVERED_PASSWORD = `${SEED_PASSWORD}-recovered`;

test("Ф3-14 · «кто я» отвечает из нашей сессии, а чужое имя печенья личностью не становится", async ({ browserName: _browserName }, testInfo) => {
  // verifies #1269 — Ф3-14 (Ф3 20ccad56…), сквозная половина: маршрут «кто я» — край.
  const owner = await newSeed(testInfo);
  const human = await seedHuman(owner, seedAddress("f3-14"));
  const ours = await carrierOf(owner, "посев Ф3-14");

  const withCarrier = await answerOf(await owner.read(SESSION_IDENTITY));
  expect(withCarrier.status, `«кто я» с живым носителем: ${withCarrier.text}`).toBe(200);
  const view = JSON.parse(withCarrier.text) as { user?: Record<string, unknown>; session?: Record<string, unknown> };
  expect(Object.keys(view.user ?? {}).sort(), `состав user — Ф3-14: ${withCarrier.text}`).toEqual(USER_KEYS);
  expect(Object.keys(view.session ?? {}).sort(), `состав session — Ф3-14 (ключа требования нет, Р8): ${withCarrier.text}`).toEqual(
    SESSION_KEYS,
  );
  expect(view.user?.email, "«кто я» назвал не того человека").toBe(human.email);
  expect(view.user?.subjectType).toBe("user");

  const anonymous = await bareSeed(testInfo);
  const foreign = await bareSeed(testInfo, [{ ...ours, name: `${SESSION_COOKIE}_foreign` }]);
  const twin = await bareSeed(testInfo, [ours]);
  try {
    const none = await answerOf(await anonymous.read(SESSION_IDENTITY));
    const other = await answerOf(await foreign.read(SESSION_IDENTITY));
    const same = await answerOf(await twin.read(SESSION_IDENTITY));
    expect({ status: none.status, text: none.text }, "без носителя — 200 и ответ без личности").toEqual({
      status: 200,
      text: NO_IDENTITY,
    });
    expect(
      { status: other.status, text: other.text },
      "значение нашего носителя под чужим именем обязано отвечать как отсутствие печенья — побайтово",
    ).toEqual({ status: none.status, text: none.text });
    // Близнец: то же значение под НАШИМ именем, перенесённое тем же способом, — человек.
    // Без него отрицание выше зеленело бы и на крае, который не принимает перенесённое печенье вовсе.
    expect(same.status, `близнец: перенесённый наш носитель — ${same.text}`).toBe(200);
    expect((JSON.parse(same.text) as { user?: { email?: unknown } }).user?.email).toBe(human.email);
  } finally {
    await Promise.all([owner.dispose(), anonymous.dispose(), foreign.dispose(), twin.dispose()]);
  }
});

/** Итог восстановления: человек, его прежние сессии и сессия R, выданная завершением. */
interface Recovered {
  human: { email: string; password: string };
  /** Контексты прежних сессий: S1 — посев, S2 — вход паролем; оба до восстановления. */
  first: Seed;
  second: Seed;
  /** Контекст, завершивший восстановление: несёт сессию R. */
  recovering: Seed;
  s1: Cookie;
  s2: Cookie;
}

/**
 * Дано Ф3-26 и Ф5-24: человек с подтверждённым адресом и ДВЕ живые сессии; затем
 * код восстановления запрошен (Ф5-01) и предъявлен с новым паролем (Ф5-03) СВОИМ
 * контекстом. Каждый шаг утверждает свой исход: шаг, собравший условие молча,
 * при отказе назвал бы виновником утверждение сценария.
 */
async function recovered(testInfo: TestInfo, scenario: string): Promise<Recovered> {
  const mailbox = stationMailbox();
  const first = await newSeed(testInfo);
  const human = await seedConfirmedHuman(first, seedAddress(scenario));
  const second = await signedIn(testInfo, human.email, human.password, "S2");
  const s1 = await carrierOf(first, "S1");
  const s2 = await carrierOf(second, "S2");

  const recovering = await bareSeed(testInfo);
  const before = new Set((await mailbox.letters(human.email)).map((l) => l.id));
  const asked = await recovering.submit(LANE.recovery, "recovery", { email: human.email });
  expect(
    { status: asked.status(), body: lastIssued(recovering, LANE.recovery).body },
    "запрос кода восстановления — один ответ `200 {nextStep}` (Ф5-01)",
  ).toEqual({ status: 200, body: { nextStep: RECOVERY_NEXT_STEP } });
  const letter = await awaitLetter(mailbox, human.email, before, undefined, RECOVERY_CODE_LINE);
  const completed = await recovering.submit(LANE.recoveryComplete, "recovery-complete", {
    email: human.email,
    code: letter.code,
    newPassword: RECOVERED_PASSWORD,
  });
  expect(
    completed.status(),
    `завершение восстановления кодом из письма ${letter.id} отвергнуто — ${JSON.stringify(
      lastIssued(recovering, LANE.recoveryComplete).body,
    )}`,
  ).toBe(200);
  const r = await carrierOf(recovering, "R");
  expect(r.value, "сессия R обязана быть НОВЫМ носителем").not.toBe(s1.value);
  return { human, first, second, recovering, s1, s2 };
}

test("Ф3-26 · завершение восстановления гасит все прежние сессии, а выданная им проходит", async ({ browserName: _browserName }, testInfo) => {
  // verifies #1269 — Ф3-26 (Ф3 20ccad56…): отказ F4d-22 прежним сессиям на полосе личности края.
  test.setTimeout(240_000);
  const got = await recovered(testInfo, "f3-26");
  const oldOne = await bareSeed(testInfo, [got.s1]);
  const oldTwo = await bareSeed(testInfo, [got.s2]);
  try {
    // Тогда: обе прежние — отказ F4d-22, и тела отказа побайтово равны.
    const refusedOne = await answerOf(await oldOne.read(SESSION_IDENTITY));
    const refusedTwo = await answerOf(await oldTwo.read(SESSION_IDENTITY));
    expectSessionLaneRefusal(refusedOne, "S1 после восстановления");
    expectSessionLaneRefusal(refusedTwo, "S2 после восстановления");
    expect(refusedTwo.text, "отказы двум прежним сессиям обязаны быть одним ответом").toBe(refusedOne.text);

    // Близнец (вторая строка Ф5-19): сессия R аутентифицирована позже отсечки — проходит.
    const underR = await answerOf(await got.recovering.read(SESSION_IDENTITY));
    expect(underR.status, `R: «кто я» — ${underR.text}`).toBe(200);
    expect((JSON.parse(underR.text) as { user?: { email?: unknown } }).user?.email).toBe(got.human.email);
  } finally {
    await Promise.all([got.first, got.second, got.recovering, oldOne, oldTwo].map((s) => s.dispose()));
  }
});

test("Ф5-24 · сессия восстановления полноправна: тот же человек и тот же исход глагола, что у сессии входа", async ({ browserName: _browserName }, testInfo) => {
  // verifies #2707 — Ф5-24 (Ф5 17b2d02c…): половина через край, связка службы с платформой.
  test.setTimeout(240_000);
  const got = await recovered(testInfo, "f5-24");
  const relogged = await signedIn(testInfo, got.human.email, RECOVERED_PASSWORD, "L");
  try {
    const underR = await answerOf(await got.recovering.read(SESSION_IDENTITY));
    const underL = await answerOf(await relogged.read(SESSION_IDENTITY));
    expect(underR.status, `R: «кто я» — ${underR.text}`).toBe(200);
    expect(underL.status, `L: «кто я» — ${underL.text}`).toBe(200);

    // «Кто я» под R и под L — один человек, один состав; ключа требования нет ни в одном.
    const viewR = JSON.parse(underR.text) as { user: Record<string, unknown>; session: Record<string, unknown> };
    const viewL = JSON.parse(underL.text) as { user: Record<string, unknown>; session: Record<string, unknown> };
    expect(viewR.user, "«кто я» под R и под L обязан назвать одного человека тем же составом").toEqual(viewL.user);
    expect(Object.keys(viewR.session).sort(), `состав session под R: ${underR.text}`).toEqual(SESSION_KEYS);
    expect(Object.keys(viewL.session).sort(), `состав session под L: ${underL.text}`).toEqual(SESSION_KEYS);
    expect(viewR.user.email).toBe(got.human.email);

    // Глагол платформы под R и под L — тот же исход. Аккаунт человека заводится
    // сам и не сразу; ждётся УСЛОВИЕ — появление аккаунта под L, — а не время.
    await expect
      .poll(
        async () => {
          const res = await relogged.read(PLATFORM_VERB);
          if (!res.ok()) return `L: ${res.status()}`;
          const body = (await res.json()) as { accounts?: unknown[] };
          return (body.accounts ?? []).length > 0 ? "есть" : "нет аккаунта";
        },
        { message: "аккаунт человека под L не появился — глагол платформы сравнивать не на чем", timeout: 45_000 },
      )
      .toBe("есть");
    const platformL = await answerOf(await relogged.read(PLATFORM_VERB));
    const platformR = await answerOf(await got.recovering.read(PLATFORM_VERB));
    expect(
      { status: platformR.status, text: platformR.text },
      "глагол платформы под сессией восстановления обязан дать тот же исход, что под сессией входа",
    ).toEqual({ status: platformL.status, text: platformL.text });
    expect(platformR.status).toBe(200);
  } finally {
    await Promise.all([got.first, got.second, got.recovering, relogged].map((s) => s.dispose()));
  }
});

test("Ф12-20 · через край пол «2» отвергает сессию пароля и проходит после повышения кодом", async ({ browserName: _browserName }, testInfo) => {
  // verifies #1281 — Ф12-20 (Ф12 c5e535f0…) через край на посадке own; он же предикат #1280 (Ф11-25, Ф11-26).
  test.setTimeout(240_000);

  // Дано: человек с подтверждённым адресом и заведённым фактором; запасные коды — из подтверждения.
  const owner = await newSeed(testInfo);
  const human = await seedConfirmedHuman(owner, seedAddress("f12-20"));
  const factor = await seedSecondFactor(owner);
  expect(factor.assuranceLevel, "подтверждение фактора обязано поднять сессию посева до «2»").toBe("2");

  // Сессия L1 — вход ОДНИМ паролем (Ф12-14): уровень «1» при заведённом факторе.
  const lane = await signedIn(testInfo, human.email, human.password, "L1");
  try {
    const me = JSON.parse(await (await lane.read(SESSION_IDENTITY)).text()) as { session?: { assuranceLevel?: unknown } };
    expect(String(me.session?.assuranceLevel), "вход паролем без кода — сессия «1» (Ф12-14)").toBe("1");

    // Предмет: группа аккаунта человека — заведение полом «1», удаление полом «2».
    await expect
      .poll(
        async () => {
          const res = await lane.read(PLATFORM_VERB);
          return res.ok() ? (((await res.json()) as { accounts?: unknown[] }).accounts ?? []).length : -res.status();
        },
        { message: "аккаунт человека не появился — предмет пробы не собран", timeout: 45_000 },
      )
      .toBeGreaterThan(0);
    const accounts = (await (await lane.read(PLATFORM_VERB)).json()) as { accounts: Array<{ id: string }> };
    const created = await lane.api.post("/iam/v1/groups", {
      data: { accountId: accounts.accounts[0].id, name: `e2e-f12-20-${runTag()}`, description: "Ф12-20" },
    });
    expect(created.status(), `заведение группы (пол «1») под L1: ${await created.text()}`).toBe(200);
    const groupId = ((await created.json()) as { metadata?: { groupId?: string } }).metadata?.groupId ?? "";
    expect(groupId, "операция не назвала идентификатор группы").not.toBe("");
    const groupPath = `/iam/v1/groups/${groupId}`;
    // Чтение группы по её собственному адресу — подтверждение, что id не фантом
    // несозданного ресурса, и оно же глагол с полом «1» под носителем L1
    // (IAM-INT-1-22, первая половина). Контекст запросов — тот же, что у L1.
    const request = lane.api;
    await expect
      .poll(async () => (await request.get(`/iam/v1/groups/${groupId}`)).status(), {
        message: "своя свежая группа не читается под L1 — право не материализовалось либо id фантомен",
        timeout: 45_000,
      })
      .toBe(200);

    // Когда/Тогда (Ф11-25): глагол с полом «2» под L1 — 401 · code 16 · вызов повышения с acr_values="2".
    const denied = await answerOf(await lane.api.delete(groupPath));
    expect(denied.status, `пол «2» под сессией «1» обязан отвергаться 401: ${denied.text}`).toBe(401);
    expect((JSON.parse(denied.text) as { code?: unknown }).code, "код отказа — UNAUTHENTICATED (16)").toBe(16);
    const challenge = denied.challenge;
    expect(challenge, "вызов повышения обязан назвать требуемый уровень").toContain('acr_values="2"');
    expect(challenge, "вызов повышения обязан назвать предъявленный уровень").toContain("presented ACR 1");

    // Повышение запасным кодом через край (ретранслировано, Ф12-38): новый носитель уровня «2».
    const before = (await carrierOf(lane, "L1")).value;
    const raised = await lane.submit(LANE.stepUp, "step-up", { method: "lookup_secret", code: factor.backupCodes[3] });
    const raisedBody = lastIssued(lane, LANE.stepUp).body as { session?: { assuranceLevel?: unknown } } | null;
    expect(raised.status(), `повышение запасным кодом отвергнуто: ${JSON.stringify(raisedBody)}`).toBe(200);
    expect(String(raisedBody?.session?.assuranceLevel), "повышение обязано дать сессию «2»").toBe("2");
    expect((await carrierOf(lane, "L2")).value, "повышение обязано перевыпустить носитель").not.toBe(before);

    // Ступень не плата за рутину (IAM-INT-1-22): глагол с полом «1» проходит под ОБОИМИ
    // носителями — под L1 он прошёл выше (чтение группы до отказа), под L2 — здесь.
    // Носитель L1 после повышения не предъявляется: повышение перевыпускает носитель
    // (Ф12-38), и «оба носителя» сценария — носитель до повышения и носитель после.
    expect((await lane.read(groupPath)).status(), "пол «1» под носителем L2 обязан проходить").toBe(200);

    // Тогда (Ф11-26): тот же глагол с полом «2» по новому носителю проходит.
    const passed = await answerOf(await lane.api.delete(groupPath));
    expect(passed.status, `пол «2» после повышения обязан проходить: ${passed.text}`).toBe(200);
  } finally {
    await Promise.all([owner.dispose(), lane.dispose()]);
  }
});

test("Ф3-15 · выход через край гасит СВОЮ сессию на стороне сервера, вторая жива, повтор — тот же 200", async ({ browserName: _browserName }, testInfo) => {
  // verifies #1269 — п.2 предиката: Ф3-15 (Ф3 20ccad56…) сквозной пробой через край на посадке own.
  test.setTimeout(240_000);
  const first = await newSeed(testInfo);
  const human = await seedConfirmedHuman(first, seedAddress("f3-15"));
  const second = await signedIn(testInfo, human.email, human.password, "S2");
  const s1 = await carrierOf(first, "S1");
  // Копия носителя S1 сохранена ВНЕ контекста, который выйдет.
  const savedCopy = await bareSeed(testInfo, [s1]);
  const repeat = await bareSeed(testInfo, [s1]);
  try {
    expect((await savedCopy.read(SESSION_IDENTITY)).status(), "копия S1 до выхода — живая сессия").toBe(200);

    // Когда: выход носителем S1 через край — 200, тело {}, носитель гасится.
    const out = await answerOf(await first.submit(LANE.logout, "logout", {}));
    expect({ status: out.status, text: out.text }, "выход — 200 и тело {}").toEqual({ status: 200, text: "{}" });
    expect(
      out.setCookie.some((v) => new RegExp(`^${SESSION_COOKIE}=;`).test(v) && /max-age=0/i.test(v)),
      `выход обязан гасить носитель, Set-Cookie: ${JSON.stringify(out.setCookie)}`,
    ).toBe(true);

    // Тогда: сохранённая копия S1 — отказ F4d-22 на стороне сервера; S2 — проходит.
    expectSessionLaneRefusal(await answerOf(await savedCopy.read(SESSION_IDENTITY)), "копия S1 после выхода");
    const alive = await answerOf(await second.read(SESSION_IDENTITY));
    expect(alive.status, `S2 после выхода из S1 обязана проходить — ${alive.text}`).toBe(200);
    expect((JSON.parse(alive.text) as { user?: { email?: unknown } }).user?.email).toBe(human.email);

    // Повтор выхода носителем снятой S1 через край — тот же 200 и то же гашение (Ф1-18, ретрансляция Р7).
    const again = await answerOf(await repeat.submit(LANE.logout, "logout", {}));
    expect({ status: again.status, text: again.text }, "повтор выхода снятым носителем — тот же 200 {}").toEqual({
      status: 200,
      text: "{}",
    });
    expect(
      again.setCookie.some((v) => new RegExp(`^${SESSION_COOKIE}=;`).test(v) && /max-age=0/i.test(v)),
      `повтор выхода обязан гасить носитель тем же образом, Set-Cookie: ${JSON.stringify(again.setCookie)}`,
    ).toBe(true);
    expect((await second.read(SESSION_IDENTITY)).status(), "повтор выхода из S1 не трогает S2").toBe(200);
  } finally {
    await Promise.all([first, second, savedCopy, repeat].map((s) => s.dispose()));
  }
});

test("Ф3-19 · смена пароля через край перевыпускает носитель, гасит прежний и прочие сессии, прежний пароль негоден", async ({ browserName: _browserName }, testInfo) => {
  // verifies #1269 — п.3 предиката: Ф3-19 и Ф1-15 (Ф3 20ccad56…) сквозной пробой через край на посадке own.
  test.setTimeout(240_000);
  const changer = await newSeed(testInfo);
  const human = await seedConfirmedHuman(changer, seedAddress("f3-19"));
  const other = await signedIn(testInfo, human.email, human.password, "прочая сессия");
  const before = await carrierOf(changer, "S");
  const oldCopy = await bareSeed(testInfo, [before]);
  const newPassword = `${SEED_PASSWORD}-changed`;
  try {
    const changed = await changer.submit(LANE.password, "password", {
      currentPassword: human.password,
      newPassword,
    });
    expect(changed.status(), `смена пароля — ${JSON.stringify(lastIssued(changer, LANE.password).body)}`).toBe(200);
    const after = await carrierOf(changer, "S после смены");
    expect(after.value, "смена пароля обязана перевыпустить носитель (F4d-19)").not.toBe(before.value);

    // Новый носитель — тот же человек; прежний носитель S и прочая сессия — отказ F4d-22.
    const current = await answerOf(await changer.read(SESSION_IDENTITY));
    expect(current.status, `новый носитель — ${current.text}`).toBe(200);
    expect((JSON.parse(current.text) as { user?: { email?: unknown } }).user?.email).toBe(human.email);
    const refusedOld = await answerOf(await oldCopy.read(SESSION_IDENTITY));
    const refusedOther = await answerOf(await other.read(SESSION_IDENTITY));
    expectSessionLaneRefusal(refusedOld, "прежний носитель S");
    expectSessionLaneRefusal(refusedOther, "прочая сессия после смены пароля");
    expect(refusedOther.text, "отказы прежнему и прочему носителю — один ответ").toBe(refusedOld.text);

    // Вход прежним паролем — 401; новым — 200 (обе стороны F4d-19).
    const oldLogin = await newSeed(testInfo);
    const oldPassword = await oldLogin.submit(LANE.login, "login", { email: human.email, password: human.password });
    await oldLogin.dispose();
    expect(oldPassword.status(), "вход прежним паролем после смены обязан отвергаться").toBe(401);
    const fresh = await signedIn(testInfo, human.email, newPassword, "вход новым паролем");
    await fresh.dispose();
  } finally {
    await Promise.all([changer, other, oldCopy].map((s) => s.dispose()));
  }
});
