// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import {
  expect,
  type Locator,
  type Page,
  type TestInfo,
} from "@playwright/test";
import {
  LANE,
  newSeed,
  seedAddress,
  seedHuman,
  transferSession,
  type SeededHuman,
} from "./ceremony-seed";
import {
  ceremonyCensus,
  formatCall,
  register,
  test,
  type CeremonyCall,
  type CeremonyCensus,
} from "./fixtures";
import { stationMailbox } from "./mail-receiver";

/**
 * Дальше входа — только с подтверждённым адресом почты: консоль (приёмка F6b, S2).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ
 *
 * Учётная запись с неподтверждённым адресом видит экраны регистрации и входа,
 * экран «подтвердите почту» и выход — и ничего больше. Каждое «Тогда» здесь —
 * наблюдаемое: адрес страницы, текст на экране и ПЕРЕПИСЬ обращений страницы
 * (`ceremonyCensus`), а не разметка. Проба о разметке зеленеет, когда каркас
 * отрисован и тут же спрятан, — а человек при этом уже получил чтения каркаса.
 *
 * ИМЯ ТЕСТА НАЧИНАЕТСЯ С ID СЦЕНАРИЯ приёмки: перепись имён даёт множество
 * исполненных сценариев. Ссылка на задачу — внутри каждого вызова `test(…)`.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ЗДЕСЬ НЕТ — И ГДЕ ЭТО ЖИВЁТ
 *
 * Сценарии с ПОДТВЕРЖДЁННЫМ человеком (посев П-п) и с письмом требуют глагола
 * подтверждения у службы, письма регистрации и чтения приёмника писем прогоном
 * (приёмка F6b, §3.4). Чтение приёмника прогоном и фикстура регистрации,
 * проходящая подтверждение экраном, заведены набором (S3, консольная часть
 * `kacho#2901`: `mail-receiver.ts`, условие `mail-receiver-reads.precondition.ts`,
 * `register` в `fixtures.ts`) — их держат F6b-32, F6b-33 и F6b-34 ниже. Посева
 * П-п и сценариев на нём здесь ещё нет. Прочие сценарии строят «Дано» посевом
 * П-н: регистрацией глаголом службы, без предъявления кода.
 */

// ─── перечень Р6: что консоль вправе звать до подтверждения ───────────────────

