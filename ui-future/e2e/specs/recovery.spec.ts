// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type BrowserContext, type Locator, type Page, type TestInfo } from "@playwright/test";
import { LANE_VERBS, captureAnswers, lanePostAnswer, type LaneAnswer } from "./answer-on-arrival";
import {
  LANE,
  SEED_PASSWORD,
  SESSION_COOKIE,
  SESSION_IDENTITY,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  transferSession,
  type SeededHuman,
} from "./ceremony-seed";
import { ceremonyCensus, formatCall, test, type CeremonyCensus } from "./fixtures";
import {
  LETTER_BUDGET_MS,
  RECOVERY_CODE_LINE,
  codeOf,
  conditionNotCreated,
  stationMailbox,
  type Letter,
  type Mailbox,
} from "./mail-receiver";
import { ACCESS_NOT_RESTORED, RECOVERY_NEXT_STEP } from "./lane-texts";
import { LANE_UNAVAILABLE, bodyOf, fulfillWith, laneTooManyAttempts } from "./producer-answers";

/**
 * Отказ не восстанавливает шаг — экран восстановления доступа и подсказки
 * следующего шага (приёмка F8-S3; находки #2952, #2953, #2955).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ ЭТА ПРОБА
 *
 * У человека, забывшего пароль, не было пути в консоли: служба оба глагола
 * восстановления несёт, край их ретранслирует, а `/recovery` отвечал «такого
 * адреса здесь нет», и путь с этой страницы вёл обратно на вход (#2952). Рядом —
 * отказы входа и регистрации без следующего шага (#2953) и страница стража, на
 * которой человеку названо внутреннее слово и не назван адресат (#2955). Каждое
 * из этих утверждений здесь — наблюдаемое: адрес страницы, текст на экране,
 * перепись обращений и печенье у браузера, а не разметка.
 *
 * ИМЯ ТЕСТА НАЧИНАЕТСЯ С ID СЦЕНАРИЯ (приёмка F8, Р8): перепись имён даёт
 * множество исполненных сценариев F8-S3, и разность с приёмкой называется
 * поимённо. Ссылка на задачу — внутри вызова `test(…)`.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО
 *
 *   • ожидания временем: ждётся условие — отрисованный экран, пришедший ответ,
 *     сменившийся адрес, пришедшее письмо;
 *   • общего адреса у сценариев: каждый сеет СВОЙ (приёмка F8, Р7 ч. 1);
 *   • подстановки там, где служба отвечает сама: подставлены только «служба не
 *     ответила» (F8S3-07, F8S3-12) и «слишком много попыток» (F8S3-11, цена
 *     настоящего отказа — пять отказов оси источника, приёмка F8-S3 Р8), и тела
 *     подстановок собраны по коду производителя (`producer-answers.ts`).
 */

// ─── условие прогона (приёмка F8-S3, §4, У1 и У2) ─────────────────────────────
//
// У1 — то же, что у F8: проект условий прогона (`preconditions/`), от которого
// зависит проект этого файла. У2 — письмо восстановления приходит к приёмнику
// стенда: его отсутствие пробой называется «условие не создано» (`awaitRecoveryLetter`),
// и гейт вердикта относит его к «не выполнилось», а не к красному.

const TITLE = "Восстановление доступа";
const CODE_FORM = "Смена пароля по коду";

/** Текст ступени 2 — один на любой ответ запроса (приёмка F8-S3, Р2, Т-код). */
const T_CODE =
  "Если адрес заведён и подтверждён, на него отправлено письмо с кодом восстановления. Код действует " +
  "ограниченное время и применяется один раз. Введите код из последнего письма и новый пароль.";

/** Новый пароль сценария: длина больше правила стенда (8), на адрес не похож. */
const NEW_PASSWORD = "Kacho-Recovery-2026!n";

/**
 * Тело ответа запроса кода — побайтово одно на любой исход (Ф5-01, Ф5-02): шаг,
 * названный условием, которое знает сам человек, а не утверждение «отправлено».
 */
const REQUESTED_BODY = JSON.stringify({ nextStep: RECOVERY_NEXT_STEP });

// Кнопка отправки — по форме и КОНЦУ имени (см. `identity-ceremony.spec.ts`,
// `submitOf`): значок ожидания кнопки оставляет в имени «loading».
function submitOf(page: Page, form: string, label: string): Locator {
  return page.getByRole("form", { name: form }).getByRole("button", { name: new RegExp(`${label}$`) });
}

function recoveryScreen(page: Page) {
  return {
    heading: page.getByRole("heading", { name: TITLE }),
    email: page.getByRole("textbox", { name: "Адрес почты" }),
    send: submitOf(page, TITLE, "Отправить код"),
    code: page.getByRole("textbox", { name: "Код из письма" }),
    newPassword: page.getByLabel("Новый пароль", { exact: true }),
    complete: submitOf(page, CODE_FORM, "Сменить пароль и войти"),
    resend: page.getByRole("button", { name: /Отправить код ещё раз$/ }),
    back: page.getByRole("link", { name: "Вернуться ко входу" }),
    refusal: page.getByRole("alert"),
    nextStep: page.getByTestId("refusal-next-step"),
    sent: page.getByText(T_CODE, { exact: true }),
  };
}

