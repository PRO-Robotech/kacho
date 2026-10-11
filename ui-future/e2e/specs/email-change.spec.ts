// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Browser, type BrowserContext, type Locator, type Page, type TestInfo } from "@playwright/test";
import { LANE_VERBS, captureAnswers, lanePostAnswer, type LaneAnswer } from "./answer-on-arrival";
import {
  LANE,
  SESSION_COOKIE,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  transferSession,
  type Seed,
  type SeededHuman,
} from "./ceremony-seed";
import { ceremonyCensus, formatCall, test, type CeremonyCensus } from "./fixtures";
import { AUTHENTICATION_FAILED } from "./lane-texts";
import {
  EMAIL_CHANGED_LINE,
  EMAIL_CHANGE_CODE_LINE,
  LETTER_BUDGET_MS,
  awaitLetter,
  awaitNotice,
  stationMailbox,
  type Mailbox,
} from "./mail-receiver";
import {
  EDGE_CREDENTIAL_STATE_UNKNOWN,
  EMAIL_CHANGE_ACCEPTED_SOONEST,
  EMAIL_CHANGE_TOO_MANY_ATTEMPTS,
  SESSION_NOT_FRESH,
  bodyOf,
  fulfillWith,
  type ProducerAnswer,
} from "./producer-answers";

/**
 * Смена адреса почты на `/settings` — раздел «Учётная запись», три состояния
 * (приёмка F8-S6, Р3), отказы (Р4), браузерный набор §5.
 *
 * Каждое «Тогда» утверждается наблюдаемым: переписью обращений страницы
 * (`ceremonyCensus`, приёмка F8, Р6), текстом раздела, письмами приёмника стенда
 * и тем, куда консоль ведёт контекст, чью сессию сняли, — а не разметкой.
 *
 * ПОСЕВЫ (§4). Человек — `seedConfirmedHuman` глаголами продукта, адрес у
 * каждого сценария свой (`seedAddress`). Посев сам берёт код подтверждения из
 * приёмника, поэтому без доставки писем ни один сценарий не строит «Дано» и
 * отвечает третьим исходом `conditionNotCreated` с причиной — не красным и не
 * пропуском (§4, kacho#1773).
 *
 * ОСЬ ВРЕМЕНИ. Свежесть и `Retry-After` не пересиживаются: ответ на ПЕРВОЕ
 * обращение подставляется побайтово ответом производителя (`producer-answers.ts`,
 * F8 Р9, ось 3а); повышение, повтор и второй запрос идут по-настоящему.
 *
 * НОВЫЙ АДРЕС. Свободный адрес `N` — своей формы у сценария (`seedAddress` с
 * пометкой «n»), его письма до запроса снимаются перечнем `before`, и письмо
 * смены ждётся по строке кода своего вида (`EMAIL_CHANGE_CODE_LINE`).
 */

const CHANGE = LANE.emailChange;
const CHANGE_CONFIRM = LANE.emailChangeConfirm;

/** Строка состояния «код» (Р3) — дословно. */
function sentLine(email: string): string {
  return `Письмо с кодом отправлено на ${email}, если этот адрес может быть использован`;
}

/** Отсчёт до следующего запроса в состоянии «код» (Р3). */
function resendWait(seconds: number | string): string {
  return `Отправить снова можно через ${seconds} с`;
}

/** Отсчёт до следующей отправки в состоянии «форма» после отказа по частоте (Р4). */
function submitWait(seconds: number | string): string {
  return `Отправить код можно через ${seconds} с`;
}

// ─── экран ───────────────────────────────────────────────────────────────────

function accountSection(page: Page) {
  const region = page.getByRole("region", { name: "Учётная запись" });
  return {
    region,
    change: region.getByRole("button", { name: "Сменить адрес" }),
    newEmail: region.getByLabel("Новый адрес", { exact: true }),
    send: region.getByRole("button", { name: /Отправить код$/ }),
    code: region.getByLabel("Код из письма", { exact: true }),
    confirm: region.getByRole("button", { name: /Подтвердить$/ }),
    resend: region.getByRole("button", { name: /Отправить снова$/ }),
    cancel: region.getByRole("button", { name: "Отмена" }),
    verified: region.getByText("Адрес подтверждён", { exact: true }),
    refusal: region.getByRole("alert"),
  };
}

type Section = ReturnType<typeof accountSection>;

function pathOf(page: Page): string {
  return new URL(page.url()).pathname;
}

