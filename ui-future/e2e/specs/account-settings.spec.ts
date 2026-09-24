// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, request, type BrowserContext, type Page, type TestInfo } from "@playwright/test";
import { LANE_VERBS, captureAnswers, lanePostAnswer, type LaneAnswer } from "./answer-on-arrival";
import {
  LANE,
  SESSION_COOKIE,
  codeOutsideWindow,
  lastIssued,
  newSeed,
  seedAddress,
  seedHuman,
  seedSecondFactor,
  totpCode,
  transferSession,
  type Seed,
  type SeededHuman,
  type SeededSecondFactor,
} from "./ceremony-seed";
import { ceremonyCensus, formatCall, test, type CeremonyCensus } from "./fixtures";
import { EDGE_SESSION_ENDED, SESSION_NOT_FRESH, bodyOf, fulfillWith } from "./producer-answers";

/**
 * Параметры учётной записи на `/settings` — смена пароля и второй фактор
 * (приёмка F8, S2, группы G и H).
 *
 * Экран ведёт церемонии своими глаголами, не покидая консоли. Каждое «Тогда»
 * утверждается наблюдаемым: переписью обращений страницы, текстом экрана,
 * значением носителя у браузера и исходом, который служба отдаёт на следующее
 * предъявление. Посев второго фактора предъявляет код по времени ОДИН раз — на
 * подтверждении; всякое следующее предъявление тем же человеком идёт запасным
 * кодом из ответа того же подтверждения (Р9, ось 4). Ожидания следующего шага
 * кода здесь нет.
 */

// Условие прогона то же, что у экранов входа (приёмка F8, §4), и живёт оно не
// здесь: его проверяет свой проект прогонщика
// (`preconditions/ceremony-landing.precondition.ts`), от которого зависит проект
// этого файла. Не создано — сценарии не стартуют, исход «не выполнилось».

// ─── экран ───────────────────────────────────────────────────────────────────

function settingsScreen(page: Page) {
  const password = page.getByRole("region", { name: "Пароль" });
  const factor = page.getByRole("region", { name: "Второй фактор" });
  return {
    heading: page.getByRole("heading", { name: "Параметры учётной записи" }),
    password: {
      region: password,
      current: password.getByLabel("Текущий пароль"),
      next: password.getByLabel("Новый пароль"),
      submit: password.getByRole("button", { name: "Сменить пароль" }),
      refusal: password.getByRole("alert"),
    },
    factor: {
      region: factor,
      enroll: factor.getByRole("button", { name: "Настроить второй фактор" }),
      firstCode: factor.getByRole("textbox", { name: "Первый код из приложения" }),
      confirm: factor.getByRole("button", { name: "Подтвердить", exact: true }),
      remove: factor.getByRole("button", { name: "Снять второй фактор" }),
      regenerate: factor.getByRole("button", { name: "Выпустить новые запасные коды" }),
      backupMethod: factor.getByRole("radio", { name: "Запасной код" }),
      code: factor.getByRole("textbox", { name: "Код", exact: true }),
      removeConfirm: factor.getByRole("button", { name: "Снять", exact: true }),
      regenerateConfirm: factor.getByRole("button", { name: "Выпустить коды", exact: true }),
      refusal: factor.getByRole("alert"),
    },
  };
}

async function openSettings(page: Page) {
  await page.goto("/settings", { waitUntil: "domcontentloaded" });
  const s = settingsScreen(page);
  await expect
    .poll(
      async () => {
        if (await s.heading.isVisible()) return "отрисован";
        const now = new URL(page.url()).pathname;
        return now === "/settings" ? "ещё не отрисован" : `страница уведена на ${now}`;
      },
      { message: "экран параметров учётной записи на /settings не отрисован консолью", timeout: 30_000 },
    )
    .toBe("отрисован");
  return s;
}

/** Ответ глагола полосы — с телом, прочитанным по прибытии (`answer-on-arrival.ts`). */
function lanePost(page: Page, path: string): Promise<LaneAnswer> {
  return lanePostAnswer(page, path);
}

async function bearerOf(context: BrowserContext): Promise<string> {
  return (await context.cookies()).find((c) => c.name === SESSION_COOKIE)?.value ?? "";
}

