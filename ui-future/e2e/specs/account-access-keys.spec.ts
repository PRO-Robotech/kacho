// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type BrowserContext, type Locator, type Page, type Request, type TestInfo } from "@playwright/test";
import {
  pageAuthenticator,
  presentAccessKey,
  seedAccessKey,
  type PageAuthenticator,
  type PresentableKey,
} from "./access-key-seed";
import {
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
import { conditionNotCreated } from "./mail-receiver";
import { ACCESS_KEY_SESSION_NOT_FRESH, fulfillWith } from "./producer-answers";

/**
 * Ключи доступа на `/settings` (приёмка F8, ред. 12, S4, группа L; Р11, Р12).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ И ЧЕМ
 *
 * Наблюдаемым: переписью обращений страницы (`ceremonyCensus`), телом запроса,
 * как его отправил браузер, текстом раздела и — главное — тем, принимает ли
 * служба ключ. «Ключ принят / не принят» (Ф7 §3.0) наблюдает ПРОБА, а не
 * страница: своим контекстом запросов, держащим сессию того же человека, она
 * зовёт `accessKeys:beginAssertion` и `accessKeys:finishAssertion` с
 * утверждением, которое собирает сама закрытым ключом удостоверения. Это запись
 * выпускающего (Р6 п. 4): перепись страницы её не видит, ось источника она не
 * тратит (N29), носителя не ставит.
 *
 * Церемония регистрации у F8-50 — НАСТОЯЩАЯ: экран зовёт
 * `navigator.credentials.create`, и отвечает ему виртуальный аутентификатор
 * страницы (шаг 1 посева К приёмки F8-S4). Закрытый ключ заведённого экраном
 * удостоверения проба берёт у аутентификатора (`WebAuthn.getCredentials`, N34).
 *
 * ПОДСТАНОВКА — ТОЛЬКО ОСЬ 3 (Р9): окно свежести Ф7 — 15 минут, и проба его не
 * пересиживает; у F8-51 и F8-57 подставлен ПЕРВЫЙ ответ названного глагола
 * ответом производителя (`producer-answers.ts`), следующие — настоящие.
 *
 * УСЛОВИЕ П4 (§4). Происхождение браузера набора служба обязана принимать для
 * ключей доступа. Не принимает — исход «не выполнилось: условие не создано», а
 * не красное о продукте.
 *
 * ИМЯ ТЕСТА НАЧИНАЕТСЯ С ID СЦЕНАРИЯ (Р8); ссылка на задачу — внутри `test(…)`.
 * Ожидания временем нет: ждётся условие — отрисованный раздел, ответ, перечень.
 */

const STEP_UP = "/iam/v1/auth/step-up";
const NOT_ACCEPTED = "access key assertion is not accepted";

const keysOf = (userId: string) => `/iam/v1/users/${userId}/accessKeys`;

// ─── экран: доступные имена, а не классы ──────────────────────────────────────

function keysScreen(page: Page) {
  const region = page.getByRole("region", { name: "Ключи доступа" });
  const list = region.getByRole("list", { name: "Заведённые ключи" });
  return {
    heading: page.getByRole("heading", { name: "Параметры учётной записи" }),
    region,
    list,
    item: (name: string) => list.getByRole("listitem").filter({ hasText: name }),
    name: region.getByLabel("Имя", { exact: true }),
    description: region.getByLabel("Описание", { exact: true }),
    add: region.getByRole("button", { name: /Добавить ключ доступа$/ }),
    empty: region.getByText("Ключей доступа нет", { exact: true }),
  };
}

async function openKeys(page: Page) {
  await page.goto("/settings", { waitUntil: "domcontentloaded" });
  const s = keysScreen(page);
  await expect
    .poll(
      async () => {
        if (await s.region.isVisible()) return "отрисован";
        const now = new URL(page.url()).pathname;
        if (now !== "/settings") return `страница уведена на ${now}`;
        return (await s.heading.isVisible())
          ? "экран параметров отрисован, раздела «Ключи доступа» на нём нет"
          : "ещё не отрисован";
      },
      { message: "раздел «Ключи доступа» на /settings не отрисован консолью", timeout: 30_000 },
    )
    .toBe("отрисован");
  return s;
}

function expectContains(census: CeremonyCensus, method: string, path: string) {
  expect(
    census.matching(method, path).length,
    `перепись обращений страницы не содержит ${method} ${path}:\n${census.describe()}`,
  ).toBeGreaterThan(0);
}

function expectNoProvider(census: CeremonyCensus) {
  expect(
    census.providerCalls().map(formatCall),
    `перепись обращений страницы содержит адрес чужого поставщика:\n${census.describe()}`,
  ).toEqual([]);
}

async function bearerOf(context: BrowserContext): Promise<string> {
  return (await context.cookies()).find((c) => c.name === SESSION_COOKIE)?.value ?? "";
}

/** Ответ на обращение страницы к пути и методу — ждать ставится ДО действия. */
function answerTo(page: Page, method: string, path: string) {
  return page.waitForResponse((r) => r.request().method() === method && new URL(r.url()).pathname === path);
}

// ─── человек сценария ─────────────────────────────────────────────────────────

interface KeysHuman {
  seed: Seed;
  human: SeededHuman;
  userId: string;
}

/**
 * Человек П-п сценария — СВОЙ — с сессией у браузера. Посев живёт до конца
 * сценария: «ключ принят» проба спрашивает его контекстом. `before` — посев К′
 * до переноса носителя.
 */
async function withKeysHuman<T>(
  testInfo: TestInfo,
  scenario: string,
  context: BrowserContext,
  body: (h: KeysHuman) => Promise<T>,
  before: (h: KeysHuman) => Promise<void> = async () => undefined,
): Promise<T> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedConfirmedHuman(seed, seedAddress(scenario));
    const me = await seed.read(SESSION_IDENTITY);
    const who = lastIssued(seed, SESSION_IDENTITY).body as { user?: { id?: unknown } } | null;
    expect(me.status(), `посев: ответ края о сессии — ${me.status()}`).toBe(200);
    const userId = typeof who?.user?.id === "string" ? who.user.id : "";
    expect(userId, "посев: край не назвал человека сессии").not.toBe("");
    const h = { seed, human, userId };
    await before(h);
    await transferSession(seed, context);
    return await body(h);
  } finally {
    await seed.dispose();
  }
}

