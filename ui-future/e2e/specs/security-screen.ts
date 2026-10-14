// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { spawnSync } from "node:child_process";
import { expect, type BrowserContext, type Page, type Request, type Route, type TestInfo } from "@playwright/test";
import {
  dropSeedSecondFactor,
  raiseSeedToLevelTwo,
  seedAccessKey,
  type SeededAccessKey,
} from "./access-key-seed";
import {
  LANE,
  SESSION_COOKIE,
  lastIssued,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  totpCode,
  transferSession,
  type Seed,
  type SeededHuman,
  type SeededSecondFactor,
} from "./ceremony-seed";
import { conditionNotCreated } from "./mail-receiver";
import { FIXTURE_UNMET_PREFIX } from "./producer-answers";

/**
 * Экран «Безопасность» и окно подтверждения любым заведённым способом — общее
 * для проб приёмки F8-S7 (`docs/specs/sub-phase-F8-S7-console-security-settings-and-step-up-methods-acceptance.md`
 * воркспейса, отпечаток 559ae688…; реализация — kacho#3230).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЗДЕСЬ
 *
 *   • доступные имена экрана — ДОСЛОВНО из приёмки (Р1…Р10): проба наблюдает то,
 *     что видит человек и экранный диктор, а не классы разметки;
 *   • посевы §3 (С-п, С-к, С-кл, С-ккл) — глаголами продукта через край, носитель
 *     переносится в браузер явно (`transferSession`);
 *   • перепись тел исходящих запросов страницы — для «секрет не уходит ни в один
 *     запрос» (F8-S7-12) и «step-up с method …» (F8-S7-21…23).
 *
 * ПОСЕВ С-л («только ключ») — С-кл, у которого строка способа «пароль» снята
 * ЗАПИСЬЮ в хранилище службы (решение диспетчера по вопросу полосы, форма —
 * посев `seed_key_person.py` службы): человека без пароля продукт действием не
 * заводит (F8 N32), и это единственный шаг посева мимо продукта. Запись — та
 * же форма «после переноса», что кладёт миграция службы: отметка открытого
 * пути восстановления и снятие строки пароля одной транзакцией. Исполнитель
 * записи — `KACHO_KANAME_STORE_EXEC` окружения стенда (`stand-ns.sh forward`);
 * его нет либо он отказал — «условие не создано», а не красное.
 *
 * Ожидания временем нет: ждётся условие — отрисованный экран, ответ, переход
 * ступени кода по времени.
 */

export const STEP_UP_BEGIN = "/iam/v1/auth/step-up/access-key/begin";

/** Названия разделов в порядке Р1. */
export const SECTION_ORDER = [
  "Сводка",
  "Способ входа",
  "Второй фактор",
  "Ключи доступа",
  "Запасные коды",
  "Активные сессии",
] as const;

/** Доступные имена экрана (Р1…Р9). */
export function securityScreen(page: Page) {
  const section = (name: string) => page.getByRole("region", { name, exact: true });
  const keys = section("Ключи доступа");
  return {
    heading: page.getByRole("heading", { level: 1, name: "Безопасность", exact: true }),
    oldHeading: page.getByRole("heading", { name: "Параметры учётной записи" }),
    nav: page.getByRole("navigation", { name: "Разделы страницы" }),
    section,
    summary: section("Сводка"),
    signIn: section("Способ входа"),
    secondFactor: section("Второй фактор"),
    keys,
    backup: section("Запасные коды"),
    sessions: section("Активные сессии"),
    keyDelete: (name: string) => keys.getByRole("button", { name: `Удалить ключ доступа «${name}»`, exact: true }),
    status: page.getByRole("status"),
    alert: page.getByRole("alert"),
  };
}

export type SecurityScreen = ReturnType<typeof securityScreen>;

/**
 * Открыть `/settings` и дождаться экрана «Безопасность». Прежний экран
 * («Параметры учётной записи») — отсутствие предмета приёмки, и отказ говорит
 * это словами, а не таймаутом.
 */
