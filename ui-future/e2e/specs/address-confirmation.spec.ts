// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import {
  expect,
  type APIResponse,
  type Locator,
  type Page,
  type TestInfo,
} from "@playwright/test";
import { parseRpcStatus, reasonOfDetails } from "../../shared/src/api/rpc-status";
import { captureAnswers, LANE_VERBS, lanePostAnswer, type LaneAnswer } from "./answer-on-arrival";
import {
  LANE,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  seedHuman,
  SESSION_COOKIE,
  transferSession,
  type Cookie,
  type Seed,
  type SeededHuman,
} from "./ceremony-seed";
import {
  ceremonyCensus,
  E2E_PASSWORD,
  formatCall,
  register,
  runTag,
  scopeIsReady,
  tenantWithProject,
  test,
  type CeremonyCall,
  type CeremonyCensus,
} from "./fixtures";
import { awaitLetter, conditionNotCreated, stationMailbox, type Letter, type Mailbox } from "./mail-receiver";

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
 * «ДАНО» — ДВА ПОСЕВА, И ОНИ РАЗЛИЧАЮТСЯ ОДНИМ ФАКТОМ (приёмка F6b, Р13)
 *
 * П-н — регистрация глаголом службы своим контекстом (`seedHuman`), код не
 * предъявлен. П-п — П-н и код из письма тем же носителем (`seedConfirmedHuman`).
 * Письмо читается у приёмника писем стенда (`mail-receiver.ts`, условие
 * `mail-receiver-reads.precondition.ts` — F6b-34); письма нет в срок — «условие
 * не создано», и проба уходит в «не выполнилось», а не в красное. Человека,
 * которого сценарий заводит ЭКРАНОМ, ведёт фикстура `register` — её путь держат
 * F6b-32 и F6b-40.
 *
 * Перечень п. 4 приёмки — 24 ID, и все они здесь: F6b-10, F6b-12 … F6b-19,
 * F6b-21 … F6b-30, F6b-32, F6b-33, F6b-36, F6b-40, F6b-41. Предикат —
 * `grep -oE '(test|it)[(]"F6b-[0-9]+' address-confirmation.spec.ts | sort -u`.
 * Компонентные пробы тех же ID (`shared/src/pages/auth/`, `api-client.test.ts`
 * у `host` и `dashboard`) держат исходы, которых на стенде не построить, и
 * браузерную не заменяют.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ГДЕ «ДАНО» ПОСТРОЕНО НЕ БУКВАЛЬНО — И ПОЧЕМУ (вопросы к приёмке, kacho#2922)
 *
 * F6b-19. Приёмка велит посеву выждать N секунд из `Retry-After` отказа по
 * частоте. Сон стенными часами в наборе запрещён держателем F8-45
 * (`wall-clock-census.spec.ts`), а без сна истечение промежутка службы
 * наблюдаемо только ответом глагола, который при истечении ставит письмо.
 * Поэтому промежуток пережидает ЭКРАН: первое нажатие даёт `429` со сроком
 * службы (его исход утверждается как условие), отсчёт экрана открывает кнопку, и
 * проба ждёт это УСЛОВИЕ. Судится второе нажатие — ровно то, о чём «Тогда».
 *
 * F6b-25. Экран подтверждения с подтверждённой сессией уходит на адрес возврата
 * (Р7), поэтому «после перехода обращений сверх перечня Р6 нет» судит обращения,
 * выпущенные документом `/verification`, а не всё, что выпустит вернувшаяся
 * страница.
 *
 * F6b-26. Раздел `vpc` повторяет упавшее чтение (`retry: 1` клиента запросов), и
 * одна подменённая выдача отказа на странице не показывается никогда. Подмена
 * поэтому действует на все чтения списка ДОКУМЕНТА, открывшего список, до его
 * ухода — одно правило для обоих близнецов: у F6b-25 документ уходит после
 * первого чтения, и подменено ровно одно (это утверждается).
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

// ─── посевы, чей носитель и письмо сценарию нужны дальше ─────────────────────

/** П-н, чей контекст посева жив: носитель у посева нужен сценарию после «Дано». */
interface HeldHuman {
  seed: Seed;
  human: SeededHuman;
  mailbox: Mailbox;
  /** Письма на адрес, лежавшие у приёмника ДО регистрации, — не её письма. */
  before: ReadonlySet<string>;
  /** Момент ответа регистрации посева (T0 приёмки) — часами прогона. */
  registeredAt: number;
}

/**
 * Посев П-н, чей контекст ОСТАЁТСЯ у сценария — его закрывает вызывающий.
 * Снимок писем берётся до регистрации: код сценарий берёт только из письма,
 * принятого после неё.
 */
async function heldUnconfirmed(testInfo: TestInfo, scenario: string): Promise<HeldHuman> {
  const mailbox = stationMailbox();
  const email = seedAddress(scenario);
  const before = new Set((await mailbox.letters(email)).map((l) => l.id));
  const seed = await newSeed(testInfo);
  try {
    const human = await seedHuman(seed, email);
    const registeredAt = Date.now();
    const me = await seed.read(SESSION_IDENTITY);
    const body = (await me.json().catch(() => null)) as { session?: { emailVerified?: unknown } } | null;
    expect(
      body?.session?.emailVerified,
      `посев П-н ${email}: ответ края о сессии посева не несёт emailVerified: false — ${JSON.stringify(body)}. ` +
        "Это УСЛОВИЕ сценария, а не его предмет",
    ).toBe(false);
    return { seed, human, mailbox, before, registeredAt };
  } catch (err) {
    await seed.dispose();
    throw err;
  }
}

/** Посев П-п; носитель переносится в браузер, если сценарий его там держит. */
async function seededConfirmed(testInfo: TestInfo, scenario: string, page?: Page): Promise<SeededHuman> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedConfirmedHuman(seed, seedAddress(scenario));
    if (page) await transferSession(seed, page.context());
    return human;
  } finally {
    await seed.dispose();
  }
}

async function disposeAll(seeds: readonly Seed[]): Promise<void> {
  for (const seed of seeds) await seed.dispose();
}

/** Печенье носителя сессии посева — таким, каким его выдала служба. */
async function sessionCookieOf(seed: Seed): Promise<Cookie> {
  const cookie = (await seed.api.storageState()).cookies.find((c) => c.name === SESSION_COOKIE);
  expect(cookie, "посев: носителя сессии у посева нет").toBeTruthy();
  return cookie as Cookie;
}

/** Значение носителя сессии у браузера либо пустая строка. */
async function browserBearer(page: Page): Promise<string> {
  return (await page.context().cookies()).find((c) => c.name === SESSION_COOKIE)?.value ?? "";
}

/** Письма на адрес, принятые приёмником после снимка `before`. */
async function lettersSince(mailbox: Mailbox, email: string, before: ReadonlySet<string>): Promise<Letter[]> {
  return (await mailbox.letters(email)).filter((l) => !before.has(l.id));
}

/** Алфавит кода подтверждения — Крокфорд (Р7 службы). */
const CODE_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/**
 * Значение той же длины и того же алфавита, что код письма, отличное от него в
 * КАЖДОМ значащем знаке: «неверный код» строится, а не выбирается наугад. Живой
 * код у человека один — последний, — и это значение им не является.
 */
function codeNotIssued(code: string): string {
  return [...code]
    .map((ch) => {
      const at = CODE_ALPHABET.indexOf(ch.toUpperCase());
      return at < 0 ? ch : CODE_ALPHABET[(at + 1) % CODE_ALPHABET.length];
    })
    .join("");
}