/** Открыть `/settings` и дождаться раздела «Учётная запись» в покое — с кнопкой «Сменить адрес». */
async function openAccount(page: Page): Promise<Section> {
  await page.goto("/settings", { waitUntil: "domcontentloaded" });
  const s = accountSection(page);
  await expect
    .poll(
      async () => {
        if (await s.change.isVisible()) return "покой";
        const now = pathOf(page);
        if (now !== "/settings") return `страница уведена на ${now}`;
        return (await s.region.isVisible()) ? "раздел без «Сменить адрес»" : "ещё не отрисован";
      },
      { message: "раздел «Учётная запись» на /settings не показал «Сменить адрес» (Р3, покой)", timeout: 30_000 },
    )
    .toBe("покой");
  return s;
}

/** Покой → форма → «Отправить код» с адресом; ответ запроса смены — со снятым телом. */
async function requestChange(page: Page, s: Section, email: string): Promise<LaneAnswer> {
  await s.change.click();
  await s.newEmail.fill(email);
  const [res] = await Promise.all([lanePostAnswer(page, CHANGE), s.send.click()]);
  return res;
}

/** Ответ запроса смены — `200 {}` с `Retry-After` целыми секундами (EC Р1). */
function retryAfterOf(res: LaneAnswer): number {
  const raw = res.headers()["retry-after"] ?? "";
  expect(raw, `ответ запроса смены без Retry-After: ${JSON.stringify(res.headers())}`).toMatch(/^[1-9]\d*$/);
  return Number(raw);
}

async function expectCodeState(s: Section, email: string) {
  await expect(s.region.getByText(sentLine(email), { exact: true }), "экран не в состоянии «код» (Р3)").toBeVisible();
  await expect(s.code).toBeVisible();
}

async function expectRestState(s: Section, address: string) {
  await expect(s.change, "экран не вернулся в покой (Р3)").toBeVisible();
  await expect(s.newEmail).toHaveCount(0);
  await expect(s.code).toHaveCount(0);
  await expect(s.region).toContainText(address);
}

async function bearerOf(context: BrowserContext): Promise<string> {
  return (await context.cookies()).find((c) => c.name === SESSION_COOKIE)?.value ?? "";
}

function postsTo(census: CeremonyCensus, path: string) {
  return census.matching("POST", path).map((c) => c.outcome);
}

function expectNoProvider(census: CeremonyCensus) {
  expect(
    census.providerCalls().map(formatCall),
    `перепись обращений страницы содержит адрес чужого поставщика (F8 Р6):\n${census.describe()}`,
  ).toEqual([]);
}

/**
 * Обращения страницы ПОСЛЕ данного — по пути, в порядке выпуска. Перепись пишет
 * выпуск в порядке страницы, когда второе выпущено после ответа на первое.
 */
function after(census: CeremonyCensus, first: { method: string; path: string }, method: string, path: string) {
  const at = census.calls.findIndex((c) => c.method === first.method && c.path === first.path);
  return census.calls.slice(at + 1).filter((c) => c.method === method && c.path === path);
}

/** Ответ производителя на ПЕРВОЕ обращение `POST <path>`; остальные — настоящие. */
async function substituteFirst(page: Page, path: string, answer: ProducerAnswer): Promise<() => boolean> {
  let substituted = false;
  await page.route(
    (u) => u.pathname === path,
    async (route) => {
      if (route.request().method() !== "POST" || substituted) return route.continue();
      substituted = true;
      await fulfillWith(route, answer);
    },
  );
  return () => substituted;
}

// ─── посевы ──────────────────────────────────────────────────────────────────

/** Свободный адрес `N` сценария — своей формы, не совпадающей с адресом посева. */
function freeAddress(scenario: string, which = "n"): string {
  return seedAddress(`${scenario}-${which}`);
}

async function idsOf(mailbox: Mailbox, address: string): Promise<Set<string>> {
  return new Set((await mailbox.letters(address)).map((l) => l.id));
}

/** Письмо смены по `address`, принятое после `before`, — код взят по строке своего вида. */
function changeLetter(mailbox: Mailbox, address: string, before: ReadonlySet<string>) {
  return awaitLetter(mailbox, address, before, LETTER_BUDGET_MS, EMAIL_CHANGE_CODE_LINE);
}

/** Посев Ч: человек с подтверждённым адресом, носитель — у страницы. */
async function withHuman<T>(
  testInfo: TestInfo,
  page: Page,
  scenario: string,
  body: (seed: Seed, human: SeededHuman) => Promise<T>,
): Promise<T> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedConfirmedHuman(seed, seedAddress(scenario));
    await transferSession(seed, page.context());
    return await body(seed, human);
  } finally {
    await seed.dispose();
  }
}

