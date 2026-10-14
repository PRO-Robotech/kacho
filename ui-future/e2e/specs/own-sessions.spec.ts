// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Browser, type BrowserContext, type Page, type TestInfo } from "@playwright/test";
import { LANE_VERBS, captureAnswers, lanePostAnswer } from "./answer-on-arrival";
import {
  LANE,
  lastIssued,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  seedSecondFactor,
  transferSession,
  type Seed,
} from "./ceremony-seed";
import {
  ceremonyCensus,
  formatCall,
  isProviderAddressText,
  test,
  type CeremonyCall,
  type CeremonyCensus,
} from "./fixtures";
import { EDGE_CREDENTIAL_STATE_UNKNOWN, SESSION_NOT_FRESH, bodyOf, fulfillWith } from "./producer-answers";

/**
 * Свои сессии на `/settings` — перечень и выход из выбранной либо из всех,
 * кроме текущей (приёмка F8-S5, §4, уровень B).
 *
 * Каждое «Тогда» утверждается наблюдаемым: переписью обращений страницы
 * (`ceremonyCensus`, приёмка F8, Р6), текстом раздела и тем, куда консоль
 * ведёт ДРУГОЙ контекст браузера, чью сессию сняли, — на экран входа, а не
 * разметкой раздела.
 *
 * «ДАНО B». Человек `H` заводится посевом A (`seedConfirmedHuman`, одна
 * регистрация, запись `S_A`) с описанием клиента `f8-s5-<NN>-seed-a`; посев B
 * входит тем же человеком (`S_B`) с описанием `f8-s5-<NN>-seed-b`. Носитель
 * `S_B` — в контексте страницы, носитель `S_A` — во втором контексте `ctxA`.
 * Описания клиента различаются, поэтому строку перечня проба находит по
 * описанию, которое прислал посев, — тем же, что видит человек.
 *
 * Ось времени «несвежая сессия» строится подстановкой ПЕРВОГО ответа снятия
 * ответом производителя (`SESSION_NOT_FRESH`, приёмка F8-S5, Р6); окна свежести
 * прогон не пересиживает.
 */

const SESSIONS = LANE.sessions;
const END = LANE.sessionsEnd;
const END_OTHERS = LANE.sessionsEndOthers;

/** Пути полосы, к которым вправе обращаться экран параметров со своими сессиями (F8-S5-12). */
const SECTION_LANE_PATHS = new Set<string>([SESSIONS, END, END_OTHERS, LANE.csrf, LANE.stepUp]);

/** Прочие обращения полосы, которые экран параметров делает своими разделами. */
const SCREEN_LANE_PATHS = new Set<string>(["/iam/v1/auth/me", LANE.secondFactor]);

/** Строка ответа перечня — в форме службы (приёмка OS, Р4). */
interface ListedSession {
  id: string;
  current: boolean;
  userAgent?: string;
}

// ─── экран ───────────────────────────────────────────────────────────────────

function sessionsSection(page: Page) {
  // Раздел «Активные сессии» экрана «Безопасность»: перечень — общей таблицей,
  // «Выйти на всех, кроме этой» — с диалогом последствия, исход — областью
  // `role="status"` экрана (приёмка F8-S7, Р3, Р6, Р9).
  const region = page.getByRole("region", { name: "Активные сессии" });
  const rows = region.getByRole("table").getByRole("row").filter({ has: page.getByRole("cell") });
  const endOthers = region.getByRole("button", { name: "Выйти на всех, кроме этой" });
  return {
    region,
    rows,
    row: (agent: string) => rows.filter({ hasText: agent }),
    /** «Выйти» у строки — доступное имя называет сессию. */
    end: (agent: string) => rows.filter({ hasText: agent }).getByRole("button", { name: /^Выйти из сессии/ }),
    endOthers,
    /** «Выйти на всех, кроме этой» и подтверждение диалога «Выйти». */
    confirmEndOthers: async () => {
      await endOthers.click();
      await page.getByRole("dialog").getByRole("button", { name: "Выйти", exact: true }).click();
    },
    status: page.getByRole("status").filter({ hasText: /\S/ }),
    refusal: region.getByRole("alert"),
  };
}

