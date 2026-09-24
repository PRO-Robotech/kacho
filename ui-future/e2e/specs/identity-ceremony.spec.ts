// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createServer, type AddressInfo } from "node:net";
import { expect, type BrowserContext, type Locator, type Page, type TestInfo } from "@playwright/test";
import { LANE_VERBS, answerOnArrival, captureAnswers, lanePostAnswer, type LaneAnswer } from "./answer-on-arrival";
import { raiseAssurance } from "./assurance";
import {
  LANE,
  SEED_PASSWORD,
  SESSION_COOKIE,
  backupCodeOutside,
  newSeed,
  seedAddress,
  seedHuman,
  seedSecondFactor,
  transferSession,
  type SeededHuman,
} from "./ceremony-seed";
import {
  ceremonyCensus,
  formatCall,
  register,
  runTag,
  tenantWithProject,
  test,
  watchRefusals,
  type CeremonyCall,
  type CeremonyCensus,
} from "./fixtures";
import { LANE_UNAVAILABLE, LOGOUT_UNAVAILABLE, bodyOf, fulfillWith } from "./producer-answers";

/**
 * Церемонии личности ведёт КОНСОЛЬ — вход, регистрация, выход (приёмка F8, S1).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ ЭТА ПРОБА (#2780)
 *
 * Рендерный гейт полосы личности доказывает, что носитель церемонии
 * ОТРИСОВАЛСЯ, а не что по нему можно ВОЙТИ. Между этими двумя утверждениями
 * лежит всё, ради чего линия существует: форма, отправка, ответ, переход,
 * названный отказ. Здесь каждое из них утверждается наблюдаемым — переписью
 * обращений страницы, текстом на экране, адресом страницы и печеньем у браузера
 * — и ни одно не утверждается разметкой: проба о разметке зеленеет при сломанном
 * обращении и краснеет при смене отступа.
 *
 * ИМЯ ТЕСТА НАЧИНАЕТСЯ С ID СЦЕНАРИЯ (Р8): перепись имён даёт множество
 * исполненных сценариев, и разность с приёмкой называется поимённо. Ссылка на
 * задачу — внутри вызова `test(…)`: S1 несёт `#2780`, S2 — `#1274`.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО
 *
 *   • ожидания временем: ждётся условие — отрисованный экран, пришедший ответ,
 *     сменившийся адрес;
 *   • общего адреса у отрицаний: каждый сценарий сеет СВОЙ, и ось потолка темпа
 *     по адресу у него своя (Р7 ч. 1);
 *   • подстановки там, где служба отвечает сама: подставляется ответ только
 *     тогда, когда условие нельзя создать иначе (служба не отвечает краю), и
 *     подставленное тело — побайтово то, что отдаёт полоса.
 */

// ─── условие прогона (приёмка F8, §4) ─────────────────────────────────────────
//
// Условия здесь НЕТ, и это его место, а не пропуск. Эти сценарии исполнимы лишь
// на посадке, где адреса церемоний отдаёт оболочка консоли; проверяет её СВОЙ
// проект прогонщика (`preconditions/ceremony-landing.precondition.ts`), от
// которого проект этого файла зависит (`playwright.config.ts`). Не создано
// условие — сценарии не стартуют, и исход называется «не выполнилось» с числом
// сценариев, а не красным о продукте.

// ─── экраны: доступные имена, а не классы ─────────────────────────────────────

// Кнопка отправки экрана церемонии — по форме и КОНЦУ имени, а не по имени
// целиком. Значок ожидания кнопки уходит из разметки только по окончании своего
// сворачивания, и когда ответ приходит раньше, чем значок развернулся (подставленный
// отказ — за миллисекунды), сворачиваться ему нечему: он остаётся свёрнутым, и
// доступное имя кнопки — «loading Войти» при кнопке, которая уже не ждёт и
// открыта. Проба по имени целиком тогда не находила кнопки вовсе и называла её
// закрытой (F8-10, посадка own @9038186d0d5: `ant-btn-loading` снят, значок —
// в `motion-leave-active` пятнадцать секунд спустя).
function submitOf(page: Page, form: string, label: string): Locator {
  return page.getByRole("form", { name: form }).getByRole("button", { name: new RegExp(`${label}$`) });
}

function loginScreen(page: Page) {
  return {
    email: page.getByRole("textbox", { name: "Адрес электронной почты" }),
    password: page.getByLabel("Пароль", { exact: true }),
    submit: submitOf(page, "Вход в консоль", "Войти"),
    refusal: page.getByRole("alert"),
  };
}

function registrationScreen(page: Page) {
  return {
    email: page.getByRole("textbox", { name: "Адрес электронной почты" }),
    password: page.getByLabel("Пароль", { exact: true }),
    submit: submitOf(page, "Новая учётная запись", "Завести учётную запись"),
    refusal: page.getByRole("alert"),
  };
}

function pathOf(page: Page): string {
  return new URL(page.url()).pathname;
}

/**
 * Экран отрисован НА СВОЁМ адресе. Падение называет причину, а не таймаут:
 * «страница уведена на /dashboard» — ровно то, что делает оболочка без маршрута
 * (замыкающее правило `*`), и это первое красное, которое обязана показать проба.
 */
async function expectScreen(page: Page, path: string, marker: Locator, what: string) {
  await expect
    .poll(
      async () => {
        if (await marker.isVisible()) return "отрисован";
        const now = pathOf(page);
        return now === path ? "ещё не отрисован" : `страница уведена на ${now}`;
      },
      { message: `${what} на ${path} не отрисован консолью`, timeout: 30_000 },
    )
    .toBe("отрисован");
}

async function expectPath(page: Page, path: string, why: string) {
  await expect.poll(() => pathOf(page), { message: why, timeout: 30_000 }).toBe(path);
}

/** Ответ глагола полосы — с телом, прочитанным по прибытии (`answer-on-arrival.ts`). */
function lanePost(page: Page, path: string): Promise<LaneAnswer> {
  return lanePostAnswer(page, path);
}

async function sessionHeld(context: BrowserContext): Promise<boolean> {
  return (await context.cookies()).some((c) => c.name === SESSION_COOKIE && c.value !== "");
}

function expectNoProvider(census: CeremonyCensus) {
  expect(
    census.providerCalls().map(formatCall),
    `перепись обращений страницы содержит адрес чужого поставщика:\n${census.describe()}`,
  ).toEqual([]);
}

function expectContains(census: CeremonyCensus, method: string, path: string, query?: string) {
  expect(
    census.matching(method, path, query).length,
    `перепись обращений страницы не содержит ${method} ${path}${query ?? ""}:\n${census.describe()}`,
  ).toBeGreaterThan(0);
}

