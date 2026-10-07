// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Browser, type BrowserContext, type Page, type TestInfo } from "@playwright/test";
import { raiseAssurance } from "./assurance";
import {
  SESSION_IDENTITY,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  transferSession,
  type Seed,
} from "./ceremony-seed";
import {
  cloudAdminAtLevelTwo,
  freshAdminSession,
  resetOwnFactor,
  type CloudAdmin,
} from "./cloud-admin";
import { E2E_PASSWORD, register, test, watchRefusals } from "./fixtures";

/**
 * Сброс второго фактора распорядителем — ЭКРАНОМ консоли (приёмка F8r, S1;
 * `PRO-Robotech/kacho#3063`).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО СУДИТСЯ
 *
 * Приёмка воркспейса `docs/specs/sub-phase-F8r-console-and-edge-login-methods-reset-acceptance.md`,
 * отпечаток ee49b89ab32a52371635fa389c5afea7034ce16e74ac30e9fe0301ab3584fc83 — APPROVED
 * (событие в kacho#3063). Глагол службы (`UserService/ResetSecondFactor`) и его
 * пересылка краем уже есть; до этой правки позвать его можно было только
 * запросом мимо интерфейса — пробой края `cloud-admin-lane.spec.ts`. Предмет
 * здесь — то, что видит и получает человек у экрана: пункт в меню строки,
 * подтверждение, вызов, исход и его последствия для того, чей фактор сброшен.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ОТРИЦАНИЕ — ТОЛЬКО В ПАРЕ, И ФАКТ МЕЖДУ НИМИ ОДИН
 *
 *   F8r-02 против F8r-01: различие — держит ли вызывающий отношение записи;
 *   F8r-03 против F8r-01: различие — уровень сессии администратора («1» и «2»);
 *   F8r-04 против F8r-01: различие — заведён ли у цели фактор.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЗАПАСНЫМ КОДОМ, А НЕ КОДОМ ПО ВРЕМЕНИ, В ОКНЕ ПОВЫШЕНИЯ (F8r-03)
 *
 * Приёмка называет «код фактора». Код по времени администратор уже предъявил
 * один раз — подтверждением заведения в посеве (`seedSecondFactor`), и второе
 * предъявление в том же шаге было бы повтором, который служба отвергает; ждать
 * следующего шага значило бы ждать времени, а не условия. Запасной код — второй
 * способ ТОГО ЖЕ фактора, выданный тем же подтверждением, и окно повышения
 * предлагает его наравне (`ceremony-seed.ts`, шапка `seedSecondFactor`).
 *
 * УБОРКА. Фактор администратора снимается его же `ResetSecondFactor` в конце
 * каждой пробы (`resetOwnFactor`); провал уборки не прячет провал пробы и не
 * прячется за ним — та же обёртка, что у `cloud-admin-lane.spec.ts`.
 */

/** Подпись пункта меню строки — дословно (Р2). */
const RESET_ITEM = "Сбросить второй фактор";
/** Заголовок подтверждения над чужой строкой и над своей (Р2). */
const CONFIRM_OTHER = "Сбросить второй фактор?";
const CONFIRM_SELF = "Сбросить СВОЙ второй фактор?";
/** Исход — сообщением механизма глагола строки (Р5). */
const SUCCEEDED = "Второй фактор сброшен";
const FAILED_LEAD = "Не удалось сбросить второй фактор: ";
/**
 * Объяснение вердикта консоли по `AUTHZ_DENIED` и по `SECOND_FACTOR_NOT_ENROLLED`
 * (Р6). Выписано ЗДЕСЬ, а не взято у консоли: проба, берущая текст у
 * проверяемого, следует за ним молча и не утверждает о нём ничего.
 */
const FORBIDDEN_EXPLANATION =
  "Этот раздел доступен администраторам платформы. Если доступ нужен по работе — запросите его у администратора вашей организации.";
const NOTHING_TO_RESET = "У пользователя не настроен второй фактор — сбрасывать нечего.";
/** Состояние фактора на странице настроек учётной записи (Э-е). */
const FACTOR_ON = "Второй фактор настроен";
const FACTOR_OFF = "Второй фактор не настроен";

const resetPath = (userId: string) => `/iam/v1/users/${userId}:resetSecondFactor`;