function expectContains(census: CeremonyCensus, method: string, path: string, query?: string) {
  expect(
    census.matching(method, path, query).length,
    `перепись обращений страницы не содержит ${method} ${path}${query ?? ""}:\n${census.describe()}`,
  ).toBeGreaterThan(0);
}

function expectNoProvider(census: CeremonyCensus) {
  expect(
    census.providerCalls().map(formatCall),
    `перепись обращений страницы содержит адрес чужого поставщика:\n${census.describe()}`,
  ).toEqual([]);
}

/** Тело формы, отправленное страницей: какой способ она назвала. */
function methodOf(res: LaneAnswer): unknown {
  return (JSON.parse(res.request().postData() ?? "{}") as { method?: unknown }).method;
}

/**
 * Человек сценария — СВОЙ — с сессией у браузера; посев живёт до конца
 * сценария, потому что предъявления вне экрана (вход прежним паролем) идут им.
 */
async function withHuman<T>(
  testInfo: TestInfo,
  scenario: string,
  context: BrowserContext,
  body: (seed: Seed, human: SeededHuman, factor: SeededSecondFactor | null) => Promise<T>,
  opts: { secondFactor: boolean } = { secondFactor: false },
): Promise<T> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedHuman(seed, seedAddress(scenario));
    // Подтверждение перевыпускает носитель, поэтому перенос — ПОСЛЕ посева фактора.
    const factor = opts.secondFactor ? await seedSecondFactor(seed) : null;
    await transferSession(seed, context);
    return await body(seed, human, factor);
  } finally {
    await seed.dispose();
  }
}

/** Вход глаголом вне экрана — отдельным контекстом, чтобы не трогать сессию браузера. */
async function loginStatus(testInfo: TestInfo, email: string, password: string): Promise<number> {
  const probe = await newSeed(testInfo);
  try {
    return (await probe.submit(LANE.login, "login", { email, password })).status();
  } finally {
    await probe.dispose();
  }
}

// Ответы глаголов полосы снимаются ДО страницы: за ними экран уходит
// документом, и тело после ухода не читается (`answer-on-arrival.ts`).
test.beforeEach(async ({ page }) => {
  await captureAnswers(page, LANE_VERBS);
});

// ═══ S2 — группа G. Смена пароля ══════════════════════════════════════════════

test("F8-23 · смена пароля внутри сессии проходит и перевыпускает носитель", async ({ page }, testInfo) => {
  // verifies #1274
  await withHuman(testInfo, "F8-23", page.context(), async (_seed, human) => {
    const census = ceremonyCensus(page.context());
    const s = await openSettings(page);
    const before = await bearerOf(page.context());
    const next = `${human.password}-nov`;
    await s.password.current.fill(human.password);
    await s.password.next.fill(next);
    const [res] = await Promise.all([lanePost(page, LANE.password), s.password.submit.click()]);
    expect(res.status(), `смена пароля не прошла: ${await res.text()}`).toBe(200);
    expect(((await res.json()) as { session?: unknown }).session, "ответ смены пароля без session").toBeTruthy();
    expectContains(census, "GET", LANE.csrf, "?form=password");
    expectContains(census, "POST", LANE.password);
    expectNoProvider(census);
    await expect.poll(() => bearerOf(page.context()), { message: "носитель сессии не перевыпущен" }).not.toBe(before);
    expect(await loginStatus(testInfo, human.email, human.password), "прежний пароль всё ещё входит").toBe(401);
    expect(await loginStatus(testInfo, human.email, next), "новый пароль не входит").toBe(200);
  });
});