/**
 * Открыть `/settings` и дождаться ответа перечня: раздел отрисован, а
 * перечень прочитан ровно тем ответом, который вернули.
 */
async function openSessions(page: Page): Promise<{ s: ReturnType<typeof sessionsSection>; listed: ListedSession[] }> {
  const read = page.waitForResponse((r) => new URL(r.url()).pathname === SESSIONS && r.request().method() === "GET", {
    timeout: 30_000,
  });
  await page.goto("/settings", { waitUntil: "domcontentloaded" });
  const s = sessionsSection(page);
  await expect
    .poll(
      async () => {
        if (await s.region.isVisible()) return "отрисован";
        const now = new URL(page.url()).pathname;
        return now === "/settings" ? "ещё не отрисован" : `страница уведена на ${now}`;
      },
      { message: "раздел «Активные сессии» на /settings не отрисован консолью", timeout: 30_000 },
    )
    .toBe("отрисован");
  const res = await read;
  expect(res.status(), `перечень своих сессий не прочитан: ${res.status()} ${await res.text()}`).toBe(200);
  const body = (await res.json()) as { sessions?: ListedSession[] };
  return { s, listed: body.sessions ?? [] };
}

/** Адрес страницы без запроса. */
function pathOf(page: Page): string {
  return new URL(page.url()).pathname;
}

/**
 * Куда консоль ведёт контекст, открывший платформенную страницу (панель):
 * «панель» — край принял носитель и каркас отрисован; «экран входа» — правило
 * `sign-in` (приёмка F8-S5, Р7) увело на вход.
 */