// ─── «ключ принят / не принят» — запись выпускающего ─────────────────────────

function originOf(testInfo: TestInfo): string {
  const base = testInfo.project.use.baseURL;
  if (!base) throw new Error("у проекта нет baseURL — происхождение браузера не названо");
  return new URL(base).origin;
}

/** Предъявление ключа службе — запись выпускающего (`presentAccessKey`, посев К). */
function present(h: KeysHuman, testInfo: TestInfo, key: PresentableKey) {
  return presentAccessKey(h.seed, testInfo.project.use.baseURL, key);
}

async function expectAccepted(h: KeysHuman, testInfo: TestInfo, key: PresentableKey, what: string) {
  const got = await present(h, testInfo, key);
  expect(got.status, `${what}: служба не приняла ключ — ${got.status} ${got.text.slice(0, 300)}`).toBe(200);
  const body = JSON.parse(got.text) as { userId?: unknown };
  expect(body.userId, `${what}: ответ утверждения не называет человека сессии`).toBe(h.userId);
}

async function expectNotAccepted(h: KeysHuman, testInfo: TestInfo, key: PresentableKey, what: string) {
  const got = await present(h, testInfo, key);
  expect(got.text, `${what}: служба ответила ${got.status} без отказа «ключ не принят»`).toContain(NOT_ACCEPTED);
}

/** Ключ посева К′ с именем и описанием сценария; П4 не создано — «не выполнилось». */
async function seedNamedKey(h: KeysHuman, testInfo: TestInfo, name: string, description?: string) {
  const key = await seedAccessKey(h.seed, testInfo.project.use.baseURL, { userVerification: true, name, description });
  return { ...key, signCount: 0 };
}