async function seeded(testInfo: TestInfo, scenario: string, context?: BrowserContext): Promise<SeededHuman> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedHuman(seed, seedAddress(scenario));
    if (context) await transferSession(seed, context);
    return human;
  } finally {
    await seed.dispose();
  }
}

/** Метка обращения внутри сценария: своя у каждого заведённого человека. */
function runStamp(): string {
  return runTag();
}

/** Тело отказа входа — ОДНО на все причины; служба собирает его одной функцией. */
const AUTHENTICATION_FAILED = { code: 16, message: "authentication failed", details: [] };

/**
 * Экран отказа входа. Одна функция на F8-05 и F8-40 — и это держатель
 * неразличимости, а не удобство: экран — функция ТОЛЬКО тела ответа, тело
 * утверждается побайтово, значит и экраны побайтово равны.
 */
async function expectAuthenticationFailedScreen(page: Page, res: LaneAnswer, email: string) {
  expect(res.status(), "отказ входа обязан быть 401").toBe(401);
  expect(await res.text(), "тело отказа входа — побайтово одно на все причины").toBe(
    JSON.stringify(AUTHENTICATION_FAILED),
  );
  const s = loginScreen(page);
  await expect(s.refusal, "отказ не назван на экране").toHaveText(AUTHENTICATION_FAILED.message);
  await expect(s.email, "введённый адрес потерян").toHaveValue(email);
  await expect(s.password).toBeVisible();
  expect(pathOf(page), "после отказа адрес страницы сменился").toBe("/login");
  expect(await sessionHeld(page.context()), "после отказа у браузера появился носитель сессии").toBe(false);
}

// Ответы глаголов полосы снимаются ДО страницы: за ними экран уходит
// документом, и тело после ухода не читается (`answer-on-arrival.ts`).
test.beforeEach(async ({ page }) => {
  await captureAnswers(page, LANE_VERBS);
});

// ═══ S1 — группа A. Адрес церемонии принадлежит консоли ═══════════════════════

test("F8-01 · экран входа отдаёт консоль, и чужой поставщик не зовётся", async ({ page }) => {
  // verifies #2780 — у церемонии входа консоли не было ни одного свидетеля.
  const census = ceremonyCensus(page.context());
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await expect(s.email).toBeVisible();
  await expect(s.password).toBeVisible();
  expectNoProvider(census);
  expect(pathOf(page), "перевода на панель быть не должно").toBe("/login");
});

test("F8-03 · адрес, которого консоль не ведёт, отвечает названной страницей", async ({ page }) => {
  // verifies #2780 — близнец F8-01: изменён только открытый адрес церемонии.
  await page.goto("/verification", { waitUntil: "domcontentloaded" });
  const heading = page.getByRole("heading", { name: "Такого адреса здесь нет" });
  await expectScreen(page, "/verification", heading, "страница «такого адреса здесь нет»");
  expect(pathOf(page), "перевода на панель быть не должно").toBe("/verification");
  // Путь наружу — действие, а не надпись: переход обязан привести ко входу.
  await page.getByRole("link", { name: "Перейти ко входу" }).click();
  await expectPath(page, "/login", "путь наружу со страницы неведомого адреса не привёл ко входу");
});

// ═══ S1 — группа B. Вход ══════════════════════════════════════════════════════

test("F8-04 · вход паролем проходит целиком и уводит на адрес возврата", async ({ page }, testInfo) => {
  // verifies #2780 — первая красная проба приёмки (§10): перепись обращений входа.
  const human = await seeded(testInfo, "F8-04");
  const census = ceremonyCensus(page.context());
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(human.email);
  await s.password.fill(human.password);
  const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  expect(res.status(), `вход не прошёл: ${await res.text()}`).toBe(200);
  await expectPath(page, "/dashboard", "после входа консоль не увела на адрес возврата");
  expectContains(census, "GET", LANE.csrf, "?form=login");
  expectContains(census, "POST", LANE.login);
  expectNoProvider(census);
  expect(await sessionHeld(page.context()), "после входа у браузера нет носителя сессии").toBe(true);
});

test("F8-05 · неверный пароль: назван один отказ, форма остаётся", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-04: изменено только значение пароля.
  const human = await seeded(testInfo, "F8-05");
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(human.email);
  await s.password.fill(`${human.password}-не-тот`);
  const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  await expectAuthenticationFailedScreen(page, res, human.email);
});

test("F8-06 · незаполненное поле названо службой по имени", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-04: пароль пуст. Консоль своего правила не
  // применяет (Р2): поле называет служба, экран отмечает названное.
  const human = await seeded(testInfo, "F8-06");
  const census = ceremonyCensus(page.context());
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(human.email);
  const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  expect(res.status()).toBe(400);
  expect(await res.json()).toEqual({ code: 3, message: "Illegal argument password: required", details: [] });
  await expect(s.password, "поле пароля, названное службой, не отмечено").toHaveAttribute("aria-invalid", "true");
  await expect(s.email, "отмечено поле, которого служба не называла").not.toHaveAttribute("aria-invalid", "true");
  await expect(page.getByText("Illegal argument password: required")).toBeVisible();
  expectContains(census, "POST", LANE.login);
});

test("F8-07 · признак формы не предъявлен: отказ назван, а не белый экран", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-04: из тела формы изъят только признак формы.
  const human = await seeded(testInfo, "F8-07");
  await page.route(
    (u) => u.pathname === LANE.login,
    async (route) => {
      const req = route.request();
      if (req.method() !== "POST") return route.continue();
      const body = JSON.parse(req.postData() ?? "{}") as Record<string, unknown>;
      delete body.csrfToken;
      await route.continue({ postData: JSON.stringify(body) });
    },
  );
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(human.email);
  await s.password.fill(human.password);
  const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  expect(res.status()).toBe(400);
  expect(await res.json()).toEqual({ code: 3, message: "Illegal argument csrfToken: required", details: [] });
  await expect(s.refusal, "отказ формы не назван на экране").toContainText("Illegal argument csrfToken: required");
  await expect(s.email, "после отказа формы введённое потеряно").toHaveValue(human.email);
});