async function secondContext(browser: Browser, testInfo: TestInfo): Promise<BrowserContext> {
  const use = testInfo.project.use;
  return browser.newContext({ baseURL: use.baseURL, ignoreHTTPSErrors: use.ignoreHTTPSErrors });
}

/** Вход человека посевом — новая сессия у этого посева; шаг утверждает исход. */
async function signInSeed(seed: Seed, email: string, password: string, which: string) {
  const res = await seed.submit(LANE.login, "login", { email, password });
  expect(
    res.status(),
    `посев ${which}: вход ${email} отвергнут — ${res.status()} ${JSON.stringify(lastIssued(seed, LANE.login).body)}. ` +
      "Это УСЛОВИЕ сценария",
  ).toBe(200);
}

/**
 * Куда консоль ведёт контекст на следующем действии: «панель» — край принял
 * носитель и каркас отрисован; «экран входа» — сессии нет.
 */
async function panelOutcome(page: Page): Promise<string> {
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" });
  const account = page.getByRole("button", { name: "Учётная запись" });
  let outcome = "ждём";
  await expect
    .poll(
      async () => {
        const now = pathOf(page);
        if (now === "/login") outcome = "экран входа";
        else if (now === "/dashboard" && (await account.isVisible())) outcome = "панель";
        else outcome = `ждём (${now})`;
        return outcome.startsWith("ждём") ? "ждём" : "решено";
      },
      { message: "консоль не решила, вести ли контекст на панель или на вход", timeout: 30_000 },
    )
    .toBe("решено");
  return outcome;
}

/** Кнопка отправки формы по концу имени: значок ожидания входит в доступное имя. */
function loginScreen(page: Page) {
  return {
    email: page.getByRole("textbox", { name: "Адрес электронной почты" }),
    password: page.getByLabel("Пароль", { exact: true }),
    submit: page.getByRole("form", { name: "Вход в консоль" }).getByRole("button", { name: /Войти$/ }),
  };
}

async function loginOnScreen(page: Page, email: string, password: string): Promise<LaneAnswer> {
  if (pathOf(page) !== "/login") await page.goto("/login", { waitUntil: "domcontentloaded" });
  const l = loginScreen(page);
  await expect(l.email, "экран входа не отрисован").toBeVisible();
  await l.email.fill(email);
  await l.password.fill(password);
  const [res] = await Promise.all([lanePostAnswer(page, LANE.login), l.submit.click()]);
  return res;
}

function locatorText(l: Locator): Promise<string> {
  return l.innerText();
}

// Ответы глаголов полосы читаются тем же путём, что у соседних экранов
// (`answer-on-arrival.ts`): перехват ставится до отправки.
test.beforeEach(async ({ page }) => {
  await captureAnswers(page, LANE_VERBS);
});

// ═══ F8-S6 · смена адреса почты ═══════════════════════════════════════════════