/** Удостоверение, которое экран завёл в аутентификаторе страницы (N34). */
async function createdOnPage(auth: PageAuthenticator): Promise<PresentableKey> {
  const { credentials } = await auth.cdp.send("WebAuthn.getCredentials", { authenticatorId: auth.authenticatorId });
  expect(credentials.length, "в аутентификаторе страницы нет удостоверения — экран его не завёл").toBe(1);
  const c = credentials[0];
  return {
    credentialId: Buffer.from(c.credentialId, "base64"),
    privateKey: Buffer.from(c.privateKey, "base64"),
    userHandle: Buffer.from(c.userHandle ?? "", "base64"),
    signCount: c.signCount,
  };
}

/** Отказ приёма результата причиной «происхождение не в перечне» — условие стенда П4. */
async function originCondition(res: Awaited<ReturnType<typeof answerTo>>, testInfo: TestInfo) {
  if (res.status() === 200) return;
  const text = await res.text();
  if (text.includes("ORIGIN_NOT_ALLOWED")) {
    conditionNotCreated(
      `условие не создано (приёмка F8, §4 П4): происхождение браузера набора ${originOf(testInfo)} не входит в перечень ` +
        "происхождений ключа доступа на стенде — церемония ключа на нём не выполнима",
    );
  }
}

/**
 * Имя доверяющей стороны испытания против происхождения браузера — тоже П4:
 * браузер ведёт церемонию только для имени, которому принадлежит адрес страницы
 * (то же имя либо его поддомен). Стенд, отдающий чужое имя, церемонии экрана не
 * допускает, и отказ браузера на нём — условие, а не исход экрана. Признаков два:
 * имя из ответа службы и адрес страницы, и оба печатаются.
 */
async function rpCondition(challenge: Awaited<ReturnType<typeof answerTo>>, testInfo: TestInfo) {
  const rpId = ((await challenge.json()) as { rp?: { id?: string } }).rp?.id ?? "";
  const host = new URL(originOf(testInfo)).hostname;
  if (rpId !== "" && host !== rpId && !host.endsWith(`.${rpId}`)) {
    conditionNotCreated(
      `условие не создано (приёмка F8, §4 П4): служба стенда выдаёт испытание для доверяющей стороны «${rpId}», ` +
        `а браузер набора открыт на ${host} — церемония ключа браузером на этом адресе не выполнима`,
    );
  }
}

/** Подставить ПЕРВЫЙ ответ глагола ответом свежести (ось 3, Р9); следующие — настоящие. */
async function firstAnswerNotFresh(page: Page, method: string, matches: (path: string) => boolean) {
  const state = { substituted: false };
  await page.route(
    (u) => matches(u.pathname),
    async (route) => {
      if (route.request().method() !== method || state.substituted) return route.continue();
      state.substituted = true;
      await fulfillWith(route, ACCESS_KEY_SESSION_NOT_FRESH);
    },
  );
  return state;
}

/** Окно повышения, открытое отказом свежести: пароль и «Войти заново» (Р12); повышение паролем. */
async function raiseWithPassword(page: Page, human: SeededHuman, census: CeremonyCensus) {
  const dialog = page.getByRole("dialog", { name: "Подтверждение действия" });
  await expect(dialog, "консоль не открыла окно повышения на SESSION_NOT_FRESH").toBeVisible();
  await expect(
    dialog.getByRole("button", { name: /Войти заново$/ }),
    "в окне свежести нет пути «Войти заново» (Р12)",
  ).toBeVisible();
  await dialog.getByRole("radio", { name: "Паролем" }).check();
  await dialog.getByLabel("Пароль", { exact: true }).fill(human.password);
  const [raised] = await Promise.all([
    answerTo(page, "POST", STEP_UP),
    dialog.getByRole("button", { name: "Подтвердить" }).click(),
  ]);
  expect(raised.status(), `повышение паролем не прошло: ${await raised.text()}`).toBe(200);
  const sent = JSON.parse(raised.request().postData() ?? "{}") as { method?: unknown };
  expect(sent.method, "повышение ушло не способом «пароль»").toBe("password");
  await expect(dialog).toBeHidden();
  expectContains(census, "POST", STEP_UP);
}