test("F8-23 · запрос каркаса, ушедший до перевыпуска носителя, не уводит человека на вход", async ({
  page,
}, testInfo) => {
  // verifies #1274 — условие C18: смена пароля перевыпускает носитель, прежний
  // дайджест перестаёт находить запись, и запрос ЭТОЙ вкладки, ушедший с
  // прежним носителем, получает `invalid_token`. Это не конец сессии — её
  // перевыпустили здесь же; человек остаётся на экране и при сессии.
  test.setTimeout(120_000);
  await withHuman(testInfo, "F8-23-inflight", page.context(), async (_seed, human) => {
    // Список аккаунтов каркаса ЗАДЕРЖАН до ответа смены пароля: он ушёл с
    // прежним носителем и доходит до края после перевыпуска.
    //
    // КАК ПОСТРОЕНО «УШЁЛ С ПРЕЖНИМ». Задержать запрос в браузере и отпустить
    // его мало: печенья браузер кладёт в запрос, когда отпускает его, а не когда
    // страница его выпустила, — и отпущенный после перевыпуска запрос уходит уже
    // с НОВЫМ носителем. Так проба и зеленела, ни разу не построив своего «Дано»
    // (посадка own @9038186d0d5, прогон @fc35fa9f651). Поэтому задержанный
    // запрос отправляет КРАЮ проба — тем, чем его выпустила страница: тем же
    // адресом и заголовками и носителем, который был у браузера в момент выпуска.
    // Ответ края отдаётся странице как есть — телом, кодом и заголовками, включая
    // печенья, которые он ставит или гасит, — и дальше их обрабатывает браузер.
    let release: () => void = () => undefined;
    const passwordAnswered = new Promise<void>((resolve) => {
      release = resolve;
    });
    const accountLists: number[] = [];
    let held = false;
    let issuedWith = "";
    const use = testInfo.project.use;
    await page.route(
      (u) => u.pathname === "/iam/v1/accounts",
      async (route) => {
        if (held) return route.continue();
        held = true;
        issuedWith = await bearerOf(page.context());
        await passwordAnswered;
        const edge = await request.newContext({
          baseURL: use.baseURL,
          ignoreHTTPSErrors: use.ignoreHTTPSErrors,
          storageState: { cookies: [], origins: [] },
        });
        try {
          const headers = { ...route.request().headers(), cookie: `${SESSION_COOKIE}=${issuedWith}` };
          const answer = await edge.fetch(route.request().url(), { method: route.request().method(), headers });
          await route.fulfill({ response: answer });
        } finally {
          await edge.dispose();
        }
      },
    );
    page.on("response", (r) => {
      if (new URL(r.url()).pathname === "/iam/v1/accounts") accountLists.push(r.status());
    });
    const s = await openSettings(page);
    await expect.poll(() => held, { message: "каркас не спросил список аккаунтов", timeout: 30_000 }).toBe(true);
    const before = await bearerOf(page.context());
    expect(issuedWith, "задержанный запрос выпущен без носителя — «Дано» не построено").not.toBe("");
    expect(issuedWith, "задержанный запрос выпущен не с тем носителем, что был у браузера").toBe(before);
    await s.password.current.fill(human.password);
    await s.password.next.fill(`${human.password}-nov`);
    const [changed] = await Promise.all([lanePost(page, LANE.password), s.password.submit.click()]);
    expect(changed.status(), `смена пароля не прошла: ${await changed.text()}`).toBe(200);
    release();
    await expect
      .poll(() => accountLists.at(-1), { message: "задержанный список не завершился", timeout: 30_000 })
      .toBeDefined();
    // Сперва — носитель: погашенный печеньем ответа край уводит на вход уже
    // следствием, и отказ обязан назвать причину, а не следствие.
    const after = await bearerOf(page.context());
    expect(after, "носитель у браузера погашен ответом на запрос с прежним носителем").not.toBe("");
    expect(after, "смена пароля не перевыпустила носитель").not.toBe(before);
    expect(new URL(page.url()).pathname, "запрос с прежним носителем увёл человека на вход").toBe("/settings");
    await expect
      .poll(() => accountLists.at(-1), {
        message: `список аккаунтов после перевыпуска не прочитан: ответы ${accountLists.join(", ")}`,
        timeout: 30_000,
      })
      .toBe(200);
  });
});

test("F8-24 · текущий пароль неверен: отказ назван, сессия цела", async ({ page }, testInfo) => {
  // verifies #1274 — близнец F8-23: изменено только значение текущего пароля.
  await withHuman(testInfo, "F8-24", page.context(), async (_seed, human) => {
    const s = await openSettings(page);
    const before = await bearerOf(page.context());
    await s.password.current.fill(`${human.password}-ne-tot`);
    await s.password.next.fill(`${human.password}-nov`);
    const [res] = await Promise.all([lanePost(page, LANE.password), s.password.submit.click()]);
    expect(res.status()).toBe(401);
    const refusal = (await res.json()) as { code: number; message: string };
    expect(refusal).toMatchObject({ code: 16, message: "authentication failed" });
    await expect(s.password.refusal, "отказ не назван дословно").toHaveText(refusal.message);
    expect(await bearerOf(page.context()), "носитель сессии изменился при отказе").toBe(before);
    expect(await loginStatus(testInfo, human.email, human.password), "прежний пароль перестал входить").toBe(200);
  });
});