export async function openSecurity(page: Page): Promise<SecurityScreen> {
  await page.goto("/settings", { waitUntil: "domcontentloaded" });
  const s = securityScreen(page);
  await expect
    .poll(
      async () => {
        if (await s.heading.isVisible()) return "отрисован";
        const now = new URL(page.url()).pathname;
        if (now !== "/settings") return `страница уведена на ${now}`;
        if (await s.oldHeading.isVisible()) return "на /settings прежний экран «Параметры учётной записи» — экрана «Безопасность» нет";
        return "ещё не отрисован";
      },
      { message: "экран «Безопасность» на /settings не отрисован консолью (приёмка F8-S7, Р1)", timeout: 30_000 },
    )
    .toBe("отрисован");
  return s;
}

/** Окно подтверждения (повышения) — доступные имена Р5. */
export function stepUpWindow(page: Page) {
  const dialog = page.getByRole("dialog");
  return {
    dialog,
    keyButton: dialog.getByRole("button", { name: "Подтвердить ключом доступа", exact: true }),
    otherGroup: dialog.getByRole("radiogroup", { name: "Другой способ" }),
    radio: (name: string) => dialog.getByRole("radio", { name, exact: true }),
    code: dialog.getByLabel("Код из приложения", { exact: true }),
    backupCode: dialog.getByLabel("Запасной код", { exact: true }),
    confirm: dialog.getByRole("button", { name: "Подтвердить", exact: true }),
    noMethod: dialog.getByText("Нет способа подтверждения", { exact: true }),
    setUp: dialog.getByRole("link", { name: "Настроить второй фактор", exact: true }),
    signInAgain: dialog.getByRole("button", { name: "Войти заново", exact: true })
      .or(dialog.getByRole("link", { name: "Войти заново", exact: true })),
    alert: dialog.getByRole("alert"),
  };
}

// ─── перепись тел исходящих запросов ─────────────────────────────────────────

export interface SentBody {
  method: string;
  path: string;
  url: string;
  body: string;
}

/** Перепись: метод, путь, адрес целиком и тело КАЖДОГО запроса контекста. */
export function sentBodies(context: BrowserContext): SentBody[] {
  const out: SentBody[] = [];
  context.on("request", (r: Request) => {
    let path = "";
    try {
      path = new URL(r.url()).pathname;
    } catch {
      return;
    }
    out.push({ method: r.method(), path, url: r.url(), body: r.postData() ?? "" });
  });
  return out;
}

/** Способы `method`, с которыми страница звала церемонию повышения. */
export function stepUpMethodsSent(sent: readonly SentBody[]): string[] {
  return sent
    .filter((s) => s.method === "POST" && s.path === LANE.stepUp)
    .map((s) => {
      try {
        return String((JSON.parse(s.body) as { method?: unknown }).method ?? "");
      } catch {
        return "";
      }
    });
}

// ─── посевы §3 ───────────────────────────────────────────────────────────────

export type SeedKind = "С-п" | "С-к" | "С-кл" | "С-ккл" | "С-л";

export interface Human {
  seed: Seed;
  human: SeededHuman;
  factor: SeededSecondFactor | null;
  key: SeededAccessKey | null;
  keyName: string;
}

/**
 * Имя ключа посева — в форме имени ресурса службы
 * (`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`): имя иной формы служба отвергает, и
 * посев падал бы на регистрации ключа раньше предмета пробы.
 */
export function seedKeyName(scenario: string): string {
  return `key-${scenario.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "")}`;
}

/**
 * Человек посева `kind` (§3), носитель — в браузере. `level` — уровень сессии
 * браузера: «2» — сессия посева (поднята подтверждением кода), «1» — отдельный
 * вход паролем тем же адресом (`sessionOne`). У С-л вход паролем невозможен по
 * построению: `before` идёт ДО снятия пароля (вторая сессия заводится входом
 * паролем, пока он есть), а уровень сессии браузера — только «2».
 */