test("F8-08 · признак формы чужого вида отвергнут, и экран даёт повторить", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-04: признак добыт запросом другого вида (`logout`).
  const human = await seeded(testInfo, "F8-08");
  const census = ceremonyCensus(page.context());
  let substituted = false;
  await page.route(
    (u) => u.pathname === LANE.csrf && u.searchParams.get("form") === "login",
    async (route) => {
      if (substituted) return route.continue();
      substituted = true;
      const u = new URL(route.request().url());
      u.searchParams.set("form", "logout");
      await route.continue({ url: u.toString() });
    },
  );
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(human.email);
  await s.password.fill(human.password);
  const [refused] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  expect(refused.status()).toBe(403);
  expect((await refused.json()) as { code: number; message: string }).toMatchObject({
    code: 7,
    message: "form token rejected",
  });
  await expect(s.refusal).toContainText("form token rejected");
  expect(substituted, "подстановка вида признака не сработала — сценарий не создал своего условия").toBe(true);

  // Экран добывает СВЕЖИЙ признак своего вида — второе обращение после отказа.
  await expect
    .poll(() => census.matching("GET", LANE.csrf, "?form=login").length, {
      message: `после отказа признака экран не добыл свежего признака вида login:\n${census.describe()}`,
      timeout: 15_000,
    })
    .toBeGreaterThanOrEqual(2);
  await expect(s.email, "введённый адрес потерян после отказа признака").toHaveValue(human.email);

  const [accepted] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  expect(accepted.status(), `повторная отправка со свежим признаком не прошла: ${await accepted.text()}`).toBe(200);
  await expectPath(page, "/dashboard", "повторная отправка прошла, а перехода нет");
});

test("F8-09 · потолок темпа: экран называет срок и не даёт бить в стену", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-05: изменено только число подряд идущих неверных
  // предъявлений. Адрес у сценария СВОЙ: исчерпывается только его ось адреса,
  // вклад в общую ось источника — пять отказов, учтённых бюджетом Р7.
  test.setTimeout(180_000);
  const human = await seeded(testInfo, "F8-09");
  const census = ceremonyCensus(page.context());
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(human.email);
  for (let attempt = 1; attempt <= 5; attempt++) {
    await s.password.fill(`${human.password}-${attempt}`);
    const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
    expect(res.status(), `неверное предъявление ${attempt} из пяти не дало 401: ${await res.text()}`).toBe(401);
    await expect(s.submit).toBeEnabled();
  }
  await s.password.fill(`${human.password}-6`);
  const [limited] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  expect(limited.status(), `шестое обращение не упёрлось в потолок: ${await limited.text()}`).toBe(429);
  expect((await limited.json()) as { code: number; message: string }).toMatchObject({
    code: 8,
    message: "too many attempts; try again later",
  });
  const retryAfter = limited.headers()["retry-after"] ?? "";
  expect(retryAfter, "отказ по частоте без Retry-After").toMatch(/^\d+$/);

  await expect(s.refusal).toContainText("too many attempts; try again later");
  await expect(s.refusal, "экран не назвал срок из заголовка Retry-After").toContainText(`через ${retryAfter} с`);
  await expect(s.submit, "до истечения срока кнопка отправки доступна").toBeDisabled();

  // Отправка клавишей ввода — тот же путь, что у кнопки, и он тоже закрыт.
  const before = census.matching("POST", LANE.login).length;
  await s.password.press("Enter");
  // Барьер порядка, а не пауза: ответ на вычисление в странице приходит ПОСЛЕ
  // событий, выпущенных её кодом раньше, — обращение, начатое обработчиком
  // отправки, было бы уже записано.
  await page.evaluate(() => undefined);
  expect(
    census.matching("POST", LANE.login).length,
    `после отказа по частоте экран отправил форму снова:\n${census.describe()}`,
  ).toBe(before);
});

test("F8-10 · служба не ответила: отказ назван, введённое цело", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-04: изменено только то, отвечает ли служба краю.
  // Условие создаётся подстановкой ответа производителя — телом и заголовками
  // (`producer-answers.ts`, условие C20): остановить службу проба не вправе, а
  // предмет сценария — что делает экран.
  const human = await seeded(testInfo, "F8-10");
  const unavailable = bodyOf(LANE_UNAVAILABLE);
  await page.route(
    (u) => u.pathname === LANE.login,
    (route) => (route.request().method() === "POST" ? fulfillWith(route, LANE_UNAVAILABLE) : route.continue()),
  );
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(human.email);
  await s.password.fill(human.password);
  const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  expect(res.status()).toBe(503);
  await expect(s.refusal).toContainText(unavailable.message);
  await expect(s.refusal, "экран не предложил повторить").toContainText("Отправьте форму ещё раз");
  await expect(s.submit, "повторить нечем — кнопка отправки закрыта").toBeEnabled();
  await expect(s.email, "введённый адрес потерян").toHaveValue(human.email);
  expect(await sessionHeld(page.context()), "при отказе службы у браузера появился носитель").toBe(false);
});

test("F8-11 · человек с живой сессией формы входа не видит", async ({ page }, testInfo) => {
  // verifies #2780
  await seeded(testInfo, "F8-11", page.context());
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  await expectPath(page, "/dashboard", "человек с живой сессией не уведён с экрана входа на адрес возврата");
  await expect(loginScreen(page).email, "форма входа показана человеку с живой сессией").toHaveCount(0);
});

test("F8-12 · негодный носитель даёт форму, а не круг переадресаций", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-11: изменена только годность носителя. Срок
  // носителя проба не пересиживает — значение печенья подменяется (Р9, ось 5).
  const base = new URL(testInfo.project.use.baseURL ?? "");
  await page.context().addCookies([
    {
      name: SESSION_COOKIE,
      value: "f8-12-negodnyj-nositel",
      domain: base.hostname,
      path: "/",
      httpOnly: true,
      secure: base.protocol === "https:",
      sameSite: "Lax",
    },
  ]);
  // ПЕРЕХОД АДРЕСА — СМЕНА АДРЕСА, а не событие навигации. Оболочка на первом
  // рендере пишет своё состояние в текущую запись истории (`history.replaceState`
  // маршрутизатора на ТОМ ЖЕ адресе), и событие навигации приходит второй раз с
  // тем же адресом; счёт событий краснел бы на любой загрузке любого экрана
  // (прогон на посадке `own` @9038186d0d5: «/login?returnTo=/dashboard →
  // /login?returnTo=/dashboard»). Круг переадресаций меняет адрес — его видит
  // перепись адресов; круг перезагрузок того же адреса адреса не меняет — его
  // видит перепись загрузок документа.
  const addresses: string[] = [];
  const documents: string[] = [];
  page.on("framenavigated", (f) => {
    if (f === page.mainFrame() && addresses.at(-1) !== f.url()) addresses.push(f.url());
  });
  page.on("request", (r) => {
    if (r.isNavigationRequest() && r.frame() === page.mainFrame()) documents.push(r.url());
  });
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  expect(addresses, `адрес страницы менялся после загрузки экрана входа: ${addresses.join(" → ")}`).toHaveLength(1);
  expect(documents, `документ экрана входа загружался повторно: ${documents.join(" → ")}`).toHaveLength(1);
  // Чем именно носитель негоден, экран не различает и различать не вправе.
  await expect(s.refusal).toHaveCount(0);
});