/** Одно обращение страницы к глаголу либо к операции — с телом запроса и ответа, снятыми по прибытии. */
interface Seen {
  method: string;
  path: string;
  requestBody: string | null;
  status: number;
  wwwAuthenticate: string;
  body: string;
}

/**
 * Перепись обращений страницы к глаголу сброса и к опросу операций (Б-д).
 * Тело ответа снимается в обработчике, пока страница на месте; `settled` ждёт
 * все снятия — утверждения читают полную запись, а не её начало.
 */
function verbCensus(page: Page) {
  const seen: Seen[] = [];
  const pending: Promise<void>[] = [];
  page.on("response", (res) => {
    const path = new URL(res.url()).pathname;
    if (!path.endsWith(":resetSecondFactor") && !path.startsWith("/operations/")) return;
    const req = res.request();
    pending.push(
      res
        .text()
        .catch(() => "")
        .then((body) => {
          seen.push({
            method: req.method(),
            path,
            requestBody: req.postData(),
            status: res.status(),
            wwwAuthenticate: res.headers()["www-authenticate"] ?? "",
            body,
          });
        }),
    );
  });
  return {
    async settled(): Promise<Seen[]> {
      await Promise.allSettled(pending);
      return seen;
    },
    describe(): string {
      return seen.length === 0
        ? "  (обращений нет)"
        : seen.map((s) => `  ${s.method} ${s.path} → ${s.status} ${s.body.slice(0, 200)}`).join("\n");
    },
  };
}

/** Тело `google.rpc.Status` отказа края: код и причины. */
function refusalOf(text: string): { code: unknown; reasons: string[] } {
  const body = JSON.parse(text) as { code?: unknown; details?: Array<{ reason?: unknown }> };
  return { code: body.code, reasons: (body.details ?? []).map((d) => String(d.reason ?? "")) };
}

/** Контекст браузера человека — со стендом и стражами набора, как у штатного. */
async function humanContext(browser: Browser, testInfo: TestInfo): Promise<{ context: BrowserContext; page: Page }> {
  const use = testInfo.project.use;
  expect(use.baseURL, "адрес стенда не передан: относительные обращения второго контекста уйдут в никуда").toBeTruthy();
  const context = await browser.newContext({ baseURL: use.baseURL, ignoreHTTPSErrors: use.ignoreHTTPSErrors });
  // Отказы полосы этого контекста идут в счёт оси источника за этой пробой (F8-41).
  watchRefusals(context, testInfo.testId);
  return { context, page: await context.newPage() };
}

/** Человек `U`: регистрация с подтверждением адреса (Б-в) и фактор, поднявший сессию до «2» (Б-г). */
async function humanWithFactor(page: Page): Promise<{ email: string; userId: string }> {
  const email = await register(page);
  await raiseAssurance(page);
  const me = await sessionOf(page, "U после подъёма уровня");
  expect(me.level, "Дано U: заведение фактора обязано поднять сессию до «2»").toBe("2");
  return { email, userId: me.userId };
}

/** «Кто я» сессии браузера: идентификатор и уровень; пустые строки — сессии нет. */
async function sessionOf(page: Page, who: string): Promise<{ status: number; userId: string; level: string }> {
  const res = await page.request.get(SESSION_IDENTITY, { headers: { Accept: "application/json" } });
  const text = await res.text();
  expect(res.status(), `${who}: «кто я» — ${text.slice(0, 300)}`).toBe(200);
  const view = JSON.parse(text) as { user?: { id?: unknown } | null; session?: { assuranceLevel?: unknown } | null };
  return {
    status: res.status(),
    userId: String(view.user?.id ?? ""),
    level: view.session?.assuranceLevel === undefined ? "" : String(view.session.assuranceLevel),
  };
}

/** Строка состояния фактора на странице настроек — то, что видит человек (Э-е). */
async function expectFactorLine(page: Page, line: string, why: string): Promise<void> {
  await page.goto("/settings", { waitUntil: "domcontentloaded" });
  await expect(page.getByText(line, { exact: false }).first(), why).toBeVisible({ timeout: 30_000 });
  if (line === FACTOR_OFF) {
    // Строка «настроен» — подстрока соседней, поэтому её отсутствие
    // утверждается отдельно: иначе «не настроен» зеленело бы рядом с ней.
    await expect(page.getByText(FACTOR_ON, { exact: false }), `${why}: рядом показано «${FACTOR_ON}»`).toHaveCount(0);
  }
}