function requestBody(req: Request): Record<string, unknown> {
  return JSON.parse(req.postData() ?? "{}") as Record<string, unknown>;
}

function expectNoDeletion(locator: Locator, what: string) {
  return expect(locator.getByRole("button", { name: /Удалить$/ }), what).toHaveCount(0);
}

// ═══ S4 — группа L ═══════════════════════════════════════════════════════════

test("F8-48 · перечень ключей прочитан и показан", async ({ page }, testInfo) => {
  // verifies #3057 — раздел «Ключи доступа» читает перечень глаголом Ф7 и
  // показывает ключ посева К′: имя, описание, момент заведения, «Ещё не использовался».
  await withKeysHuman(
    testInfo,
    "F8-48",
    page.context(),
    async ({ userId }) => {
      const census = ceremonyCensus(page.context());
      const listed = answerTo(page, "GET", keysOf(userId));
      const s = await openKeys(page);
      const answer = await listed;
      expect(answer.status(), `перечень ключей не прочитан: ${await answer.text()}`).toBe(200);
      const body = (await answer.json()) as {
        accessKeys?: Array<{ name?: string; createdAt?: string; lastUsedAt?: string }>;
      };
      expect(
        body.accessKeys?.map((k) => k.name),
        "ответ перечня — не один ключ посева",
      ).toEqual(["seed-f8-48"]);
      expect(body.accessKeys?.[0].lastUsedAt, "ключ посева уже использован — «Дано» не построено").toBeUndefined();
      expectContains(census, "GET", keysOf(userId));

      await expect(s.list.getByRole("listitem"), "раздел показывает не ровно один ключ").toHaveCount(1);
      const item = s.item("seed-f8-48");
      await expect(item).toContainText("Рабочий ноутбук");
      const created = new Date(body.accessKeys![0].createdAt!).toLocaleDateString("ru-RU", {
        day: "2-digit",
        month: "2-digit",
        year: "numeric",
      });
      await expect(item, "момент заведения из ответа перечня не показан").toContainText(created);
      await expect(item).toContainText("Ещё не использовался");
      await expect(item.getByRole("button", { name: /Удалить$/ })).toBeVisible();
      await expect(s.add).toBeVisible();
      expectNoProvider(census);
    },
    async (h) => {
      await seedNamedKey(h, testInfo, "seed-f8-48", "Рабочий ноутбук");
    },
  );
});

test("F8-49 · ключей нет: раздел говорит это и предлагает добавить", async ({ page }, testInfo) => {
  // verifies #3057 — близнец F8-48: изменено только то, заведён ли ключ.
  await withKeysHuman(testInfo, "F8-49", page.context(), async ({ userId }) => {
    const census = ceremonyCensus(page.context());
    const s = await openKeys(page);
    await expect(s.empty, "раздел не сказал «Ключей доступа нет»").toBeVisible();
    await expect(s.add).toBeVisible();
    await expectNoDeletion(s.region, "у пустого перечня есть кнопка «Удалить»");
    expectContains(census, "GET", keysOf(userId));
  });
});