function pathOf(page: Page): string {
  return new URL(page.url()).pathname;
}

function addressOf(page: Page): string {
  const u = new URL(page.url());
  return `${u.pathname}${u.search}`;
}

/** Экран отрисован НА СВОЁМ адресе; падение называет, куда увела страница. */
async function expectScreen(page: Page, path: string, marker: Locator, what: string) {
  await expect
    .poll(
      async () => {
        if (await marker.isVisible()) return "отрисован";
        const now = pathOf(page);
        if (now !== path) return `страница уведена на ${addressOf(page)}`;
        // Что на экране вместо него — заголовок, который видит человек: причина
        // красного называется им, а не таймаутом.
        const shown = await page.getByRole("heading", { level: 1 }).allInnerTexts();
        return `ещё не отрисован (на экране: ${shown.map((h) => `«${h}»`).join(", ") || "заголовка нет"})`;
      },
      { message: `${what} на ${path} не отрисован консолью`, timeout: 30_000 },
    )
    .toBe("отрисован");
}

async function expectAddress(page: Page, address: string, why: string) {
  await expect.poll(() => addressOf(page), { message: why, timeout: 30_000 }).toBe(address);
}

async function sessionHeld(context: BrowserContext): Promise<boolean> {
  return (await context.cookies()).some((c) => c.name === SESSION_COOKIE && c.value !== "");
}

function lanePost(page: Page, path: string): Promise<LaneAnswer> {
  return lanePostAnswer(page, path);
}

function expectNoProvider(census: CeremonyCensus) {
  expect(
    census.providerCalls().map(formatCall),
    `перепись обращений страницы содержит адрес чужого поставщика:\n${census.describe()}`,
  ).toEqual([]);
}

function countOf(census: CeremonyCensus, method: string, path: string, query?: string): number {
  return census.matching(method, path, query).length;
}

/** Ключи тела запроса, который ушёл со страницы, — множеством, а не «содержит». */
function bodyKeys(res: LaneAnswer): string[] {
  const raw = res.request().postData() ?? "";
  return Object.keys(JSON.parse(raw || "{}") as Record<string, unknown>).sort();
}

function bodyOfRequest(res: LaneAnswer): Record<string, unknown> {
  return JSON.parse(res.request().postData() ?? "{}") as Record<string, unknown>;
}

/** Человек сценария — посевом П-п (адрес подтверждён), свой у каждого сценария. */
async function seeded(testInfo: TestInfo, scenario: string, context?: BrowserContext): Promise<SeededHuman> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedConfirmedHuman(seed, seedAddress(scenario));
    if (context) await transferSession(seed, context);
    return human;
  } finally {
    await seed.dispose();
  }
}

// ─── средство П-В: письмо восстановления из приёмника стенда ─────────────────

const UNMET_RECOVERY_LETTER = "условие не создано: письмо восстановления не дошло до приёмника";

async function lettersBefore(mailbox: Mailbox, email: string): Promise<ReadonlySet<string>> {
  return new Set((await mailbox.letters(email)).map((l) => l.id));
}

/**
 * Ровно одно новое письмо на `email` после `before`, и код в нём — под строкой
 * «Код восстановления:» (письмо подтверждения сюда не годится). Ждётся письмо, а
 * не время; письма нет в срок — «условие не создано» (У2), не красное.
 */
async function awaitRecoveryLetter(mailbox: Mailbox, email: string, before: ReadonlySet<string>): Promise<Letter> {
  const last: { fresh: Letter[] } = { fresh: [] };
  let arrived = true;
  try {
    await expect
      .poll(
        async () => {
          last.fresh = (await mailbox.letters(email)).filter((l) => !before.has(l.id));
          return last.fresh.length > 0 ? "письмо есть" : "письма нет";
        },
        { timeout: LETTER_BUDGET_MS, intervals: [500, 1_000] },
      )
      .toBe("письмо есть");
  } catch (_budgetSpent) {
    arrived = false;
  }
  if (!arrived) {
    return conditionNotCreated(
      `${UNMET_RECOVERY_LETTER}: адрес ${email}, приёмник ${mailbox.base}, ждали ${LETTER_BUDGET_MS / 1000} с после запроса кода`,
    );
  }
  expect(last.fresh.length, `писем на ${email} после запроса кода ${last.fresh.length}, ждали ровно одно`).toBe(1);
  const letter = { ...last.fresh[0], code: codeOf(last.fresh[0].text, RECOVERY_CODE_LINE) };
  expect(
    letter.code,
    `письмо на ${email} принято, но кода под строкой «${RECOVERY_CODE_LINE}» нет:\n${letter.text.slice(0, 600)}`,
  ).not.toBe("");
  return letter;
}