export async function withSeeded<T>(
  testInfo: TestInfo,
  scenario: string,
  kind: SeedKind,
  context: BrowserContext,
  body: (h: Human) => Promise<T>,
  opts: { level?: "1" | "2"; before?: (h: Human) => Promise<void> } = {},
): Promise<T> {
  const seed = await newSeed(testInfo);
  try {
    const human = await seedConfirmedHuman(seed, seedAddress(scenario));
    const keyName = seedKeyName(scenario);
    let factor: SeededSecondFactor | null = null;
    let key: SeededAccessKey | null = null;
    if (kind !== "С-п") factor = await raiseSeedToLevelTwo(seed);
    if (kind === "С-кл" || kind === "С-ккл" || kind === "С-л") {
      key = await seedAccessKey(seed, testInfo.project.use.baseURL, { userVerification: true, name: keyName });
    }
    if ((kind === "С-кл" || kind === "С-л") && factor) {
      await dropSeedSecondFactor(seed, factor);
      factor = null;
    }
    const h: Human = { seed, human, factor, key, keyName };
    if (opts.before) await opts.before(h);
    if (kind === "С-л") {
      expect(opts.level ?? "2", "посев С-л: входа паролем у человека только с ключом нет").toBe("2");
      // Исход записи утверждает её возврат (отметка и одна снятая строка пароля);
      // вход прежним паролем для проверки не делается: отказ входа тратит общую
      // ось источника прогона (`ceremony-budget.ts`), а следствие уже прочитано.
      dropPasswordByRecord(human.email);
    }
    if ((opts.level ?? "2") === "1") {
      await sessionOne(testInfo, human, context);
    } else {
      await transferSession(seed, context);
    }
    return await body(h);
  } finally {
    await seed.dispose();
  }
}

/** Сессия «1» того же человека — вход паролем своим контекстом, носитель — в браузер. */
export async function sessionOne(testInfo: TestInfo, human: SeededHuman, context: BrowserContext): Promise<void> {
  const one = await newSeed(testInfo);
  try {
    const res = await one.submit(LANE.login, "login", { email: human.email, password: human.password });
    expect(
      res.status(),
      `посев: вход паролем ${human.email} — ${res.status()} ${JSON.stringify(lastIssued(one, LANE.login).body)}`,
    ).toBe(200);
    const view = lastIssued(one, LANE.login).body as { session?: { assuranceLevel?: unknown } } | null;
    expect(String(view?.session?.assuranceLevel ?? ""), "посев: вход паролем — сессия «1»").toBe("1");
    await context.clearCookies({ name: SESSION_COOKIE });
    await transferSession(one, context);
  } finally {
    await one.dispose();
  }
}

/** Ещё одна сессия того же человека — «другие сессии» для «Выйти на всех, кроме этой». */
export async function anotherSession(testInfo: TestInfo, human: SeededHuman): Promise<void> {
  const other = await newSeed(testInfo);
  try {
    const res = await other.submit(LANE.login, "login", { email: human.email, password: human.password });
    expect(res.status(), `посев: вторая сессия ${human.email} — ${res.status()}`).toBe(200);
  } finally {
    await other.dispose();
  }
}

/**
 * Код следующей ступени времени: подтверждение посева истратило текущую, и её
 * повтор служба судит повтором (Ф12 Р7). Ждётся УСЛОВИЕ — смена ступени.
 */
export async function nextStepCode(secret: string, after: number = Date.now()): Promise<string> {
  const spent = Math.floor(after / 30_000);
  await expect
    .poll(() => Math.floor(Date.now() / 30_000) > spent, {
      message: "ступень кода по времени не сменилась в пределе ожидания",
      timeout: 40_000,
    })
    .toBe(true);
  return totpCode(secret, Date.now());
}

