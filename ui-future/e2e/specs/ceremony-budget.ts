// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Бюджет оси ИСТОЧНИКА — сторож решения Р7 приёмки F8 (F8-41, ч. 1).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * Потолок темпа полосы формы двухосевой: по адресу (свой у каждого сценария) и
 * по ИСТОЧНИКУ — адресу, выведенному краем. Бегун у набора один, значит ось
 * источника одна на весь прогон, и снимается она только временем. Сценарии
 * тратят её ОТКАЗАМИ, идущими в счёт, и бюджет объявлен числом: без сторожа
 * первым о переполнении скажет чужой сценарий, упавший по чужой причине, — и
 * разбор уйдёт не туда.
 *
 * ПРАВИЛО СЧЁТА — В ТОЙ ПАРЕ, КОТОРУЮ ВИДИТ БРАУЗЕР (Р7). Браузер мест вызова
 * службы не видит; он видит пару «путь · отказ». В счёт идёт ответ `401` с
 * `code` = 16 и текстом `authentication failed` на путях СЕМИ глаголов, пишущих
 * след. Отказ края с тем же кодом и другим текстом (`session ended; sign in
 * again`) в счёт не идёт; тот же отказ на глаголе вне семи (регистрация) — тоже.
 * Счёт по этой паре НЕ МЕНЬШЕ истинного: исчерпание ёмкости проверяющего даёт ту
 * же пару без следа — сторона ошибки выбрана безопасной, сторож краснеет раньше.
 *
 * КТО ПИШЕТ И КТО СУДИТ. Пишут все обращения прогона, какие только тратят ось:
 * контекст страницы пробы (ответы страницы) и посев (ответы своего контекста
 * запросов). Каждая проба вкладывает свою запись в отчёт прогона вложением
 * `BUDGET_ATTACHMENT`; судит — `scripts/ceremony-budget.ts` по отчёту, когда
 * прогон закончен. В файловую систему проба не пишет: запись несёт отчёт.
 *
 * Модуль без зависимостей: его читает и фикстура, и сторож, запускаемый голым
 * `node` после прогона.
 */

/** Имя вложения, которым проба сдаёт свою запись. */
export const BUDGET_ATTACHMENT = "f8-41-source-axis-refusals";

/** Семь глаголов, чей отказ пишет след по оси источника (приёмка F8, Р7). */
export const SOURCE_AXIS_VERBS: readonly string[] = [
  "/iam/v1/auth/login",
  "/iam/v1/auth/password",
  "/iam/v1/auth/recovery/complete",
  "/iam/v1/auth/second-factor/confirm",
  "/iam/v1/auth/second-factor/backup-codes",
  "/iam/v1/auth/second-factor/remove",
  "/iam/v1/auth/step-up",
];

/** Текст отказа, идущего в счёт, — один на все причины. */
export const AUTHENTICATION_FAILED = "authentication failed";

/** Отказ `401` полосы формы — в той форме, в какой его получил выпускающий. */
export interface RecordedRefusal {
  path: string;
  status: number;
  code: number | null;
  message: string;
}

/** Запись одной пробы. */
export interface ScenarioRefusals {
  scenario: string;
  refusals: RecordedRefusal[];
}

/** Записывается ли ответ: `401` на пути полосы формы — любой, судит сторож. */
export function recordableRefusal(path: string, status: number, text: string): RecordedRefusal | null {
  if (status !== 401 || !path.startsWith("/iam/v1/auth/")) return null;
  let code: number | null = null;
  let message = "";
  try {
    const body = JSON.parse(text) as { code?: unknown; message?: unknown };
    code = typeof body.code === "number" ? body.code : null;
    message = typeof body.message === "string" ? body.message : "";
  } catch {
    // Тело не `google.rpc.Status` — записывается как есть; в счёт оно не пойдёт.
  }
  return { path, status, code, message };
}

/** Идёт ли отказ в счёт оси источника. */
export function countsTowardSourceAxis(r: RecordedRefusal): boolean {
  return r.status === 401 && r.code === 16 && r.message === AUTHENTICATION_FAILED && SOURCE_AXIS_VERBS.includes(r.path);
}

export interface BudgetVerdict {
  /** Отказов в счёт по прогонам — в порядке прогонов. */
  perRun: number[];
  total: number;
  /** Сценарий → отказов в счёт по всем прогонам. */
  byScenario: Map<string, number>;
  ok: boolean;
  /** Строки вердикта для человека: перепись и причина, если бюджет превышен. */
  report: string[];
}

/**
 * Судить бюджет: каждый прогон ≤ `budget`, все вместе ≤ `budget × прогонов`.
 * Бюджет — ВХОД, а не умолчание: без объявленной величины вердикта нет.
 */
export function judgeBudget(runs: ReadonlyArray<ReadonlyArray<ScenarioRefusals>>, budget: number): BudgetVerdict {
  if (!Number.isInteger(budget) || budget < 0) {
    throw new Error(`бюджет оси источника не объявлен целым неотрицательным числом: ${String(budget)}`);
  }
  if (runs.length === 0) throw new Error("сторож бюджета не получил ни одного прогона — вердикта нет");
  const byScenario = new Map<string, number>();
  const perRun = runs.map((run) => {
    let n = 0;
    for (const s of run) {
      const counted = s.refusals.filter(countsTowardSourceAxis).length;
      if (counted > 0) byScenario.set(s.scenario, (byScenario.get(s.scenario) ?? 0) + counted);
      n += counted;
    }
    return n;
  });
  const total = perRun.reduce((a, b) => a + b, 0);
  const ceiling = budget * runs.length;
  const overRuns = perRun.map((n, i) => ({ n, i })).filter((r) => r.n > budget);
  const ok = overRuns.length === 0 && total <= ceiling;
  const report = [
    `[F8-41] отказов в счёт оси источника: по прогонам ${perRun.join(" · ")} · всего ${total}` +
      ` · бюджет ${budget} на прогон, ${ceiling} на ${runs.length}`,
    ...[...byScenario.entries()].sort().map(([s, n]) => `    ${n} — ${s}`),
  ];
  if (!ok) {
    for (const r of overRuns) report.push(`БЮДЖЕТ ПРЕВЫШЕН: прогон ${r.i + 1} потратил ${r.n} при бюджете ${budget}`);
    if (total > ceiling)
      report.push(`БЮДЖЕТ ПРЕВЫШЕН: ${runs.length} прогонов потратили ${total} при бюджете ${ceiling}`);
  }
  return { perRun, total, byScenario, ok, report };
}

// ─── запись в ходе прогона ─────────────────────────────────────────────────────

const ledger = new Map<string, RecordedRefusal[]>();

/** Записать отказ за пробой (по её идентификатору в прогоне). */
export function noteRefusal(testId: string, r: RecordedRefusal): void {
  const list = ledger.get(testId) ?? [];
  list.push(r);
  ledger.set(testId, list);
}

/** Забрать запись пробы — ровно один раз, в конце пробы. */
export function takeRefusals(testId: string): RecordedRefusal[] {
  const list = ledger.get(testId) ?? [];
  ledger.delete(testId);
  return list;
}