test("F8-50 · заведение ключа на /settings проходит, и служба принимает этот ключ", async ({ page }, testInfo) => {
  // verifies #3057 — церемония браузера настоящая: аутентификатор страницы.
  test.setTimeout(120_000);
  const auth = await pageAuthenticator(page, true);
  await withKeysHuman(testInfo, "F8-50", page.context(), async (h) => {
    const census = ceremonyCensus(page.context());
    const s = await openKeys(page);
    await expect(s.empty).toBeVisible();
    const before = await bearerOf(page.context());

    const begun = answerTo(page, "POST", `${keysOf(h.userId)}:beginRegistration`);
    const finished = answerTo(page, "POST", keysOf(h.userId));
    await s.name.fill("key-f8-50");
    await s.description.fill("Ноутбук");
    await s.add.click();
    const challenge = await begun;
    expect(challenge.status(), `выдача испытания регистрации: ${await challenge.text()}`).toBe(200);
    await rpCondition(challenge, testInfo);
    const issued = Buffer.from(((await challenge.json()) as { challenge?: string }).challenge ?? "", "base64");
    const accepted = await finished;
    await originCondition(accepted, testInfo);
    expect(accepted.status(), `приём результата отвергнут: ${await accepted.text()}`).toBe(200);
    const op = (await accepted.json()) as { id?: string };
    expect(op.id ?? "", "ответ приёма результата — не Operation").not.toBe("");

    await expect(s.item("key-f8-50"), "раздел не показал заведённый ключ").toBeVisible({ timeout: 30_000 });
    await expect(s.item("key-f8-50")).toContainText("Ноутбук");

    // Тело приёма результата — ровно объявленные поля; значения — байты ответа браузера.
    const sent = requestBody(accepted.request());
    expect(Object.keys(sent).sort()).toEqual(["credential", "description", "name"]);
    const credential = sent.credential as Record<string, unknown>;
    expect(Object.keys(credential).sort()).toEqual(["attestationObject", "clientDataJson", "discoverable", "id"]);
    expect({ name: sent.name, description: sent.description, discoverable: credential.discoverable }).toEqual({
      name: "key-f8-50",
      description: "Ноутбук",
      discoverable: true,
    });
    const onPage = await createdOnPage(auth);
    expect(
      Buffer.from(String(credential.id), "base64").equals(onPage.credentialId),
      "id — не удостоверение аутентификатора",
    ).toBe(true);
    const clientData = JSON.parse(Buffer.from(String(credential.clientDataJson), "base64").toString("utf8")) as {
      type?: string;
      challenge?: string;
    };
    expect(clientData.type).toBe("webauthn.create");
    expect(
      Buffer.from(clientData.challenge ?? "", "base64url").equals(issued),
      "клиентские данные несут не выданное испытание",
    ).toBe(true);
    expect(Buffer.from(String(credential.attestationObject), "base64").length).toBeGreaterThan(0);

    // Порядок переписи: испытание → приём результата → опрос операции → перечень.
    expectContains(census, "GET", `/operations/${op.id}`);
    expect(census.matching("GET", keysOf(h.userId)).length, "перечень не перечитан после done").toBeGreaterThan(1);

    await expectAccepted(h, testInfo, onPage, "ключ, заведённый экраном");
    for (const path of ["/iam/v1/auth/login", "/iam/v1/auth/access-key/begin", "/iam/v1/auth/access-key/login"]) {
      expect(census.matching("POST", path).length, `перепись содержит ${path}`).toBe(0);
    }
    expectNoProvider(census);
    expect(new URL(page.url()).pathname).toBe("/settings");
    expect(await bearerOf(page.context()), "заведение ключа сменило носитель сессии").toBe(before);
  });
});

test("F8-51 · сессия не свежа: окно повышения, затем тот же шаг", async ({ page }, testInfo) => {
  // verifies #3057 — близнец F8-50: изменён только первый ответ на выдачу
  // испытания регистрации (подставлен ответом производителя, ось 3).
  test.setTimeout(120_000);
  await pageAuthenticator(page, true);
  const state = await firstAnswerNotFresh(page, "POST", (p) => p.endsWith("/accessKeys:beginRegistration"));
  await withKeysHuman(testInfo, "F8-51", page.context(), async (h) => {
    const census = ceremonyCensus(page.context());
    const s = await openKeys(page);
    await s.name.fill("key-f8-51");
    await s.add.click();
    const retried = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === `${keysOf(h.userId)}:beginRegistration` &&
        r.status() === 200,
    );
    await raiseWithPassword(page, h.human, census);
    const again = await retried;
    expect(again.status()).toBe(200);
    expect(state.substituted).toBe(true);
    expect(
      census.matching("POST", `${keysOf(h.userId)}:beginRegistration`).length,
      "выдача испытания не повторена сама",
    ).toBe(2);
    await rpCondition(again, testInfo);
    const finished = await answerTo(page, "POST", keysOf(h.userId));
    await originCondition(finished, testInfo);
    await expect(s.item("key-f8-51"), "ключ не заведён после повышения").toBeVisible({ timeout: 30_000 });
    expect(new URL(page.url()).pathname).toBe("/settings");
  });
});