/**
 * Ступень 1 пройдена экраном: адрес введён, «Отправить код» нажата, ответ `200`,
 * ступень 2 отрисована, письмо прочитано средством П-В.
 */
async function requestCode(page: Page, human: SeededHuman, mailbox: Mailbox): Promise<{ res: LaneAnswer; letter: Letter }> {
  const s = recoveryScreen(page);
  await expectScreen(page, "/recovery", s.send, "экран восстановления доступа");
  const before = await lettersBefore(mailbox, human.email);
  await s.email.fill(human.email);
  const [res] = await Promise.all([lanePost(page, LANE.recovery), s.send.click()]);
  expect(res.status(), `запрос кода не прошёл: ${await res.text()}`).toBe(200);
  await expect(s.sent, "ступень 2 не отрисована текстом Т-код").toBeVisible();
  const letter = await awaitRecoveryLetter(mailbox, human.email, before);
  return { res, letter };
}

/** Поля ступени 2 заполнены, «Сменить пароль и войти» нажата; ответ завершения. */
async function completeWith(page: Page, code: string, password: string): Promise<LaneAnswer> {
  const s = recoveryScreen(page);
  await s.code.fill(code);
  await s.newPassword.fill(password);
  const [res] = await Promise.all([lanePost(page, LANE.recoveryComplete), s.complete.click()]);
  return res;
}

/** Вошёл — печенье сессии у браузера и адрес возврата. */
async function expectSignedInAt(page: Page, address: string, why: string) {
  await expectAddress(page, address, why);
  expect(await sessionHeld(page.context()), `${why}: у браузера нет носителя сессии`).toBe(true);
}

// Ответы глаголов полосы снимаются ДО страницы: за ответом завершения экран
// уходит документом, и тело после ухода не читается (`answer-on-arrival.ts`).
test.beforeEach(async ({ page }) => {
  await captureAnswers(page, LANE_VERBS);
});

// ═══ F8-S3 — группа R. Адрес и вход на экран ══════════════════════════════════

test("F8S3-01 · /recovery без сессии отдаёт экран восстановления, и открытие не отправляет ни одной формы", async ({
  page,
}) => {
  // verifies #2952 — адрес отвечал «такого адреса здесь нет», а путь с него вёл обратно на вход.
  const census = ceremonyCensus(page.context());
  await page.goto("/recovery", { waitUntil: "domcontentloaded" });
  const s = recoveryScreen(page);
  await expectScreen(page, "/recovery", s.heading, "экран восстановления доступа");
  await expect(s.email, "поля «Адрес почты» нет").toBeVisible();
  await expect(s.send, "кнопки «Отправить код» нет").toBeVisible();
  await expect(s.back, "ссылки «Вернуться ко входу» нет").toBeVisible();
  expect(pathOf(page), "перевода на панель или на вход быть не должно").toBe("/recovery");
  expect(countOf(census, "POST", LANE.recovery), `открытие экрана отправило запрос кода:\n${census.describe()}`).toBe(0);
  expect(countOf(census, "POST", LANE.recoveryComplete), `открытие экрана отправило завершение:\n${census.describe()}`).toBe(0);
  expectNoProvider(census);
});

test("F8S3-02 · подтверждённая сессия на /recovery формы не видит и уходит на адрес возврата", async ({
  page,
}, testInfo) => {
  // verifies #2952 — близнец F8S3-01: у браузера есть сессия подтверждённого человека.
  await seeded(testInfo, "F8S3-02", page.context());
  const census = ceremonyCensus(page.context());
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  await expectAddress(page, "/dashboard", "подтверждённая сессия не уведена с /recovery на адрес возврата");
  await expect(recoveryScreen(page).email, "подтверждённой сессии показана форма восстановления").toHaveCount(0);
  expect(countOf(census, "POST", LANE.recovery), `запрос кода у живой сессии:\n${census.describe()}`).toBe(0);
});

test("F8S3-03 · на экране входа есть путь на восстановление, и он несёт адрес возврата, но не адрес почты", async ({
  page,
}) => {
  // verifies #2952 — на экране входа единственной ссылкой была регистрация: пути для забывшего пароль не было.
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const login = page.getByRole("form", { name: "Вход в консоль" }).getByRole("button", { name: /Войти$/ });
  await expectScreen(page, "/login", login, "экран входа");
  await page.getByRole("textbox", { name: "Адрес электронной почты" }).fill(seedAddress("F8S3-03"));
  // Положительная сторона распознавателя: тот же предикат перехода находит регистрацию.
  const destinations = await page
    .getByRole("link")
    .evaluateAll((links) => links.map((a) => new URL((a as HTMLAnchorElement).href).pathname));
  expect(destinations, "перехода на регистрацию нет — предикат ничего не распознаёт").toContain("/registration");
  const recovery = page.getByRole("link", { name: "Не получается войти?" });
  await expect(recovery, "на экране входа нет пути «Не получается войти?»").toBeVisible();
  await recovery.click();
  await expectAddress(page, "/recovery?returnTo=%2Fdashboard", "ссылка «Не получается войти?» не привела на экран восстановления");
  await expectScreen(page, "/recovery", recoveryScreen(page).heading, "экран восстановления доступа");
  const u = new URL(page.url());
  expect(decodeURIComponent(`${u.search}${u.hash}`), "адрес почты ушёл в адрес страницы").not.toContain("@");
});

