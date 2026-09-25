// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, request, type BrowserContext, type Page, type Request, type TestInfo } from "@playwright/test";
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
import { CANCELLED_BY_PAGE, ceremonyCensus, formatCall, test, type CeremonyCall, type CeremonyCensus } from "./fixtures";
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

/** Текст вызова края, гасящего носитель, не нашедший записи (F4d-22, §1.9). */
const SESSION_ENDED_TEXT = bodyOf(EDGE_SESSION_ENDED).message;

/** Ответ края `401` «сессия кончилась» — тот, что гасит носитель у браузера. */
function endedSession(call: CeremonyCall): boolean {
  return call.outcome === 401 && (call.challenge ?? "").includes(SESSION_ENDED_TEXT);
}

/** Путь платформы, на котором край носитель, не нашедший записи, гасит (§1.9). */
const HELD_READ = "/iam/v1/accounts";

/**
 * «Дано» F8-46 и F8-47: первое чтение страницы по пути платформы ЗАДЕРЖАНО и
 * отпускается правилом без времени, у которого ветвей две.
 *
 *   • страница задержанное ОТМЕНИЛА — к краю не уходит ничего;
 *   • страница выпустила глагол сценария, пока исхода у задержанного нет, —
 *     проба дожидается ответа глагола и только после него отправляет
 *     задержанное краю САМА: отдельным контекстом запросов, не делящим банку
 *     печенья с браузером, и с носителем момента выпуска. Ответ края —
 *     настоящий — отдаётся странице как есть: код, тело, заголовки, включая
 *     печенья, которые он ставит или гасит.
 *
 * Отпустить задержанное браузером мало: печенья браузер кладёт в запрос, когда
 * отпускает его, а не когда страница его выпустила, и обращение ушло бы с новым
 * носителем — так проба прежде и зеленела, ни разу не построив «Дано».
 *
 * Ветвь выбирается порядком событий слушателя, а он порядком страницы не
 * является (Р6 п. 1, N19): у исправной консоли вторая ветвь срабатывает в части
 * исполнений. Поэтому ветвь — СВЕДЕНИЕ, которое проба печатает, а не
 * утверждение: ответ, отданный обращению, которое страница уже отменила, до
 * браузера не доходит (N19: 550 из 550), и «Тогда» верно при любой ветви.
 */