test("F8-13 · адрес возврата чужого происхождения отвергнут во всех четырёх формах", async ({ browser }, testInfo) => {
  // verifies #2780 — близнец F8-04: изменено только значение `returnTo`.
  test.setTimeout(180_000);
  const human = await seeded(testInfo, "F8-13");
  const use = testInfo.project.use;
  const origin = new URL(use.baseURL ?? "").origin;

  /**
   * Войти с данным адресом возврата и дождаться, куда консоль увела страницу.
   * Ждётся условие — ожидаемый адрес; не дождались — падение называет адрес, на
   * котором страница оказалась (в том числе чужой).
   */
  async function landsAt(returnTo: string, expected: string) {
    const context = await browser.newContext({ baseURL: use.baseURL, ignoreHTTPSErrors: use.ignoreHTTPSErrors });
    // Контекст заведён пробой сама — его отказы полосы пишет она же (F8-41).
    const reading = watchRefusals(context, testInfo.testId);
    try {
      const page = await context.newPage();
      await captureAnswers(page, LANE_VERBS);
      await page.goto(`/login?returnTo=${encodeURIComponent(returnTo)}`, { waitUntil: "domcontentloaded" });
      const s = loginScreen(page);
      await expectScreen(page, "/login", s.submit, "экран входа");
      await s.email.fill(human.email);
      await s.password.fill(human.password);
      const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
      expect(res.status(), `вход не прошёл: ${await res.text()}`).toBe(200);
      await expect
        .poll(() => page.url(), {
          message: `returnTo=${returnTo}: после входа консоль увела не туда`,
          timeout: 30_000,
        })
        .toBe(expected);
      expect(new URL(page.url()).origin, `returnTo=${returnTo}: происхождение страницы после входа чужое`).toBe(origin);
    } finally {
      await reading.settled();
      await context.close();
    }
  }

  // Корень консоли уводит на панель (`index` оболочки), поэтому отвергнутый
  // адрес возврата оканчивается ровно на `/dashboard` без строки запроса.
  for (const hostile of [
    "https://evil.example/dashboard",
    "//evil.example/dashboard",
    "/\\evil.example/dashboard",
    "javascript:alert(1)",
  ]) {
    await landsAt(hostile, `${origin}/dashboard`);
  }

  // Положительный близнец: свой адрес возврата уводит ИМЕННО туда, со своей
  // строкой запроса, — отрицание не тождественно «всегда на корень».
  await landsAt("/dashboard?f8-13=kontrol", `${origin}/dashboard?f8-13=kontrol`);
});

test("F8-13 · отскок живой сессии и возврат после регистрации идут через тот же валидатор", async ({
  browser,
}, testInfo) => {
  // verifies #2780 — условие C10: КАЖДАЯ навигация по адресу возврата — после
  // входа, после регистрации и отскок человека с живой сессией с экрана входа —
  // проходит один валидатор. Близнец положительный — свой адрес соблюдён.
  test.setTimeout(180_000);
  const use = testInfo.project.use;
  const origin = new URL(use.baseURL ?? "").origin;

  async function inFreshContext(body: (page: Page) => Promise<void>, withSession: boolean) {
    const context = await browser.newContext({ baseURL: use.baseURL, ignoreHTTPSErrors: use.ignoreHTTPSErrors });
    const reading = watchRefusals(context, testInfo.testId);
    try {
      if (withSession) await seeded(testInfo, `F8-13-bounce-${runStamp()}`, context);
      const page = await context.newPage();
      await captureAnswers(page, LANE_VERBS);
      await body(page);
    } finally {
      await reading.settled();
      await context.close();
    }
  }

  const expectLanded = async (page: Page, returnTo: string, expected: string) => {
    await expect
      .poll(() => page.url(), { message: `returnTo=${returnTo}: консоль увела не туда`, timeout: 30_000 })
      .toBe(expected);
    expect(new URL(page.url()).origin, `returnTo=${returnTo}: происхождение страницы чужое`).toBe(origin);
  };

  for (const [returnTo, expected] of [
    ["//evil.example/dashboard", `${origin}/dashboard`],
    ["https://evil.example/dashboard", `${origin}/dashboard`],
    ["/dashboard?f8-13=otskok", `${origin}/dashboard?f8-13=otskok`],
  ] as const) {
    // Отскок: у браузера живая сессия, экран входа уводит сразу.
    await inFreshContext(async (page) => {
      await page.goto(`/login?returnTo=${encodeURIComponent(returnTo)}`, { waitUntil: "domcontentloaded" });
      await expectLanded(page, returnTo, expected);
    }, true);
    // После регистрации: адрес возврата несёт экран регистрации.
    await inFreshContext(async (page) => {
      await page.goto(`/registration?returnTo=${encodeURIComponent(returnTo)}`, { waitUntil: "domcontentloaded" });
      const s = registrationScreen(page);
      await expectScreen(page, "/registration", s.submit, "экран регистрации");
      await s.email.fill(seedAddress(`F8-13-reg-${runStamp()}`));
      await s.password.fill(SEED_PASSWORD);
      const [res] = await Promise.all([lanePost(page, LANE.register), s.submit.click()]);
      expect(res.status(), `регистрация не прошла: ${await res.text()}`).toBe(200);
      await expectLanded(page, returnTo, expected);
    }, false);
  }
});

test("F8-39 · на экране входа нет пути на восстановление и подтверждение адреса", async ({ page }) => {
  // verifies #2780 — держатель стороны «путь не обещается» решения Р4: пути,
  // которого на посадке нет, экран не обещает.
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  await expectScreen(page, "/login", loginScreen(page).submit, "экран входа");
  const destinations = await page
    .getByRole("link")
    .evaluateAll((links) => links.map((a) => new URL((a as HTMLAnchorElement).href).pathname));
  expect(destinations, "экран входа обещает восстановление доступа").not.toContain("/recovery");
  expect(destinations, "экран входа обещает подтверждение адреса").not.toContain("/verification");
  // Положительный близнец: предикат «перехода нет» умеет распознать переход.
  expect(destinations, "перехода на регистрацию на экране нет — предикат ничего не распознаёт").toContain(
    "/registration",
  );
});

test("F8-40 · не заведённый адрес даёт побайтово тот же экран, что неверный пароль", async ({ page }) => {
  // verifies #2780 — держатель стороны «причины неразличимы» решения Р4; близнец
  // F8-05, изменено только то, заведён ли адрес. Посев тривиален: его не делают.
  const email = seedAddress("F8-40-unknown");
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(email);
  await s.password.fill(SEED_PASSWORD);
  const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  await expectAuthenticationFailedScreen(page, res, email);
});