// ═══ F8-S3 — группа S. Ступень 1 — запрос кода ═══════════════════════════════

test("F8S3-04 · запрос кода проходит: ступень 2 с одним текстом, письмо пришло к приёмнику", async ({
  page,
}, testInfo) => {
  // verifies #2952
  const human = await seeded(testInfo, "F8S3-04");
  const mailbox = stationMailbox();
  const census = ceremonyCensus(page.context());
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const { res } = await requestCode(page, human, mailbox);
  expect(await res.text(), "тело ответа запроса кода").toBe(REQUESTED_BODY);
  expect(countOf(census, "GET", LANE.csrf, "?form=recovery"), `признак вида recovery не добыт:\n${census.describe()}`).toBeGreaterThan(0);
  expect(countOf(census, "POST", LANE.recovery), `запросов кода не ровно один:\n${census.describe()}`).toBe(1);
  expect(bodyKeys(res), "тело запроса кода — ровно email и csrfToken").toEqual(["csrfToken", "email"]);
  expect(bodyOfRequest(res).email).toBe(human.email);
  const s = recoveryScreen(page);
  await expect(s.email, "адрес ступени 1 не только для чтения").toHaveAttribute("readonly", "");
  await expect(s.email).toHaveValue(human.email);
  await expect(s.code).toBeVisible();
  await expect(s.newPassword).toBeVisible();
  await expect(s.complete).toBeEnabled();
  await expect(s.resend).toBeEnabled();
  expect(await sessionHeld(page.context()), "запрос кода выдал носитель сессии").toBe(false);
  expect(pathOf(page)).toBe("/recovery");
});

test("F8S3-05 · незаведённый адрес: побайтово тот же ответ и тот же экран", async ({ page }) => {
  // verifies #2952 — близнец F8S3-04: адрес не заведён.
  const email = seedAddress("F8S3-05-unknown");
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const s = recoveryScreen(page);
  await expectScreen(page, "/recovery", s.send, "экран восстановления доступа");
  await s.email.fill(email);
  const [res] = await Promise.all([lanePost(page, LANE.recovery), s.send.click()]);
  expect(res.status()).toBe(200);
  expect(await res.text(), "тело ответа отличается от ответа на заведённый адрес").toBe(REQUESTED_BODY);
  await expect(s.sent, "текст Т-код не тот, что у заведённого адреса").toBeVisible();
  await expect(s.refusal, "экран различил незаведённый адрес отказом").toHaveCount(0);
});

test("F8S3-06 · пустой адрес назван службой по имени поля", async ({ page }) => {
  // verifies #2952 — близнец F8S3-04: поле адреса пусто; своего правила экран не применяет.
  const census = ceremonyCensus(page.context());
  await page.goto("/recovery", { waitUntil: "domcontentloaded" });
  const s = recoveryScreen(page);
  await expectScreen(page, "/recovery", s.send, "экран восстановления доступа");
  const [res] = await Promise.all([lanePost(page, LANE.recovery), s.send.click()]);
  expect(res.status()).toBe(400);
  expect((await res.json()) as { code: number; message: string }).toMatchObject({
    code: 3,
    message: "Illegal argument email: required",
  });
  expect(countOf(census, "POST", LANE.recovery)).toBe(1);
  await expect(s.email, "поле адреса не отмечено").toHaveAttribute("aria-invalid", "true");
  await expect(page.getByText("Illegal argument email: required")).toBeVisible();
  await expect(s.sent, "ступень 2 отрисована на отказе").toHaveCount(0);
});

