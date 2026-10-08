// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Browser, type BrowserContext, type Locator, type Page, type TestInfo } from "@playwright/test";
import { authenticatorWithKey, revokeAccessKey, seedAccessKey, type SeededAccessKey } from "./access-key-seed";
import { captureAnswers, lanePostAnswer, type LaneAnswer } from "./answer-on-arrival";
import {
  LANE,
  SESSION_COOKIE,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  seedSecondFactor,
} from "./ceremony-seed";
import { ceremonyCensus, formatCall, test, watchRefusals, type CeremonyCensus } from "./fixtures";

/**
 * Вход ключом доступа на экране входа консоли (приёмка F8-S4, группа K).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ И ЧЕМ
 *
 * Наблюдаемым, а не разметкой: переписью обращений страницы (`ceremonyCensus`),
 * телом запроса, как его отправил браузер, телом ответа, снятым до страницы
 * (`answer-on-arrival.ts`), адресом страницы и печеньем у браузера. Церемония
 * браузера — НАСТОЯЩАЯ: экран зовёт `navigator.credentials.get`, и отвечает ему
 * виртуальный аутентификатор браузера с удостоверением посева К
 * (`access-key-seed.ts`); подмены интерфейса браузера здесь нет.
 *
 * ИМЯ ТЕСТА НАЧИНАЕТСЯ С ID СЦЕНАРИЯ (соглашения §11 приёмки F8). Ссылка на
 * задачу — внутри вызова `test(…)`.
 *
 * УСЛОВИЕ ПРОГОНА (§4 П4). Происхождение браузера набора служба обязана принимать
 * для ключей доступа. Не принимает — посев К называет «условие не создано», и
 * исход сценария — «не выполнилось», а не красное о продукте.
 *
 * ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО
 *
 *   • ожидания временем: ждётся условие — отрисованный экран, ответ, адрес;
 *   • общего человека у сценариев: каждый сеет своего (Р7 ч. 1 приёмки F8);
 *   • подставленных ответов: каждый ответ — ответ службы через край.
 */

const BEGIN = "/iam/v1/auth/access-key/begin";
const ACCESS_KEY_LOGIN = "/iam/v1/auth/access-key/login";
const ACCESS_KEY_VERBS = [BEGIN, ACCESS_KEY_LOGIN] as const;

/**
 * Текст консоли на отказ церемонии браузером (приёмка F8-S4, Р5) — дословно;
 * литерал, а не импорт экрана: проба судит то, что видит человек, а не
 * константу, которую экран мог бы переписать вместе с ней.
 */
const ACCESS_KEY_BROWSER_REFUSED = "Вход ключом прерван в браузере — повторите или войдите паролем";

/** Тело отказа входа — одно на все причины (Ф13 Р7). */
const AUTHENTICATION_FAILED = { code: 16, message: "authentication failed", details: [] };

// ─── экран: доступные имена, а не классы ──────────────────────────────────────

// Кнопка — по КОНЦУ доступного имени: значок ожидания кнопки остаётся в разметке
// свёрнутым, и имя бывает «loading Войти…» при кнопке, которая уже не ждёт
// (тот же довод, что у `submitOf` в `identity-ceremony.spec.ts`).
function loginScreen(page: Page) {
  return {
    email: page.getByRole("textbox", { name: "Адрес электронной почты" }),
    password: page.getByLabel("Пароль", { exact: true }),
    submit: page.getByRole("form", { name: "Вход в консоль" }).getByRole("button", { name: /Войти$/ }),
    key: page.getByRole("button", { name: /Войти ключом доступа$/ }),
    refusal: page.getByRole("alert"),
  };
}

function pathOf(page: Page): string {
  return new URL(page.url()).pathname;
}

/** Экран отрисован НА СВОЁМ адресе; падение называет, куда увели страницу. */
async function expectScreen(page: Page, marker: Locator, what: string) {
  await expect
    .poll(
      async () => {
        if (await marker.isVisible()) return "отрисован";
        const now = pathOf(page);
        return now === "/login" ? "ещё не отрисован" : `страница уведена на ${now}`;
      },
      { message: `${what} на /login не отрисован консолью`, timeout: 30_000 },
    )
    .toBe("отрисован");
}