// ═══ S1 — группа C. Регистрация ═══════════════════════════════════════════════

test("F8-14 · регистрация заводит человека и сразу даёт сессию", async ({ page }) => {
  // verifies #2780
  const census = ceremonyCensus(page.context());
  await page.goto("/registration", { waitUntil: "domcontentloaded" });
  const s = registrationScreen(page);
  await expectScreen(page, "/registration", s.submit, "экран регистрации");
  await s.email.fill(seedAddress("F8-14"));
  await s.password.fill(SEED_PASSWORD);
  const [res] = await Promise.all([lanePost(page, LANE.register), s.submit.click()]);
  expect(res.status(), `регистрация не прошла: ${await res.text()}`).toBe(200);
  await expectPath(page, "/dashboard", "после регистрации консоль не увела на панель");
  expectContains(census, "GET", LANE.csrf, "?form=register");
  expectContains(census, "POST", LANE.register);
  expectNoProvider(census);
  expect(await sessionHeld(page.context()), "после регистрации у браузера нет носителя сессии").toBe(true);
});

test("F8-15 · занятый адрес: отказ дословно, и экран не говорит, занят ли адрес", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-14: изменено только то, заведён ли адрес.
  const human = await seeded(testInfo, "F8-15");
  await page.goto("/registration", { waitUntil: "domcontentloaded" });
  const s = registrationScreen(page);
  await expectScreen(page, "/registration", s.submit, "экран регистрации");
  await s.email.fill(human.email);
  await s.password.fill(SEED_PASSWORD);
  const [res] = await Promise.all([lanePost(page, LANE.register), s.submit.click()]);
  expect(res.status()).toBe(400);
  const refusal = (await res.json()) as { code: number; message: string };
  expect(refusal.code, "отказ регистрации — FAILED_PRECONDITION").toBe(9);
  // Дословно и БЕЗ добавки: ни слова о том, заведён ли адрес.
  await expect(s.refusal).toHaveText(refusal.message);
  expect(await sessionHeld(page.context()), "при отказе регистрации у браузера появился носитель").toBe(false);
});

test("F8-16 · пароль не отвечает правилу службы: поле названо ответом", async ({ page }) => {
  // verifies #2780 — близнец F8-14: изменено только значение пароля.
  const census = ceremonyCensus(page.context());
  await page.goto("/registration", { waitUntil: "domcontentloaded" });
  const s = registrationScreen(page);
  await expectScreen(page, "/registration", s.submit, "экран регистрации");
  await s.email.fill(seedAddress("F8-16"));
  await s.password.fill("abc");
  const [res] = await Promise.all([lanePost(page, LANE.register), s.submit.click()]);
  expect(res.status()).toBe(400);
  const refusal = (await res.json()) as { code: number; message: string };
  expect(refusal.code).toBe(3);
  expect(refusal.message).toMatch(/^Illegal argument password: /);
  await expect(s.password, "поле пароля, названное службой, не отмечено").toHaveAttribute("aria-invalid", "true");
  await expect(page.getByText(refusal.message)).toBeVisible();
  // Консоль своего правила не применяла: отказ пришёл от службы.
  expectContains(census, "POST", LANE.register);
});

test("F8-17 · признак подтверждённости адреса показан, а действия, которого нет, не предложено", async ({ page }) => {
  // verifies #2780
  await page.goto("/registration", { waitUntil: "domcontentloaded" });
  const s = registrationScreen(page);
  await expectScreen(page, "/registration", s.submit, "экран регистрации");
  const email = seedAddress("F8-17");
  await s.email.fill(email);
  await s.password.fill(SEED_PASSWORD);
  await Promise.all([lanePost(page, LANE.register), s.submit.click()]);
  await expectPath(page, "/dashboard", "после регистрации консоль не увела на панель");

  // Состояние учётной записи — из ответа края о сессии, а не из догадки экрана.
  const me = page.waitForResponse((r) => new URL(r.url()).pathname === "/iam/v1/auth/me");
  await page.reload({ waitUntil: "domcontentloaded" });
  const body = (await (await me).json()) as { session?: { emailVerified?: boolean } };
  expect(typeof body.session?.emailVerified, "ответ о сессии не несёт emailVerified").toBe("boolean");

  await page.getByRole("button", { name: "Учётная запись" }).click();
  const account = page.getByRole("dialog", { name: "Учётная запись" });
  await expect(account).toContainText(email);
  await expect(account).toContainText(body.session?.emailVerified ? "Адрес подтверждён" : "Адрес не подтверждён");
  await expect(
    account.getByRole("button", { name: /подтвердить/i }).or(account.getByRole("link", { name: /подтвердить/i })),
    "предложено действие, производителя которого на посадке нет",
  ).toHaveCount(0);
});

// ═══ S1 — группа D. Выход ═════════════════════════════════════════════════════