test("F8S3-07 · служба не ответила на запрос: отказ назван, введённое цело", async ({ page }) => {
  // verifies #2952 — близнец F8S3-04: служба не ответила (ответ подставлен по коду производителя).
  const unavailable = bodyOf(LANE_UNAVAILABLE);
  await page.route(
    (u) => u.pathname === LANE.recovery,
    (route) => (route.request().method() === "POST" ? fulfillWith(route, LANE_UNAVAILABLE) : route.continue()),
  );
  const email = seedAddress("F8S3-07");
  await page.goto("/recovery", { waitUntil: "domcontentloaded" });
  const s = recoveryScreen(page);
  await expectScreen(page, "/recovery", s.send, "экран восстановления доступа");
  await s.email.fill(email);
  const [res] = await Promise.all([lanePost(page, LANE.recovery), s.send.click()]);
  expect(res.status()).toBe(503);
  await expect(s.refusal).toContainText(unavailable.message);
  await expect(s.refusal, "экран не предложил отправить снова").toContainText("Отправьте форму ещё раз");
  await expect(s.send, "кнопка «Отправить код» закрыта").toBeEnabled();
  await expect(s.email, "введённый адрес потерян").toHaveValue(email);
  await expect(s.sent, "ступень 2 отрисована на отказе").toHaveCount(0);
});

// ═══ F8-S3 — группа C. Ступень 2 — код и новый пароль ════════════════════════

test("F8S3-08 · восстановление проходит целиком и уводит на адрес возврата", async ({ page }, testInfo) => {
  // verifies #2952
  const human = await seeded(testInfo, "F8S3-08");
  const mailbox = stationMailbox();
  const census = ceremonyCensus(page.context());
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const { letter } = await requestCode(page, human, mailbox);
  const res = await completeWith(page, letter.code, NEW_PASSWORD);
  expect(res.status(), `завершение не прошло: ${await res.text()}`).toBe(200);
  const signed = (await res.json()) as { user?: unknown; session?: unknown };
  expect([typeof signed.user, typeof signed.session], "тело завершения без user и session").toEqual(["object", "object"]);
  expect(bodyKeys(res), "тело завершения — ровно email, code, newPassword, csrfToken").toEqual([
    "code",
    "csrfToken",
    "email",
    "newPassword",
  ]);
  expect(bodyOfRequest(res)).toMatchObject({ email: human.email, code: letter.code });
  await expectSignedInAt(page, "/dashboard", "после восстановления консоль не увела на адрес возврата");
  expect(countOf(census, "GET", LANE.csrf, "?form=recovery-complete")).toBeGreaterThan(0);
  expect(countOf(census, "POST", LANE.recoveryComplete), `завершений не ровно одно:\n${census.describe()}`).toBe(1);
  expectNoProvider(census);
});

test("F8S3-09 · неверный код: назван один отказ, сессии нет, путь «код ещё раз» на месте", async ({
  page,
}, testInfo) => {
  // verifies #2952 — близнец F8S3-08: изменено только значение кода.
  const human = await seeded(testInfo, "F8S3-09");
  const mailbox = stationMailbox();
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const { letter } = await requestCode(page, human, mailbox);
  const wrong = letter.code.startsWith("0") ? `1${letter.code.slice(1)}` : `0${letter.code.slice(1)}`;
  const res = await completeWith(page, wrong, NEW_PASSWORD);
  expect(res.status()).toBe(401);
  expect(await res.text(), "тело отказа — одно на все причины").toBe(
    JSON.stringify({ code: 16, message: ACCESS_NOT_RESTORED, details: [] }),
  );
  const s = recoveryScreen(page);
  await expect(s.refusal, "текст отказа на экране не дословный").toHaveText(ACCESS_NOT_RESTORED);
  await expect(s.nextStep, "к отказу не назван следующий шаг").toBeVisible();
  expect(await sessionHeld(page.context()), "после отказа у браузера появился носитель").toBe(false);
  expect(pathOf(page)).toBe("/recovery");
  await expect(s.resend, "«Отправить код ещё раз» недоступна").toBeEnabled();
});

test("F8S3-10 · новый пароль не отвечает правилу: поле названо, код не потрачен", async ({ page }, testInfo) => {
  // verifies #2952 — близнец F8S3-08: изменено только значение нового пароля.
  const human = await seeded(testInfo, "F8S3-10");
  const mailbox = stationMailbox();
  const census = ceremonyCensus(page.context());
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const { letter } = await requestCode(page, human, mailbox);
  const refused = await completeWith(page, letter.code, "Ab1");
  expect(refused.status()).toBe(400);
  const message = "Illegal argument newPassword: shorter than the declared minimum length";
  expect((await refused.json()) as { code: number; message: string }).toMatchObject({ code: 3, message });
  const s = recoveryScreen(page);
  await expect(s.newPassword, "поле нового пароля не отмечено").toHaveAttribute("aria-invalid", "true");
  await expect(s.code, "отмечено поле, которого служба не называла").not.toHaveAttribute("aria-invalid", "true");
  expect(await sessionHeld(page.context())).toBe(false);
  const accepted = await completeWith(page, letter.code, NEW_PASSWORD);
  expect(accepted.status(), `тот же код после отказа правила не прошёл: ${await accepted.text()}`).toBe(200);
  await expectSignedInAt(page, "/dashboard", "второй заход тем же кодом не увёл на адрес возврата");
  expect(countOf(census, "POST", LANE.recovery), "экран потребовал нового письма").toBe(1);
});