/** Путь, который перепись считает обращением к API (приёмка F6b, Р6). */
const API_PATH = [/^\/[a-z-]+\/v1(\/|$)/, /^\/operations(\/|$)/, /^\/oauth\//];

const VERIFY_EMAIL = "/iam/v1/auth/verify-email";
const VERIFY_EMAIL_CONFIRM = "/iam/v1/auth/verify-email/confirm";
const SESSION_IDENTITY = "/iam/v1/auth/me";

/** Разрешённые до подтверждения обращения — таблица Р6, и только она. */
const ALLOWED_BEFORE_CONFIRMATION: ReadonlyArray<{
  method: string;
  path: string;
}> = [
  { method: "GET", path: SESSION_IDENTITY },
  { method: "GET", path: LANE.csrf },
  { method: "POST", path: LANE.login },
  { method: "POST", path: LANE.register },
  { method: "POST", path: LANE.logout },
  { method: "POST", path: VERIFY_EMAIL },
  { method: "POST", path: VERIFY_EMAIL_CONFIRM },
];

function isApiCall(c: CeremonyCall): boolean {
  return c.kind === "запрос" && API_PATH.some((re) => re.test(c.path));
}

function allowedBeforeConfirmation(c: CeremonyCall): boolean {
  return ALLOWED_BEFORE_CONFIRMATION.some(
    (a) => a.method === c.method && a.path === c.path,
  );
}

/**
 * Обращения к API в переписи (начиная с `from`) лежат в перечне Р6.
 *
 * ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ И НЕ БЫВАЕТ НУЛЁМ (приёмка F6b, §6 п. 6): «лишних
 * ноль» при нуле осмотренных — перепись, которая ничего не видела, а не успех.
 * Лишнее обращение печатается целиком, вместе со всей переписью.
 */
function expectOnlyAllowedCalls(
  census: CeremonyCensus,
  where: string,
  from = 0,
) {
  const inspected = census.calls.slice(from).filter(isApiCall);
  console.log(
    `[перепись Р6] ${where}: осмотрено обращений к API — ${inspected.length}`,
  );
  expect(
    inspected.length,
    `перепись Р6 (${where}) не видела ни одного обращения к API — «лишних ноль» при нуле осмотренных ` +
      `не отличим от переписи, которая ничего не видела:\n${census.describe()}`,
  ).toBeGreaterThan(0);
  const extra = inspected.filter((c) => !allowedBeforeConfirmation(c));
  expect(
    extra.map(formatCall),
    `неподтверждённой учётной записи консоль выпустила обращения сверх перечня Р6 (${where}):\n${census.describe()}`,
  ).toEqual([]);
}

function expectNotCalled(
  census: CeremonyCensus,
  method: string,
  path: string,
  where: string,
) {
  expect(
    census.matching(method, path).map(formatCall),
    `перепись (${where}) содержит ${method} ${path}, которого здесь быть не должно:\n${census.describe()}`,
  ).toEqual([]);
}

// ─── экран подтверждения: доступные имена, а не классы ────────────────────────

const VERIFICATION_TITLE = "Подтвердите адрес почты";

function verificationScreen(page: Page) {
  return {
    heading: page.getByRole("heading", { name: VERIFICATION_TITLE }),
    code: page.getByRole("textbox", { name: "Код из письма" }),
    confirm: page.getByRole("button", { name: /Подтвердить$/ }),
    resend: page.getByRole("button", { name: /Отправить новое письмо$/ }),
    logout: page.getByRole("button", { name: /Выйти$/ }),
  };
}

function addressOf(page: Page): string {
  const u = new URL(page.url());
  return `${u.pathname}${u.search}`;
}

/** Адрес экрана подтверждения с адресом возврата — как его собирает консоль (`URLSearchParams`). */
function verificationAddressFor(pathname: string): string {
  if (pathname === "/") return "/verification";
  return `/verification?${new URLSearchParams({ returnTo: pathname }).toString()}`;
}

async function expectAddress(page: Page, address: string, why: string) {
  await expect
    .poll(() => addressOf(page), { message: why, timeout: 30_000 })
    .toBe(address);
}

/**
 * Экран подтверждения отрисован И выпустил свои обращения открытия: ответ края
 * о сессии получен. Только после этого перепись полна для утверждения «лишних
 * нет»: страж решил, и каркасу монтироваться больше неоткуда.
 */
async function expectVerificationSettled(
  census: CeremonyCensus,
  from: number,
  heading: Locator,
) {
  await expect(heading, "экран «подтвердите почту» не отрисован").toBeVisible({
    timeout: 30_000,
  });
  await expect
    .poll(
      () =>
        census.calls
          .slice(from)
          .some(
            (c) =>
              c.method === "GET" &&
              c.path === SESSION_IDENTITY &&
              typeof c.outcome === "number",
          )
          ? "ответ о сессии получен"
          : "ответа о сессии нет",
      {
        message: `экран подтверждения не спросил край о сессии:\n${census.describe()}`,
        timeout: 30_000,
      },
    )
    .toBe("ответ о сессии получен");
}

/** Посев П-н: регистрация глаголом службы, код не предъявлен (приёмка F6b, Р13). */
async function seededUnconfirmed(
  testInfo: TestInfo,
  scenario: string,
  page?: Page,
): Promise<SeededHuman> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedHuman(seed, seedAddress(scenario));
    const me = await seed.read(SESSION_IDENTITY);
    const body = (await me.json()) as {
      session?: { emailVerified?: unknown };
    } | null;
    expect(
      body?.session?.emailVerified,
      `посев П-н: ответ края о сессии посева не несёт emailVerified: false — ${JSON.stringify(body)}. ` +
        "Это УСЛОВИЕ сценария, а не его предмет",
    ).toBe(false);
    if (page) await transferSession(seed, page.context());
    return human;
  } finally {
    await seed.dispose();
  }
}

// ═══ S2 — страж над каркасом ══════════════════════════════════════════════════