test("F8-56 · удаление ключа: ключ уходит из перечня, служба его больше не принимает, второй цел", async ({
  page,
}, testInfo) => {
  // verifies #3057 — снятие по платформенному id ключа (ban #15), с подтверждением в диалоге.
  test.setTimeout(120_000);
  const keys: { a?: PresentableKey & { accessKeyId: string }; b?: PresentableKey & { accessKeyId: string } } = {};
  await withKeysHuman(
    testInfo,
    "F8-56",
    page.context(),
    async (h) => {
      const census = ceremonyCensus(page.context());
      const s = await openKeys(page);
      await expect(s.item("key-a")).toBeVisible();
      await s
        .item("key-a")
        .getByRole("button", { name: /Удалить$/ })
        .click();
      const dialog = page.getByRole("dialog").filter({ hasText: "key-a" });
      await expect(dialog, "диалог удаления не назвал ключ").toBeVisible();
      const removed = answerTo(page, "DELETE", `${keysOf(h.userId)}/${keys.a!.accessKeyId}`);
      await dialog.getByRole("button", { name: /Удалить$/ }).click();
      const res = await removed;
      expect(res.status(), `снятие отвергнуто: ${await res.text()}`).toBe(200);
      expect(keys.a!.accessKeyId, "путь снятия несёт не платформенный id").toMatch(/^ak/);
      await expect(s.item("key-a"), "снятый ключ остался в перечне").toHaveCount(0, { timeout: 30_000 });
      await expect(s.list.getByRole("listitem")).toHaveCount(1);
      await expect(s.item("key-b")).toBeVisible();
      expectContains(census, "DELETE", `${keysOf(h.userId)}/${keys.a!.accessKeyId}`);
      await expectNotAccepted(h, testInfo, keys.a!, "снятый ключ key-a");
      await expectAccepted(h, testInfo, keys.b!, "оставшийся ключ key-b");
    },
    async (h) => {
      keys.a = await seedNamedKey(h, testInfo, "key-a");
      keys.b = await seedNamedKey(h, testInfo, "key-b");
      await expectAccepted(h, testInfo, keys.a, "посев key-a");
      await expectAccepted(h, testInfo, keys.b, "посев key-b");
    },
  );
});

test("F8-57 · удаление при несвежей сессии: окно повышения, затем тот же шаг", async ({ page }, testInfo) => {
  // verifies #3057 — близнец F8-56: изменён только первый ответ на снятие
  // (подставлен ответом производителя, ось 3); согласие человек уже дал.
  test.setTimeout(120_000);
  const keys: { a?: PresentableKey & { accessKeyId: string } } = {};
  const state = await firstAnswerNotFresh(page, "DELETE", (p) => /\/accessKeys\/[^/]+$/.test(p));
  await withKeysHuman(
    testInfo,
    "F8-57",
    page.context(),
    async (h) => {
      const census = ceremonyCensus(page.context());
      const s = await openKeys(page);
      await s
        .item("key-a")
        .getByRole("button", { name: /Удалить$/ })
        .click();
      await page
        .getByRole("dialog")
        .filter({ hasText: "key-a" })
        .getByRole("button", { name: /Удалить$/ })
        .click();
      const path = `${keysOf(h.userId)}/${keys.a!.accessKeyId}`;
      const retried = page.waitForResponse(
        (r) => r.request().method() === "DELETE" && new URL(r.url()).pathname === path && r.status() === 200,
      );
      await raiseWithPassword(page, h.human, census);
      expect((await retried).status()).toBe(200);
      expect(state.substituted).toBe(true);
      expect(census.matching("DELETE", path).length, "снятие не повторено само").toBe(2);
      await expect(s.item("key-a")).toHaveCount(0, { timeout: 30_000 });
      await expect(s.item("key-b")).toBeVisible();
    },
    async (h) => {
      keys.a = await seedNamedKey(h, testInfo, "key-a");
      await seedNamedKey(h, testInfo, "key-b");
    },
  );
});