test("F8S3-11 · частота: экран называет срок и до него не отправляет", async ({ page }, testInfo) => {
  // verifies #2952 — близнец F8S3-08: ответ завершения 429 подставлен по коду производителя.
  const human = await seeded(testInfo, "F8S3-11");
  const mailbox = stationMailbox();
  const census = ceremonyCensus(page.context());
  const limited = laneTooManyAttempts(30);
  await page.route(
    (u) => u.pathname === LANE.recoveryComplete,
    (route) => (route.request().method() === "POST" ? fulfillWith(route, limited) : route.continue()),
  );
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const { letter } = await requestCode(page, human, mailbox);
  const res = await completeWith(page, letter.code, NEW_PASSWORD);
  expect(res.status()).toBe(429);
  expect((await res.json()) as { code: number; message: string }).toMatchObject({
    code: 8,
    message: "too many attempts; try again later",
  });
  const s = recoveryScreen(page);
  await expect(s.refusal).toContainText("too many attempts; try again later");
  await expect(s.refusal, "экран не назвал срок из Retry-After").toContainText("Повторить можно через 30 с");
  // Число убывает — отсчёт экрана, а не застывший заголовок.
  await expect(s.refusal, "срок не убывает").toContainText(/Повторить можно через (2\d|1\d|\d) с/, { timeout: 5_000 });
  await expect(s.complete, "до срока кнопка отправки доступна").toBeDisabled();
  await s.complete.click({ force: true });
  await s.newPassword.press("Enter");
  // Барьер порядка, а не пауза: события, выпущенные кодом страницы раньше, уже записаны.
  await page.evaluate(() => undefined);
  expect(
    countOf(census, "POST", LANE.recoveryComplete),
    `до срока экран выпустил завершение снова:\n${census.describe()}`,
  ).toBe(1);
});

test("F8S3-12 · служба не ответила на завершение: отказ назван, введённое цело, сессии нет", async ({
  page,
}, testInfo) => {
  // verifies #2952 — близнец F8S3-08: служба не ответила (ответ подставлен по коду производителя).
  const human = await seeded(testInfo, "F8S3-12");
  const mailbox = stationMailbox();
  await page.route(
    (u) => u.pathname === LANE.recoveryComplete,
    (route) => (route.request().method() === "POST" ? fulfillWith(route, LANE_UNAVAILABLE) : route.continue()),
  );
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const { letter } = await requestCode(page, human, mailbox);
  const res = await completeWith(page, letter.code, NEW_PASSWORD);
  expect(res.status()).toBe(503);
  const s = recoveryScreen(page);
  await expect(s.refusal).toContainText(bodyOf(LANE_UNAVAILABLE).message);
  await expect(s.refusal, "экран не предложил отправить снова").toContainText("Отправьте форму ещё раз");
  await expect(s.complete).toBeEnabled();
  await expect(s.code, "введённый код потерян").toHaveValue(letter.code);
  expect(await sessionHeld(page.context())).toBe(false);
  expect(pathOf(page)).toBe("/recovery");
});

test("F8S3-13 · признак формы чужого вида отвергнут, и экран даёт повторить", async ({ page }, testInfo) => {
  // verifies #2952 — близнец F8S3-08: первый признак завершения добыт запросом вида logout.
  const human = await seeded(testInfo, "F8S3-13");
  const mailbox = stationMailbox();
  const census = ceremonyCensus(page.context());
  let substituted = false;
  await page.route(
    (u) => u.pathname === LANE.csrf && u.searchParams.get("form") === "recovery-complete",
    async (route) => {
      if (substituted) return route.continue();
      substituted = true;
      const u = new URL(route.request().url());
      u.searchParams.set("form", "logout");
      await route.continue({ url: u.toString() });
    },
  );
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const { letter } = await requestCode(page, human, mailbox);
  const refused = await completeWith(page, letter.code, NEW_PASSWORD);
  expect(refused.status()).toBe(403);
  expect((await refused.json()) as { code: number; message: string }).toMatchObject({
    code: 7,
    message: "form token rejected",
  });
  expect(substituted, "подстановка вида признака не сработала — условие не создано").toBe(true);
  await expect
    .poll(() => countOf(census, "GET", LANE.csrf, "?form=recovery-complete"), {
      message: `после отказа признака экран не добыл свежего признака вида recovery-complete:\n${census.describe()}`,
      timeout: 15_000,
    })
    .toBeGreaterThanOrEqual(2);
  const s = recoveryScreen(page);
  await expect(s.code, "код потерян после отказа признака").toHaveValue(letter.code);
  await expect(s.newPassword, "новый пароль потерян после отказа признака").toHaveValue(NEW_PASSWORD);
  const [accepted] = await Promise.all([lanePost(page, LANE.recoveryComplete), s.complete.click()]);
  expect(accepted.status(), `повтор со свежим признаком не прошёл: ${await accepted.text()}`).toBe(200);
  await expectSignedInAt(page, "/dashboard", "повтор прошёл, а перехода нет");
});