test("F8-25 · служба молчит на глаголе с носителем: отказ края назван, сессия не гасится", async ({
  page,
}, testInfo) => {
  // verifies #1274 — близнец F8-23: изменено только то, отвечает ли служба краю.
  // Ответ края (F4d-23) подставляется так, как его отдаёт производитель: тело
  // без `details` и вызов `Bearer error="invalid_token"` (условие C20). Экран
  // обязан назвать отказ, а не принять вызов края за «войдите».
  const ended = bodyOf(EDGE_SESSION_ENDED);
  await page.route(
    (u) => u.pathname === LANE.password,
    (route) => (route.request().method() === "POST" ? fulfillWith(route, EDGE_SESSION_ENDED) : route.continue()),
  );
  await withHuman(testInfo, "F8-25", page.context(), async (_seed, human) => {
    const s = await openSettings(page);
    const before = await bearerOf(page.context());
    await s.password.current.fill(human.password);
    await s.password.next.fill(`${human.password}-nov`);
    const [res] = await Promise.all([lanePost(page, LANE.password), s.password.submit.click()]);
    expect(res.status()).toBe(401);
    await expect(s.password.refusal, "отказ края не назван на экране").toContainText(ended.message);
    expect(new URL(page.url()).pathname, "консоль ушла с экрана параметров").toBe("/settings");
    expect(await bearerOf(page.context()), "консоль погасила носитель сессии").toBe(before);
  });
});

// ═══ S2 — группа H. Второй фактор ════════════════════════════════════════════

test("F8-26 · состояние второго фактора прочитано и показано", async ({ page }, testInfo) => {
  // verifies #1274
  await withHuman(testInfo, "F8-26", page.context(), async () => {
    const census = ceremonyCensus(page.context());
    const status = page.waitForResponse(
      (r) => new URL(r.url()).pathname === LANE.secondFactor && r.request().method() === "GET",
    );
    const s = await openSettings(page);
    const body = (await (await status).json()) as { totp?: { enrolled?: boolean } };
    expectContains(census, "GET", LANE.secondFactor);
    expect(body.totp?.enrolled, "свежий человек посева с заведённым фактором").toBe(false);
    await expect(s.factor.region, "состояние из ответа не показано").toContainText("Второй фактор не настроен");
  });
});

test("F8-27 · заведение второго фактора доводится до подтверждения и запасных кодов", async ({ page }, testInfo) => {
  // verifies #1274 — единственное предъявление кода по времени у этого человека
  // и есть предмет сценария (Р9, ось 4).
  await withHuman(testInfo, "F8-27", page.context(), async () => {
    const census = ceremonyCensus(page.context());
    const s = await openSettings(page);
    const [enrolled] = await Promise.all([lanePost(page, LANE.enroll), s.factor.enroll.click()]);
    expect(enrolled.status(), `заведение не прошло: ${await enrolled.text()}`).toBe(200);
    const { secret } = (await enrolled.json()) as { secret: string };
    await expect(s.factor.region, "материал заведения из ответа не показан").toContainText(secret);

    await s.factor.firstCode.fill(totpCode(secret, Date.now()));
    const [confirmed] = await Promise.all([lanePost(page, LANE.confirm), s.factor.confirm.click()]);
    expect(confirmed.status(), `подтверждение не прошло: ${await confirmed.text()}`).toBe(200);
    const { backupCodes } = (await confirmed.json()) as { backupCodes: string[] };
    for (const code of backupCodes) await expect(s.factor.region).toContainText(code);
    await expect(s.factor.region, "не предупреждено, что коды показаны один раз").toContainText(
      "показываются один раз",
    );

    const posts = census.calls.filter((c) => c.method === "POST").map((c) => c.path);
    expect(posts.indexOf(LANE.enroll), `заведение не предшествует подтверждению:\n${census.describe()}`).toBeLessThan(
      posts.indexOf(LANE.confirm),
    );
    // Повторное чтение состояния — после подтверждения.
    await expect
      .poll(() => census.matching("GET", LANE.secondFactor).length, { message: "состояние не перечитано" })
      .toBeGreaterThanOrEqual(2);
    await expect(s.factor.region).toContainText("Второй фактор настроен");
  });
});