async function holdFirstPlatformRead(page: Page, testInfo: TestInfo, verb: string) {
  const context = page.context();
  const use = testInfo.project.use;
  let heldRequest: Request | null = null;
  let issuedWith = "";
  let branch = "не выбрана: задержанного нет";
  let released = false;
  let settled!: (r: Request) => void;
  const heldIssued = new Promise<Request>((resolve) => {
    settled = resolve;
  });
  await page.route(
    (u) => u.pathname === HELD_READ,
    async (route) => {
      if (heldRequest !== null) return route.continue();
      const held = route.request();
      heldRequest = held;
      // Слушатели ветвей — ДО первого ожидания: отмена, случившаяся раньше, чем
      // их поставили, выбрала бы вторую ветвь на отменённом обращении.
      const cancelled = new Promise<"отменено">((resolve) => {
        context.on("requestfailed", (r) => {
          if (r === held) resolve("отменено");
        });
      });
      const verbIssued = new Promise<Request>((resolve) => {
        context.on("request", (r) => {
          if (r.method() === "POST" && new URL(r.url()).pathname === verb) resolve(r);
        });
      });
      try {
        issuedWith = await bearerOf(context);
        settled(held);
        const first = await Promise.race([cancelled, verbIssued]);
        if (first === "отменено") {
          branch = "первая: страница отменила задержанное, к краю не ушло ничего";
          return;
        }
        await first.response();
        const edge = await request.newContext({
          baseURL: use.baseURL,
          ignoreHTTPSErrors: use.ignoreHTTPSErrors,
          storageState: { cookies: [], origins: [] },
        });
        try {
          const answer = await edge.fetch(held.url(), {
            method: held.method(),
            headers: { ...held.headers(), cookie: `${SESSION_COOKIE}=${issuedWith}` },
          });
          const fulfilled = await route.fulfill({ response: answer }).then(
            () => "отдан",
            (e: unknown) => `не отдан: ${e instanceof Error ? e.message.split("\n")[0] : String(e)}`,
          );
          branch = `вторая: ответ края ${answer.status()} на носитель момента выпуска ${fulfilled} странице`;
        } finally {
          await edge.dispose();
        }
      } finally {
        released = true;
      }
    },
  );
  return {
    /**
     * «Дано» построено до отправки формы — утверждается, а не предполагается:
     * задержанное есть и выпущено с тем носителем, который у браузера.
     */
    async built(ctx: BrowserContext): Promise<string> {
      await expect
        .poll(() => heldRequest !== null, { message: `«Дано» не построено: страница не прочитала ${HELD_READ}`, timeout: 30_000 })
        .toBe(true);
      await heldIssued;
      const now = await bearerOf(ctx);
      expect(issuedWith, "«Дано» не построено: задержанное выпущено без носителя").not.toBe("");
      expect(issuedWith, "«Дано» не построено: задержанное выпущено не с тем носителем, что у браузера").toBe(now);
      return now;
    },
    /** Исход задержанного в переписи — ждётся условием, а не временем. */
    async outcome(census: CeremonyCensus): Promise<CeremonyCall> {
      const held = await heldIssued;
      await expect
        .poll(
          () => {
            const outcome = census.of(held)?.outcome;
            return outcome === undefined || outcome === "ждём" ? "исхода нет" : "исход есть";
          },
          { message: `у задержанного обращения нет исхода:\n${census.describe()}`, timeout: 30_000 },
        )
        .toBe("исход есть");
      return census.of(held) as CeremonyCall;
    },
    /** Прочие обращения страницы к тому же чтению — выпущенные снова. */
    others(census: CeremonyCensus): CeremonyCall[] {
      const held = heldRequest === null ? undefined : census.of(heldRequest);
      return census.matching("GET", HELD_READ).filter((c) => c !== held);
    },
    /** Сработавшая ветвь — сведением, не утверждением. */
    async report(): Promise<void> {
      await expect
        .poll(() => released, { message: `правило отпускания не завершилось: ${branch}`, timeout: 30_000 })
        .toBe(true);
      testInfo.annotations.push({ type: "ветвь правила отпускания", description: branch });
      console.log(`[${testInfo.title}] ветвь правила отпускания — ${branch}`);
    },
  };
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

test("F8-46 · чтение, бывшее в полёте при смене пароля, не гасит перевыпущенный носитель", async ({
  page,
}, testInfo) => {
  // verifies #1274 — близнец F8-47: изменено только то, ставит ли глагол носитель.
  test.setTimeout(120_000);
  await withHuman(testInfo, "F8-46", page.context(), async (_seed, human) => {
    const census = ceremonyCensus(page.context());
    const held = await holdFirstPlatformRead(page, testInfo, LANE.password);
    const s = await openSettings(page);
    const before = await held.built(page.context());

    await s.password.current.fill(human.password);
    await s.password.next.fill(`${human.password}-nov`);
    const [changed] = await Promise.all([lanePost(page, LANE.password), s.password.submit.click()]);
    const outcome = await held.outcome(census);
    await held.report();

    // Сперва — ПРИЧИНА, названная по исходу задержанного: у консоли без
    // упорядочения ответ края на него гасит носитель, который смена пароля
    // только что поставила, и всё дальнейшее — следствие.
    expect(
      endedSession(outcome),
      `носитель погашен ответом на обращение с прежним носителем: ${formatCall(outcome)}\n${census.describe()}`,
    ).toBe(false);
    expect(outcome.outcome, `задержанное чтение не отменено страницей:\n${census.describe()}`).toBe(CANCELLED_BY_PAGE);

    expect(changed.status(), `смена пароля не прошла: ${await changed.text()}`).toBe(200);
    expect(((await changed.json()) as { session?: unknown }).session, "ответ смены пароля без session").toBeTruthy();
    const after = await bearerOf(page.context());
    expect(after, "носитель у браузера пуст после смены пароля").not.toBe("");
    expect(after, "смена пароля не перевыпустила носитель").not.toBe(before);

    await expect
      .poll(() => held.others(census).some((c) => c.outcome === 200), {
        message: `отменённое чтение не выпущено снова с ответом 200:\n${census.describe()}`,
        timeout: 30_000,
      })
      .toBe(true);
    expect(new URL(page.url()).pathname, "страница уведена со своего адреса").toBe("/settings");
    expect(
      census.calls.filter(endedSession).map(formatCall),
      `обращение страницы получило ответ «${SESSION_ENDED_TEXT}»`,
    ).toEqual([]);
  });
});

test("F8-47 · глагол, не ставящий носитель, чтения в полёте не отменяет, и отпущенное после него проходит", async ({
  page,
}, testInfo) => {
  // verifies #1274 — положительный близнец F8-46: изменено только то, ставит
  // ли глагол носитель.
  test.setTimeout(120_000);
  await withHuman(testInfo, "F8-47", page.context(), async () => {
    const census = ceremonyCensus(page.context());
    const held = await holdFirstPlatformRead(page, testInfo, LANE.enroll);
    const s = await openSettings(page);
    const before = await held.built(page.context());

    const [enrolled] = await Promise.all([lanePost(page, LANE.enroll), s.factor.enroll.click()]);
    const outcome = await held.outcome(census);
    await held.report();

    expect(
      outcome.outcome,
      `чтение, бывшее в полёте при глаголе без носителя, не получило ответа края 200:\n${census.describe()}`,
    ).toBe(200);
    expect(enrolled.status(), `заведение не прошло: ${await enrolled.text()}`).toBe(200);
    expect(await bearerOf(page.context()), "заведение второго фактора сменило носитель").toBe(before);
    expect(new URL(page.url()).pathname, "страница уведена со своего адреса").toBe("/settings");
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