/** Адреса консоли F6b-15; `<id>` — любой: страж решает раньше, чем каркас его прочтёт. */
const CONSOLE_ADDRESSES = [
  "/",
  "/dashboard",
  "/projects/prj-f6b15-any/vpc/networks",
  "/projects/prj-f6b15-any/compute",
  "/iam/",
  "/system/",
  "/settings",
  "/recovery",
  "/такого-адреса-нет",
];

test("F6b-15 · любой адрес консоли у неподтверждённой сессии ведёт на экран подтверждения", async ({
  page,
}, testInfo) => {
  // verifies #2898 — каркас открывался учётной записи с неподтверждённым адресом.
  await seededUnconfirmed(testInfo, "F6b-15", page);
  const census = ceremonyCensus(page.context());
  const s = verificationScreen(page);
  for (const address of CONSOLE_ADDRESSES) {
    const from = census.calls.length;
    await page.goto(address, { waitUntil: "domcontentloaded" });
    const pathname = new URL(address, page.url()).pathname;
    await expectAddress(
      page,
      verificationAddressFor(pathname),
      `адрес ${address} у неподтверждённой сессии не увёл на экран подтверждения`,
    );
    await expectVerificationSettled(census, from, s.heading);
    expectOnlyAllowedCalls(census, `адрес ${address}`, from);
  }
});

// ═══ S2 — регистрация и вход ══════════════════════════════════════════════════

test("F6b-12 · регистрация ведёт на экран подтверждения, а не в консоль, и письма консоль не шлёт", async ({
  page,
}) => {
  // verifies #2898 — после регистрации консоль уводила неподтверждённого в каркас.
  const census = ceremonyCensus(page.context());
  await page.goto("/registration?returnTo=/dashboard", {
    waitUntil: "domcontentloaded",
  });
  const email = seedAddress("F6b-12");
  await page
    .getByRole("textbox", { name: "Адрес электронной почты" })
    .fill(email);
  await page.getByLabel("Пароль", { exact: true }).fill("Kacho-E2E-2026!x");
  await page.getByRole("button", { name: /Завести учётную запись$/ }).click();
  await expectAddress(
    page,
    "/verification?returnTo=%2Fdashboard",
    "регистрация не увела на экран подтверждения",
  );
  const s = verificationScreen(page);
  await expectVerificationSettled(census, 0, s.heading);
  await expect(
    page.getByText(email, { exact: false }),
    "на экране нет адреса, который нужно подтвердить",
  ).toBeVisible();
  expectOnlyAllowedCalls(census, "регистрация");
  expectNotCalled(census, "GET", "/iam/v1/me", "регистрация");
  expectNotCalled(census, "GET", "/iam/v1/accounts", "регистрация");
  expectNotCalled(
    census,
    "POST",
    VERIFY_EMAIL,
    "регистрация: первое письмо ставит служба, а не консоль",
  );
});

test("F6b-13 · вход неподтверждённого ведёт на экран подтверждения с адресом возврата", async ({
  page,
}, testInfo) => {
  // verifies #2898 — вход уводил неподтверждённого прямо на адрес возврата, в каркас.
  const human = await seededUnconfirmed(testInfo, "F6b-13");
  const census = ceremonyCensus(page.context());
  await page.goto("/login?returnTo=/settings", {
    waitUntil: "domcontentloaded",
  });
  await page
    .getByRole("textbox", { name: "Адрес электронной почты" })
    .fill(human.email);
  await page.getByLabel("Пароль", { exact: true }).fill(human.password);
  await page.getByRole("button", { name: /Войти$/ }).click();
  await expectAddress(
    page,
    "/verification?returnTo=%2Fsettings",
    "вход не увёл на экран подтверждения",
  );
  await expectVerificationSettled(census, 0, verificationScreen(page).heading);
  expectOnlyAllowedCalls(census, "вход");
  await expect(
    page.getByRole("navigation"),
    "на экране подтверждения отрисован рейл каркаса",
  ).toHaveCount(0);
});