test("F8-18 · выход гасит носитель и возвращает на экран входа", async ({ page }, testInfo) => {
  // verifies #2780
  await seeded(testInfo, "F8-18", page.context());
  const origin = new URL(testInfo.project.use.baseURL ?? "").origin;
  const census = ceremonyCensus(page.context());
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Учётная запись" }).click();
  const [res] = await Promise.all([
    lanePost(page, LANE.logout),
    page.getByRole("dialog", { name: "Учётная запись" }).getByRole("button", { name: "Выйти" }).click(),
  ]);
  expect(res.status(), `выход не прошёл: ${await res.text()}`).toBe(200);
  await expectPath(page, "/login", "после выхода консоль не вернула на экран входа");
  expectContains(census, "GET", LANE.csrf, "?form=logout");
  expectContains(census, "POST", LANE.logout);
  expectNoProvider(census);
  expect(await sessionHeld(page.context()), "после выхода носитель сессии у браузера остался").toBe(false);

  // ── положительная сторона отрицания (Р6 п. 3): подсадка из кода страницы ──
  //
  // По одному обращению на каждую форму адреса поставщика и на каждый вид
  // обращения. Перепись обязана назвать каждое методом и адресом; без подсадки
  // она выше была пуста — значит отрицание не тождественно.
  //
  // Обращение БЕЗ ОТВЕТА — на чужое происхождение, поверхность потоков
  // поставщика. Запросом страницы его не выпустить: политика консоли
  // `connect-src 'self'` отвергает чужое происхождение ДО выпуска, и такого
  // обращения нет ни у сервера, ни в переписи (посадка own @9038186d0d5: из
  // четырёх подсаженных перепись назвала три). Поэтому оно — окно, открытое
  // страницей: переход документа политика не закрывает, и окно — тоже
  // обращение консоли (Р6 п. 1). Адрес — петлевой сервер пробы, который
  // принимает соединение и рвёт его, не ответив: «ответа нет» здесь построено,
  // а не зависит от того, свободен ли чей-то порт.
  const silent = createServer((socket) => socket.destroy());
  await new Promise<void>((resolve) => silent.listen(0, "127.0.0.1", resolve));
  const silentOrigin = `http://127.0.0.1:${(silent.address() as AddressInfo).port}`;
  try {
    await page.evaluate(async (away) => {
      await fetch("/.ory/kratos/public/sessions/whoami").catch(() => undefined);
      await fetch("/oauth2/auth").catch(() => undefined);
      window.open(`${away}/self-service/logout/browser`);
    }, silentOrigin);
    // Переход окна выпущен и остался без ответа — до ухода страницы: порядок
    // переписи тогда тот, в каком подсажено.
    await expect
      .poll(() => census.providerCalls().find((c) => c.origin === silentOrigin)?.outcome, {
        message: `обращение без ответа не записано как «ответа нет»:\n${census.describe()}`,
        timeout: 15_000,
      })
      .toBe("ответа нет");
    await page.evaluate(() => {
      window.location.assign("/.ory/kratos/public/self-service/login/browser");
    });
    await expect
      .poll(() => census.providerCalls().map((c) => `${c.method} ${c.origin}${c.path} ${c.kind}`), {
        message: `перепись не назвала подсаженные обращения к поставщику:\n${census.describe()}`,
        timeout: 15_000,
      })
      .toEqual([
        `GET ${origin}/.ory/kratos/public/sessions/whoami запрос`,
        `GET ${origin}/oauth2/auth запрос`,
        `GET ${silentOrigin}/self-service/logout/browser документ`,
        `GET ${origin}/.ory/kratos/public/self-service/login/browser документ`,
      ]);
  } finally {
    await new Promise<void>((resolve) => silent.close(() => resolve()));
  }
});

test("F8-18 · после выхода следующий человек в этом браузере не видит чужих аккаунта и проекта", async ({
  page,
}, testInfo) => {
  // verifies #2780 — условие C14: выход снимает состояние браузера, привязанное
  // к человеку (выбранные аккаунт и проект с их именами), и вход другим
  // человеком его не применяет. Оставляемое (тема) — остаётся.
  test.setTimeout(180_000);
  const first = await tenantWithProject(page);
  const accounts = (await (await page.request.get("/iam/v1/accounts?pageSize=1000")).json()) as {
    accounts?: Array<{ id: string; name?: string }>;
  };
  const foreign = accounts.accounts?.[0];
  expect(foreign?.id, "у первого человека нет аккаунта — условие сценария не создано").toBeTruthy();
  await page.goto(`/projects/${first.projectId}/dashboard`, { waitUntil: "domcontentloaded" });
  const stored = () =>
    page.evaluate(() => {
      try {
        return window.localStorage.getItem("kacho.context.v2") ?? "";
      } catch {
        return "";
      }
    });
  await expect
    .poll(stored, { message: "каркас не запомнил выбранный аккаунт — нечего проверять после выхода", timeout: 30_000 })
    .toContain(foreign!.id);
  await page.evaluate(() => window.localStorage.setItem("kacho-theme", "light"));

  await page.getByRole("button", { name: "Учётная запись" }).click();
  // Нажатие ограничено сроком: на панели проекта «Выйти» может закрывать
  // страница, и тогда отказ обязан назвать перехватчика нажатия, а не
  // «ответа выхода не было» по сроку всего сценария.
  const [out] = await Promise.all([
    lanePost(page, LANE.logout),
    page
      .getByRole("dialog", { name: "Учётная запись" })
      .getByRole("button", { name: "Выйти" })
      .click({ timeout: 15_000 }),
  ]);
  expect(out.status(), `выход не прошёл: ${await out.text()}`).toBe(200);
  await expectPath(page, "/login", "после выхода консоль не вернула на экран входа");
  expect(await stored(), "после выхода в браузере остались чужие аккаунт и проект").not.toContain(foreign!.id);

  // Второй человек входит в том же браузере.
  const second = await seeded(testInfo, "F8-18-second");
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(second.email);
  await s.password.fill(second.password);
  const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  expect(res.status(), `вход второго человека не прошёл: ${await res.text()}`).toBe(200);
  // Вход без адреса возврата уводит на корень консоли, а корень — на панель.
  await expectPath(page, "/dashboard", "после входа второго человека консоль не увела на панель");
  await expect(page.getByRole("navigation", { name: "Host navigation" })).toBeVisible({ timeout: 30_000 });
  expect(await stored(), "второму человеку применён чужой аккаунт").not.toContain(foreign!.id);
  if (foreign!.name) {
    await expect(
      page.getByText(foreign!.name, { exact: true }),
      "у второго человека видно имя чужого аккаунта",
    ).toHaveCount(0);
  }
  // Оставляемое — осталось: тема не привязана к человеку.
  expect(await page.evaluate(() => window.localStorage.getItem("kacho-theme"))).toBe("light");
});

test("F8-19 · служба не подтвердила выход: экран не делает вид, что вышли", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-18: изменено только то, отвечает ли служба краю.
  await seeded(testInfo, "F8-19", page.context());
  const unavailable = bodyOf(LOGOUT_UNAVAILABLE);
  await page.route(
    (u) => u.pathname === LANE.logout,
    (route) => (route.request().method() === "POST" ? fulfillWith(route, LOGOUT_UNAVAILABLE) : route.continue()),
  );
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Учётная запись" }).click();
  const account = page.getByRole("dialog", { name: "Учётная запись" });
  const [res] = await Promise.all([
    lanePost(page, LANE.logout),
    account.getByRole("button", { name: "Выйти" }).click(),
  ]);
  expect(res.status()).toBe(503);
  await expect(account.getByRole("alert")).toContainText(unavailable.message);
  expect(pathOf(page), "экран сделал вид, что вышли: адрес сменился").toBe("/dashboard");
  expect(await sessionHeld(page.context()), "носитель сессии погашен, хотя служба выхода не подтвердила").toBe(true);
});

// ═══ S1 — группа E. Посев набора переезжает на наши экраны ════════════════════

test("F8-20 · «Дано» браузерного набора строится нашим экраном регистрации", async ({ page }) => {
  // verifies #2780 — фикстура набора заводит человека НАШИМ глаголом.
  const census = ceremonyCensus(page.context());
  await register(page);
  expectContains(census, "POST", LANE.register);
  expectNoProvider(census);
});