test("F8-28 · неверный первый код: заведение не завершено, запасных кодов нет", async ({ page }, testInfo) => {
  // verifies #1274 — близнец F8-27: изменено только значение первого кода, и
  // «неверный» выбран построением — вне всех трёх кодов окна ±1 шаг.
  await withHuman(testInfo, "F8-28", page.context(), async (seed) => {
    const s = await openSettings(page);
    const [enrolled] = await Promise.all([lanePost(page, LANE.enroll), s.factor.enroll.click()]);
    expect(enrolled.status()).toBe(200);
    const { secret } = (await enrolled.json()) as { secret: string };
    await s.factor.firstCode.fill(codeOutsideWindow(secret, Date.now()));
    const [refused] = await Promise.all([lanePost(page, LANE.confirm), s.factor.confirm.click()]);
    expect(refused.status()).toBe(401);
    const refusal = (await refused.json()) as { code: number; message: string };
    await expect(s.factor.refusal, "отказ не назван дословно").toHaveText(refusal.message);
    await expect(s.factor.region.getByRole("list", { name: "Запасные коды" })).toHaveCount(0);
    const state = await seed.read(LANE.secondFactor);
    expect(state.status()).toBe(200);
    expect(
      (lastIssued(seed, LANE.secondFactor).body as { totp?: { enrolled?: boolean } }).totp?.enrolled,
      "после неподошедшего первого кода фактор числится заведённым",
    ).toBe(false);
  });
});

test("F8-29 · сессия не свежа: консоль ведёт повышение и возвращает на тот же шаг", async ({ page }, testInfo) => {
  // verifies #1274 — близнец F8-27: изменена только свежесть предъявления в
  // сессии. Окна свежести проба не пересиживает — ответ на ПЕРВОЕ заведение
  // подставляется побайтово (Р9, ось 3а); повышение и повтор идут по-настоящему.
  // Ответ производителя — телом и заголовками (`producer-answers.ts`, условие C20).
  let substituted = false;
  await page.route(
    (u) => u.pathname === LANE.enroll,
    async (route) => {
      if (route.request().method() !== "POST" || substituted) return route.continue();
      substituted = true;
      await fulfillWith(route, SESSION_NOT_FRESH);
    },
  );
  await withHuman(testInfo, "F8-29", page.context(), async (_seed, human) => {
    const census = ceremonyCensus(page.context());
    const s = await openSettings(page);
    const [refused] = await Promise.all([lanePost(page, LANE.enroll), s.factor.enroll.click()]);
    expect(refused.status()).toBe(403);

    const dialog = page.getByRole("dialog", { name: "Подтверждение действия" });
    await expect(dialog, "консоль не открыла церемонию повышения на SESSION_NOT_FRESH").toBeVisible();
    // Ветвь пароля: подпись переключателя — «Паролем», поле — «Пароль»; одно
    // имя на обоих сделало бы выбор поля неоднозначным.
    await dialog.getByRole("radio", { name: "Паролем" }).check();
    await dialog.getByLabel("Пароль", { exact: true }).fill(human.password);
    // Ожидание повтора ставится ДО нажатия: повтор уходит сразу за ответом
    // повышения, и ожидание, поставленное после, могло бы его пропустить.
    const retried = lanePost(page, LANE.enroll);
    const [raised] = await Promise.all([
      lanePost(page, LANE.stepUp),
      dialog.getByRole("button", { name: "Подтвердить" }).click(),
    ]);
    expect(raised.status(), `повышение паролем не прошло: ${await raised.text()}`).toBe(200);

    // Тот же шаг: заведение повторено само, и его материал на экране.
    const again = await retried;
    await expect(dialog).toBeHidden();
    expect(again.status(), `повтор заведения после повышения не прошёл: ${await again.text()}`).toBe(200);
    const { secret } = (await again.json()) as { secret: string };
    await expect(s.factor.region, "человек не возвращён на шаг заведения").toContainText(secret);
    expect(new URL(page.url()).pathname, "после повышения человека увели с экрана параметров").toBe("/settings");
    expectContains(census, "POST", LANE.stepUp);
    expect(substituted).toBe(true);
  });
});