test("F6b-17 · экран входа при живой неподтверждённой сессии формы не показывает", async ({
  page,
}, testInfo) => {
  // verifies #2898 — экран входа уводил живую неподтверждённую сессию в каркас.
  await seededUnconfirmed(testInfo, "F6b-17", page);
  const census = ceremonyCensus(page.context());
  await page.goto("/login?returnTo=/dashboard", {
    waitUntil: "domcontentloaded",
  });
  await expectAddress(
    page,
    "/verification?returnTo=%2Fdashboard",
    "экран входа не увёл на экран подтверждения",
  );
  await expectVerificationSettled(census, 0, verificationScreen(page).heading);
  await expect(
    page.getByRole("button", { name: /Войти$/ }),
    "на экране осталась форма входа",
  ).toHaveCount(0);
  expectOnlyAllowedCalls(census, "экран входа при живой сессии");
});

// ═══ S2 — экран подтверждения ═════════════════════════════════════════════════

test("F6b-18 · вид экрана подтверждения и его обращения при открытии", async ({
  page,
}, testInfo) => {
  // verifies #2898 — адрес /verification отвечал «такого адреса здесь нет».
  const human = await seededUnconfirmed(testInfo, "F6b-18", page);
  const census = ceremonyCensus(page.context());
  await page.goto("/verification", { waitUntil: "domcontentloaded" });
  const s = verificationScreen(page);
  await expectVerificationSettled(census, 0, s.heading);
  expect(addressOf(page), "экран подтверждения увёл со своего адреса").toBe(
    "/verification",
  );
  await expect(
    page.getByText(
      `Чтобы продолжить работу в консоли, подтвердите адрес ${human.email}: введите код из письма, отправленного на этот адрес.`,
    ),
  ).toBeVisible();
  await expect(
    page.getByText(
      "Письмо с кодом приходит после регистрации. Если письма нет или код не подходит, отправьте новое.",
    ),
  ).toBeVisible();
  await expect(s.code).toBeVisible();
  await expect(s.confirm).toBeVisible();
  await expect(s.resend).toBeVisible();
  await expect(s.logout).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Продолжить$/ }),
    "кнопка «Продолжить» есть",
  ).toHaveCount(0);
  await expect(
    page.getByRole("navigation"),
    "на экране подтверждения отрисован рейл каркаса",
  ).toHaveCount(0);
  expectOnlyAllowedCalls(census, "открытие /verification");
  const posts = census.calls.filter((c) => isApiCall(c) && c.method === "POST");
  expect(
    posts.map(formatCall),
    `открытие экрана выпустило POST:\n${census.describe()}`,
  ).toEqual([]);
});

test("F6b-22 · выход с экрана подтверждения", async ({ page }, testInfo) => {
  // verifies #2898 — выхода с экрана подтверждения не было: экрана не было.
  await seededUnconfirmed(testInfo, "F6b-22", page);
  const census = ceremonyCensus(page.context());
  await page.goto("/verification", { waitUntil: "domcontentloaded" });
  const s = verificationScreen(page);
  await expectVerificationSettled(census, 0, s.heading);
  await s.logout.click();
  await expectAddress(
    page,
    "/login",
    "после выхода с экрана подтверждения консоль не вернула на экран входа",
  );
  expect(
    census.matching("GET", LANE.csrf, "?form=logout").length,
    census.describe(),
  ).toBeGreaterThan(0);
  expect(
    census.matching("POST", LANE.logout).length,
    census.describe(),
  ).toBeGreaterThan(0);
  const held = (await page.context().cookies()).some(
    (c) => c.name === "kaname_session" && c.value !== "",
  );
  expect(held, "после выхода носитель сессии у браузера остался").toBe(false);
  await page.goto("/verification", { waitUntil: "domcontentloaded" });
  await expectAddress(
    page,
    "/login",
    "открытие /verification без сессии не увело на вход",
  );
});

// ═══ S3 — набор ═══════════════════════════════════════════════════════════════

/** Код из тела отправки формы подтверждения — как его выпустила страница. */
function codeSent(body: string | null): string {
  try {
    const parsed = JSON.parse(body ?? "") as { code?: unknown };
    return typeof parsed.code === "string" ? parsed.code : "(код не строкой)";
  } catch (_notJson) {
    return `(тело не JSON: ${String(body).slice(0, 80)})`;
  }
}