test("F8-21 · фикстура, не дошедшая до сессии, падает текстом с экрана", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-20: изменён только исход церемонии (адрес занят).
  const human = await seeded(testInfo, "F8-21");
  const failure = await register(page, human.email).then(
    () => null,
    (e: unknown) => (e instanceof Error ? e.message : String(e)),
  );
  expect(failure, "фикстура дошла до сессии на занятом адресе").not.toBeNull();
  expect(failure, "фикстура упала не текстом отказа с экрана").toContain("registration refused");
  expect(failure, "в тексте падения не назван шаг, на котором поток остановился").toMatch(/на шаге [^:]+:/);
});

// ═══ S1 — группа F. Достижимость с клавиатуры ═════════════════════════════════

test("F8-22 · церемония входа проходится с клавиатуры целиком", async ({ page }, testInfo) => {
  // verifies #2780
  const human = await seeded(testInfo, "F8-22");
  const census = ceremonyCensus(page.context());
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");

  /** Клавишей перехода до элемента — не больше двадцати нажатий, условие, а не время. */
  async function tabTo(target: Locator, what: string) {
    for (let i = 0; i < 20; i++) {
      if (await target.evaluate((el) => el === document.activeElement)) return;
      await page.keyboard.press("Tab");
    }
    throw new Error(`${what} недостижимо клавишей перехода за двадцать нажатий`);
  }

  await tabTo(s.email, "поле адреса");
  await page.keyboard.type(human.email);
  await tabTo(s.password, "поле пароля");
  await page.keyboard.type(human.password);
  await tabTo(s.submit, "кнопка отправки");
  const [res] = await Promise.all([lanePost(page, LANE.login), page.keyboard.press("Enter")]);
  expect(res.status(), `вход с клавиатуры не прошёл: ${await res.text()}`).toBe(200);
  await expectPath(page, "/dashboard", "вход с клавиатуры прошёл, а перехода нет");
  expectContains(census, "GET", LANE.csrf, "?form=login");
  expectContains(census, "POST", LANE.login);
  expectNoProvider(census);
  // Клавиша ввода не отправила форму нативно (условие C26): у нативной отправки
  // формы без метода значения полей уходят строкой запроса адреса, и пароль
  // оседал бы в истории браузера и в журналах раздачи.
  const carriesPassword = (c: CeremonyCall) => {
    const address = decodeURIComponent(`${c.path}${c.query}`);
    return address.includes(human.password);
  };
  expect(
    census.calls.filter(carriesPassword).map(formatCall),
    `пароль ушёл в адресе обращения:\n${census.describe()}`,
  ).toEqual([]);
});

// ═══ S2 — группа I. Вход со вторым фактором и повышение уровня ════════════════
//
// Посев второго фактора предъявил код по времени ОДИН раз — на подтверждении;
// дальше этот человек предъявляет второй фактор только ЗАПАСНЫМ кодом из ответа
// того же подтверждения (Р9, ось 4). Ветвь «код из приложения» тех же форм держит
// модульная проба формы предъявления кода.

/** Человек сценария со вторым фактором; посев живёт до конца сценария. */
async function withSecondFactor<T>(
  testInfo: TestInfo,
  scenario: string,
  body: (human: SeededHuman, backupCodes: string[]) => Promise<T>,
): Promise<T> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedHuman(seed, seedAddress(scenario));
    const factor = await seedSecondFactor(seed);
    return await body(human, factor.backupCodes);
  } finally {
    await seed.dispose();
  }
}

function secondFactorOnLogin(page: Page) {
  return {
    toggle: page.getByRole("checkbox", { name: "Подтвердить вторым фактором" }),
    backup: page.getByRole("radio", { name: "Запасной код" }),
    code: page.getByRole("textbox", { name: "Код", exact: true }),
  };
}

test("F8-33 · вход человека с заведённым вторым фактором доходит до уровня 2", async ({ page }, testInfo) => {
  // verifies #1274
  await withSecondFactor(testInfo, "F8-33", async (human, codes) => {
    await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
    const s = loginScreen(page);
    await expectScreen(page, "/login", s.submit, "экран входа");
    await s.email.fill(human.email);
    await s.password.fill(human.password);
    const f = secondFactorOnLogin(page);
    await f.toggle.check();
    await f.backup.check();
    await f.code.fill(codes[0]);
    const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
    const sent = JSON.parse(res.request().postData() ?? "{}") as { secondFactor?: unknown };
    expect(sent.secondFactor, "тело входа не несёт secondFactor запасным кодом").toEqual({
      method: "lookup_secret",
      code: codes[0],
    });
    expect(res.status(), `вход со вторым фактором не прошёл: ${await res.text()}`).toBe(200);
    const body = (await res.json()) as { session?: { assuranceLevel?: unknown } };
    expect(String(body.session?.assuranceLevel), "вход со вторым фактором не дал уровня 2").toBe("2");
    expect(await sessionHeld(page.context()), "после входа у браузера нет носителя сессии").toBe(true);
  });
});

test("F8-34 · неверный код второго фактора: тот же один текст отказа", async ({ page }, testInfo) => {
  // verifies #1274 — близнец F8-33: изменено только значение запасного кода,
  // форма та же; значение выбрано построением — проба знает весь набор.
  await withSecondFactor(testInfo, "F8-34", async (human, codes) => {
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    const s = loginScreen(page);
    await expectScreen(page, "/login", s.submit, "экран входа");
    await s.email.fill(human.email);
    await s.password.fill(human.password);
    const f = secondFactorOnLogin(page);
    await f.toggle.check();
    await f.backup.check();
    await f.code.fill(backupCodeOutside(codes));
    const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
    // Экран не сообщает, какая из двух величин не подошла: отказ — тот же, что у
    // неверного пароля, и экран — та же функция того же тела.
    await expectAuthenticationFailedScreen(page, res, human.email);
  });
});

/**
 * Предмет повышения: группа, заведённая на ДОСТУПНОМ уровне, и её удаление,
 * связанное полом «2», — действие консоли, на котором край требует повышения.
 */
async function ownGroup(page: Page): Promise<string> {
  let accountId = "";
  await expect
    .poll(
      async () => {
        const res = await page.request.get("/iam/v1/accounts?pageSize=1000");
        if (!res.ok()) return "";
        accountId = ((await res.json()) as { accounts?: Array<{ id: string }> }).accounts?.[0]?.id ?? "";
        return accountId;
      },
      { message: "аккаунт человека не появился — предмет повышения не собран", timeout: 60_000 },
    )
    .not.toBe("");
  const created = await page.request.post("/iam/v1/groups", {
    data: { accountId, name: `e2e-stepup-${Date.now().toString(36)}`, description: "предмет повышения F8" },
  });
  expect(created.status(), `заведение группы не удалось: ${await created.text()}`).toBe(200);
  const groupId = ((await created.json()) as { metadata?: { groupId?: string } }).metadata?.groupId ?? "";
  expect(groupId, "операция не назвала идентификатор группы").not.toBe("");
  await expect
    .poll(async () => (await page.request.get(`/iam/v1/groups/${groupId}`)).status(), {
      message: "своя свежая группа не читается: право не материализовалось либо идентификатор — фантом",
      timeout: 60_000,
    })
    .toBe(200);
  return groupId;
}