async function panelOutcome(page: Page, how: "goto" | "reload"): Promise<string> {
  if (how === "goto") await page.goto("/dashboard", { waitUntil: "domcontentloaded" });
  else await page.reload({ waitUntil: "domcontentloaded" });
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

/**
 * F8-S5-12: обращения полосы раздела — только свои пути, и ни одного адреса
 * чужого поставщика. Возвращает находки текстом — пусто значит «чисто».
 */
function sectionCensusFindings(calls: readonly CeremonyCall[]): string[] {
  const found: string[] = [];
  for (const c of calls) {
    if (isProviderAddressText(c.path)) found.push(`адрес чужого поставщика: ${formatCall(c)}`);
    else if (c.path.startsWith("/iam/v1/auth/") && !SECTION_LANE_PATHS.has(c.path) && !SCREEN_LANE_PATHS.has(c.path)) {
      found.push(`путь полосы вне раздела: ${formatCall(c)}`);
    }
  }
  return found;
}

function expectSectionCensusClean(census: CeremonyCensus) {
  expect(
    sectionCensusFindings(census.calls.filter((c) => c.kind === "запрос")),
    `F8-S5-12 · перепись обращений страницы:\n${census.describe()}`,
  ).toEqual([]);
}

function expectExactlyOneListRead(census: CeremonyCensus) {
  expect(
    census.matching("GET", SESSIONS).map((c) => c.outcome),
    `при открытии экрана перечень своих сессий читается ровно один раз:\n${census.describe()}`,
  ).toEqual([200]);
}

// ─── «Дано B» ────────────────────────────────────────────────────────────────

interface GivenB {
  seedA: Seed;
  seedB: Seed;
  email: string;
  password: string;
  agentA: string;
  agentB: string;
  ctxA: BrowserContext;
  pageA: Page;
  /** Запасные коды человека — только у «Дано» с кодом (F8-S5-05). */
  backupCodes: string[];
}

function agent(nn: string, which: string): string {
  return `f8-s5-${nn}-seed-${which}`;
}

/** Вход человека посевом — новая запись сессии у этого посева; шаг утверждает исход. */
async function signInSeed(seed: Seed, email: string, password: string, which: string) {
  const res = await seed.submit(LANE.login, "login", { email, password });
  expect(
    res.status(),
    `посев ${which}: вход ${email} отвергнут — ${res.status()} ${JSON.stringify(lastIssued(seed, LANE.login).body)}. ` +
      "Это УСЛОВИЕ сценария",
  ).toBe(200);
  expect(await seed.sessionBearer(), `посев ${which}: вход прошёл, а носителя у посева нет`).not.toBe("");
}

/** Второй контекст браузера — к тому же стенду, своей банкой печенья. */
async function secondContext(browser: Browser, testInfo: TestInfo): Promise<BrowserContext> {
  const use = testInfo.project.use;
  return browser.newContext({ baseURL: use.baseURL, ignoreHTTPSErrors: use.ignoreHTTPSErrors });
}

async function withGivenB<T>(
  testInfo: TestInfo,
  browser: Browser,
  page: Page,
  nn: string,
  body: (g: GivenB) => Promise<T>,
  opts: { secondFactor?: boolean } = {},
): Promise<T> {
  const agentA = agent(nn, "a");
  const agentB = agent(nn, "b");
  const seedA = await newSeed(testInfo, [], { userAgent: agentA });
  const seedB = await newSeed(testInfo, [], { userAgent: agentB });
  const ctxA = await secondContext(browser, testInfo);
  try {
    const human = await seedConfirmedHuman(seedA, seedAddress(`F8-S5-${nn}`));
    await signInSeed(seedB, human.email, human.password, "B");
    // Код заводится сессией B: подтверждение перевыпускает её носитель, поэтому
    // перенос в браузер — после посева фактора.
    const factor = opts.secondFactor ? await seedSecondFactor(seedB) : null;
    await transferSession(seedB, page.context());
    await transferSession(seedA, ctxA);
    const pageA = await ctxA.newPage();
    return await body({
      seedA,
      seedB,
      email: human.email,
      password: human.password,
      agentA,
      agentB,
      ctxA,
      pageA,
      backupCodes: factor?.backupCodes ?? [],
    });
  } finally {
    await ctxA.close();
    await seedA.dispose();
    await seedB.dispose();
  }
}

/** Строка перечня по описанию клиента — её `id` из ответа службы. */
function idOf(listed: ListedSession[], agentText: string): string {
  const row = listed.find((r) => r.userAgent === agentText);
  expect(row, `в ответе перечня нет записи с описанием клиента ${agentText}: ${JSON.stringify(listed)}`).toBeTruthy();
  return row!.id;
}

// Ответы глаголов полосы читаются тем же путём, что у соседних экранов
// (`answer-on-arrival.ts`): перехват ставится до отправки.
test.beforeEach(async ({ page }) => {
  await captureAnswers(page, LANE_VERBS);
});

// ═══ F8-S5 · свои сессии ══════════════════════════════════════════════════════

test("F8-S5-01 · перечень: обе сессии, текущая помечена, описание клиента как прислано", async ({
  page,
  browser,
}, testInfo) => {
  // verifies #3061 — близнец пометки: тот же «Дано», страница открыта в ctxA.
  await withGivenB(testInfo, browser, page, "01", async (g) => {
    const census = ceremonyCensus(page.context());
    const { s, listed } = await openSessions(page);
    await expect(s.rows, "раздел показывает не две строки").toHaveCount(2);
    await expect(s.rows.nth(0), "порядок строк — не порядок службы (S_A, затем S_B)").toContainText(g.agentA);
    await expect(s.rows.nth(1)).toContainText(g.agentB);
    await expect(s.row(g.agentB)).toContainText("Эта сессия");
    await expect(s.row(g.agentB).getByRole("button", { name: /^Выйти из сессии/ })).toHaveCount(0);
    await expect(s.end(g.agentA)).toBeVisible();
    await expect(s.row(g.agentA)).not.toContainText("Эта сессия");
    expect(listed.map((r) => [r.userAgent, r.current])).toEqual([
      [g.agentA, false],
      [g.agentB, true],
    ]);
    expectExactlyOneListRead(census);
    expectSectionCensusClean(census);

    // Близнец: носитель страницы — S_A; пометка переходит к его строке.
    const twin = sessionsSection(g.pageA);
    const read = g.pageA.waitForResponse(
      (r) => new URL(r.url()).pathname === SESSIONS && r.request().method() === "GET",
    );
    await g.pageA.goto("/settings", { waitUntil: "domcontentloaded" });
    expect((await read).status()).toBe(200);
    await expect(twin.row(g.agentA), "в ctxA пометка «Эта сессия» не у строки S_A").toContainText("Эта сессия");
    await expect(twin.row(g.agentB)).not.toContainText("Эта сессия");
  });
});

test("F8-S5-02 · выход из выбранной: та сессия уходит на вход, эта жива", async ({ page, browser }, testInfo) => {
  // verifies #3061 — близнец: F8-S5-01 (тот же «Дано» без нажатия) — ctxA остаётся на панели.
  await withGivenB(testInfo, browser, page, "02", async (g) => {
    expect(await panelOutcome(g.pageA, "goto"), "Дано: ctxA не открыла панель").toBe("панель");
    const census = ceremonyCensus(page.context());
    const { s, listed } = await openSessions(page);
    const idA = idOf(listed, g.agentA);
    const reread = page.waitForResponse(
      (r) => new URL(r.url()).pathname === SESSIONS && r.request().method() === "GET",
    );
    const [res] = await Promise.all([
      lanePostAnswer(page, END),
      s.end(g.agentA).click(),
    ]);
    expect(res.status(), `снятие не прошло: ${await res.text()}`).toBe(200);
    expect(await res.json()).toEqual({ ended: true });
    expect(JSON.parse(res.request().postData() ?? "{}")).toMatchObject({ sessionId: idA });
    expect(Object.keys(JSON.parse(res.request().postData() ?? "{}")).sort()).toEqual(["csrfToken", "sessionId"]);
    await expect(s.status, "раздел не назвал исход снятия").toContainText("Сессия завершена");
    const after = (await (await reread).json()) as { sessions?: ListedSession[] };
    expect((after.sessions ?? []).map((r) => r.userAgent)).toEqual([g.agentB]);
    await expect(s.rows).toHaveCount(1);
    expect(census.matching("GET", LANE.csrf, "?form=session-end").length, census.describe()).toBeGreaterThan(0);
    expectSectionCensusClean(census);

    expect(await panelOutcome(g.pageA, "reload"), "снятая сессия ctxA не ушла на вход").toBe("экран входа");
    expect(await panelOutcome(page, "goto"), "сессия страницы погасла вместе со снятой").toBe("панель");
  });
});

test("F8-S5-03 · выход на всех, кроме этой: прочие ушли, эта жива", async ({ page, browser }, testInfo) => {
  // verifies #3061 — близнец: F8-S5-04 (прочих живых сессий нет).
  await withGivenB(testInfo, browser, page, "03", async (g) => {
    const agentC = agent("03", "c");
    const seedC = await newSeed(testInfo, [], { userAgent: agentC });
    const ctxC = await secondContext(browser, testInfo);
    try {
      await signInSeed(seedC, g.email, g.password, "C");
      await transferSession(seedC, ctxC);
      const pageC = await ctxC.newPage();
      expect(await panelOutcome(g.pageA, "goto"), "Дано: ctxA не открыла панель").toBe("панель");
      expect(await panelOutcome(pageC, "goto"), "Дано: ctxC не открыла панель").toBe("панель");

      const census = ceremonyCensus(page.context());
      const { s } = await openSessions(page);
      await expect(s.rows).toHaveCount(3);
      const reread = page.waitForResponse(
        (r) => new URL(r.url()).pathname === SESSIONS && r.request().method() === "GET",
      );
      const [res] = await Promise.all([lanePostAnswer(page, END_OTHERS), s.confirmEndOthers()]);
      expect(res.status(), `«все, кроме этой» не прошло: ${await res.text()}`).toBe(200);
      expect(await res.json()).toEqual({ ended: 2 });
      await expect(s.status).toContainText("Завершено сессий: 2");
      const after = (await (await reread).json()) as { sessions?: ListedSession[] };
      expect((after.sessions ?? []).map((r) => [r.userAgent, r.current])).toEqual([[g.agentB, true]]);
      await expect(s.rows).toHaveCount(1);
      await expect(s.rows.first()).toContainText("Эта сессия");
      expectSectionCensusClean(census);

      expect(await panelOutcome(g.pageA, "reload"), "ctxA не ушла на вход").toBe("экран входа");
      expect(await panelOutcome(pageC, "reload"), "ctxC не ушла на вход").toBe("экран входа");
      expect(await panelOutcome(page, "goto"), "сессия страницы погасла").toBe("панель");
    } finally {
      await ctxC.close();
      await seedC.dispose();
    }
  });
});

test("F8-S5-04 · единственная сессия: «Других активных сессий нет.», снимать нечего", async ({ page }, testInfo) => {
  // verifies #3061 — близнец: F8-S5-03; различие одно — прочих живых сессий нет.
  // Вид — приёмка F8-S7, Р8 (замена F8-S5, §5): при одной текущей сессии раздел
  // говорит это словами, и кнопки «Выйти на всех, кроме этой» нет — снимать нечего.
  const seedA = await newSeed(testInfo, [], { userAgent: agent("04", "a") });
  try {
    await seedConfirmedHuman(seedA, seedAddress("F8-S5-04"));
    await transferSession(seedA, page.context());
    const census = ceremonyCensus(page.context());
    const { s } = await openSessions(page);
    await expect(s.rows).toHaveCount(1);
    await expect(s.rows.first()).toContainText("Эта сессия");
    await expect(s.region).toContainText("Других активных сессий нет.");
    await expect(s.endOthers, "кнопка «Выйти на всех, кроме этой» при одной сессии").toHaveCount(0);
    expect(census.matching("POST", END_OTHERS), census.describe()).toHaveLength(0);
    expectSectionCensusClean(census);
    expect(await panelOutcome(page, "goto"), "единственная сессия погасла").toBe("панель");
  } finally {
    await seedA.dispose();
  }
});

test("F8-S5-05 · несвежая сессия: повышение и ровно один повтор", async ({ page, browser }, testInfo) => {
  // verifies #3061 — близнец: F8-S5-02 без подстановки — окна повышения нет, снятие одно.
  // Первый ответ снятия — ответ производителя SESSION_NOT_FRESH (Р6); повтор — настоящий.
  let substituted = false;
  await page.route(
    (u) => u.pathname === END,
    async (route) => {
      if (route.request().method() !== "POST" || substituted) return route.continue();
      substituted = true;
      await fulfillWith(route, SESSION_NOT_FRESH);
    },
  );
  await withGivenB(testInfo, browser, page, "05", async (g) => {
    expect(await panelOutcome(g.pageA, "goto"), "Дано: ctxA не открыла панель").toBe("панель");
    const census = ceremonyCensus(page.context());
    const { s } = await openSessions(page);
    const [refused] = await Promise.all([
      lanePostAnswer(page, END),
      s.end(g.agentA).click(),
    ]);
    expect(refused.status()).toBe(403);
    // Окно подтверждения — способы службы (приёмка F8-S7, Р5; пароля в окне нет,
    // §5): человек «Дано» с кодом подтверждает запасным кодом.
    const dialog = page.getByRole("dialog", { name: "Подтверждение действия" });
    await expect(dialog, "консоль не открыла церемонию повышения на SESSION_NOT_FRESH").toBeVisible();
    await dialog.getByRole("radio", { name: "Запасной код", exact: true }).check();
    await dialog.getByRole("textbox", { name: "Запасной код", exact: true }).fill(g.backupCodes[0]);
    const retried = lanePostAnswer(page, END);
    const [raised] = await Promise.all([
      lanePostAnswer(page, LANE.stepUp),
      dialog.getByRole("button", { name: "Подтвердить", exact: true }).click(),
    ]);
    expect(raised.status(), `повышение запасным кодом не прошло: ${await raised.text()}`).toBe(200);
    const again = await retried;
    expect(again.status(), `повтор снятия после повышения не прошёл: ${await again.text()}`).toBe(200);
    expect(await again.json()).toEqual({ ended: true });
    await expect(dialog).toBeHidden();
    await expect(s.status).toContainText("Сессия завершена");
    expect(
      census.matching("POST", END).map((c) => c.outcome),
      `обращений к снятию — ровно два:\n${census.describe()}`,
    ).toEqual([403, 200]);
    expect(census.matching("POST", LANE.stepUp).map((c) => c.outcome)).toEqual([200]);
    expect(substituted).toBe(true);
    expectSectionCensusClean(census);
    expect(await panelOutcome(g.pageA, "reload"), "снятая сессия ctxA не ушла на вход").toBe("экран входа");
  }, { secondFactor: true });
});

test("F8-S5-06 · сессия ушла до нажатия: отказ назван, строка исчезает", async ({ page, browser }, testInfo) => {
  // verifies #3061 — близнец: F8-S5-02; различие одно — S_A снята до нажатия.
  await withGivenB(testInfo, browser, page, "06", async (g) => {
    const census = ceremonyCensus(page.context());
    const { s } = await openSessions(page);
    await expect(s.row(g.agentA)).toBeVisible();
    const out = await g.seedA.submit(LANE.logout, "logout", {});
    expect(out.status(), `Дано: посев A не вышел — ${out.status()}`).toBe(200);

    const reread = page.waitForResponse(
      (r) => new URL(r.url()).pathname === SESSIONS && r.request().method() === "GET",
    );
    const [res] = await Promise.all([
      lanePostAnswer(page, END),
      s.end(g.agentA).click(),
    ]);
    expect(res.status()).toBe(404);
    const refused = (await res.json()) as { message?: string; details?: Array<{ reason?: string }> };
    expect(refused.message).toBe("session not found");
    expect((refused.details ?? []).map((d) => d.reason)).toContain("SESSION_NOT_FOUND");
    await expect(s.refusal, "текст отказа службы не назван дословно").toContainText("session not found");
    const after = (await (await reread).json()) as { sessions?: ListedSession[] };
    expect((after.sessions ?? []).map((r) => r.userAgent)).toEqual([g.agentB]);
    await expect(s.rows).toHaveCount(1);
    await expect(page.getByRole("dialog", { name: "Подтверждение действия" })).toHaveCount(0);
    expect(pathOf(page), "консоль ушла с экрана параметров").toBe("/settings");
    expectSectionCensusClean(census);
  });
});

test("F8-S5-07 · служба молчит: ответ края назван, ничего не гасится", async ({ page, browser }, testInfo) => {
  // verifies #3061 — близнец: F8-S5-02; различие одно — служба не ответила краю.
  const unanswered = bodyOf(EDGE_CREDENTIAL_STATE_UNKNOWN);
  await page.route(
    (u) => u.pathname === END,
    (route) =>
      route.request().method() === "POST" ? fulfillWith(route, EDGE_CREDENTIAL_STATE_UNKNOWN) : route.continue(),
  );
  await withGivenB(testInfo, browser, page, "07", async (g) => {
    expect(await panelOutcome(g.pageA, "goto"), "Дано: ctxA не открыла панель").toBe("панель");
    const census = ceremonyCensus(page.context());
    const { s } = await openSessions(page);
    const [res] = await Promise.all([
      lanePostAnswer(page, END),
      s.end(g.agentA).click(),
    ]);
    expect(res.status()).toBe(EDGE_CREDENTIAL_STATE_UNKNOWN.status);
    await expect(s.refusal, "ответ края не назван на экране").toContainText(unanswered.message);
    await expect(page.getByRole("dialog", { name: "Подтверждение действия" })).toHaveCount(0);
    expect(census.matching("POST", END), `повтор снятия:\n${census.describe()}`).toHaveLength(1);
    expect(pathOf(page), "консоль ушла с экрана параметров").toBe("/settings");
    expectSectionCensusClean(census);
    expect(await panelOutcome(page, "goto"), "сессия страницы погашена").toBe("панель");
    expect(await panelOutcome(g.pageA, "reload"), "ctxA ушла на вход, хотя снятия не было").toBe("панель");
  });
});

test("F8-S5-11 · раздел достижим с клавиатуры", async ({ page, browser }, testInfo) => {
  // verifies #3061 — близнец: F8-S5-02 (нажатие указателем).
  await withGivenB(testInfo, browser, page, "11", async (g) => {
    const census = ceremonyCensus(page.context());
    const { s } = await openSessions(page);
    await expect(s.rows).toHaveCount(2);
    // Фокус — с последнего управляющего элемента раздела перед «Активными
    // сессиями», у которого он есть: «Ключи доступа» (у «Запасных кодов» без
    // кода действий нет; порядок разделов — приёмка F8-S7, Р1).
    const keys = page.getByRole("region", { name: "Ключи доступа" });
    await keys.getByRole("button").last().focus();
    const focused: string[] = [];
    for (let i = 0; i < 2; i++) {
      await page.keyboard.press("Tab");
      focused.push(
        await page.evaluate(() => {
          const el = document.activeElement as HTMLElement | null;
          const row = el?.closest("tr");
          return `${el?.textContent?.trim() ?? ""}${row ? ` · ${row.textContent ?? ""}` : ""}`;
        }),
      );
    }
    expect(focused[0], `фокус после «Ключей доступа»: ${focused.join(" → ")}`).toMatch(/^Выйти · /);
    expect(focused[0]).toContain(g.agentA);
    expect(focused[1], `фокус после «Выйти»: ${focused.join(" → ")}`).toBe("Выйти на всех, кроме этой");

    await s.end(g.agentA).focus();
    const [res] = await Promise.all([lanePostAnswer(page, END), page.keyboard.press("Enter")]);
    expect(res.status(), `снятие с клавиатуры не прошло: ${await res.text()}`).toBe(200);
    expect(await res.json()).toEqual({ ended: true });
    await expect(s.status).toContainText("Сессия завершена");
    expectSectionCensusClean(census);
  });
});

test("F8-S5-12 · перепись раздела: только свои пути, ни одного адреса поставщика — проверка видит подсаженное", () => {
  // verifies #3061 — сама перепись исполняется в каждом сценарии выше
  // (`expectSectionCensusClean`); здесь — положительный близнец проверки: на
  // подсаженном пути полосы вне раздела она находит его, на своих путях —
  // молчит. Близнец адреса поставщика — у предиката F8 (`isProviderAddressText`,
  // его инъекция — в наборе F8): адреса поставщика в коде набора не пишутся
  // (F8-43), и подсаживать его здесь нечем.
  const call = (path: string): CeremonyCall => ({
    method: "GET",
    origin: "http://console.kacho.test",
    path,
    query: "",
    kind: "запрос",
    outcome: 200,
    challenge: null,
  });
  const own = [SESSIONS, END, END_OTHERS, LANE.csrf, LANE.stepUp, "/iam/v1/auth/me", "/dashboard"].map(call);
  expect(sectionCensusFindings(own)).toEqual([]);
  expect(sectionCensusFindings([...own, call("/iam/v1/auth/sessions/other")])).toHaveLength(1);
});
