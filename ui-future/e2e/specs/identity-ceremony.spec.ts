// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import {
  expect,
  type BrowserContext,
  type Locator,
  type Page,
  type Response,
  type TestInfo,
} from "@playwright/test";
import {
  LANE,
  SEED_PASSWORD,
  SESSION_COOKIE,
  newSeed,
  seedAddress,
  seedHuman,
  transferSession,
  type SeededHuman,
} from "./ceremony-seed";
import { ceremonyCensus, formatCall, register, test, type CeremonyCensus } from "./fixtures";

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

function loginScreen(page: Page) {
  return {
    email: page.getByRole("textbox", { name: "Адрес электронной почты" }),
    password: page.getByLabel("Пароль", { exact: true }),
    submit: page.getByRole("button", { name: "Войти", exact: true }),
    refusal: page.getByRole("alert"),
  };
}

function registrationScreen(page: Page) {
  return {
    email: page.getByRole("textbox", { name: "Адрес электронной почты" }),
    password: page.getByLabel("Пароль", { exact: true }),
    submit: page.getByRole("button", { name: "Завести учётную запись", exact: true }),
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

function lanePost(page: Page, path: string): Promise<Response> {
  return page.waitForResponse((r) => new URL(r.url()).pathname === path && r.request().method() === "POST");
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

/** Тело отказа входа — ОДНО на все причины; служба собирает его одной функцией. */
const AUTHENTICATION_FAILED = { code: 16, message: "authentication failed", details: [] };

/**
 * Экран отказа входа. Одна функция на F8-05 и F8-40 — и это держатель
 * неразличимости, а не удобство: экран — функция ТОЛЬКО тела ответа, тело
 * утверждается побайтово, значит и экраны побайтово равны.
 */
async function expectAuthenticationFailedScreen(page: Page, res: Response, email: string) {
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
  // Условие создаётся подстановкой ответа, побайтово равного ответу полосы:
  // остановить службу проба не вправе, а предмет сценария — что делает экран.
  const human = await seeded(testInfo, "F8-10");
  const unavailable = { code: 14, message: "request not performed; try again later", details: [] };
  await page.route(
    (u) => u.pathname === LANE.login,
    (route) =>
      route.request().method() === "POST"
        ? route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify(unavailable) })
        : route.continue(),
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
  const navigations: string[] = [];
  page.on("framenavigated", (f) => {
    if (f === page.mainFrame()) navigations.push(f.url());
  });
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expectScreen(page, "/login", s.submit, "экран входа");
  expect(navigations, `адрес страницы менялся после загрузки экрана входа: ${navigations.join(" → ")}`).toHaveLength(1);
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
    try {
      const page = await context.newPage();
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
  await page.evaluate(async () => {
    await fetch("/.ory/kratos/public/sessions/whoami").catch(() => undefined);
    await fetch("/oauth2/auth").catch(() => undefined);
    // Обращение БЕЗ ОТВЕТА, чужое происхождение, поверхность потоков поставщика.
    await fetch("https://127.0.0.1:9/self-service/logout/browser", { mode: "no-cors" }).catch(() => undefined);
  });
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
      "GET https://127.0.0.1:9/self-service/logout/browser запрос",
      `GET ${origin}/.ory/kratos/public/self-service/login/browser документ`,
    ]);
  await expect
    .poll(() => census.providerCalls().find((c) => c.origin === "https://127.0.0.1:9")?.outcome, {
      message: "обращение без ответа не записано как «ответа нет»",
      timeout: 15_000,
    })
    .toBe("ответа нет");
});

test("F8-19 · служба не подтвердила выход: экран не делает вид, что вышли", async ({ page }, testInfo) => {
  // verifies #2780 — близнец F8-18: изменено только то, отвечает ли служба краю.
  await seeded(testInfo, "F8-19", page.context());
  const unavailable = { code: 14, message: "logout not performed; try again later", details: [] };
  await page.route(
    (u) => u.pathname === LANE.logout,
    (route) =>
      route.request().method() === "POST"
        ? route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify(unavailable) })
        : route.continue(),
  );
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Учётная запись" }).click();
  const account = page.getByRole("dialog", { name: "Учётная запись" });
  const [res] = await Promise.all([lanePost(page, LANE.logout), account.getByRole("button", { name: "Выйти" }).click()]);
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
});