test("F8S6-01 · смена адреса целиком на /settings, вход новым адресом", async ({ page, browser }, testInfo) => {
  // verifies #3062 — близнецы: F8S6-13 (тот же шаг с «Отменой»), F8S6-04 (а) (неверный код).
  test.setTimeout(240_000);
  const mailbox = stationMailbox();
  const seedB = await newSeed(testInfo);
  const ctxB = await secondContext(browser, testInfo);
  try {
    await withHuman(testInfo, page, "F8S6-01", async (_seed, human) => {
      // Посев Ч2: второй контекст — другая сессия того же человека.
      await signInSeed(seedB, human.email, human.password, "B");
      await transferSession(seedB, ctxB);
      const pageB = await ctxB.newPage();
      // Вход во втором контексте читается тем же путём, что ответы первого:
      // перехват ставится до отправки (`answer-on-arrival.ts`).
      await captureAnswers(pageB, LANE_VERBS);
      expect(await panelOutcome(pageB), "Дано: второй контекст не открыл панель").toBe("панель");

      const n = freeAddress("F8S6-01");
      const beforeN = await idsOf(mailbox, n);
      const beforeOld = await idsOf(mailbox, human.email);
      const census = ceremonyCensus(page.context());
      const s = await openAccount(page);
      const requested = await requestChange(page, s, n);
      expect(requested.status(), `запрос смены не прошёл: ${await requested.text()}`).toBe(200);
      const wait = retryAfterOf(requested);
      await expectCodeState(s, n);
      await expect(s.resend, "«Отправить снова» включена сразу после запроса").toBeDisabled();
      await expect(s.region.getByText(resendWait(wait), { exact: true }), "отсчёт не равен Retry-After").toBeVisible();

      const letter = await changeLetter(mailbox, n, beforeN);
      await s.code.fill(letter.code);
      const me = page.waitForResponse(
        (r) => new URL(r.url()).pathname === SESSION_IDENTITY && r.request().method() === "GET",
      );
      const [confirmed] = await Promise.all([lanePostAnswer(page, CHANGE_CONFIRM), s.confirm.click()]);
      expect(confirmed.status(), `предъявление кода не прошло: ${await confirmed.text()}`).toBe(200);
      expect((await me).status(), "«кто я» после смены не прочитан").toBe(200);
      await expect(s.region, "раздел не показывает новый адрес").toContainText(n);
      await expect(s.verified).toBeVisible();
      await expect(s.change).toBeVisible();

      expect(postsTo(census, CHANGE), census.describe()).toEqual([200]);
      expect(postsTo(census, CHANGE_CONFIRM), census.describe()).toEqual([200]);
      expect(
        after(census, { method: "POST", path: CHANGE_CONFIRM }, "GET", SESSION_IDENTITY).map((c) => c.outcome)[0],
        `после предъявления «кто я» не спрошен:\n${census.describe()}`,
      ).toBe(200);
      expectNoProvider(census);

      // Вторая сессия снята исходом смены (EC Р8 п. 4).
      expect(await panelOutcome(pageB), "сессия второго контекста не снята сменой").toBe("экран входа");
      // Уведомление — на прежний адрес, нового адреса в нём нет (EC Р7).
      const notice = await awaitNotice(mailbox, human.email, beforeOld, EMAIL_CHANGED_LINE);
      expect(notice.text.toLowerCase(), "уведомление на прежний адрес несёт новый адрес").not.toContain(
        n.toLowerCase(),
      );
      // Вход прежним адресом — отказ; новым — консоль (EC-03).
      // Отказ — тот, что служба отвечает на неверное предъявление входа: 401 с
      // единым телом (код 16, текст службы, без причины края), и экран его называет.
      const refused = await loginOnScreen(pageB, human.email, human.password);
      expect(refused.status(), "вход прежним адресом: отказ входа обязан быть 401").toBe(401);
      expect(await refused.text(), "вход прежним адресом: тело — не единый отказ входа службы").toBe(
        JSON.stringify({ code: 16, message: AUTHENTICATION_FAILED, details: [] }),
      );
      await expect(pageB.getByRole("alert"), "отказ входа прежним адресом не назван на экране").toHaveText(
        AUTHENTICATION_FAILED,
      );
      expect(pathOf(pageB)).toBe("/login");
      const signed = await loginOnScreen(pageB, n, human.password);
      expect(signed.status(), `вход новым адресом не прошёл: ${await signed.text()}`).toBe(200);
      expect(await panelOutcome(pageB), "вход новым адресом не открыл консоль").toBe("панель");
    });
  } finally {
    await ctxB.close();
    await seedB.dispose();
  }
});

test("F8S6-02 · занятый адрес — экран тот же, что для свободного", async ({ page, browser }, testInfo) => {
  // verifies #3062 — посевы различаются только занятостью адреса.
  test.setTimeout(240_000);
  const mailbox = stationMailbox();
  const seedTaken = await newSeed(testInfo);
  const seedFree = await newSeed(testInfo);
  const ctxFree = await secondContext(browser, testInfo);
  try {
    await withHuman(testInfo, page, "F8S6-02", async () => {
      // Посев Занято: адрес N — у второго человека, его письмо регистрации истрачено.
      const n = freeAddress("F8S6-02");
      await seedConfirmedHuman(seedTaken, n);
      // Отдельный посев Ч тем же шагом; его N₂ свободен.
      await seedConfirmedHuman(seedFree, seedAddress("F8S6-02-free"));
      await transferSession(seedFree, ctxFree);
      const pageFree = await ctxFree.newPage();
      await captureAnswers(pageFree, LANE_VERBS);
      const n2 = freeAddress("F8S6-02", "n2");
      const beforeN = await idsOf(mailbox, n);
      const beforeN2 = await idsOf(mailbox, n2);

      const s = await openAccount(page);
      const taken = await requestChange(page, s, n);
      const sFree = await openAccount(pageFree);
      const free = await requestChange(pageFree, sFree, n2);
      expect([taken.status(), free.status()], "ответы на занятый и свободный различаются статусом").toEqual([200, 200]);
      expect(await taken.text(), "тела ответов различаются").toBe(await free.text());
      expect(retryAfterOf(taken), "отсчёты разной длины").toBe(retryAfterOf(free));
      await expectCodeState(s, n);
      await expectCodeState(sFree, n2);

      await changeLetter(mailbox, n2, beforeN2);
      // Срок отсутствия задаёт письмо по N₂, запрошенное позже: снимок — не ожидание.
      const lettersN = (await mailbox.letters(n)).filter((l) => !beforeN.has(l.id));
      expect(
        lettersN.filter((l) => l.text.includes(EMAIL_CHANGE_CODE_LINE)).map((l) => l.id),
        "по занятому адресу ушло письмо смены",
      ).toEqual([]);
    });
  } finally {
    await ctxFree.close();
    await seedTaken.dispose();
    await seedFree.dispose();
  }
});