test("F8S3-14 · «Отправить код ещё раз»: работает код из последнего письма", async ({ page }, testInfo) => {
  // verifies #2952 — вариант F8S3-08: изменено число запросов кода.
  const human = await seeded(testInfo, "F8S3-14");
  const mailbox = stationMailbox();
  const census = ceremonyCensus(page.context());
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const first = await requestCode(page, human, mailbox);
  const s = recoveryScreen(page);
  const before = await lettersBefore(mailbox, human.email);
  const [again] = await Promise.all([lanePost(page, LANE.recovery), s.resend.click()]);
  expect(again.status()).toBe(200);
  expect(await again.text()).toBe(REQUESTED_BODY);
  expect(bodyKeys(again)).toEqual(["csrfToken", "email"]);
  expect(bodyOfRequest(again).email).toBe(human.email);
  await expect(s.sent, "после второго запроса текст Т-код не тот").toBeVisible();
  const second = await awaitRecoveryLetter(mailbox, human.email, before);
  expect(second.id, "второе письмо — то же, что первое").not.toBe(first.letter.id);
  expect(countOf(census, "POST", LANE.recovery), `запросов кода не ровно два:\n${census.describe()}`).toBe(2);
  const res = await completeWith(page, second.code, NEW_PASSWORD);
  expect(res.status(), `код последнего письма не принят: ${await res.text()}`).toBe(200);
  await expectSignedInAt(page, "/dashboard", "после восстановления кодом второго письма перехода нет");
});

test("F8S3-15 · адрес возврата чужого происхождения отвергнут после восстановления", async ({ page }, testInfo) => {
  // verifies #2952 — близнец F8S3-08: изменено только значение returnTo.
  const human = await seeded(testInfo, "F8S3-15");
  const mailbox = stationMailbox();
  const origin = new URL(testInfo.project.use.baseURL ?? "").origin;
  await page.goto(`/recovery?returnTo=${encodeURIComponent("//evil.example/dashboard")}`, {
    waitUntil: "domcontentloaded",
  });
  const { letter } = await requestCode(page, human, mailbox);
  const res = await completeWith(page, letter.code, NEW_PASSWORD);
  expect(res.status(), `завершение не прошло: ${await res.text()}`).toBe(200);
  // Корень консоли уводит на панель (`index` оболочки).
  await expect
    .poll(() => page.url(), { message: "документ ушёл не на корень своей консоли", timeout: 30_000 })
    .toBe(`${origin}/dashboard`);
});