async function expectAddress(page: Page, address: string, why: string) {
  await expect
    .poll(
      () => {
        const u = new URL(page.url());
        return `${u.pathname}${u.search}`;
      },
      { message: why, timeout: 30_000 },
    )
    .toBe(address);
}

async function sessionHeld(context: BrowserContext): Promise<boolean> {
  return (await context.cookies()).some((c) => c.name === SESSION_COOKIE && c.value !== "");
}

function countOf(census: CeremonyCensus, method: string, path: string, query?: string): number {
  return census.matching(method, path, query).length;
}

function expectNoProvider(census: CeremonyCensus) {
  expect(
    census.providerCalls().map(formatCall),
    `перепись обращений страницы содержит адрес чужого поставщика:\n${census.describe()}`,
  ).toEqual([]);
}

/** Ключи объекта по порядку имени — форма тела, а не его значения. */
function keysOf(value: unknown): string[] {
  return value && typeof value === "object" ? Object.keys(value).sort() : [];
}

/**
 * Порядок «ответ испытания → запрос входа»: вход выпущен после ответа на своё
 * испытание. Лента пишется событиями контекста, не временем.
 */
function watchOrder(context: BrowserContext): string[] {
  const tape: string[] = [];
  context.on("request", (r) => {
    const p = new URL(r.url()).pathname;
    if (r.method() === "POST" && p === ACCESS_KEY_LOGIN) tape.push("запрос входа");
  });
  context.on("response", (r) => {
    const p = new URL(r.url()).pathname;
    if (r.request().method() === "POST" && p === BEGIN) tape.push(`ответ испытания ${r.status()}`);
  });
  return tape;
}

// ─── «Дано» ───────────────────────────────────────────────────────────────────

interface Given {
  human: { email: string };
  key: SeededAccessKey;
}

/**
 * Посев П-п и посев К (либо К-снят) своим контекстом запросов. `secondFactor` —
 * до посева К человеку заводится второй фактор (F8S4-10).
 */
async function given(
  testInfo: TestInfo,
  scenario: string,
  opts: { userVerification?: boolean; revoked?: boolean; secondFactor?: boolean } = {},
): Promise<Given> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedConfirmedHuman(seed, seedAddress(scenario));
    if (opts.secondFactor) await seedSecondFactor(seed);
    const key = await seedAccessKey(seed, testInfo.project.use.baseURL, {
      userVerification: opts.userVerification ?? true,
    });
    if (opts.revoked) await revokeAccessKey(seed, key);
    return { human, key };
  } finally {
    await seed.dispose();
  }
}