test("F8S6-03 · несвежая сессия — повышение и возврат к тому же шагу", async ({ page }, testInfo) => {
  // verifies #3062 — близнец: F8S6-01 (тот же шаг без подставленного отказа).
  const substituted = await substituteFirst(page, CHANGE, SESSION_NOT_FRESH);
  await withHuman(testInfo, page, "F8S6-03", async (_seed, human) => {
    const census = ceremonyCensus(page.context());
    const s = await openAccount(page);
    const n = freeAddress("F8S6-03");
    const refused = await requestChange(page, s, n);
    expect(refused.status()).toBe(403);
    const dialog = page.getByRole("dialog", { name: "Подтверждение действия" });
    await expect(dialog, "консоль не открыла церемонию повышения на SESSION_NOT_FRESH").toBeVisible();
    await dialog.getByRole("radio", { name: "Паролем" }).check();
    await dialog.getByLabel("Пароль", { exact: true }).fill(human.password);
    const retried = lanePostAnswer(page, CHANGE);
    const [raised] = await Promise.all([
      lanePostAnswer(page, LANE.stepUp),
      dialog.getByRole("button", { name: "Подтвердить" }).click(),
    ]);
    expect(raised.status(), `повышение паролем не прошло: ${await raised.text()}`).toBe(200);
    expect((await retried).status(), "повтор запроса смены после повышения не прошёл").toBe(200);
    await expect(dialog).toBeHidden();
    await expectCodeState(s, n);
    expect(pathOf(page), "консоль ушла с экрана параметров").toBe("/settings");
    expect(postsTo(census, CHANGE), census.describe()).toEqual([403, 200]);
    expect(postsTo(census, LANE.stepUp), census.describe()).toEqual([200]);
    expect(substituted()).toBe(true);
    expectNoProvider(census);
  });
});

test("F8S6-04 · неверный код назван, состояние сохранено", async ({ page }, testInfo) => {
  // verifies #3062 — (б) — близнец (а): тот же шаг с кодом из письма.
  test.setTimeout(180_000);
  const mailbox = stationMailbox();
  await withHuman(testInfo, page, "F8S6-04", async (_seed, human) => {
    const n = freeAddress("F8S6-04");
    const beforeN = await idsOf(mailbox, n);
    const census = ceremonyCensus(page.context());
    const s = await openAccount(page);
    expect((await requestChange(page, s, n)).status()).toBe(200);
    await expectCodeState(s, n);
    const letter = await changeLetter(mailbox, n, beforeN);
    const wrong = letter.code.replace(/[-\s]/g, "").toUpperCase() === "0000000000" ? "1111111111" : "0000000000";

    await s.code.fill(wrong);
    const [refused] = await Promise.all([lanePostAnswer(page, CHANGE_CONFIRM), s.confirm.click()]);
    expect(refused.status()).toBe(401);
    await expect(s.region.getByText("Код не подошёл", { exact: true })).toBeVisible();
    await expect(s.code, "поле кода не очищено").toHaveValue("");
    await expectCodeState(s, n);
    await expect(s.region).toContainText(human.email);
    expect(
      after(census, { method: "POST", path: CHANGE_CONFIRM }, "GET", SESSION_IDENTITY).map((c) => c.outcome),
      `после 401 «кто я» спрошен не один раз либо не 200:\n${census.describe()}`,
    ).toEqual([200]);

    await s.code.fill(letter.code);
    const [confirmed] = await Promise.all([lanePostAnswer(page, CHANGE_CONFIRM), s.confirm.click()]);
    expect(confirmed.status(), `код из письма не принят: ${await confirmed.text()}`).toBe(200);
    await expect(s.region).toContainText(n);
    expect(postsTo(census, CHANGE_CONFIRM), census.describe()).toEqual([401, 200]);
    expectNoProvider(census);
  });
});