/** Подтверждение кодом в посеве — шаг «Дано» F8-S7-10. */
export async function seedStepUp(seed: Seed, method: "totp" | "lookup_secret", code: string): Promise<void> {
  const res = await seed.submit(LANE.stepUp, "step-up", { method, code });
  expect(
    res.status(),
    `посев: повышение ${method} — ${res.status()} ${JSON.stringify(lastIssued(seed, LANE.stepUp).body)}`,
  ).toBe(200);
}

/** Адрес, который оператор записи кладёт в текст без экранирования, — только безопасного алфавита. */
const STORE_SAFE_EMAIL = /^[a-z0-9.-]+@[a-z0-9.-]+$/;

/**
 * Форма «после переноса» одной транзакцией (та же, что у посева службы
 * `seed_key_person.py`, `drop_password_sql`): отметка открытого пути
 * восстановления и снятие строки способа «пароль». `ACTIVE` без строки пароля
 * законна в продукте ровно с этой отметкой.
 */
export function dropPasswordSql(email: string): string {
  return (
    "BEGIN;\n" +
    "UPDATE kaname.users SET recovery_path_opened_at = now()\n" +
    ` WHERE email = '${email}' AND recovery_path_opened_at IS NULL\n` +
    "RETURNING 'opened';\n" +
    "DELETE FROM kaname.user_login_methods m USING kaname.users u\n" +
    ` WHERE u.id = m.user_id AND u.email = '${email}' AND m.kind = 'password'\n` +
    "RETURNING m.kind;\n" +
    "COMMIT;\n"
  );
}

/** Снять строку пароля записью; оператор утверждает обе строки возврата. */
function dropPasswordByRecord(email: string): void {
  const exec = process.env.KACHO_KANAME_STORE_EXEC ?? "";
  if (exec === "") {
    conditionNotCreated(
      `${FIXTURE_UNMET_PREFIX} (приёмка F8-S7, §3 посев С-л) исполнителя записи в хранилище службы нет ` +
        "(KACHO_KANAME_STORE_EXEC окружения стенда) — человека только с ключом не построить",
    );
  }
  if (!STORE_SAFE_EMAIL.test(email)) {
    conditionNotCreated(`${FIXTURE_UNMET_PREFIX} посев С-л: адрес вне безопасного алфавита оператора — посев не пишет`);
  }
  const r = spawnSync("bash", ["-c", `${exec} -qtAX -v ON_ERROR_STOP=1 -f -`], {
    input: dropPasswordSql(email),
    encoding: "utf8",
    timeout: 120_000,
  });
  if (r.status !== 0) {
    conditionNotCreated(
      `${FIXTURE_UNMET_PREFIX} посев С-л: исполнитель записи отказал (код ${r.status ?? r.signal ?? r.error?.message}): ` +
        `${(r.stderr ?? "").trim().slice(-300)}`,
    );
  }
  const rows = (r.stdout ?? "").split("\n").filter((l) => l.trim() !== "");
  expect(rows, "посев С-л: форма «после переноса» — отметка открытого пути и одна снятая строка пароля").toEqual([
    "opened",
    "password",
  ]);
}

/**
 * Подстановка ответа производителя (F8 Р9): чтение состояния второго фактора с
 * ключами выключенными на установке — НАСТОЯЩИЙ ответ службы, в котором
 * `accessKeys.available` = `false`, а `webauthn` снят со `stepUpMethods`
 * (SUA Р7: ключи выключены — `webauthn` не доступен). Настоящий производитель — SUA-22.
 */
export async function keysTurnedOff(route: Route): Promise<void> {
  const res = await route.fetch();
  let body: Record<string, unknown> = {};
  try {
    body = (await res.json()) as Record<string, unknown>;
  } catch {
    body = {};
  }
  const methods = Array.isArray(body.stepUpMethods) ? (body.stepUpMethods as unknown[]).map(String) : [];
  body.accessKeys = { available: false };
  body.stepUpMethods = methods.filter((m) => m !== "webauthn");
  await route.fulfill({ response: res, body: JSON.stringify(body) });
}