/**
 * Открыть пункт сброса на строке человека `email` в списке пользователей и
 * вернуть окно подтверждения. Строку находит поиск по почте — так её ищет
 * администратор; условие пробы (строка есть) утверждается отдельно от предмета
 * (пункт есть).
 */
async function openResetFor(page: Page, email: string, confirmTitle: string) {
  await page.goto("/iam/users", { waitUntil: "domcontentloaded" });
  await page.getByPlaceholder("Поиск по почте или идентификатору").fill(email);
  const row = page.locator("tr").filter({ hasText: email }).first();
  await expect(row, `строки ${email} нет в списке пользователей — условие пробы не создано`).toBeVisible({
    timeout: 60_000,
  });
  const actions = row.getByRole("button", { name: "Действия" }).first();
  await expect(actions, "у строки пользователя нет меню действий").toBeVisible({ timeout: 15_000 });
  await actions.click();
  // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: меню открыто и глаголы строки разрешились — соседний
  // пункт виден. Без него «пункта сброса нет» было бы неотличимо от «меню не открылось».
  await expect(
    page.getByRole("menuitem", { name: /Запретить участие|Вернуть участие/ }).first(),
    "меню строки открылось без глаголов строки — о пункте сброса такой прогон не говорит ничего",
  ).toBeVisible({ timeout: 15_000 });
  const item = page.getByRole("menuitem", { name: RESET_ITEM }).first();
  await expect(
    item,
    `в меню строки нет «${RESET_ITEM}»: глагол служба и край подают, а позвать его из консоли нечем`,
  ).toBeVisible({ timeout: 15_000 });
  // Пункт ВКЛЮЧЁН на каждой строке: права решает край, консоль не предугадывает (Р3).
  await expect(item, `пункт «${RESET_ITEM}» выключен — консоль судит о праве сама`).not.toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await item.click();
  const dialog = page.getByRole("dialog", { name: confirmTitle });
  await expect(dialog, `подтверждения «${confirmTitle}» нет — действие ушло бы одним нажатием`).toBeVisible({
    timeout: 15_000,
  });
  return dialog;
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

/** Обращения к глаголу сброса цели `userId` в записи страницы. */
function resetCalls(seen: Seen[], userId: string): Seen[] {
  return seen.filter((s) => s.method === "POST" && s.path === resetPath(userId));
}

test("F8r-01 · администратор облака сбрасывает второй фактор человека через экран", async ({ page, browser }, testInfo) => {
  // verifies #3063 — F8r-01 (F8r ee49b89a…): пункт, подтверждение, вызов, исход и последствия для U.
  test.setTimeout(300_000);
  await withCloudAdmin(testInfo, async (admin) => {
    const u = await humanContext(browser, testInfo);
    try {
      const human = await humanWithFactor(u.page);
      // Положительный контроль наблюдения: до сброса страница U называет фактор настроенным.
      await expectFactorLine(u.page, FACTOR_ON, "Дано U: страница настроек до сброса");
      await u.page.goto("/dashboard", { waitUntil: "domcontentloaded" });

      await transferSession(admin.seed, page.context());
      const census = verbCensus(page);
      const dialog = await openResetFor(page, human.email, CONFIRM_OTHER);
      await dialog.getByRole("button", { name: "Сбросить" }).click();
      await expect(page.getByText(SUCCEEDED, { exact: true }), "исход сброса не назван").toBeVisible({
        timeout: 60_000,
      });

      const seen = await census.settled();
      const posts = resetCalls(seen, human.userId);
      expect(posts.length, `ожидался ровно один вызов сброса:\n${census.describe()}`).toBe(1);
      expect(posts[0].requestBody, "тело вызова сброса — пустой объект").toBe("{}");
      expect(posts[0].status, `ответ края на вызов сброса:\n${census.describe()}`).toBe(200);
      const op = JSON.parse(posts[0].body) as { id?: unknown; done?: unknown; error?: unknown };
      expect(String(op.id ?? ""), "ответ вызова сброса без идентификатора операции").not.toBe("");
      const polls = seen.filter((s) => s.method === "GET" && s.path === `/operations/${String(op.id)}`);
      const last = polls.length > 0 ? (JSON.parse(polls[polls.length - 1].body) as { done?: unknown; error?: unknown }) : op;
      expect({ done: last.done, error: last.error }, `операция ${String(op.id)} не завершилась успехом:\n${census.describe()}`).toEqual({
        done: true,
        error: undefined,
      });

      // Вкладка U на следующем обращении получает отказ края и уходит на вход с адресом возврата.
      const refused = await u.page.request.get("/iam/v1/accounts?pageSize=1", { headers: { Accept: "application/json" } });
      expect(refused.status(), `следующее обращение вкладки U после сброса: ${await refused.text()}`).toBe(401);
      expect(refusalOf(await refused.text()).code, "отказ отсечённой сессии — UNAUTHENTICATED (16)").toBe(16);
      await u.page.reload({ waitUntil: "domcontentloaded" });
      await expect
        .poll(() => {
          const url = new URL(u.page.url());
          return `${url.pathname}?returnTo=${url.searchParams.get("returnTo") ?? ""}`;
        }, { message: "вкладка U с отсечённой сессией не ушла на вход с адресом возврата", timeout: 30_000 })
        .toBe("/login?returnTo=/dashboard");

      // U входит паролем — сессия «1», фактор не настроен, заведение заново поднимает до «2».
      const login = u.page.getByRole("form", { name: "Вход в консоль" });
      await login.getByRole("textbox", { name: "Адрес электронной почты" }).fill(human.email);
      await login.getByLabel("Пароль", { exact: true }).fill(E2E_PASSWORD);
      await login.getByRole("button", { name: /Войти$/ }).click();
      await expect
        .poll(async () => (await sessionOf(u.page, "U после входа паролем")).level, {
          message: "вход U паролем после сброса не дал сессии",
          timeout: 30_000,
        })
        .toBe("1");
      await expectFactorLine(u.page, FACTOR_OFF, "U после сброса: страница настроек");
      await raiseAssurance(u.page);
      expect((await sessionOf(u.page, "U после заведения заново")).level, "заведение фактора заново не подняло сессию").toBe("2");
    } finally {
      await u.context.close();
    }
  });
});

test("F8r-02 · не-держатель видит пункт и получает названный отказ; у цели ничего не изменено", async ({ page }) => {
  // verifies #3063 — F8r-02 (F8r ee49b89a…): близнец F8r-01, различие — вызывающий не держит отношение.
  test.setTimeout(240_000);
  const human = await humanWithFactor(page);
  const census = verbCensus(page);
  const dialog = await openResetFor(page, human.email, CONFIRM_SELF);
  await dialog.getByRole("button", { name: "Сбросить" }).click();
  await expect(
    page.getByText(`${FAILED_LEAD}${FORBIDDEN_EXPLANATION}`, { exact: true }),
    "отказ края не назван объяснением вердикта AUTHZ_DENIED",
  ).toBeVisible({ timeout: 30_000 });

  const seen = await census.settled();
  const posts = resetCalls(seen, human.userId);
  expect(posts.length, `ожидался ровно один вызов сброса:\n${census.describe()}`).toBe(1);
  expect(posts[0].status, `ответ края не-держателю:\n${census.describe()}`).toBe(403);
  const refusal = refusalOf(posts[0].body);
  expect(refusal.code, "код отказа — PERMISSION_DENIED (7)").toBe(7);
  expect(refusal.reasons, "причина отказа — AUTHZ_DENIED").toContain("AUTHZ_DENIED");
  expect(seen.filter((s) => s.path.startsWith("/operations/")), "отказ породил операцию").toEqual([]);

  // У цели ничего не изменено: сессия жива на прежнем уровне, фактор настроен.
  expect((await sessionOf(page, "U после отказа")).level, "сессия U после отказа").toBe("2");
  await expectFactorLine(page, FACTOR_ON, "U после отказа: страница настроек");
});

test("F8r-03 · сессия «1»: окно повышения и один повтор", async ({ page, browser }, testInfo) => {
  // verifies #3063 — F8r-03 (F8r ee49b89a…): близнец F8r-01, различие — уровень сессии администратора.
  test.setTimeout(300_000);
  await withCloudAdmin(testInfo, async (admin) => {
    const u = await humanContext(browser, testInfo);
    let fresh: Seed | null = null;
    try {
      const human = await humanWithFactor(u.page);
      fresh = await freshAdminSession(testInfo, "F8r-03: свежий вход паролем");
      await transferSession(fresh, page.context());
      expect((await sessionOf(page, "A₁")).level, "Дано A₁: свежий вход паролем — сессия «1»").toBe("1");

      const census = verbCensus(page);
      const dialog = await openResetFor(page, human.email, CONFIRM_OTHER);
      await dialog.getByRole("button", { name: "Сбросить" }).click();

      const stepUp = page.getByRole("dialog", { name: "Подтверждение действия" });
      await expect(stepUp, "край потребовал уровня «2», а окна повышения нет").toBeVisible({ timeout: 30_000 });
      await stepUp.getByRole("radio", { name: "Запасной код" }).check();
      await stepUp.getByRole("textbox", { name: "Код", exact: true }).fill(admin.factor.backupCodes[0]);
      await stepUp.getByRole("button", { name: "Подтвердить" }).click();
      await expect(page.getByText(SUCCEEDED, { exact: true }), "исход сброса после повышения не назван").toBeVisible({
        timeout: 60_000,
      });

      const seen = await census.settled();
      const posts = resetCalls(seen, human.userId);
      expect(posts.map((p) => p.status), `вызов и ровно один повтор:\n${census.describe()}`).toEqual([401, 200]);
      expect(refusalOf(posts[0].body).code, "первый вызов — UNAUTHENTICATED (16)").toBe(16);
      expect(posts[0].wwwAuthenticate, "вызов повышения уровня края").toContain("insufficient_user_authentication");
      expect(posts[0].wwwAuthenticate, "вызов называет уровень «2»").toContain('acr_values="2"');
      const op = JSON.parse(posts[1].body) as { id?: unknown };
      const polls = seen.filter((s) => s.method === "GET" && s.path === `/operations/${String(op.id ?? "")}`);
      expect(polls.length, `операции повтора не опрошены:\n${census.describe()}`).toBeGreaterThan(0);
      const last = JSON.parse(polls[polls.length - 1].body) as { done?: unknown; error?: unknown };
      expect({ done: last.done, error: last.error }, "операция повтора не завершилась успехом").toEqual({
        done: true,
        error: undefined,
      });
    } finally {
      if (fresh) await fresh.dispose();
      await u.context.close();
    }
  });
});

test("F8r-04 · сбрасывать нечего: названный отказ", async ({ page, browser }, testInfo) => {
  // verifies #3063 — F8r-04 (F8r ee49b89a…): близнец F8r-01, различие — у цели фактор не заводился.
  test.setTimeout(240_000);
  await withCloudAdmin(testInfo, async (admin) => {
    // W — регистрация с подтверждением адреса (Б-в) без шага Б-г: фактора нет, сессия «1».
    const wSeed = await newSeed(testInfo);
    const w = await humanContext(browser, testInfo);
    try {
      const human = await seedConfirmedHuman(wSeed, seedAddress("f8r-04-w"));
      await transferSession(wSeed, w.context);
      const before = await sessionOf(w.page, "W до вызова");
      expect(before.level, "Дано W: сессия без фактора — «1»").toBe("1");

      await transferSession(admin.seed, page.context());
      const census = verbCensus(page);
      const dialog = await openResetFor(page, human.email, CONFIRM_OTHER);
      await dialog.getByRole("button", { name: "Сбросить" }).click();
      await expect(
        page.getByText(`${FAILED_LEAD}${NOTHING_TO_RESET}`, { exact: true }),
        "отказ «нечего сбрасывать» не назван вердиктом консоли",
      ).toBeVisible({ timeout: 30_000 });

      const seen = await census.settled();
      const posts = resetCalls(seen, before.userId);
      expect(posts.length, `ожидался ровно один вызов сброса:\n${census.describe()}`).toBe(1);
      expect(posts[0].status, `ответ края на сброс несуществующего фактора:\n${census.describe()}`).toBe(400);
      const refusal = refusalOf(posts[0].body);
      expect(refusal.code, "код отказа — FAILED_PRECONDITION (9)").toBe(9);
      expect(refusal.reasons, "причина отказа — SECOND_FACTOR_NOT_ENROLLED").toContain("SECOND_FACTOR_NOT_ENROLLED");
      expect(seen.filter((s) => s.path.startsWith("/operations/")), "отказ породил операцию").toEqual([]);

      // У W ничего не изменено: сессия жива, фактор по-прежнему не настроен.
      expect((await sessionOf(w.page, "W после отказа")).userId, "сессия W после отказа").toBe(before.userId);
      await expectFactorLine(w.page, FACTOR_OFF, "W после отказа: страница настроек");
    } finally {
      await w.context.close();
      await wSeed.dispose();
    }
  });
});