test("F8S6-05 · адрес заняли до подтверждения — отказ назван", async ({ page }, testInfo) => {
  // verifies #3062 — близнец: F8S6-04 (б) — тот же порядок без занятия адреса.
  test.setTimeout(240_000);
  const mailbox = stationMailbox();
  const seedTaker = await newSeed(testInfo);
  try {
    await withHuman(testInfo, page, "F8S6-05", async (_seed, human) => {
      const n = freeAddress("F8S6-05");
      const beforeN = await idsOf(mailbox, n);
      const census = ceremonyCensus(page.context());
      const s = await openAccount(page);
      expect((await requestChange(page, s, n)).status()).toBe(200);
      await expectCodeState(s, n);
      // Код взят ДО посева занятости: перечень посева уже несёт письмо смены.
      const letter = await changeLetter(mailbox, n, beforeN);
      await seedConfirmedHuman(seedTaker, n);

      await s.code.fill(letter.code);
      const [refused] = await Promise.all([lanePostAnswer(page, CHANGE_CONFIRM), s.confirm.click()]);
      expect(refused.status(), await refused.text()).toBe(409);
      const body = (await refused.json()) as { details?: Array<{ reason?: string }> };
      expect((body.details ?? []).map((d) => d.reason)).toContain("EMAIL_IN_USE");
      await expect(s.region.getByText("Этот адрес уже используется", { exact: true })).toBeVisible();
      await expect(s.newEmail, "экран не в состоянии «форма»").toBeVisible();
      await expect(s.code).toHaveCount(0);
      await expect(s.region).toContainText(human.email);
      expectNoProvider(census);
    });
  } finally {
    await seedTaker.dispose();
  }
});

test("F8S6-06 · темп — отсчёт по ответу службы", async ({ page }, testInfo) => {
  // verifies #3062 — близнец: F8S6-01 (то же первое обращение без подставленного отказа даёт «код»).
  const substituted = await substituteFirst(page, CHANGE, EMAIL_CHANGE_TOO_MANY_ATTEMPTS);
  const seconds = Number(EMAIL_CHANGE_TOO_MANY_ATTEMPTS.headers["Retry-After"]);
  await withHuman(testInfo, page, "F8S6-06", async () => {
    const census = ceremonyCensus(page.context());
    const s = await openAccount(page);
    const refused = await requestChange(page, s, freeAddress("F8S6-06"));
    expect(refused.status()).toBe(429);
    await expect(s.region.getByText("Слишком много попыток", { exact: true })).toBeVisible();
    await expect(
      s.region.getByText(submitWait(seconds), { exact: true }),
      "отсчёт не начат с Retry-After",
    ).toBeVisible();
    await expect(s.send, "«Отправить код» включена во время отсчёта").toBeDisabled();
    await expect(s.newEmail, "экран не в состоянии «форма»").toBeVisible();
    await expect(s.code).toHaveCount(0);
    expect(postsTo(census, CHANGE), census.describe()).toEqual([429]);
    expect(postsTo(census, CHANGE_CONFIRM), census.describe()).toEqual([]);
    expect(substituted()).toBe(true);
    expectNoProvider(census);
  });
});

test("F8S6-07 · отказ поля у поля", async ({ page }, testInfo) => {
  // verifies #3062 — близнец: F8S6-01 (тот же шаг со свободным адресом даёт «код»).
  await withHuman(testInfo, page, "F8S6-07", async (_seed, human) => {
    const census = ceremonyCensus(page.context());
    const s = await openAccount(page);
    const invalid = await requestChange(page, s, "not-an-address");
    expect(invalid.status()).toBe(400);
    await expect(s.region.getByText("Illegal argument newEmail: invalid format", { exact: true })).toBeVisible();
    await expect(s.newEmail).toHaveAttribute("aria-invalid", "true");
    await expect(s.code).toHaveCount(0);

    await s.newEmail.fill(human.email.toUpperCase());
    const [same] = await Promise.all([lanePostAnswer(page, CHANGE), s.send.click()]);
    expect(same.status()).toBe(400);
    await expect(
      s.region.getByText("Illegal argument newEmail: must differ from the current address", { exact: true }),
    ).toBeVisible();
    await expect(s.newEmail).toBeVisible();
    await expect(s.code).toHaveCount(0);
    expect(postsTo(census, CHANGE_CONFIRM), census.describe()).toEqual([]);
    expectNoProvider(census);
  });
});

