// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type BrowserContext, type Page, type Response, type TestInfo } from "@playwright/test";
import { pageAuthenticator, type PageAuthenticator } from "./access-key-seed";
import { captureAnswers, lanePostAnswer, type LaneAnswer } from "./answer-on-arrival";
import {
  LANE,
  SESSION_COOKIE,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  transferSession,
} from "./ceremony-seed";
import { ceremonyCensus, formatCall, test, type CeremonyCensus } from "./fixtures";
import { conditionNotCreated } from "./mail-receiver";
import { ACCESS_KEY_LIFECYCLE_CONDITIONS, FIXTURE_UNMET_PREFIX } from "./producer-answers";

/**
 * Сквозной путь ключа доступа сквозь консоль и край (#3059): завести ключ на
 * `/settings` → выйти → войти ключом без пароля → удалить ключ → вход удалённым
 * ключом отказан.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЭТО ЗА ПРОБА И ЧЕМ ОНА НЕ ЯВЛЯЕТСЯ
 *
 * Сценарии путь не заводит заново — он исполняет сценарии двух одобренных
 * приёмок ОДНИМ человеком и ОДНИМ ключом подряд: заведение экраном (F8-50),
 * вход ключом (F8S4-02 при проверке пользователя, F8S4-03 без неё), снятие
 * (F8-56) и отказ снятого (F8S4-04). Порознь эти сценарии держат свои пробы
 * (`account-access-keys.spec.ts`, `access-key-login.spec.ts`), и у каждой своё
 * «Дано» посевом: ключ входа там заводит КОД ПОСЕВА, а не экран. Здесь посева
 * ключа нет вовсе — ключ, которым человек входит, завёл экран консоли настоящей
 * церемонией браузера, и снимает его тоже экран. Это и есть предмет: годится ли
 * замена чужого поставщика для того, кто ею пользуется, — от заведения до снятия.
 *
 * Экраны (KA6, KA13) здесь не переутверждаются: тела запросов, перепись форм и
 * тексты отказов судят их пробы. Путь утверждает только то, что видно на стыках
 * шагов: ключ, заведённый экраном, открывает вход; снятый экраном — больше нет.
 *
 * ИМЯ ТЕСТА НАЧИНАЕТСЯ С ID СЦЕНАРИЯ S4 (Р8 приёмки F8): первым стоит сценарий
 * входа, по которому различаются два теста, — уровень сессии. Ссылка на задачу —
 * внутри `test(…)`.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * УРОВЕНЬ СЕССИИ — ПО ФАКТУ ПРОВЕРКИ ПОЛЬЗОВАТЕЛЯ, А НЕ ПО ОЖИДАНИЮ
 *
 * Служба выдаёт «3», если аутентификатор подтвердил пользователя (флаг UV данных
 * аутентификатора), и «2», если только присутствие (UP). Проба читает флаг из
 * утверждения, КАК ЕГО ОТПРАВИЛ браузер, и из него выводит ожидаемый уровень;
 * отдельно утверждает, что флаг — тот, какой даёт аутентификатор сценария.
 * Иначе тест «уровень 3» был бы зелёным на любом уровне, совпавшем с ожиданием
 * случайно, а не по построению.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * УСЛОВИЕ П4 — ДВА ПРИЗНАКА, А НЕ ОДНА ФРАЗА
 *
 * Происхождение браузера набора служба обязана принимать для ключей доступа;
 * производит это слой площадки (`ACCESS_KEY_LIFECYCLE_CONDITIONS.origins`).
 * Отказ `ORIGIN_NOT_ALLOWED` называется «условие не создано» ТОЛЬКО если
 * клиентские данные, которые браузер отправил, несут происхождение консоли:
 * тогда служба отвергла своё же происхождение, и это профиль стенда. Ключ,
 * заведённый под ЧУЖИМ происхождением, — красное о продукте, а не условие:
 * консоль не вправе отправить службе чужое происхождение, и проба утверждает
 * это раньше, чем читает ответ. Без второго признака любая подмена
 * происхождения пряталась бы за «не выполнилось».
 *
 * ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО: ожидания временем (ждётся условие — раздел, ответ,
 * адрес); подставленных ответов (каждый ответ — служба через край); общего
 * человека у тестов (каждый сеет своего, Р7 ч. 1 приёмки F8).
 */