/** Адреса в теле письма — как их видит человек. */
function addressesIn(text: string): string[] {
  return [...text.matchAll(/https?:\/\/[^\s<>"]+/g)].map((m) => m[0]);
}

/** Происхождение консоли — то, к которому идёт прогон. */
function consoleOrigin(testInfo: TestInfo): string {
  return new URL(String(testInfo.project.use.baseURL)).origin;
}

// ─── ответы края и глаголов: исход, а не вид ─────────────────────────────────

const PROJECTS = "/iam/v1/projects";
const ACCOUNTS = "/iam/v1/accounts";
const NETWORKS = "/vpc/v1/networks";
const CHECK_CODE_TEXT = "Проверьте код или отправьте новое письмо.";
const SESSION_ENDED_TEXT = "session ended; sign in again";

/** Пути, чьи ответы снимаются до страницы: глаголы полосы и запрос письма. */
const ANSWERED_PATHS: readonly string[] = [...LANE_VERBS, VERIFY_EMAIL];

interface EdgeAnswer {
  status: number;
  code: number | null;
  message: string | null;
  reason: string | null;
  domain: unknown;
  headers: ReadonlyArray<{ name: string; value: string }>;
  text: string;
}

/** Ответ края посеву — разобранным ТЕМ ЖЕ разборщиком `google.rpc.Status`, что у экранов. */
async function edgeAnswerOf(res: APIResponse): Promise<EdgeAnswer> {
  const text = await res.text();
  const status = parseRpcStatus(text);
  const info = status?.details.find((d) => d && typeof d === "object" && "reason" in d) as
    | { domain?: unknown }
    | undefined;
  return {
    status: res.status(),
    code: status?.code ?? null,
    message: status?.message ?? null,
    reason: status ? reasonOfDetails(status.details) : null,
    domain: info?.domain,
    headers: res.headersArray(),
    text,
  };
}

function headerValues(a: EdgeAnswer, name: string): string[] {
  return a.headers.filter((h) => h.name.toLowerCase() === name).map((h) => h.value);
}

/** Отказ Р3: `403`, тело службы, без вызова и без печенья. */
async function expectAddressRefusal(res: APIResponse, where: string): Promise<EdgeAnswer> {
  const a = await edgeAnswerOf(res);
  expect(
    {
      status: a.status,
      code: a.code,
      message: a.message,
      reason: a.reason,
      domain: a.domain,
      challenge: headerValues(a, "www-authenticate").length > 0,
      setsCookie: headerValues(a, "set-cookie").length > 0,
    },
    `${where}: ответ края ${a.status} ${a.text.slice(0, 300)}`,
  ).toEqual({
    status: 403,
    code: 7,
    message: "email address is not verified",
    reason: "EMAIL_NOT_VERIFIED",
    domain: "iam.kaname.cloud",
    challenge: false,
    setsCookie: false,
  });
  return a;
}

/** Отказ F4d-22: `401`, текст отсечки, вызов края и ГАСЯЩЕЕ печенье носителя. */
async function expectSessionEnded(res: APIResponse, where: string): Promise<void> {
  const a = await edgeAnswerOf(res);
  const carrierEnded = headerValues(a, "set-cookie").some(
    (v) => v.startsWith(`${SESSION_COOKIE}=;`) && /max-age=0/i.test(v),
  );
  expect(
    {
      status: a.status,
      code: a.code,
      message: a.message,
      challenge: headerValues(a, "www-authenticate").join(" · "),
      carrierEnded,
    },
    `${where}: ответ края ${a.status} ${a.text.slice(0, 300)}; set-cookie: ${headerValues(a, "set-cookie").join(" | ")}`,
  ).toEqual({
    status: 401,
    code: 16,
    message: SESSION_ENDED_TEXT,
    challenge: `Bearer error="invalid_token", error_description="${SESSION_ENDED_TEXT}"`,
    carrierEnded: true,
  });
}

/** Ответ глагола, снятый до страницы, — статус, код, текст и причина. */
async function laneOutcomeOf(answer: LaneAnswer): Promise<{
  status: number;
  code: number | null;
  message: string | null;
  reason: string | null;
  text: string;
}> {
  const text = await answer.text();
  const status = parseRpcStatus(text);
  return {
    status: answer.status(),
    code: status?.code ?? null,
    message: status?.message ?? null,
    reason: status ? reasonOfDetails(status.details) : null,
    text,
  };
}

/** Тело разобранным JSON либо как есть: утверждение назовёт то, что пришло, а не отказ разбора. */
function parsedOrText(text: string | null): unknown {
  if (text === null) return null;
  try {
    return JSON.parse(text) as unknown;
  } catch (_notJson) {
    return text;
  }
}

function emailVerifiedOf(body: unknown): unknown {
  return (body as { session?: { emailVerified?: unknown } } | null)?.session?.emailVerified;
}

// ─── что видела вкладка ───────────────────────────────────────────────────────

/** Каркас консоли — рейл оболочки, доступным именем. */
function shellOf(page: Page): Locator {
  return page.getByRole("navigation", { name: "Host navigation" });
}

async function expectShell(page: Page, why: string): Promise<void> {
  await expect(shellOf(page), why).toBeVisible({ timeout: 30_000 });
}

/**
 * Ответы «кто я», которые получила страница, — с телом, в порядке выпуска.
 * Тело снимается с документа, который остался: у ушедшего его может не быть, и
 * такой ответ записывается без тела, а не выбрасывается.
 */
function sessionAnswers(page: Page) {
  const answers: Array<{ at: number; status: number; body: unknown }> = [];
  let issued = 0;
  page.on("response", (res) => {
    if (res.request().method() !== "GET" || new URL(res.url()).pathname !== SESSION_IDENTITY) return;
    const at = issued++;
    res.text().then(
      (text) => answers.push({ at, status: res.status(), body: parsedOrText(text) }),
      () => answers.push({ at, status: res.status(), body: undefined }),
    );
  });
  return {
    /** Число ответов, полученных до этого места. */
    mark: () => issued,
    since: (mark: number) => answers.filter((a) => a.at >= mark).sort((x, y) => x.at - y.at),
  };
}

/** Адреса главного документа по порядку — и переходы документом, и переходы страницы. */
function visitedAddresses(page: Page): string[] {
  const visited: string[] = [];
  page.on("framenavigated", (f) => {
    if (f !== page.mainFrame()) return;
    const u = new URL(f.url());
    visited.push(`${u.pathname}${u.search}`);
  });
  return visited;
}

/** Переходы документа в переписи, начиная с `from`. */
function documentsSince(census: CeremonyCensus, from: number): CeremonyCall[] {
  return census.calls.slice(from).filter((c) => c.kind === "документ");
}

async function signIn(page: Page, human: SeededHuman): Promise<void> {
  await page.getByRole("textbox", { name: "Адрес электронной почты" }).fill(human.email);
  await page.getByLabel("Пароль", { exact: true }).fill(human.password);
  await page.getByRole("button", { name: /Войти$/ }).click();
}

// ═══ S1 — край: подтверждение действует сразу ═════════════════════════════════

test("F6b-10 · подтверждение действует без нового входа — новым носителем из ответа подтверждения", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #2922 — у сценария не было пробы: носитель из ответа подтверждения на стенде не судился.
  const held = await heldUnconfirmed(testInfo, "F6b-10");
  const seeds: Seed[] = [held.seed];
  try {
    const b1 = await sessionCookieOf(held.seed);
    // Близнец — то же обращение ДО подтверждения: отказ Р3.
    await expectAddressRefusal(await held.seed.read(PROJECTS), "носитель B1 до подтверждения");

    const letter = await awaitLetter(held.mailbox, held.human.email, held.before);
    const from = held.seed.issued.length;
    const confirmed = await held.seed.submit(LANE.verifyEmailConfirm, "verify-email-confirm", { code: letter.code });
    const confirmedBody = held.seed.issued[held.seed.issued.length - 1].body;
    const carrier = confirmed
      .headersArray()
      .filter((h) => h.name.toLowerCase() === "set-cookie")
      .map((h) => h.value)
      .find((v) => v.startsWith(`${SESSION_COOKIE}=`));
    expect(
      {
        status: confirmed.status(),
        emailVerified: emailVerifiedOf(confirmedBody),
        newCarrier: carrier !== undefined && !carrier.startsWith(`${SESSION_COOKIE}=;`),
      },
      `ответ подтверждения кодом письма ${letter.id}: ${JSON.stringify(confirmedBody)}; set-cookie: ${carrier ?? "(нет)"}`,
    ).toEqual({ status: 200, emailVerified: true, newCarrier: true });
    const b2 = await held.seed.sessionBearer();
    expect(b2 !== "" && b2 !== b1.value, "подтверждение прошло, а носитель у посева прежний").toBe(true);

    const after = await edgeAnswerOf(await held.seed.read(PROJECTS));
    expect(
      { status: after.status, addressRefused: after.reason === "EMAIL_NOT_VERIFIED" },
      `носитель B2 после подтверждения: ${after.status} ${after.text.slice(0, 300)}`,
    ).toEqual({ status: 200, addressRefused: false });
    expect(
      held.seed.issued
        .slice(from)
        .filter((c) => c.method === "POST" && c.path === LANE.login)
        .map((c) => `${c.method} ${c.path} → ${c.status}`),
      "между двумя обращениями посев входил заново",
    ).toEqual([]);

    // Прежний носитель B1 больше не годен (Р10 службы): отказ F4d-22.
    const stale = await newSeed(testInfo, [b1]);
    seeds.push(stale);
    await expectSessionEnded(await stale.read(PROJECTS), "прежний носитель B1 после подтверждения");
    console.log(`[F6b-10] B1 → Р3, код письма ${letter.id} → 200 с новым носителем, B2 → ${after.status}, B1 → F4d-22`);
  } finally {
    await disposeAll(seeds);
  }
});

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

test("F6b-16 · подтверждённая сессия на тех же адресах получает то же, что до этой под-фазы", async ({
  page,
}, testInfo) => {
  // verifies #2922 — у близнеца F6b-15 не было браузерной пары: подтверждённую сессию судила только компонентная проба.
  test.setTimeout(150_000);
  await seededConfirmed(testInfo, "F6b-16", page);
  const census = ceremonyCensus(page.context());
  const visited = visitedAddresses(page);
  for (const address of CONSOLE_ADDRESSES) {
    const from = census.calls.length;
    const seen = visited.length;
    await page.goto(address, { waitUntil: "domcontentloaded" });
    if (address === "/recovery") {
      // Страница неведомого адреса стоит за стражем: она отрисована — страж пропустил.
      await expect(
        page.getByRole("heading", { name: "Такого адреса здесь нет" }),
        "адрес /recovery у подтверждённой сессии не ответил названной страницей",
      ).toBeVisible({ timeout: 30_000 });
      expect(addressOf(page), "названная страница увела со своего адреса").toBe("/recovery");
    } else {
      // Каркас рисуется только после ответа стража «подтверждён».
      await expectShell(page, `адрес ${address} у подтверждённой сессии не отрисован в каркасе`);
      if (address === "/такого-адреса-нет") {
        await expectAddress(page, "/dashboard", "неведомый адрес у подтверждённой сессии не увёл на /dashboard");
      }
    }
    const detour = [
      ...visited.slice(seen).filter((a) => a.startsWith("/verification")),
      ...documentsSince(census, from)
        .filter((c) => c.path === "/verification")
        .map(formatCall),
    ];
    console.log(
      `[F6b-16] ${address}: обращений ${census.calls.length - from}, адреса вкладки ${visited.slice(seen).join(" → ")}`,
    );
    expect(detour, `адрес ${address} у подтверждённой сессии привёл на экран подтверждения:\n${census.describe()}`).toEqual(
      [],
    );
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

test("F6b-14 · вход подтверждённого ведёт на адрес возврата, в обычную консоль", async ({ page }, testInfo) => {
  // verifies #2922 — у близнеца F6b-13 не было пары: вход подтверждённого не судила ни одна проба.
  test.setTimeout(120_000);
  // Носитель — у посева, не у браузера: вход проходит экраном.
  const human = await seededConfirmed(testInfo, "F6b-14");
  const census = ceremonyCensus(page.context());
  await page.goto("/login?returnTo=/settings", { waitUntil: "domcontentloaded" });
  await signIn(page, human);
  await expectAddress(page, "/settings", "вход подтверждённого не увёл на адрес возврата");
  await expectShell(page, "после входа подтверждённого каркас с рейлом не отрисован");
  await expect
    .poll(() => census.matching("GET", ACCOUNTS).length, {
      message: `перепись не содержит обращения каркаса GET ${ACCOUNTS} — того, которого у F6b-13 нет:\n${census.describe()}`,
      timeout: 30_000,
    })
    .toBeGreaterThan(0);
  expect(
    documentsSince(census, 0)
      .filter((c) => c.path === "/verification")
      .map(formatCall),
    `вход подтверждённого прошёл через экран подтверждения:\n${census.describe()}`,
  ).toEqual([]);
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

// ═══ S2 — запрос письма с экрана ══════════════════════════════════════════════

function sentText(email: string): string {
  return `Письмо с новым кодом отправлено на ${email}. Прежний код больше не действует.`;
}

function waitText(seconds: number): string {
  return `Отправить новое письмо можно через ${seconds} с`;
}

/** Срок из `Retry-After` ответа — целым числом секунд, иначе `null`. */
function retryAfterOf(answer: LaneAnswer): number | null {
  const raw = answer.headers()["retry-after"] ?? "";
  return /^\d+$/.test(raw) ? Number(raw) : null;
}

/** Открыть `/verification` носителем П-н, перенесённым в браузер; перепись — с открытия. */
async function openVerification(page: Page, address = "/verification") {
  await captureAnswers(page, ANSWERED_PATHS);
  const census = ceremonyCensus(page.context());
  const s = verificationScreen(page);
  await page.goto(address, { waitUntil: "domcontentloaded" });
  await expectVerificationSettled(census, 0, s.heading);
  return { census, s };
}

test("F6b-19 · новое письмо после промежутка: запрос, текст, отсчёт из ответа службы, прежний код вытеснен", async ({
  page,
}, testInfo) => {
  // verifies #2922 — у сценария была только компонентная проба: ответ службы, письмо и вытеснение кода не судились.
  test.setTimeout(240_000);
  const held = await heldUnconfirmed(testInfo, "F6b-19");
  try {
    await transferSession(held.seed, page.context());
  } finally {
    await held.seed.dispose();
  }
  const k1 = await awaitLetter(held.mailbox, held.human.email, held.before);
  const { census, s } = await openVerification(page);

  // ДАНО: промежуток, открытый письмом регистрации, истёк. Его срок называет
  // служба отказом по частоте; исход этой пробы — УСЛОВИЕ, и иной ответ — «не
  // выполнилось» (приёмка: `200` сам поставил бы письмо и открыл новый
  // промежуток). Пережидает срок отсчёт экрана, а проба ждёт, когда он откроет
  // кнопку, — см. шапку файла.
  const probe = lanePostAnswer(page, VERIFY_EMAIL);
  await s.resend.click();
  const refused = await probe;
  const interval = retryAfterOf(refused);
  if (refused.status() !== 429 || interval === null || interval < 1) {
    const text = await refused.text().catch(() => "(тела нет)");
    conditionNotCreated(
      `условие не создано: проба промежутка получила ${refused.status()} ` +
        `Retry-After=${refused.headers()["retry-after"] ?? "(нет)"} ${text.slice(0, 200)}`,
    );
  }
  console.log(`[F6b-19] промежуток службы после письма регистрации — ${interval} с; ждём, пока отсчёт экрана откроет кнопку`);
  await expect(s.resend, "после отказа по частоте кнопка отправки открыта").toBeDisabled();
  await expect(s.resend, `отсчёт экрана не открыл кнопку за срок службы ${interval} с`).toBeEnabled({
    timeout: ((interval ?? 0) + 15) * 1000,
  });

  // КОГДА: человек нажимает «Отправить новое письмо».
  const from = census.calls.length;
  const answered = lanePostAnswer(page, VERIFY_EMAIL);
  await s.resend.click();
  const answer = await answered;
  const text = await answer.text();
  const next = retryAfterOf(answer);
  expect(
    { status: answer.status(), body: parsedOrText(text), retryAfterAtLeastOne: next !== null && next >= 1 },
    `ответ POST ${VERIFY_EMAIL}: ${answer.status()} Retry-After=${answer.headers()["retry-after"] ?? "(нет)"} ${text}`,
  ).toEqual({ status: 200, body: {}, retryAfterAtLeastOne: true });
  const posted = answer.request().postData();
  expect(
    parsedOrText(posted),
    `тело POST ${VERIFY_EMAIL} — только признак формы, без адреса: ${posted}`,
  ).toEqual({ csrfToken: expect.any(String) });
  expect(
    census.matching("GET", LANE.csrf, "?form=verify-email").length,
    `перепись не содержит GET ${LANE.csrf}?form=verify-email:\n${census.describe()}`,
  ).toBeGreaterThan(0);
  await expect(page.getByText(sentText(held.human.email))).toBeVisible();
  await expect(page.getByText(waitText(next ?? 0)), "отсчёт не взят из Retry-After ответа").toBeVisible();

  // Кнопка закрыта, и клавиша ввода второго письма не выпускает — сразу после нажатия.
  await expect(s.resend).toBeDisabled();
  await s.resend.focus();
  await page.keyboard.press("Enter");
  await page.evaluate(() => document.readyState);
  expect(
    census.calls
      .slice(from)
      .filter((c) => c.method === "POST" && c.path === VERIFY_EMAIL)
      .map(formatCall),
    `за нажатием и клавишей ввода ждали ровно один POST ${VERIFY_EMAIL}:\n${census.describe()}`,
  ).toHaveLength(1);

  // Письмо с новым кодом пришло, и прежний код больше не действует.
  const k2 = await awaitLetter(held.mailbox, held.human.email, new Set([...held.before, k1.id]));
  expect(k2.code, `письмо ${k2.id} после нажатия несёт прежний код`).not.toBe(k1.code);
  await s.code.fill(k1.code);
  const stale = lanePostAnswer(page, VERIFY_EMAIL_CONFIRM);
  await s.confirm.click();
  const outcome = await laneOutcomeOf(await stale);
  expect(
    { status: outcome.status, code: outcome.code },
    `прежний код K1 после нового письма: ${outcome.status} ${outcome.text.slice(0, 200)}`,
  ).toEqual({ status: 401, code: 16 });
});

test("F6b-21 · новое письмо сразу после регистрации: отказ по частоте, текст службы дословно и её срок", async ({
  page,
}, testInfo) => {
  // verifies #2922 — у сценария была только компонентная проба: промежуток, открытый регистрацией, не судился.
  test.setTimeout(120_000);
  const held = await heldUnconfirmed(testInfo, "F6b-21");
  try {
    await transferSession(held.seed, page.context());
  } finally {
    await held.seed.dispose();
  }
  const { s } = await openVerification(page);

  const pressedAt = Date.now();
  const answered = lanePostAnswer(page, VERIFY_EMAIL);
  await s.resend.click();
  const answer = await answered;
  const outcome = await laneOutcomeOf(answer);
  const elapsed = pressedAt - held.registeredAt;
  console.log(
    `[F6b-21] T0 ответ регистрации ${new Date(held.registeredAt).toISOString()} · ` +
      `T1 нажатие ${new Date(pressedAt).toISOString()} · T1−T0 ${elapsed} мс · ответ ${outcome.status}`,
  );
  // Промежуток называет сама служба: `Retry-After` успеха — срок, который она
  // открыла этим письмом. Истёк он до нажатия — сценарий не построен.
  const interval = retryAfterOf(answer);
  if (answer.status() === 200 && interval !== null && elapsed >= interval * 1000) {
    conditionNotCreated(
      `условие не создано: промежуток истёк до нажатия — T0 ${new Date(held.registeredAt).toISOString()}, ` +
        `T1 ${new Date(pressedAt).toISOString()}, промежуток службы ${interval} с`,
    );
  }
  expect(
    {
      status: outcome.status,
      code: outcome.code,
      reason: outcome.reason,
      retryAfterAtLeastOne: interval !== null && interval >= 1,
    },
    `ответ POST ${VERIFY_EMAIL} через ${elapsed} мс после регистрации: ${outcome.status} ${outcome.text.slice(0, 200)}`,
  ).toEqual({ status: 429, code: 8, reason: "TOO_MANY_ATTEMPTS", retryAfterAtLeastOne: true });
  await expect(page.getByRole("alert"), "текст отказа службы не показан дословно").toContainText(
    outcome.message ?? "(текста нет)",
  );
  await expect(page.getByText(waitText(interval ?? 0)), "отсчёт не взят из Retry-After отказа").toBeVisible();
  await expect(s.resend).toBeDisabled();
  await expect(page.getByText(/Письмо с новым кодом отправлено/)).toHaveCount(0);

  // Письмо регистрации — ровно одно, и отказ нового не поставил.
  const registration = await awaitLetter(held.mailbox, held.human.email, held.before);
  expect(
    (await lettersSince(held.mailbox, held.human.email, held.before)).map((l) => `${l.id} · ${l.created}`),
    "у приёмника сверх письма регистрации есть письмо после отказа по частоте",
  ).toEqual([`${registration.id} · ${registration.created}`]);
});

// ═══ S2 — предъявление кода ═══════════════════════════════════════════════════

test("F6b-23 · код из письма, введённый на экране, ведёт на адрес возврата в обычную консоль", async ({
  page,
}, testInfo) => {
  // verifies #2922 — у сценария была только компонентная проба: новый носитель и каркас после кода не судились.
  test.setTimeout(120_000);
  const held = await heldUnconfirmed(testInfo, "F6b-23");
  try {
    await transferSession(held.seed, page.context());
  } finally {
    await held.seed.dispose();
  }
  const letter = await awaitLetter(held.mailbox, held.human.email, held.before);
  const sessions = sessionAnswers(page);
  const visited = visitedAddresses(page);
  const { census, s } = await openVerification(page, "/verification?returnTo=%2Fdashboard");
  const bearerBefore = await browserBearer(page);

  await s.code.fill(letter.code);
  const mark = sessions.mark();
  const seen = visited.length;
  const answered = lanePostAnswer(page, VERIFY_EMAIL_CONFIRM);
  await s.confirm.click();
  const answer = await answered;
  const body = (await answer.json().catch(() => null)) as unknown;
  const setCookie = (await (await answer.request().response())?.headerValue("set-cookie")) ?? "";
  expect(
    {
      status: answer.status(),
      emailVerified: emailVerifiedOf(body),
      newCarrier: setCookie.includes(`${SESSION_COOKIE}=`) && !setCookie.includes(`${SESSION_COOKIE}=;`),
    },
    `ответ POST ${VERIFY_EMAIL_CONFIRM}: ${JSON.stringify(body)}; set-cookie: ${setCookie}`,
  ).toEqual({ status: 200, emailVerified: true, newCarrier: true });
  expect(
    parsedOrText(answer.request().postData()),
    "тело предъявления — код как введён и признак формы",
  ).toEqual({ code: letter.code, csrfToken: expect.any(String) });
  expect(
    census.matching("GET", LANE.csrf, "?form=verify-email-confirm").length,
    `перепись не содержит GET ${LANE.csrf}?form=verify-email-confirm:\n${census.describe()}`,
  ).toBeGreaterThan(0);

  await expectAddress(page, "/dashboard", "код из письма не увёл на адрес возврата");
  await expectShell(page, "после подтверждения каркас не отрисован");
  expect(await browserBearer(page), "носитель браузера после подтверждения прежний").not.toBe(bearerBefore);
  await expect
    .poll(() => sessions.since(mark).some((a) => emailVerifiedOf(a.body) === true), {
      message: "новый ответ «кто я» после подтверждения не несёт emailVerified: true",
      timeout: 30_000,
    })
    .toBe(true);
  expect(
    visited.slice(seen).filter((a) => a.startsWith("/login")),
    `между нажатием и каркасом была форма входа: ${visited.slice(seen).join(" → ")}`,
  ).toEqual([]);
});

test("F6b-24 · неверный код оставляет на экране, текст службы дословно", async ({ page }, testInfo) => {
  // verifies #2922 — у близнеца F6b-23 была только компонентная проба: отказ службы на стенде не судился.
  test.setTimeout(120_000);
  const held = await heldUnconfirmed(testInfo, "F6b-24");
  try {
    await transferSession(held.seed, page.context());
  } finally {
    await held.seed.dispose();
  }
  const letter = await awaitLetter(held.mailbox, held.human.email, held.before);
  const sessions = sessionAnswers(page);
  const opened = "/verification?returnTo=%2Fdashboard";
  const { census, s } = await openVerification(page, opened);

  await s.code.fill(codeNotIssued(letter.code));
  const from = census.calls.length;
  const mark = sessions.mark();
  const answered = lanePostAnswer(page, VERIFY_EMAIL_CONFIRM);
  await s.confirm.click();
  const outcome = await laneOutcomeOf(await answered);
  expect(
    { status: outcome.status, code: outcome.code },
    `ответ на значение, которого служба не выдавала: ${outcome.status} ${outcome.text.slice(0, 200)}`,
  ).toEqual({ status: 401, code: 16 });
  const alert = page.getByRole("alert");
  await expect(alert, "текст службы не показан дословно").toContainText(outcome.message ?? "(текста нет)");
  await expect(alert).toContainText(CHECK_CODE_TEXT);

  // После ответа — ОДИН вопрос «кто я», и сессия в нём есть: экран остался.
  await expect
    .poll(() => sessions.since(mark).length, { message: "после 401 экран не спросил «кто я»", timeout: 15_000 })
    .toBeGreaterThan(0);
  const asked = census.calls.slice(from).filter((c) => c.method === "GET" && c.path === SESSION_IDENTITY);
  expect(asked.map(formatCall), `после отказа ждали один новый GET ${SESSION_IDENTITY}:\n${census.describe()}`).toHaveLength(
    1,
  );
  expect(
    (sessions.since(mark)[0]?.body as { user?: unknown } | null)?.user ?? null,
    "в ответе «кто я» после отказа сессии нет",
  ).not.toBeNull();
  expect(addressOf(page), "неверный код увёл с экрана").toBe(opened);

  await page.goto("/dashboard", { waitUntil: "domcontentloaded" });
  await expectAddress(page, "/verification?returnTo=%2Fdashboard", "после неверного кода /dashboard открылся мимо экрана");
});

test("F6b-29 · код другого человека в сессии этого браузера не подтверждает никого", async ({
  page,
}, testInfo) => {
  // verifies #2922 — привязка кода к человеку на стенде не судилась ни одной пробой.
  test.setTimeout(180_000);
  const one = await heldUnconfirmed(testInfo, "F6b-29-one");
  const seeds: Seed[] = [one.seed];
  try {
    const two = await heldUnconfirmed(testInfo, "F6b-29-two");
    seeds.push(two.seed);
    await transferSession(two.seed, page.context());
    const letterOne = await awaitLetter(one.mailbox, one.human.email, one.before);
    const letterTwo = await awaitLetter(two.mailbox, two.human.email, two.before);
    const sessions = sessionAnswers(page);
    const { s } = await openVerification(page);

    await s.code.fill(letterOne.code);
    const mark = sessions.mark();
    const answered = lanePostAnswer(page, VERIFY_EMAIL_CONFIRM);
    await s.confirm.click();
    const outcome = await laneOutcomeOf(await answered);
    expect(
      { status: outcome.status, code: outcome.code },
      `код П-н1 в сессии П-н2: ${outcome.status} ${outcome.text.slice(0, 200)}`,
    ).toEqual({ status: 401, code: 16 });
    await expect(page.getByRole("alert"), "текст службы не показан дословно").toContainText(
      outcome.message ?? "(текста нет)",
    );
    await expect
      .poll(
        () => {
          const last = sessions.since(mark).at(-1)?.body as {
            user?: { email?: unknown } | null;
            session?: { emailVerified?: unknown };
          } | null;
          return JSON.stringify({ email: last?.user?.email, emailVerified: last?.session?.emailVerified });
        },
        { message: "ответ «кто я» браузера после чужого кода", timeout: 15_000 },
      )
      .toBe(JSON.stringify({ email: two.human.email, emailVerified: false }));
    expect(addressOf(page), "чужой код увёл с экрана").toBe("/verification");
    await expectAddressRefusal(await one.seed.read(PROJECTS), "носитель посева П-н1 после предъявления его кода чужой сессией");

    // Близнец — код из письма П-н2 в том же браузере: подтверждён и каркас (F6b-23).
    await s.code.fill(letterTwo.code);
    const own = lanePostAnswer(page, VERIFY_EMAIL_CONFIRM);
    await s.confirm.click();
    expect((await own).status(), "свой код П-н2 в своей сессии не принят").toBe(200);
    await expectAddress(page, "/dashboard", "свой код не увёл в консоль");
    await expectShell(page, "после своего кода каркас не отрисован");
  } finally {
    await disposeAll(seeds);
  }
});

test("F6b-30 · сессия снята подтверждением в другом месте: экран уводит на вход", async ({ page }, testInfo) => {
  // verifies #2922 — у близнеца F6b-24 была только компонентная проба: снятие сессии подтверждением не судилось.
  test.setTimeout(240_000);
  await captureAnswers(page, ANSWERED_PATHS);
  const census = ceremonyCensus(page.context());
  const s = verificationScreen(page);
  const seeds: Seed[] = [];
  try {
    for (const verb of [VERIFY_EMAIL_CONFIRM, VERIFY_EMAIL]) {
      const which = verb === VERIFY_EMAIL_CONFIRM ? "а · «Подтвердить»" : "б · «Отправить новое письмо»";
      await page.context().clearCookies();
      // Дано: сессия S1 у посева, S2 — вход браузера; S1 предъявила код.
      const held = await heldUnconfirmed(testInfo, verb === VERIFY_EMAIL_CONFIRM ? "F6b-30-a" : "F6b-30-b");
      seeds.push(held.seed);
      const letter = await awaitLetter(held.mailbox, held.human.email, held.before);
      const opened = census.calls.length;
      await page.goto("/login", { waitUntil: "domcontentloaded" });
      await signIn(page, held.human);
      await expectAddress(page, "/verification", `(${which}) вход П-н не привёл на экран подтверждения`);
      await expectVerificationSettled(census, opened, s.heading);
      const confirmed = await held.seed.submit(LANE.verifyEmailConfirm, "verify-email-confirm", { code: letter.code });
      expect(
        confirmed.status(),
        `(${which}) посев: код из письма в S1 не принят — ${confirmed.status()} ${await confirmed.text()}. ` +
          "Это УСЛОВИЕ сценария",
      ).toBe(200);

      // Когда: человек в браузере действует на экране снятой сессии.
      const from = census.calls.length;
      const answered = lanePostAnswer(page, verb);
      if (verb === VERIFY_EMAIL_CONFIRM) {
        await s.code.fill(codeNotIssued(letter.code));
        await s.confirm.click();
      } else {
        await s.resend.click();
      }
      const outcome = await laneOutcomeOf(await answered);
      expect(
        { status: outcome.status, code: outcome.code },
        `(${which}) ответ глагола снятой сессии: ${outcome.status} ${outcome.text.slice(0, 200)}`,
      ).toEqual({ status: 401, code: 16 });
      await expectAddress(page, "/login", `(${which}) экран снятой сессии не увёл на вход`);
      // Экран уходит на вход только по ответу «сессии нет» — последний «кто я» до ухода отвечен 200.
      const after = census.calls.slice(from);
      const leftAt = after.findIndex((c) => c.kind === "документ" && c.path === "/login");
      const asked = after.slice(0, leftAt < 0 ? after.length : leftAt).filter(
        (c) => c.method === "GET" && c.path === SESSION_IDENTITY,
      );
      expect(
        asked.at(-1)?.outcome,
        `(${which}) после 401 «кто я» не ответил «сессии нет»:\n${census.describe()}`,
      ).toBe(200);
      const me = await page.request.get(SESSION_IDENTITY);
      expect(await me.json().catch(() => null), `(${which}) у браузера после ухода осталась сессия`).toEqual({
        user: null,
      });

      // Вход тем же человеком ведёт в каркас: адрес подтверждён в S1.
      const beforeSignIn = census.calls.length;
      await signIn(page, held.human);
      await expectAddress(page, "/dashboard", `(${which}) вход после подтверждения в другом месте не увёл в консоль`);
      await expectShell(page, `(${which}) после входа каркас не отрисован`);
      expect(
        documentsSince(census, beforeSignIn)
          .filter((c) => c.path === "/verification")
          .map(formatCall),
        `(${which}) вход подтверждённого прошёл через экран подтверждения`,
      ).toEqual([]);
    }
  } finally {
    await disposeAll(seeds);
  }
});

/** Чужие адреса возврата F6b-36 — четыре формы, каждую отвергает `safeInternalPath`. */
const FOREIGN_RETURN_TO = [
  "https://evil.example/dashboard",
  "//evil.example/dashboard",
  "/\\evil.example/dashboard",
  "javascript:alert(1)",
];

test("F6b-36 · адрес возврата чужого происхождения на /verification отвергнут: после подтверждения — корень консоли", async ({
  page,
}, testInfo) => {
  // verifies #2922 — у сценария была только компонентная проба: уход документа на стенде не судился.
  test.setTimeout(300_000);
  const origin = consoleOrigin(testInfo);
  await captureAnswers(page, ANSWERED_PATHS);
  const census = ceremonyCensus(page.context());
  const s = verificationScreen(page);
  // Близнец — свой адрес `/dashboard` (F6b-23): отрицание не тождественно.
  for (const [i, returnTo] of [...FOREIGN_RETURN_TO, "/dashboard"].entries()) {
    const own = returnTo === "/dashboard";
    await page.context().clearCookies();
    const held = await heldUnconfirmed(testInfo, `F6b-36-${i}`);
    try {
      await transferSession(held.seed, page.context());
    } finally {
      await held.seed.dispose();
    }
    const letter = await awaitLetter(held.mailbox, held.human.email, held.before);
    const opened = census.calls.length;
    await page.goto(`/verification?${new URLSearchParams({ returnTo }).toString()}`, {
      waitUntil: "domcontentloaded",
    });
    await expectVerificationSettled(census, opened, s.heading);
    await s.code.fill(letter.code);
    const at = census.calls.length;
    const answered = lanePostAnswer(page, VERIFY_EMAIL_CONFIRM);
    await s.confirm.click();
    expect((await answered).status(), `returnTo=${returnTo}: код из письма не принят`).toBe(200);
    await expectShell(page, `returnTo=${returnTo}: после подтверждения каркас не отрисован`);
    const left = documentsSince(census, at)[0];
    expect(
      {
        document: left ? `${left.origin}${left.path}${left.query}` : "(перехода документа нет)",
        origin: new URL(page.url()).origin,
      },
      `returnTo=${returnTo}: куда ушёл документ после подтверждения:\n${census.describe()}`,
    ).toEqual({ document: `${origin}${own ? "/dashboard" : "/"}`, origin });
  }
});

// ═══ S2 — отказ края на обращении платформы ═══════════════════════════════════

/** Ответ края, снятый посевом, — байты тела, статус и заголовки, кроме транспортных. */
interface CapturedAnswer {
  status: number;
  headers: Record<string, string>;
  body: Buffer;
}

/** Заголовки, которые описывают доставку, а не ответ: подставляющий ставит их сам. */
const TRANSPORT_HEADERS = new Set(["content-length", "content-encoding", "transfer-encoding", "connection", "date"]);

async function capturedAnswerOf(res: APIResponse): Promise<CapturedAnswer> {
  const headers: Record<string, string> = {};
  for (const h of res.headersArray()) {
    const name = h.name.toLowerCase();
    if (TRANSPORT_HEADERS.has(name)) continue;
    headers[name] = headers[name] === undefined ? h.value : `${headers[name]}, ${h.value}`;
  }
  return { status: res.status(), headers, body: await res.body() };
}

/**
 * Подменить снятыми байтами чтения списка сетей, которые выпустит ДОКУМЕНТ,
 * открывший список, — до его ухода (см. шапку файла, F6b-26). Уход документа —
 * переход главного кадра после первой подмены; дальше чтения идут к краю.
 */
async function substituteNetworkReads(page: Page, answer: CapturedAnswer): Promise<{ substituted: string[] }> {
  const substituted: string[] = [];
  let armed = true;
  page.context().on("request", (r) => {
    if (armed && substituted.length > 0 && r.isNavigationRequest() && r.frame() === page.mainFrame()) armed = false;
  });
  await page.route(
    (url) => url.pathname === NETWORKS,
    async (route) => {
      if (!armed || route.request().method() !== "GET") return route.fallback();
      substituted.push(route.request().url());
      await route.fulfill({ status: answer.status, headers: answer.headers, body: answer.body });
    },
  );
  return { substituted };
}

/**
 * Обращения к API по ДОКУМЕНТУ, который их выпустил: адрес кадра в момент
 * выпуска. Обращение уходящего документа, выпущенное после начала перехода,
 * несёт его адрес, а не адрес нового.
 */
function apiCallsByDocument(page: Page): Array<{ document: string; method: string; path: string }> {
  const calls: Array<{ document: string; method: string; path: string }> = [];
  page.context().on("request", (r) => {
    if (r.isNavigationRequest()) return;
    let path = "";
    let document = "";
    try {
      path = new URL(r.url()).pathname;
      document = new URL(r.frame().url()).pathname;
    } catch (_notAFrameRequest) {
      return;
    }
    if (API_PATH.some((re) => re.test(path))) calls.push({ document, method: r.method(), path });
  });
  return calls;
}

test("F6b-25 · отказ края EMAIL_NOT_VERIFIED на обращении платформы ведёт на экран подтверждения", async ({
  page,
}, testInfo) => {
  // verifies #2922 — у сценария были только компонентные пробы: уход по НАСТОЯЩИМ байтам края не судился.
  test.setTimeout(300_000);
  // Байты отказа Р3 сняты с настоящего края носителем П-н.
  const held = await heldUnconfirmed(testInfo, "F6b-25-refused");
  let refusal: CapturedAnswer;
  try {
    const res = await held.seed.read(PROJECTS);
    await expectAddressRefusal(res, "снятие байтов: носитель П-н на GET /iam/v1/projects");
    refusal = await capturedAnswerOf(res);
  } finally {
    await held.seed.dispose();
  }
  // П-п в браузере, список сетей его проекта.
  const { projectId } = await tenantWithProject(page);
  await scopeIsReady(page, projectId);
  const list = `/projects/${projectId}/vpc/networks`;
  const census = ceremonyCensus(page.context());
  const byDocument = apiCallsByDocument(page);
  const swap = await substituteNetworkReads(page, refusal);
  const from = census.calls.length;
  await page.goto(list, { waitUntil: "domcontentloaded" });

  const expected = verificationAddressFor(list);
  await expect
    .poll(() => documentsSince(census, from).some((c) => `${c.path}${c.query}` === expected), {
      message: `отказ EMAIL_NOT_VERIFIED не увёл на ${expected}:\n${census.describe()}`,
      timeout: 30_000,
    })
    .toBe(true);
  // Экран с подтверждённой сессией уходит на адрес возврата (Р7): окно документа
  // `/verification` закрыто, когда список открыт снова.
  await expectAddress(page, list, "экран подтверждения подтверждённой сессии не вернул на адрес возврата");
  expect(swap.substituted, "подменено не ровно одно чтение списка сетей").toHaveLength(1);
  const onScreen = byDocument.filter((c) => c.document === "/verification");
  console.log(`[F6b-25] обращений к API документа /verification — ${onScreen.length}; подменено чтений ${swap.substituted.length}`);
  expect(onScreen.length, "документ /verification не выпустил ни одного обращения — перепись ничего не видела").toBeGreaterThan(
    0,
  );
  expect(
    onScreen
      .filter((c) => !ALLOWED_BEFORE_CONFIRMATION.some((a) => a.method === c.method && a.path === c.path))
      .map((c) => `${c.method} ${c.path}`),
    `после перехода на экран подтверждения — обращения сверх перечня Р6:\n${census.describe()}`,
  ).toEqual([]);
});

test("F6b-26 · отказ края по каталогу прав на той же странице никуда не уводит", async ({ page }, testInfo) => {
  // verifies #2922 — у близнеца F6b-25 были только компонентные пробы: отказ каталога прав на стенде не судился.
  test.setTimeout(300_000);
  const { projectId } = await tenantWithProject(page);
  // Проект другого посеянного арендатора — байты отказа по каталогу прав снимаются мутацией в нём.
  const seed = await newSeed(testInfo);
  let foreignProjectId = "";
  try {
    await seedConfirmedHuman(seed, seedAddress("F6b-26-foreign"));
    await expect
      .poll(
        async () => {
          const res = await seed.read(PROJECTS);
          if (!res.ok()) return "";
          const body = (await res.json()) as { projects?: Array<{ id: string }> };
          foreignProjectId = body.projects?.[0]?.id ?? "";
          return foreignProjectId;
        },
        { message: "у другого арендатора не появился проект — условие сценария не создано", timeout: 45_000 },
      )
      .not.toBe("");
  } finally {
    await seed.dispose();
  }
  const denied = await page.request.post(NETWORKS, {
    data: { projectId: foreignProjectId, name: `f6b26-${runTag()}` },
  });
  const deniedAnswer = await edgeAnswerOf(denied);
  expect(
    { status: deniedAnswer.status, reason: deniedAnswer.reason },
    `снятие байтов: мутация П-п в проекте ${foreignProjectId}: ${deniedAnswer.status} ${deniedAnswer.text.slice(0, 300)}`,
  ).toEqual({ status: 403, reason: "AUTHZ_DENIED" });
  const refusal = await capturedAnswerOf(denied);

  await scopeIsReady(page, projectId);
  const list = `/projects/${projectId}/vpc/networks`;
  const census = ceremonyCensus(page.context());
  const visited = visitedAddresses(page);
  const swap = await substituteNetworkReads(page, refusal);
  const from = census.calls.length;
  await page.goto(list, { waitUntil: "domcontentloaded" });
  await expect(
    page.getByText("Недостаточно прав", { exact: true }),
    "отказ каталога прав не показан на странице списка",
  ).toBeVisible({ timeout: 30_000 });
  console.log(`[F6b-26] подменено чтений ${swap.substituted.length}; адреса вкладки ${visited.join(" → ")}`);
  expect(swap.substituted.length, "ни одно чтение списка не подменено — предмета нет").toBeGreaterThan(0);
  expect(addressOf(page), "отказ каталога прав увёл со страницы").toBe(list);
  expect(
    [
      ...visited.filter((a) => a.startsWith("/verification")),
      ...documentsSince(census, from)
        .filter((c) => c.path === "/verification")
        .map(formatCall),
    ],
    `отказ каталога прав увёл на экран подтверждения:\n${census.describe()}`,
  ).toEqual([]);
});

// ═══ S2 — письмо открыто там, где сессии нет ══════════════════════════════════

test("F6b-27 · письмо открыто там, где сессии нет: вход, затем код, затем консоль", async ({ page }, testInfo) => {
  // verifies #2922 — у сценария не было пробы: путь из письма в браузер без сессии не судился.
  test.setTimeout(120_000);
  const held = await heldUnconfirmed(testInfo, "F6b-27");
  try {
    const letter = await awaitLetter(held.mailbox, held.human.email, held.before);
    const screens = addressesIn(letter.text);
    expect(screens, `в письме ${letter.id} ждали ровно один адрес экрана:\n${letter.text}`).toHaveLength(1);
    expect(await browserBearer(page), "у браузера сценария есть печенье сессии").toBe("");
    await captureAnswers(page, ANSWERED_PATHS);
    const census = ceremonyCensus(page.context());
    const s = verificationScreen(page);

    await page.goto(screens[0], { waitUntil: "domcontentloaded" });
    await expectAddress(page, "/login", "адрес из письма без сессии не увёл на вход");
    await expect(page.getByRole("button", { name: /Войти$/ }), "экран входа не показал форму").toBeVisible();
    expect(
      census.calls.filter((c) => c.method === "POST").map(formatCall),
      `открытие адреса из письма выпустило POST:\n${census.describe()}`,
    ).toEqual([]);

    const signedAt = census.calls.length;
    await signIn(page, held.human);
    await expectAddress(page, "/verification", "вход П-н не привёл на экран подтверждения");
    await expectVerificationSettled(census, signedAt, s.heading);

    await s.code.fill(letter.code);
    const at = census.calls.length;
    const answered = lanePostAnswer(page, VERIFY_EMAIL_CONFIRM);
    await s.confirm.click();
    expect((await answered).status(), "код из письма не принят").toBe(200);
    await expectShell(page, "после подтверждения каркас не отрисован");
    const left = documentsSince(census, at)[0];
    expect(
      left ? `${left.path}${left.query}` : "(перехода документа нет)",
      `после подтверждения документ ушёл не на корень консоли:\n${census.describe()}`,
    ).toBe("/");
    await expectSessionEnded(
      await held.seed.read(PROJECTS),
      "носитель посева B1 после подтверждения в браузере: прочие сессии не сняты",
    );
  } finally {
    await held.seed.dispose();
  }
});

test("F6b-28 · адрес из письма кода не несёт, и его открытие ничего не подтверждает", async ({
  page,
}, testInfo) => {
  // verifies #2922 — у близнеца F6b-27 не было пробы: адрес экрана в письме не судился.
  test.setTimeout(120_000);
  const held = await heldUnconfirmed(testInfo, "F6b-28");
  try {
    const letter = await awaitLetter(held.mailbox, held.human.email, held.before);
    const screen = `${consoleOrigin(testInfo)}/verification`;
    const addresses = addressesIn(letter.text);
    console.log(`[F6b-28] адресов в теле письма ${letter.id}: ${addresses.length} — ${addresses.join(", ")}`);
    expect(addresses.length, `в теле письма нет ни одного адреса:\n${letter.text}`).toBeGreaterThan(0);
    expect(
      addresses.filter((a) => a !== screen),
      `адреса письма, отличные от ${screen} (со строкой запроса, фрагментом или иной властью):\n${letter.text}`,
    ).toEqual([]);

    const census = ceremonyCensus(page.context());
    const s = verificationScreen(page);
    await page.goto(addresses[0], { waitUntil: "domcontentloaded" });
    await expectAddress(page, "/login", "адрес из письма без сессии не увёл на вход");
    const signedAt = census.calls.length;
    await signIn(page, held.human);
    await expectAddress(page, "/verification", "вход П-н не привёл на экран подтверждения");
    await expectVerificationSettled(census, signedAt, s.heading);
    expectNotCalled(census, "POST", VERIFY_EMAIL_CONFIRM, "открытие адреса из письма и вход без кода");
    await page.goto("/dashboard", { waitUntil: "domcontentloaded" });
    await expectAddress(page, "/verification?returnTo=%2Fdashboard", "без кода /dashboard открылся мимо экрана");
    expectNotCalled(census, "POST", VERIFY_EMAIL_CONFIRM, "открытие /dashboard без кода");
  } finally {
    await held.seed.dispose();
  }
});

// ═══ S2 — главный путь ════════════════════════════════════════════════════════

/** Пароль, которого правило службы не принимает: короче объявленного минимума. */
const REFUSED_PASSWORD = "Kx1!";

test("F6b-40 · главный путь: регистрация → письмо без нажатия → код → обычная консоль", async ({ page }) => {
  // verifies #2922 — у сценария не было пробы: письмо регистрации и путь до каркаса экраном не судились вместе.
  test.setTimeout(120_000);
  const mailbox = stationMailbox();
  const email = seedAddress("F6b-40");
  const before = new Set((await mailbox.letters(email)).map((l) => l.id));
  await captureAnswers(page, ANSWERED_PATHS);
  const census = ceremonyCensus(page.context());
  const sessions = sessionAnswers(page);
  const visited = visitedAddresses(page);
  const s = verificationScreen(page);

  await page.goto("/registration?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  await page.getByRole("textbox", { name: "Адрес электронной почты" }).fill(email);
  await page.getByLabel("Пароль", { exact: true }).fill(E2E_PASSWORD);
  await page.getByRole("button", { name: /Завести учётную запись$/ }).click();
  await expectAddress(page, "/verification?returnTo=%2Fdashboard", "регистрация не увела на экран подтверждения");
  await expectVerificationSettled(census, 0, s.heading);

  const letter = await awaitLetter(mailbox, email, before);
  expect(letter.code.replace(/[-\s]/g, ""), `код письма ${letter.id} не из 10 знаков`).toHaveLength(10);
  expectNotCalled(census, "POST", VERIFY_EMAIL, "регистрация: первое письмо ставит служба, а не консоль");

  await s.code.fill(letter.code);
  const mark = sessions.mark();
  const answered = lanePostAnswer(page, VERIFY_EMAIL_CONFIRM);
  await s.confirm.click();
  expect((await answered).status(), "код письма регистрации не принят").toBe(200);
  await expectAddress(page, "/dashboard", "код из письма не увёл на адрес возврата");
  await expectShell(page, "после подтверждения каркас не отрисован");
  await expect
    .poll(() => sessions.since(mark).some((a) => emailVerifiedOf(a.body) === true), {
      message: "ответ «кто я» после подтверждения не несёт emailVerified: true",
      timeout: 30_000,
    })
    .toBe(true);

  // Пароль второй раз не вводился: ни глагола входа, ни экрана входа.
  expect(census.matching("POST", LANE.login).map(formatCall), census.describe()).toEqual([]);
  expect(visited.filter((a) => a.startsWith("/login")), `адреса вкладки: ${visited.join(" → ")}`).toEqual([]);
  expect(census.matching("POST", LANE.register).length, census.describe()).toBe(1);
  expect(
    (await lettersSince(mailbox, email, before)).map((l) => l.id),
    `у приёмника на ${email} ждали ровно одно письмо — письмо регистрации`,
  ).toEqual([letter.id]);
});

test("F6b-41 · отвергнутая регистрация письма не ставит", async ({ page }) => {
  // verifies #2922 — у близнеца F6b-40 не было пробы: письмо на отвергнутой регистрации не судилось.
  test.setTimeout(120_000);
  const mailbox = stationMailbox();
  const email = seedAddress("F6b-41");
  const before = new Set((await mailbox.letters(email)).map((l) => l.id));
  await captureAnswers(page, ANSWERED_PATHS);
  const password = page.getByLabel("Пароль", { exact: true });
  const submit = page.getByRole("button", { name: /Завести учётную запись$/ });

  await page.goto("/registration?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  await page.getByRole("textbox", { name: "Адрес электронной почты" }).fill(email);
  await password.fill(REFUSED_PASSWORD);
  const first = lanePostAnswer(page, LANE.register);
  await submit.click();
  const refused = await laneOutcomeOf(await first);
  expect(
    { status: refused.status, code: refused.code },
    `регистрация с паролем вне правила службы: ${refused.status} ${refused.text.slice(0, 200)}`,
  ).toEqual({ status: 400, code: 3 });
  await expect(password, "отказ службы не назвал поле пароля").toHaveAttribute("aria-invalid", "true");
  expect(new URL(page.url()).pathname, "отвергнутая регистрация увела с экрана").toBe("/registration");

  await password.fill(E2E_PASSWORD);
  const sentAt = Date.now();
  const second = lanePostAnswer(page, LANE.register);
  await submit.click();
  expect((await second).status(), "регистрация с годным паролем отвергнута").toBe(200);
  await expectAddress(page, "/verification?returnTo=%2Fdashboard", "вторая отправка не увела на экран подтверждения");

  const letter = await awaitLetter(mailbox, email, before);
  expect(
    (await lettersSince(mailbox, email, before)).map((l) => `${l.id} · ${l.created}`),
    `у приёмника на ${email} ждали ровно одно письмо`,
  ).toEqual([`${letter.id} · ${letter.created}`]);
  expect(
    Date.parse(letter.created) >= sentAt,
    `письмо ${letter.id} принято ${letter.created} — раньше второй отправки ${new Date(sentAt).toISOString()}`,
  ).toBe(true);
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

    await expectAddressRefusal(await seed.read(PROJECTS), "ответ края на GET /iam/v1/projects носителем П-н");
  } finally {
    await seed.dispose();
  }
});