test("F8S6-08 · служба молчит на предъявлении — ответ края, сессия цела", async ({ page }, testInfo) => {
  // verifies #3062 — близнец: F8S6-04 (б) (тот же шаг, служба отвечает).
  const unanswered = bodyOf(EDGE_CREDENTIAL_STATE_UNKNOWN);
  await page.route(
    (u) => u.pathname === CHANGE_CONFIRM,
    (route) =>
      route.request().method() === "POST" ? fulfillWith(route, EDGE_CREDENTIAL_STATE_UNKNOWN) : route.continue(),
  );
  await withHuman(testInfo, page, "F8S6-08", async (_seed, human) => {
    const census = ceremonyCensus(page.context());
    const s = await openAccount(page);
    const n = freeAddress("F8S6-08");
    expect((await requestChange(page, s, n)).status()).toBe(200);
    await expectCodeState(s, n);
    const carrier = await bearerOf(page.context());
    await s.code.fill("ABCDE-FGHJK");
    const [res] = await Promise.all([lanePostAnswer(page, CHANGE_CONFIRM), s.confirm.click()]);
    expect(res.status()).toBe(EDGE_CREDENTIAL_STATE_UNKNOWN.status);
    await expect(s.refusal, "ответ края не назван на экране").toContainText(unanswered.message);
    expect(await bearerOf(page.context()), "носитель в хранилище браузера сменился").toBe(carrier);
    const me = await page.request.get(SESSION_IDENTITY);
    expect(me.status(), "следующее чтение «кто я» не 200").toBe(200);
    expect(((await me.json()) as { user?: { email?: string } }).user?.email).toBe(human.email);
    expect(postsTo(census, CHANGE_CONFIRM), census.describe()).toHaveLength(1);
    expect(pathOf(page), "консоль ушла с экрана параметров").toBe("/settings");
    await expectCodeState(s, n);
    expectNoProvider(census);
  });
});

test("F8S6-09 · сессию сняли до предъявления — экран входа, а не «код не подошёл»", async ({
  page,
  browser,
}, testInfo) => {
  // verifies #3062 — близнец: F8S6-04 (б) (тот же код из письма при живой сессии даёт 200).
  test.setTimeout(180_000);
  const mailbox = stationMailbox();
  const ctxSame = await secondContext(browser, testInfo);
  try {
    await withHuman(testInfo, page, "F8S6-09", async (seed) => {
      // Посев Ч′: второй контекст несёт ТОТ ЖЕ носитель.
      await transferSession(seed, ctxSame);
      const pageSame = await ctxSame.newPage();
      const n = freeAddress("F8S6-09");
      const beforeN = await idsOf(mailbox, n);
      const census = ceremonyCensus(page.context());
      const s = await openAccount(page);
      expect((await requestChange(page, s, n)).status()).toBe(200);
      await expectCodeState(s, n);
      const letter = await changeLetter(mailbox, n, beforeN);

      expect(await panelOutcome(pageSame), "Дано: второй контекст не открыл панель").toBe("панель");
      await pageSame.getByRole("button", { name: "Учётная запись" }).click();
      await pageSame
        .getByRole("dialog", { name: "Учётная запись" })
        .getByRole("button", { name: /Выйти$/ })
        .click();
      await expect
        .poll(() => pathOf(pageSame), { message: "второй контекст не вышел", timeout: 30_000 })
        .toBe("/login");

      await s.code.fill(letter.code);
      const [refused] = await Promise.all([lanePostAnswer(page, CHANGE_CONFIRM), s.confirm.click()]);
      expect(refused.status()).toBe(401);
      await expect
        .poll(() => pathOf(page), { message: "консоль не увела на экран входа", timeout: 30_000 })
        .toBe("/login");
      expect(
        after(census, { method: "POST", path: CHANGE_CONFIRM }, "GET", SESSION_IDENTITY).map((c) => c.outcome)[0],
        `после 401 «кто я» не спрошен либо не 401:\n${census.describe()}`,
      ).toBe(401);
      expect(postsTo(census, CHANGE_CONFIRM), `второе предъявление:\n${census.describe()}`).toHaveLength(1);
      await expect(page.getByText("Код не подошёл", { exact: true })).toHaveCount(0);
      expectNoProvider(census);
    });
  } finally {
    await ctxSame.close();
  }
});