const ACCESS_KEY_BEGIN = "/iam/v1/auth/access-key/begin";
const ACCESS_KEY_LOGIN = "/iam/v1/auth/access-key/login";
const FORM_LANE_VERBS = [LANE.logout, ACCESS_KEY_BEGIN, ACCESS_KEY_LOGIN] as const;

/** Тело отказа входа — одно на все причины (Ф13 Р7). */
const AUTHENTICATION_FAILED = { code: 16, message: "authentication failed", details: [] };

/** Флаги данных аутентификатора (WebAuthn §6.1): присутствие и проверка пользователя. */
const FLAG_UP = 0x01;
const FLAG_UV = 0x04;
/** Смещение байта флагов: за хешем имени доверяющей стороны (32 байта). */
const FLAGS_OFFSET = 32;

const keysPath = (userId: string) => `/iam/v1/users/${userId}/accessKeys`;

// ─── экраны: доступные имена, а не классы ─────────────────────────────────────

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

// Кнопки — по КОНЦУ доступного имени: значок ожидания остаётся в разметке
// свёрнутым (тот же довод, что у `access-key-login.spec.ts`).
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

function originOf(testInfo: TestInfo): string {
  const base = testInfo.project.use.baseURL;
  if (!base) throw new Error("у проекта нет baseURL — происхождение браузера не названо");
  return new URL(base).origin;
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

/** Ответ на обращение страницы — ждать ставится ДО действия. */
function answerTo(page: Page, method: string, path: string) {
  return page.waitForResponse((r) => r.request().method() === method && new URL(r.url()).pathname === path);
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

/** Раздел «Ключи доступа» отрисован НА СВОЁМ адресе; падение называет, куда увели. */
async function openKeys(page: Page) {
  await page.goto("/settings", { waitUntil: "domcontentloaded" });
  return keysShown(page);
}

async function keysShown(page: Page) {
  const s = keysScreen(page);
  await expect
    .poll(
      async () => {
        if (await s.region.isVisible()) return "отрисован";
        const now = pathOf(page);
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

// ─── условие П4 — два признака ────────────────────────────────────────────────

/**
 * Имя доверяющей стороны испытания против хоста консоли: браузер ведёт церемонию
 * только для имени, которому принадлежит адрес страницы. Чужое имя — профиль
 * стенда (П4), а не исход экрана; печатаются оба признака.
 */
async function rpCondition(challenge: Awaited<ReturnType<typeof answerTo>>, testInfo: TestInfo) {
  const rpId = ((await challenge.json()) as { rp?: { id?: string } }).rp?.id ?? "";
  const host = new URL(originOf(testInfo)).hostname;
  if (rpId !== "" && host !== rpId && !host.endsWith(`.${rpId}`)) {
    conditionNotCreated(
      `${FIXTURE_UNMET_PREFIX} ${ACCESS_KEY_LIFECYCLE_CONDITIONS.origins.condition} — нет: служба стенда выдаёт испытание ` +
        `для доверяющей стороны «${rpId}», а браузер набора открыт на ${host}. Производит условие ` +
        ACCESS_KEY_LIFECYCLE_CONDITIONS.origins.producer,
    );
  }
}

/** Происхождение, под которым консоль отправила результат церемонии регистрации. */
function sentOrigin(accepted: Awaited<ReturnType<typeof answerTo>>): string {
  const sent = JSON.parse(accepted.request().postData() ?? "{}") as { credential?: { clientDataJson?: unknown } };
  const raw = Buffer.from(String(sent.credential?.clientDataJson ?? ""), "base64").toString("utf8");
  try {
    return String((JSON.parse(raw) as { origin?: unknown }).origin ?? "");
  } catch {
    return "";
  }
}

/**
 * Приём результата регистрации. ПЕРВЫЙ признак — происхождение в отправленных
 * клиентских данных равно происхождению консоли: иначе ключ заведён под чужим
 * происхождением, и это красное, что бы служба ни ответила. ВТОРОЙ — отказ
 * службы `ORIGIN_NOT_ALLOWED`: при своём происхождении это профиль стенда.
 */
async function enrolmentAccepted(accepted: Awaited<ReturnType<typeof answerTo>>, testInfo: TestInfo) {
  const own = originOf(testInfo);
  expect(sentOrigin(accepted), "ключ заведён под чужим происхождением: клиентские данные несут не консоль").toBe(own);
  if (accepted.status() === 200) return;
  const text = await accepted.text();
  if (text.includes("ORIGIN_NOT_ALLOWED")) {
    conditionNotCreated(
      `${FIXTURE_UNMET_PREFIX} ${ACCESS_KEY_LIFECYCLE_CONDITIONS.origins.condition} — нет: служба стенда отвергла ` +
        `происхождение консоли ${own} (ORIGIN_NOT_ALLOWED). Производит условие ` +
        ACCESS_KEY_LIFECYCLE_CONDITIONS.origins.producer,
    );
  }
  expect(accepted.status(), `приём результата регистрации отвергнут: ${text.slice(0, 300)}`).toBe(200);
}

// ─── шаги пути ────────────────────────────────────────────────────────────────

interface Walker {
  page: Page;
  testInfo: TestInfo;
  census: CeremonyCensus;
  userId: string;
}

/** Шаг 1 — заведение ключа экраном настоящей церемонией; возвращает его `id` из перечня. */
async function enrolOnSettings(w: Walker, auth: PageAuthenticator, name: string): Promise<string> {
  const { page, testInfo, userId } = w;
  const s = await openKeys(page);
  await expect(s.empty, "у человека пути уже есть ключи — «Дано» не построено").toBeVisible();

  // Ответы перечня собираются событиями страницы с самого начала: перечитанный
  // после `done` перечень может прийти раньше, чем проба дочитала приём результата.
  const listings: Array<Promise<unknown>> = [];
  const onListing = (r: Response) => {
    if (r.request().method() === "GET" && new URL(r.url()).pathname === keysPath(userId) && r.status() === 200) {
      listings.push(r.json());
    }
  };
  page.on("response", onListing);
  const begun = answerTo(page, "POST", `${keysPath(userId)}:beginRegistration`);
  const finished = answerTo(page, "POST", keysPath(userId));
  await s.name.fill(name);
  await s.description.fill("Сквозной путь");
  await s.add.click();
  const challenge = await begun;
  expect(challenge.status(), `выдача испытания регистрации: ${await challenge.text()}`).toBe(200);
  await rpCondition(challenge, testInfo);
  const accepted = await finished;
  await enrolmentAccepted(accepted, testInfo);
  const op = (await accepted.json()) as { id?: string };
  expect(op.id ?? "", "ответ приёма результата — не Operation").not.toBe("");

  // Перечень, перечитанный после `done`, называет ключ платформенным `id`.
  await expect(s.item(name), "раздел не показал заведённый ключ").toBeVisible({ timeout: 30_000 });
  page.off("response", onListing);
  const read = (await Promise.all(listings)) as Array<{ accessKeys?: Array<{ id?: string; name?: string }> }>;
  const id = read.flatMap((l) => l.accessKeys ?? []).find((k) => k.name === name)?.id ?? "";
  expect(id, "перечень после заведения не называет ключ платформенным id").toMatch(/^ak/);

  const { credentials } = await auth.cdp.send("WebAuthn.getCredentials", { authenticatorId: auth.authenticatorId });
  expect(credentials.length, "в аутентификаторе страницы нет удостоверения — экран его не завёл").toBe(1);
  return id;
}

/**
 * Шаг выхода — с `/settings`, где человек и стоит: экран живёт в каркасе, и
 * панель «Учётная запись» у него та же. Носитель погашен, экран входа.
 */
async function signOut(w: Walker) {
  const { page, census } = w;
  expect(pathOf(page), "выход начат не с экрана параметров").toBe("/settings");
  await page.getByRole("button", { name: "Учётная запись" }).click();
  const [res] = await Promise.all([
    lanePostAnswer(page, LANE.logout),
    page.getByRole("dialog", { name: "Учётная запись" }).getByRole("button", { name: /Выйти$/ }).click(),
  ]);
  expect(res.status(), `выход не прошёл: ${await res.text()}`).toBe(200);
  await expectAddress(page, "/login", "после выхода консоль не вернула на экран входа");
  expect(await sessionHeld(page.context()), "после выхода носитель сессии у браузера остался").toBe(false);
  expect(census.matching("POST", LANE.logout).length).toBeGreaterThan(0);
}

/** Нажать кнопку ключа на экране входа и получить ответ входа — со снятым телом. */
async function pressKey(page: Page, returnTo: string): Promise<LaneAnswer> {
  await page.goto(`/login?returnTo=${encodeURIComponent(returnTo)}`, { waitUntil: "domcontentloaded" });
  const s = loginScreen(page);
  await expect(s.key, "кнопки входа ключом на /login нет").toBeVisible({ timeout: 30_000 });
  const [res] = await Promise.all([lanePostAnswer(page, ACCESS_KEY_LOGIN), s.key.click()]);
  return res;
}

/** Флаги данных аутентификатора из утверждения, как его отправил браузер. */
function sentFlags(res: LaneAnswer): number {
  const sent = JSON.parse(res.request().postData() ?? "null") as {
    credential?: { response?: { authenticatorData?: unknown } };
  } | null;
  // Разбор `base64` в Node принимает оба алфавита, с дополнением и без.
  const data = Buffer.from(String(sent?.credential?.response?.authenticatorData ?? ""), "base64");
  expect(data.length, "утверждение без данных аутентификатора").toBeGreaterThan(FLAGS_OFFSET);
  return data[FLAGS_OFFSET];
}

/** Шаг входа ключом: `200`, уровень — по факту проверки пользователя, уход на адрес возврата. */
async function signInWithKey(w: Walker, userVerification: boolean) {
  const { page } = w;
  const res = await pressKey(page, "/settings");
  expect(res.status(), `вход ключом, заведённым экраном, не прошёл: ${await res.text()}`).toBe(200);

  const flags = sentFlags(res);
  expect(flags & FLAG_UP, "утверждение без флага присутствия пользователя").toBe(FLAG_UP);
  const verified = (flags & FLAG_UV) === FLAG_UV;
  expect(verified, "флаг проверки пользователя — не тот, что даёт аутентификатор сценария").toBe(userVerification);
  const body = (await res.json()) as { session?: { assuranceLevel?: unknown } };
  expect(
    String(body.session?.assuranceLevel ?? ""),
    `уровень сессии при ${verified ? "проверке пользователя (UV)" : "одном присутствии (UP)"}`,
  ).toBe(verified ? "3" : "2");

  await expectAddress(page, "/settings", "после входа ключом консоль не увела на адрес возврата");
  expect(await sessionHeld(page.context()), "после входа ключом у браузера нет носителя сессии").toBe(true);
  expect(w.census.matching("POST", LANE.login).length, "вход ключом прошёл через вход паролем").toBe(0);
}

/** Шаг снятия: «Удалить» → диалог называет ключ → подтверждение; снятие по `id` (ban #15). */
async function revokeOnSettings(w: Walker, name: string, id: string) {
  const { page, userId } = w;
  const s = await keysShown(page);
  await s.item(name).getByRole("button", { name: /Удалить$/ }).click();
  const dialog = page.getByRole("dialog").filter({ hasText: name });
  await expect(dialog, "диалог удаления не назвал ключ").toBeVisible();
  const removed = answerTo(page, "DELETE", `${keysPath(userId)}/${id}`);
  await dialog.getByRole("button", { name: /Удалить$/ }).click();
  const res = await removed;
  expect(res.status(), `снятие ключа отвергнуто: ${await res.text()}`).toBe(200);
  const op = (await res.json()) as { id?: string };
  expect(op.id ?? "", "ответ снятия — не Operation").not.toBe("");
  await expect(s.item(name), "снятый ключ остался в перечне").toHaveCount(0, { timeout: 30_000 });
  await expect(s.empty, "после снятия единственного ключа раздел не сказал «Ключей доступа нет»").toBeVisible();
}

/** Шаг отказа: тот же ключ в аутентификаторе, служба о нём больше не знает. */
async function revokedKeyRefused(w: Walker) {
  const { page } = w;
  const res = await pressKey(page, "/dashboard");
  expect(res.status(), "вход снятым ключом обязан быть отказан 401").toBe(401);
  expect(await res.text(), "тело отказа входа — побайтово одно на все причины").toBe(
    JSON.stringify(AUTHENTICATION_FAILED),
  );
  const s = loginScreen(page);
  await expect(s.refusal, "отказ не назван на экране").toHaveText(AUTHENTICATION_FAILED.message);
  expect(pathOf(page), "после отказа адрес страницы сменился").toBe("/login");
  expect(await sessionHeld(page.context()), "после отказа у браузера появился носитель сессии").toBe(false);
  for (const control of [s.email, s.password, s.submit, s.key]) await expect(control).toBeEnabled();
}

// ─── путь целиком ─────────────────────────────────────────────────────────────

async function walkLifecycle(page: Page, testInfo: TestInfo, scenario: string, userVerification: boolean) {
  test.setTimeout(180_000);
  // Аутентификатор — ДО открытия страницы; ответы глаголов полосы снимаются до страницы.
  const auth = await pageAuthenticator(page, userVerification);
  await captureAnswers(page, FORM_LANE_VERBS);
  const seed = await newSeed(testInfo);
  try {
    await seedConfirmedHuman(seed, seedAddress(scenario));
    const me = await seed.read(SESSION_IDENTITY);
    expect(me.status(), `посев: ответ края о сессии — ${me.status()}`).toBe(200);
    const who = lastIssued(seed, SESSION_IDENTITY).body as { user?: { id?: unknown } } | null;
    const userId = typeof who?.user?.id === "string" ? who.user.id : "";
    expect(userId, "посев: край не назвал человека сессии").not.toBe("");
    await transferSession(seed, page.context());

    const w: Walker = { page, testInfo, census: ceremonyCensus(page.context()), userId };
    const name = `key-${scenario.toLowerCase()}`;
    const id = await enrolOnSettings(w, auth, name);
    await signOut(w);
    await signInWithKey(w, userVerification);
    await revokeOnSettings(w, name, id);
    await signOut(w);
    await revokedKeyRefused(w);

    expect(
      [ACCESS_KEY_BEGIN, ACCESS_KEY_LOGIN].map((p) => w.census.matching("POST", p).length),
      `два входа ключом — два испытания и два предъявления:\n${w.census.describe()}`,
    ).toEqual([2, 2]);
    expectNoProvider(w.census);
  } finally {
    await seed.dispose();
  }
}

// ═══ S4 — путь ключа сквозь консоль ══════════════════════════════════════════

test("F8S4-02 · путь ключа сквозь консоль: завести на /settings → выйти → войти ключом (уровень «3») → удалить → вход удалённым отказан", async ({
  page,
}, testInfo) => {
  // verifies #3059 — F8-50 → F8S4-02 → F8-56 → F8S4-04 одним человеком и одним
  // ключом, заведённым экраном; аутентификатор проверяет пользователя.
  await walkLifecycle(page, testInfo, "F8S4-02-path", true);
});

test("F8S4-03 · путь ключа сквозь консоль без проверки пользователя: вход ключом даёт уровень «2», удалённый отказан", async ({
  page,
}, testInfo) => {
  // verifies #3059 — вариант пути F8S4-02, различие одно: аутентификатор
  // подтверждает только присутствие (F8S4-03).
  await walkLifecycle(page, testInfo, "F8S4-03-path", false);
});