test("F6b-32 · фикстура регистрации отдаёт человека с подтверждённым адресом тем же путём, что человек", async ({
  page,
}) => {
  // verifies #2901 — фикстура регистрации отдавала неподтверждённого человека, и набор падал одним текстом.
  const mailbox = stationMailbox();
  const email = seedAddress("F6b-32");
  // Письма, лежавшие у приёмника на этот адрес ДО регистрации, — не её письма.
  const before = new Set((await mailbox.letters(email)).map((l) => l.id));
  const census = ceremonyCensus(page.context());
  const sentCodes: string[] = [];
  page.context().on("request", (r) => {
    if (r.method() === "POST" && new URL(r.url()).pathname === VERIFY_EMAIL_CONFIRM) {
      sentCodes.push(codeSent(r.postData()));
    }
  });
  const visited: string[] = [];
  page.on("framenavigated", (f) => {
    if (f === page.mainFrame()) visited.push(new URL(f.url()).pathname);
  });

  await register(page, email);

  expect(
    census.matching("POST", LANE.register).length,
    `перепись фикстуры не содержит POST ${LANE.register}:\n${census.describe()}`,
  ).toBe(1);
  expect(
    visited,
    `вкладка фикстуры не побывала на экране подтверждения; адреса документа по порядку: ${visited.join(" → ")}`,
  ).toContain("/verification");
  const letters = (await mailbox.letters(email)).filter((l) => !before.has(l.id));
  expect(
    letters.map((l) => `${l.id} · ${l.created}`),
    `у приёмника на ${email} после регистрации ждали ровно одно письмо`,
  ).toHaveLength(1);
  expect(
    sentCodes,
    `фикстура предъявила не код письма, принятого после регистрации:\n${census.describe()}`,
  ).toEqual([letters[0].code]);
  expectNotCalled(
    census,
    "POST",
    VERIFY_EMAIL,
    "фикстура регистрации: первое письмо ставит регистрация, письма фикстура не просит",
  );

  const me = await page.request.get(SESSION_IDENTITY);
  const body = (await me.json().catch(() => null)) as {
    session?: { emailVerified?: unknown };
  } | null;
  expect(
    { status: me.status(), emailVerified: body?.session?.emailVerified },
    `ответ края о сессии браузера после фикстуры: ${JSON.stringify(body)}`,
  ).toEqual({ status: 200, emailVerified: true });
});

test("F6b-33 · посев П-н оставляет адрес неподтверждённым, и край отвечает ему отказом Р3", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #2901 — посев П-н не утверждал, что адрес остался неподтверждённым.
  const seed = await newSeed(testInfo);
  try {
    await seedHuman(seed, seedAddress("F6b-33"));
    const me = await seed.read(SESSION_IDENTITY);
    const meBody = (await me.json().catch(() => null)) as {
      session?: { emailVerified?: unknown };
    } | null;
    expect(
      { status: me.status(), emailVerified: meBody?.session?.emailVerified },
      `ответ края о сессии посева П-н: ${JSON.stringify(meBody)}`,
    ).toEqual({ status: 200, emailVerified: false });

    const refused = await seed.read("/iam/v1/projects");
    const text = await refused.text();
    const refusal = (() => {
      try {
        return JSON.parse(text) as {
          code?: unknown;
          message?: unknown;
          details?: Array<{ reason?: unknown; domain?: unknown }>;
        };
      } catch (_notJson) {
        return null;
      }
    })();
    const headers = refused.headersArray().map((h) => h.name.toLowerCase());
    expect(
      {
        status: refused.status(),
        code: refusal?.code,
        message: refusal?.message,
        reason: refusal?.details?.[0]?.reason,
        domain: refusal?.details?.[0]?.domain,
        challenge: headers.includes("www-authenticate"),
        setsCookie: headers.includes("set-cookie"),
      },
      `ответ края на GET /iam/v1/projects носителем П-н: ${refused.status()} ${text.slice(0, 300)}`,
    ).toEqual({
      status: 403,
      code: 7,
      message: "email address is not verified",
      reason: "EMAIL_NOT_VERIFIED",
      domain: "iam.kaname.cloud",
      challenge: false,
      setsCookie: false,
    });
  } finally {
    await seed.dispose();
  }
});