test("F8S6-12 · пустой код — отказ службы у поля кода", async ({ page }, testInfo) => {
  // verifies #3062 — близнец: F8S6-04 (б) (тот же шаг с кодом из письма даёт 200).
  await withHuman(testInfo, page, "F8S6-12", async (_seed, human) => {
    const census = ceremonyCensus(page.context());
    const s = await openAccount(page);
    const n = freeAddress("F8S6-12");
    expect((await requestChange(page, s, n)).status()).toBe(200);
    await expectCodeState(s, n);
    await expect(s.code).toHaveValue("");
    await expect(s.confirm, "«Подтвердить» выключена на пустом поле").toBeEnabled();
    const [refused] = await Promise.all([lanePostAnswer(page, CHANGE_CONFIRM), s.confirm.click()]);
    expect(refused.status()).toBe(400);
    expect(((await refused.json()) as { code?: number }).code).toBe(3);
    await expect(s.region.getByText("Illegal argument code: required", { exact: true })).toBeVisible();
    await expect(s.code).toHaveAttribute("aria-invalid", "true");
    await expectCodeState(s, n);
    await expect(s.region).toContainText(human.email);
    expect(
      after(census, { method: "POST", path: CHANGE_CONFIRM }, "GET", SESSION_IDENTITY),
      `после 400 спрошен «кто я»:\n${census.describe()}`,
    ).toEqual([]);
    expectNoProvider(census);
  });
});

test("F8S6-13 · «Отмена» возвращает в покой без обращения к глаголам", async ({ page }, testInfo) => {
  // verifies #3062 — близнец: F8S6-01 (тот же шаг «Отправить код» без «Отмены» оставляет «код»).
  await withHuman(testInfo, page, "F8S6-13", async (_seed, human) => {
    const census = ceremonyCensus(page.context());
    const s = await openAccount(page);
    const n = freeAddress("F8S6-13");
    // (а) из формы.
    await s.change.click();
    await s.newEmail.fill(n);
    await s.cancel.click();
    await expectRestState(s, human.email);
    expect([postsTo(census, CHANGE), postsTo(census, CHANGE_CONFIRM)], census.describe()).toEqual([[], []]);
    // (б) из кода.
    expect((await requestChange(page, s, n)).status()).toBe(200);
    await expectCodeState(s, n);
    await s.cancel.click();
    await expectRestState(s, human.email);
    expect(await locatorText(s.region), "в покое адрес не прежний").toContain(human.email);
    expect([postsTo(census, CHANGE), postsTo(census, CHANGE_CONFIRM)], census.describe()).toEqual([[200], []]);
    expectNoProvider(census);
  });
});

test("F8S6-14 · «Отправить снова» по концу отсчёта зовёт запрос ещё раз", async ({ page }, testInfo) => {
  // verifies #3062 — близнец: F8S6-01 (настоящий первый ответ оставляет «Отправить снова» выключенной).
  test.setTimeout(180_000);
  const mailbox = stationMailbox();
  const substituted = await substituteFirst(page, CHANGE, EMAIL_CHANGE_ACCEPTED_SOONEST);
  await withHuman(testInfo, page, "F8S6-14", async () => {
    const n = freeAddress("F8S6-14");
    const beforeN = await idsOf(mailbox, n);
    const census = ceremonyCensus(page.context());
    const s = await openAccount(page);
    const first = await requestChange(page, s, n);
    expect(first.status()).toBe(200);
    expect(substituted(), "первый ответ не подставлен").toBe(true);
    await expectCodeState(s, n);
    await expect(s.resend).toBeDisabled();
    await expect(s.region.getByText(resendWait(1), { exact: true })).toBeVisible();
    // Ждётся УСЛОВИЕ «кнопка включена», а не время (F6b-19).
    await expect(s.resend, "«Отправить снова» не включилась по концу отсчёта").toBeEnabled();
    const [second] = await Promise.all([lanePostAnswer(page, CHANGE), s.resend.click()]);
    expect(second.status(), `второй запрос не прошёл: ${await second.text()}`).toBe(200);
    const wait = retryAfterOf(second);
    await expectCodeState(s, n);
    await expect(s.resend).toBeDisabled();
    await expect(s.region.getByText(resendWait(wait), { exact: true }), "отсчёт не по второму ответу").toBeVisible();
    expect(postsTo(census, CHANGE), census.describe()).toEqual([200, 200]);
    expect(JSON.parse(first.request().postData() ?? "{}").newEmail).toBe(n);
    expect(JSON.parse(second.request().postData() ?? "{}").newEmail).toBe(n);
    expect(postsTo(census, CHANGE_CONFIRM), census.describe()).toEqual([]);
    await changeLetter(mailbox, n, beforeN);
    expectNoProvider(census);
  });
});