test("F8S3-16 · восстановление проходится с клавиатуры целиком", async ({ page }, testInfo) => {
  // verifies #2952 — вариант F8S3-08: устройство ввода — только клавиши.
  const human = await seeded(testInfo, "F8S3-16");
  const mailbox = stationMailbox();
  await page.goto("/recovery?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const s = recoveryScreen(page);
  await expectScreen(page, "/recovery", s.send, "экран восстановления доступа");

  /** Клавишей перехода до элемента — не больше двадцати нажатий: условие, а не время. */
  async function tabTo(target: Locator, what: string) {
    for (let i = 0; i < 20; i++) {
      if (await target.evaluate((el) => el === document.activeElement)) return;
      await page.keyboard.press("Tab");
    }
    throw new Error(`${what} недостижимо клавишей перехода за двадцать нажатий`);
  }

  const before = await lettersBefore(mailbox, human.email);
  await tabTo(s.email, "поле «Адрес почты»");
  await page.keyboard.type(human.email);
  await tabTo(s.send, "кнопка «Отправить код»");
  const [requested] = await Promise.all([lanePost(page, LANE.recovery), page.keyboard.press("Enter")]);
  expect(requested.status()).toBe(200);
  await expect(s.sent).toBeVisible();
  const letter = await awaitRecoveryLetter(mailbox, human.email, before);
  await tabTo(s.code, "поле «Код из письма»");
  await page.keyboard.type(letter.code);
  await tabTo(s.newPassword, "поле «Новый пароль»");
  await page.keyboard.type(NEW_PASSWORD);
  await tabTo(s.complete, "кнопка «Сменить пароль и войти»");
  const [res] = await Promise.all([lanePost(page, LANE.recoveryComplete), page.keyboard.press("Enter")]);
  expect(res.status(), `завершение с клавиатуры не прошло: ${await res.text()}`).toBe(200);
  await expectSignedInAt(page, "/dashboard", "восстановление с клавиатуры прошло, а перехода нет");
});

// ═══ #2953 — отказы входа и регистрации называют следующий шаг ═══════════════

test("2953 · неверный пароль: текст службы дословно, и рядом назван путь «Не получается войти?»", async ({
  page,
}, testInfo) => {
  // verifies #2953 — «authentication failed» и больше ни слова: человек не знал, что делать дальше.
  const human = await seeded(testInfo, "N2953-login");
  await page.goto("/login?returnTo=/dashboard", { waitUntil: "domcontentloaded" });
  const submit = page.getByRole("form", { name: "Вход в консоль" }).getByRole("button", { name: /Войти$/ });
  await expectScreen(page, "/login", submit, "экран входа");
  await page.getByRole("textbox", { name: "Адрес электронной почты" }).fill(human.email);
  await page.getByLabel("Пароль", { exact: true }).fill(`${human.password}-не-тот`);
  const [res] = await Promise.all([lanePost(page, LANE.login), submit.click()]);
  expect(res.status()).toBe(401);
  await expect(page.getByRole("alert"), "текст отказа не дословный").toHaveText("authentication failed");
  const step = page.getByTestId("refusal-next-step");
  await expect(step, "к отказу входа не назван следующий шаг").toContainText("«Не получается войти?»");
  await expect(step, "подсказка различает причину отказа").not.toContainText(/неверн|не найден|заблокир/i);
  // Названный путь — действие, а не надпись: он ведёт на экран восстановления.
  const recovery = page.getByRole("link", { name: "Не получается войти?" });
  await expect(recovery, "названного пути на экране нет").toBeVisible();
  await recovery.click();
  await expectScreen(page, "/recovery", recoveryScreen(page).heading, "экран восстановления доступа");
});

test("2953 · занятый адрес при регистрации: текст службы дословно, и рядом — войти или восстановить доступ", async ({
  page,
}, testInfo) => {
  // verifies #2953 — «registration refused» и только «Уже есть учётная запись — войти».
  const human = await seeded(testInfo, "N2953-register");
  await page.goto("/registration", { waitUntil: "domcontentloaded" });
  const submit = page
    .getByRole("form", { name: "Новая учётная запись" })
    .getByRole("button", { name: /Завести учётную запись$/ });
  await expectScreen(page, "/registration", submit, "экран регистрации");
  await page.getByRole("textbox", { name: "Адрес электронной почты" }).fill(human.email);
  await page.getByLabel("Пароль", { exact: true }).fill(SEED_PASSWORD);
  const [res] = await Promise.all([lanePost(page, LANE.register), submit.click()]);
  expect(res.status()).toBe(400);
  const refusal = (await res.json()) as { code: number; message: string };
  expect(refusal.code).toBe(9);
  await expect(page.getByRole("alert"), "текст отказа не дословный").toHaveText(refusal.message);
  const step = page.getByTestId("refusal-next-step");
  await expect(step, "к отказу регистрации не назван следующий шаг").toContainText("войдите");
  await expect(step).toContainText("восстановите доступ");
  await expect(step, "подсказка говорит, занят ли адрес").not.toContainText(/занят|уже существует/i);
});

// ═══ #2955 — страница «признак адреса не назван» называет шаг словами человека ═

test("2955 · ответ о сессии без признака подтверждённости: страница без «край», шаг и адресат названы", async ({
  page,
}, testInfo) => {
  // verifies #2955 — страница называла человеку «край» и не говорила, к кому идти.
  await seeded(testInfo, "N2955", page.context());
  let stripped = 0;
  await page.route(
    (u) => u.pathname === SESSION_IDENTITY,
    async (route) => {
      // Настоящий ответ края о сессии без одного поля — того, которого не назвала
      // посадка (`session.emailVerified`); остальное — как прислал производитель.
      // Заголовки — те, что браузер действительно отправил (с печеньем сессии):
      // ответ снимается с той же сессии, что у страницы.
      const real = await route.fetch({ headers: await route.request().allHeaders() });
      const body = (await real.json()) as { session?: Record<string, unknown> };
      if (body.session) delete body.session.emailVerified;
      stripped += 1;
      await route.fulfill({ response: real, body: JSON.stringify(body) });
    },
  );
  await page.goto("/dashboard", { waitUntil: "domcontentloaded" });
  const heading = page.getByRole("heading", { name: "Не удалось проверить сессию" });
  await expectScreen(page, "/dashboard", heading, "страница «признак адреса не назван»");
  expect(stripped, "ответ о сессии не снят — условие не создано").toBeGreaterThan(0);
  const text = (await page.locator("main").innerText()).trim();
  expect(text, "страница называет человеку внутреннее слово").not.toMatch(/кра[йяюе]/i);
  await expect(page.locator("main"), "следующий шаг не назван").toContainText("Проверить снова");
  await expect(page.locator("main"), "адресат обращения не назван").toContainText("администратор");
  await page.getByRole("button", { name: "Проверить снова" }).click();
  await expect(heading, "«Проверить снова» увела со страницы при том же ответе").toBeVisible();
  await expect(page.getByRole("button", { name: "Выйти" })).toBeEnabled();
});
