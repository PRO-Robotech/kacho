// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Бюджет окна РЕГИСТРАЦИЙ источника — запись и суд (kacho#2909).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * С kaname#456 каждая регистрация списывается с окна источника ДО всякой
 * другой проверки — величина окна `authn.login.source-attempts` /
 * `source-window` посадки, полоса своя (регистрация), счёт тот же. Бегун у
 * набора один, значит источник один на весь прогон. Без сторожа первой о
 * переполнении скажет чужая проба, упавшая на отказе фикстуры регистрации
 * (наблюдалось на запросе #2908: пятьдесят первая регистрация получила
 * `registration refused`, и пять проб упали по чужой причине).
 *
 * ПРАВИЛО СЧЁТА. В счёт идёт КАЖДЫЙ отправленный `POST /iam/v1/auth/register`
 * — и принятый, и отвергнутый: служба списывает окно до решения. Счёт НЕ
 * МЕНЬШЕ истинного: регистрация, ответ на которую подставила сама проба
 * (`route.fulfill`), до службы не доходит, но в счёт идёт — сторона ошибки
 * выбрана безопасной, сторож краснеет раньше окна.
 *
 * КТО ПИШЕТ И КТО СУДИТ. Пишут все места, откуда набор регистрирует: каждый
 * контекст браузера (их выдаёт одна фабрика `browser.newContext`, обёрнутая
 * фикстурой; контекст мимо неё уже роняет пробу стражем мест выпуска F8-46) и
 * посев (`specs/ceremony-seed.ts`, свой контекст запросов). Проба сдаёт запись
 * вложением `REGISTRATION_ATTACHMENT`; судит `scripts/registration-budget.ts`
 * по отчёту, когда прогон закончен.
 *
 * Модуль без зависимостей: его читает и фикстура, и сторож, запускаемый голым
 * `node` после прогона.
 */

/** Имя вложения, которым проба сдаёт счёт своих регистраций. */
export const REGISTRATION_ATTACHMENT = "kacho-2909-source-registrations";

/** Глагол регистрации — единственный, что списывает окно регистраций источника. */
export const REGISTRATION_PATH = "/iam/v1/auth/register";

/** Списывает ли обращение окно регистраций источника. */
export function isRegistration(method: string, path: string): boolean {
  return method.toUpperCase() === "POST" && path === REGISTRATION_PATH;
}

/** Запись одной пробы. */
export interface ScenarioRegistrations {
  scenario: string;
  registrations: number;
}

export interface RegistrationVerdict {
  /** Регистраций по прогонам — в порядке прогонов. */
  perRun: number[];
  ok: boolean;
  /** Строки вердикта: перепись и причина, если бюджет превышен. */
  report: string[];
}

/**
 * Судить бюджет: КАЖДЫЙ прогон ≤ `budget`. Суммы по прогонам нет: окно
 * снимается временем, и два прогона в одно окно не складываются.
 * Бюджет — ВХОД, а не умолчание: без объявленной величины вердикта нет.
 */
export function judgeRegistrations(
  runs: ReadonlyArray<ReadonlyArray<ScenarioRegistrations>>,
  budget: number,
): RegistrationVerdict {
  if (!Number.isInteger(budget) || budget < 0) {
    throw new Error(`бюджет окна регистраций не объявлен целым неотрицательным числом: ${String(budget)}`);
  }
  if (runs.length === 0) throw new Error("сторож окна регистраций не получил ни одного прогона — вердикта нет");
  const report: string[] = [];
  const perRun = runs.map((run) => {
    let n = 0;
    for (const s of run) {
      if (!Number.isInteger(s.registrations) || s.registrations < 0) {
        throw new Error(`запись пробы «${s.scenario}» несёт не счёт регистраций: ${String(s.registrations)}`);
      }
      n += s.registrations;
    }
    return n;
  });
  report.push(`[#2909] регистраций за прогон: ${perRun.join(" · ")} · бюджет ${budget} на прогон`);
  const top = new Map<string, number>();
  for (const run of runs) for (const s of run) top.set(s.scenario, (top.get(s.scenario) ?? 0) + s.registrations);
  for (const [s, n] of [...top.entries()].filter(([, n]) => n > 1).sort((a, b) => b[1] - a[1]).slice(0, 10)) {
    report.push(`    ${n} — ${s}`);
  }
  const over = perRun.map((n, i) => ({ n, i })).filter((r) => r.n > budget);
  for (const r of over) {
    report.push(`БЮДЖЕТ ПРЕВЫШЕН: прогон ${r.i + 1} отправил ${r.n} регистраций при бюджете ${budget}`);
  }
  return { perRun, ok: over.length === 0, report };
}

// ─── запись в ходе прогона ─────────────────────────────────────────────────────

/** Ключ записи, сделанной вне пробы: она не теряется, а отдаётся следующей пробе. */
export const OUTSIDE_TEST = "(вне пробы)";

const ledger = new Map<string, number>();

/** Записать регистрацию за пробой (по её идентификатору в прогоне). */
export function noteRegistration(testId: string): void {
  ledger.set(testId, (ledger.get(testId) ?? 0) + 1);
}

/** Забрать счёт пробы — ровно один раз, в конце пробы; записанное вне пробы идёт с ним. */
export function takeRegistrations(testId: string): number {
  const n = (ledger.get(testId) ?? 0) + (ledger.get(OUTSIDE_TEST) ?? 0);
  ledger.delete(testId);
  ledger.delete(OUTSIDE_TEST);
  return n;
}