test("F8-30 · снятие второго фактора с подтверждением запасным кодом", async ({ page }, testInfo) => {
  // verifies #1274
  await withHuman(
    testInfo,
    "F8-30",
    page.context(),
    async (_seed, _human, factor) => {
      const s = await openSettings(page);
      await s.factor.remove.click();
      await s.factor.backupMethod.check();
      await s.factor.code.fill(factor!.backupCodes[0]);
      const [res] = await Promise.all([lanePost(page, LANE.remove), s.factor.removeConfirm.click()]);
      expect(res.status(), `снятие не прошло: ${await res.text()}`).toBe(200);
      expect(methodOf(res), "форма назвала не тот способ").toBe("lookup_secret");
      await expect(s.factor.region).toContainText("Второй фактор не настроен");
    },
    { secondFactor: true },
  );
});

test("F8-31 · перечеканка запасных кодов", async ({ page }, testInfo) => {
  // verifies #1274
  await withHuman(
    testInfo,
    "F8-31",
    page.context(),
    async (_seed, _human, factor) => {
      const s = await openSettings(page);
      await s.factor.regenerate.click();
      await s.factor.backupMethod.check();
      await s.factor.code.fill(factor!.backupCodes[0]);
      const [res] = await Promise.all([lanePost(page, LANE.backupCodes), s.factor.regenerateConfirm.click()]);
      expect(res.status(), `перечеканка не прошла: ${await res.text()}`).toBe(200);
      expect(methodOf(res), "форма назвала не тот способ").toBe("lookup_secret");
      const { backupCodes } = (await res.json()) as { backupCodes: string[] };
      for (const code of backupCodes) await expect(s.factor.region).toContainText(code);
      await expect(s.factor.region, "не предупреждено, что прежний набор недействителен").toContainText(
        "Прежние запасные коды больше недействительны",
      );
    },
    { secondFactor: true },
  );
});

test("F8-32 · снятие того, чего нет: отказ назван", async ({ page }, testInfo) => {
  // verifies #1274 — близнец F8-30: изменено только то, заведён ли фактор к
  // моменту отправки. Экран открыт при заведённом факторе; снимает его ТА ЖЕ
  // сессия вне экрана — посев, чей носитель у браузера, — и форма экрана уходит
  // к уже снятому.
  //
  // Не другая сессия: снятие фактора гасит ВСЕ ПРОЧИЕ сессии человека (Ф12 Р9),
  // и сессия экрана, будь она прочей, получила бы `401 authentication failed`
  // вместо отказа о состоянии — так проба и падала на посадке own @9038186d0d5.
  // Текущая сессия снятием жива, но носитель её перевыпущен, поэтому новый
  // носитель переносится в браузер тем же способом, что и в «Дано».
  await withHuman(
    testInfo,
    "F8-32",
    page.context(),
    async (seed, _human, factor) => {
      const s = await openSettings(page);
      await s.factor.remove.click();

      const removed = await seed.submit(LANE.remove, "second-factor", {
        method: "lookup_secret",
        code: factor!.backupCodes[0],
      });
      expect(removed.status(), `посев: снятие той же сессией не прошло: ${await removed.text()}`).toBe(200);
      await transferSession(seed, page.context());

      await s.factor.backupMethod.check();
      await s.factor.code.fill(factor!.backupCodes[1]);
      const [res] = await Promise.all([lanePost(page, LANE.remove), s.factor.removeConfirm.click()]);
      expect(methodOf(res), "форма той же формы, что в F8-30").toBe("lookup_secret");
      expect(res.status()).toBe(400);
      const refusal = (await res.json()) as { message: string; details: Array<{ reason?: string }> };
      expect(refusal.details.map((d) => d.reason)).toContain("SECOND_FACTOR_NOT_ENROLLED");
      expect(refusal.message).toBe("second factor is not enrolled");
      await expect(s.factor.refusal).toContainText("second factor is not enrolled");
    },
    { secondFactor: true },
  );
});