/** Войти экраном ОДНИМ паролем — сессия уровня 1. */
async function signInWithPasswordOnly(page: Page, human: SeededHuman) {
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  await s.email.fill(human.email);
  await s.password.fill(human.password);
  const [res] = await Promise.all([lanePost(page, LANE.login), s.submit.click()]);
  expect(res.status(), `вход паролем не прошёл: ${await res.text()}`).toBe(200);
  expect(String(((await res.json()) as { session?: { assuranceLevel?: unknown } }).session?.assuranceLevel)).toBe("1");
  await expectPath(page, "/dashboard", "после входа консоль не увела на адрес возврата");
}

/** Начать удаление группы с её карточки — действие, на котором край зовёт повышение. */
async function startGroupDeletion(page: Page, groupId: string) {
  await page.goto(`/iam/groups/${groupId}`, { waitUntil: "domcontentloaded" });
  // «Удалить» — видимая кнопка шапки карточки, а не пункт меню «Действия»
  // (`DetailOverviewActions`): меню на карточке группы нет вовсе, и проба,
  // искавшая его, ждала элемента, которого консоль не рисует.
  await page.getByRole("button", { name: "Удалить" }).click();
  const deletion = page.getByRole("dialog").filter({ has: page.getByRole("button", { name: "Удалить" }) });
  const challenged = page.waitForResponse(
    (r) => new URL(r.url()).pathname === `/iam/v1/groups/${groupId}` && r.request().method() === "DELETE",
  );
  await deletion.getByRole("button", { name: "Удалить" }).click();
  const res = await challenged;
  expect(res.status(), "край не потребовал повышения на удалении группы").toBe(401);
  expect(res.headers()["www-authenticate"] ?? "").toContain("insufficient_user_authentication");
}

test("F8-35 · повышение уровня по вызову края идёт НАШИМ глаголом", async ({ page }, testInfo) => {
  // verifies #1274
  test.setTimeout(240_000);
  await withSecondFactor(testInfo, "F8-35", async (human, codes) => {
    await signInWithPasswordOnly(page, human);
    const groupId = await ownGroup(page);
    const census = ceremonyCensus(page.context());
    await startGroupDeletion(page, groupId);

    const dialog = page.getByRole("dialog", { name: "Подтверждение действия" });
    await expect(dialog, "консоль не ответила на вызов края церемонией повышения").toBeVisible();
    await dialog.getByRole("radio", { name: "Запасной код" }).check();
    await dialog.getByRole("textbox", { name: "Код", exact: true }).fill(codes[0]);
    const replayed = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === `/iam/v1/groups/${groupId}` &&
        r.request().method() === "DELETE" &&
        r.status() !== 401,
    );
    const [raised] = await Promise.all([
      lanePost(page, LANE.stepUp),
      dialog.getByRole("button", { name: "Подтвердить" }).click(),
    ]);
    expect((JSON.parse(raised.request().postData() ?? "{}") as { method?: unknown }).method).toBe("lookup_secret");
    expect(raised.status(), `повышение запасным кодом не прошло: ${await raised.text()}`).toBe(200);
    expect((await replayed).status(), "исходное действие после повышения не повторено либо не прошло").toBe(200);
    expectContains(census, "POST", LANE.stepUp);
    expectNoProvider(census);
    await expect
      .poll(async () => (await page.request.get(`/iam/v1/groups/${groupId}`)).status(), {
        message: "группа не удалена после повышения",
        timeout: 60_000,
      })
      .toBe(404);
  });
});

test("F8-36 · повышать нечем: назван отказ и путь, а не пустое окно", async ({ page }, testInfo) => {
  // verifies #1274 — близнец F8-35: изменено только то, заведён ли второй фактор.
  test.setTimeout(240_000);
  const human = await seeded(testInfo, "F8-36");
  await signInWithPasswordOnly(page, human);
  const groupId = await ownGroup(page);
  await startGroupDeletion(page, groupId);

  const dialog = page.getByRole("dialog", { name: "Подтверждение действия" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("radio", { name: "Запасной код" }).check();
  await dialog.getByRole("textbox", { name: "Код", exact: true }).fill("ABCDEFGHJK");
  const [refused] = await Promise.all([
    lanePost(page, LANE.stepUp),
    dialog.getByRole("button", { name: "Подтвердить" }).click(),
  ]);
  expect(refused.status()).toBe(400);
  const refusal = (await refused.json()) as { message: string; details: Array<{ reason?: string }> };
  expect(refusal.details.map((d) => d.reason)).toContain("SECOND_FACTOR_NOT_ENROLLED");
  await expect(dialog.getByRole("alert"), "окно не назвало отказ").toContainText(refusal.message);
  await expect(dialog, "окно повышения закрылось молча").toBeVisible();
  await dialog.getByRole("link", { name: "Настроить второй фактор" }).click();
  await expectPath(page, "/settings", "путь на экран заведения второго фактора не привёл туда");
});

// ═══ S2 — второй путь посева переезжает на наши глаголы ═══════════════════════

test("F8-43 · оснастка поднимает уровень НАШИМИ глаголами, одним предъявлением кода", async ({ page }) => {
  // verifies #1274
  const registered = answerOnArrival(
    page,
    (r) => new URL(r.url()).pathname === LANE.register && r.request().method() === "POST",
  );
  await register(page);
  const before = (await (await registered).json()) as { session?: { assuranceLevel?: unknown } };
  expect(String(before.session?.assuranceLevel), "до подъёма уровень ответа регистрации не «1»").toBe("1");

  const issued = await raiseAssurance(page);
  const posts = issued.filter((c) => c.method === "POST").map((c) => c.path);
  expect(posts, "оснастка вела не заведение и подтверждение").toEqual([LANE.enroll, LANE.confirm]);
  expect(posts, "оснастка звала повышение: уровень поднимает само подтверждение").not.toContain(LANE.stepUp);
  const confirm = issued.find((c) => c.path === LANE.confirm)!;
  expect(confirm.status).toBe(200);
  expect(
    String((confirm.body as { session?: { assuranceLevel?: unknown } }).session?.assuranceLevel),
    "после подтверждения уровень ответа не «2»",
  ).toBe("2");
});