/** Страница сценария: аутентификатор с ключом — ДО открытия, ответы снимаются до страницы. */
async function openLogin(page: Page, key: SeededAccessKey, returnTo = "/dashboard", k = 1) {
  await authenticatorWithKey(page, key, k);
  await captureAnswers(page, ACCESS_KEY_VERBS);
  await page.goto(`/login?returnTo=${encodeURIComponent(returnTo)}`, { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, s.key, "кнопка входа ключом доступа");
  return s;
}

/** Нажать кнопку ключа и получить ответ входа ключом — со снятым телом. */
async function pressKey(page: Page, press: () => Promise<void>): Promise<LaneAnswer> {
  const [res] = await Promise.all([lanePostAnswer(page, ACCESS_KEY_LOGIN), press()]);
  return res;
}

async function sessionLevel(res: LaneAnswer): Promise<string> {
  const body = (await res.json()) as { session?: { assuranceLevel?: unknown } };
  return String(body.session?.assuranceLevel ?? "");
}

/** Отказ входа ключом на экране: единый отказ дословно, сессии нет, пароль доступен. */
async function expectRefusedScreen(page: Page, res: LaneAnswer) {
  expect(res.status(), "отказ входа ключом обязан быть 401").toBe(401);
  expect(await res.text(), "тело отказа входа — побайтово одно на все причины").toBe(
    JSON.stringify(AUTHENTICATION_FAILED),
  );
  const s = loginScreen(page);
  await expect(s.refusal, "отказ не назван на экране").toHaveText(AUTHENTICATION_FAILED.message);
  expect(pathOf(page), "после отказа адрес страницы сменился").toBe("/login");
  expect(await sessionHeld(page.context()), "после отказа у браузера появился носитель сессии").toBe(false);
  for (const control of [s.email, s.password, s.submit, s.key]) await expect(control).toBeEnabled();
}

// ═══ Группа K. Экран и полоса входа ключом ════════════════════════════════════

test("F8S4-01 · кнопка ключа стоит рядом с формой пароля, и открытие экрана испытания не просит", async ({
  page,
}, testInfo) => {
  // verifies #1282 — близнец F8S4-02: «Дано» и адрес те же, кнопка не нажата.
  const { key } = await given(testInfo, "F8S4-01");
  const census = ceremonyCensus(page.context());
  const s = await openLogin(page, key);
  for (const control of [s.email, s.password, s.submit, s.key]) await expect(control).toBeVisible();
  expect(
    [countOf(census, "POST", BEGIN), countOf(census, "POST", ACCESS_KEY_LOGIN)],
    `открытие экрана обратилось к полосе ключа:\n${census.describe()}`,
  ).toEqual([0, 0]);
  expect(pathOf(page)).toBe("/login");
  expect(await sessionHeld(page.context()), "печенье сессии появилось без входа").toBe(false);
});

test("F8S4-02 · вход ключом проходит целиком и уводит на адрес возврата; ключ с проверкой пользователя даёт уровень «3»", async ({
  page,
}, testInfo) => {
  // verifies #1282 — положительный путь полосы ключа: перепись, тела, уровень, уход.
  const { key } = await given(testInfo, "F8S4-02");
  const census = ceremonyCensus(page.context());
  const order = watchOrder(page.context());
  const s = await openLogin(page, key);
  const beginRequest = page.waitForRequest((r) => r.method() === "POST" && new URL(r.url()).pathname === BEGIN);
  const res = await pressKey(page, () => s.key.click());

  expect(res.status(), `вход ключом не прошёл: ${await res.text()}`).toBe(200);
  expect(await sessionLevel(res), "уровень сессии ключа с проверкой пользователя").toBe("3");
  await expectAddress(page, "/dashboard", "после входа ключом консоль не увела на адрес возврата");

  expect(
    {
      csrfBegin: countOf(census, "GET", LANE.csrf, "?form=access-key-begin") > 0,
      csrfLogin: countOf(census, "GET", LANE.csrf, "?form=access-key-login") > 0,
      begin: countOf(census, "POST", BEGIN),
      login: countOf(census, "POST", ACCESS_KEY_LOGIN),
      passwordLogin: countOf(census, "POST", LANE.login),
      assertionVerbs:
        countOf(census, "POST", "/iam/v1/accessKeys:beginAssertion") +
        countOf(census, "POST", "/iam/v1/accessKeys:finishAssertion"),
    },
    `перепись обращений входа ключом:\n${census.describe()}`,
  ).toEqual({ csrfBegin: true, csrfLogin: true, begin: 1, login: 1, passwordLogin: 0, assertionVerbs: 0 });
  expect(order, "вход выпущен не после ответа на испытание").toEqual(["ответ испытания 200", "запрос входа"]);
  expectNoProvider(census);

  // Тела — ровно объявленные поля (Р4): поля сериализации браузера сняты.
  expect(keysOf(JSON.parse((await beginRequest).postData() ?? "null"))).toEqual(["csrfToken"]);
  const sent = JSON.parse(res.request().postData() ?? "null") as { credential?: { type?: unknown; response?: unknown } };
  expect({
    body: keysOf(sent),
    credential: keysOf(sent.credential),
    type: sent.credential?.type,
    response: keysOf(sent.credential?.response),
  }).toEqual({
    body: ["credential", "csrfToken"],
    credential: ["id", "rawId", "response", "type"],
    type: "public-key",
    response: ["authenticatorData", "clientDataJSON", "signature", "userHandle"],
  });
  expect(await sessionHeld(page.context()), "после входа ключом у браузера нет носителя сессии").toBe(true);
});

test("F8S4-03 · ключ без проверки пользователя при входе без имени: браузер отверг церемонию, консоль называет следующий шаг, глагол входа не зовётся", async ({
  page,
}, testInfo) => {
  // verifies #1282 — F8S4-03 редакции 4 приёмки F8-S4 (переутверждение — #3059):
  // близнец F8S4-02, различие одно — аутентификатор пользователя не проверяет.
  // Уровень «2» такого ключа здесь не утверждается: в браузере утверждения с
  // UV = 0 на пути входа без имени нет (N17), свойство держит проба службы (N18).
  const { key } = await given(testInfo, "F8S4-03", { userVerification: false });
  const census = ceremonyCensus(page.context());
  const s = await openLogin(page, key);
  const challenge = page.waitForResponse(
    (r) => r.request().method() === "POST" && new URL(r.url()).pathname === BEGIN,
  );
  await s.key.click();
  const issued = await challenge;
  expect(issued.status(), `испытание входа ключом не выдано: ${await issued.text()}`).toBe(200);

  // Отказ браузера назван текстом консоли (Р5) — ждётся условие, а не время.
  await expect(s.refusal, "отказ церемонии браузером не назван на экране").toHaveText(ACCESS_KEY_BROWSER_REFUSED);
  expect(
    [countOf(census, "POST", BEGIN), countOf(census, "POST", ACCESS_KEY_LOGIN)],
    `после отказа браузера глагол входа позван:\n${census.describe()}`,
  ).toEqual([1, 0]);
  expect(pathOf(page), "после отказа браузера адрес страницы сменился").toBe("/login");
  expect(await sessionHeld(page.context()), "после отказа браузера у браузера появился носитель сессии").toBe(false);
  for (const control of [s.email, s.password, s.submit, s.key]) await expect(control).toBeEnabled();
});

test("F8S4-04 · снятый ключ: назван единый отказ, сессии нет, пароль доступен", async ({ page }, testInfo) => {
  // verifies #1282 — близнец F8S4-02: ключ снят посевом К-снят.
  const { key } = await given(testInfo, "F8S4-04", { revoked: true });
  const s = await openLogin(page, key);
  const res = await pressKey(page, () => s.key.click());
  await expectRefusedScreen(page, res);
});

test("F8S4-05 · повторная попытка начинается с нового испытания", async ({ page }, testInfo) => {
  // verifies #1282 — одна проба, два нажатия: испытание не переиспользуется (Р3).
  const { key } = await given(testInfo, "F8S4-05", { revoked: true });
  const census = ceremonyCensus(page.context());
  const order = watchOrder(page.context());
  const s = await openLogin(page, key);
  await expectRefusedScreen(page, await pressKey(page, () => s.key.click()));
  const second = await pressKey(page, () => s.key.click());
  await expectRefusedScreen(page, second);
  expect(
    [countOf(census, "POST", BEGIN), countOf(census, "POST", ACCESS_KEY_LOGIN)],
    `перепись двух попыток:\n${census.describe()}`,
  ).toEqual([2, 2]);
  expect(order, "второй вход выпущен не после ответа на второе испытание").toEqual([
    "ответ испытания 200",
    "запрос входа",
    "ответ испытания 200",
    "запрос входа",
  ]);
});

test("F8S4-09 · адрес возврата чужого происхождения отвергнут и на полосе ключа", async ({ browser }, testInfo) => {
  // verifies #1282 — близнец F8S4-02: изменено только значение `returnTo`.
  test.setTimeout(240_000);
  const { key } = await given(testInfo, "F8S4-09");
  const hostile = [
    "https://evil.example/dashboard",
    "//evil.example/dashboard",
    "/\\evil.example/dashboard",
    "javascript:alert(1)",
  ];
  // Контексты — последовательно, у каждого свой `k` и счётчик `100·k` (§6.0 шаг 3).
  for (const [i, returnTo] of hostile.entries()) {
    await keyLoginLandsAtRoot(browser, testInfo, key, returnTo, i + 1);
  }
});

/** Вход ключом в своём контексте `k`: уход — на корень консоли своего происхождения. */
async function keyLoginLandsAtRoot(
  browser: Browser,
  testInfo: TestInfo,
  key: SeededAccessKey,
  returnTo: string,
  k: number,
): Promise<void> {
  const use = testInfo.project.use;
  const origin = new URL(use.baseURL ?? "").origin;
  const context = await browser.newContext({ baseURL: use.baseURL, ignoreHTTPSErrors: use.ignoreHTTPSErrors });
  // Контекст заведён пробой сама — его отказы полосы пишет она же (F8-41).
  const reading = watchRefusals(context, testInfo.testId);
  try {
    const census = ceremonyCensus(context);
    const page = await context.newPage();
    const s = await openLogin(page, key, returnTo, k);
    const res = await pressKey(page, () => s.key.click());
    expect(res.status(), `returnTo=${returnTo}: вход ключом не прошёл — ${await res.text()}`).toBe(200);
    // Корень консоли уводит на панель (`index` оболочки): уход консоли — документ
    // на `/`, и страница оканчивается на `/dashboard` своего происхождения.
    await expect
      .poll(() => page.url(), { message: `returnTo=${returnTo}: после входа консоль увела не туда`, timeout: 30_000 })
      .toBe(`${origin}/dashboard`);
    expect(
      census.calls.some((c) => c.kind === "документ" && c.origin === origin && c.path === "/"),
      `returnTo=${returnTo}: ухода документом на корень консоли нет:\n${census.describe()}`,
    ).toBe(true);
    expect(
      census.calls.filter((c) => c.origin.includes("evil.example")).map(formatCall),
      `returnTo=${returnTo}: переход на чужое происхождение`,
    ).toEqual([]);
    expect(new URL(page.url()).origin).toBe(origin);
  } finally {
    await reading.settled();
    await context.close();
  }
}

test("F8S4-10 · заведённый второй фактор входа ключом не задерживает", async ({ page }, testInfo) => {
  // verifies #1282 — вариант F8S4-02: человеку заведён второй фактор.
  const { key } = await given(testInfo, "F8S4-10", { secondFactor: true });
  const census = ceremonyCensus(page.context());
  const s = await openLogin(page, key);
  const res = await pressKey(page, () => s.key.click());
  expect(res.status(), `вход ключом не прошёл: ${await res.text()}`).toBe(200);
  await expectAddress(page, "/dashboard", "после входа ключом консоль не увела на адрес возврата");
  const sent = JSON.parse(res.request().postData() ?? "null") as Record<string, unknown>;
  expect("secondFactor" in sent, "тело входа ключом несёт второй фактор").toBe(false);
  expect(
    census.calls
      .filter((c) => c.path.startsWith("/iam/v1/auth/second-factor") || c.path === "/iam/v1/auth/step-up")
      .map(formatCall),
    "вход ключом обратился к второму фактору",
  ).toEqual([]);
  await expect(page.getByRole("textbox", { name: /код/i }), "экран спросил код").toHaveCount(0);
});

test("F8S4-12 · вход ключом проходится с клавиатуры", async ({ page }, testInfo) => {
  // verifies #1282 — вариант F8S4-02: кнопка достигнута клавишей перехода и нажата клавишей ввода.
  const { key } = await given(testInfo, "F8S4-12");
  const census = ceremonyCensus(page.context());
  const s = await openLogin(page, key);
  await s.email.focus();
  await expect
    .poll(
      async () => {
        if (await s.key.evaluate((el) => el === document.activeElement)) return "на кнопке ключа";
        await page.keyboard.press("Tab");
        return "дальше";
      },
      { message: "клавиша перехода не довела до кнопки входа ключом", timeout: 15_000 },
    )
    .toBe("на кнопке ключа");
  const res = await pressKey(page, () => page.keyboard.press("Enter"));
  expect(res.status(), `вход ключом не прошёл: ${await res.text()}`).toBe(200);
  await expectAddress(page, "/dashboard", "после входа ключом консоль не увела на адрес возврата");
  expect([countOf(census, "POST", BEGIN), countOf(census, "POST", ACCESS_KEY_LOGIN)]).toEqual([1, 1]);
});
