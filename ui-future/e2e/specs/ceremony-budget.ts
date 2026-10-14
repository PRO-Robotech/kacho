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
 * `code` = 16 и текстом отказа службы (`SERVICE_REFUSAL_TEXTS`) на путях СЕМИ
 * глаголов, пишущих след. Текстов три, а не один: служба различает ими форму
 * запроса и глагол (вход с полем `secondFactor`, завершение восстановления), но
 * не причину, — и след по оси пишет каждый из них. Отказ КРАЯ в удостоверении (приёмка KA1, Р2) несёт тот же код и тот же
 * текст — и в счёт не идёт: след пишет служба, а край отвечает раньше неё.
 * Различает их причина тела — `AUTHN_REQUIRED` несёт только край
 * (`gateway/internal/authnrefusal`), у отказа службы `details` пуст. Тот же отказ
 * на глаголе вне семи (регистрация) в счёт тоже не идёт. Счёт НЕ МЕНЬШЕ
 * истинного: исчерпание ёмкости проверяющего даёт ту же пару без следа, а
 * неизвестная причина идёт в счёт — сторона ошибки выбрана безопасной, сторож
 * краснеет раньше.
 *
 * КТО ПИШЕТ И КТО СУДИТ. Пишут все обращения прогона, какие только тратят ось:
 * контекст страницы пробы (ответы страницы) и посев (ответы своего контекста
 * запросов). Каждая проба вкладывает свою запись в отчёт прогона вложением
 * `BUDGET_ATTACHMENT`; судит — `scripts/ceremony-budget.ts` по отчёту, когда
 * прогон закончен. В файловую систему проба не пишет: запись несёт отчёт.
 *
 * ДОЛЯ S4 (приёмка F8-S4, Р8, F8S4-15) — на ТОМ ЖЕ счёте неверных
 * предъявлений, своим числом и своей ручкой. Попытка входа ключом (Ф13 Р9) —
 * выдача испытания (`POST /iam/v1/auth/access-key/begin` → `200`) и каждый
 * единый отказ входа ключом (`POST /iam/v1/auth/access-key/login` → `401` ·
 * `code` 16 · `authentication failed` без причины края). Не попытка — признак
 * формы, успешный вход и отказы `400`, `403`, `429`. Выдачу испытания запись
 * прежде не видела вовсе (она писала только `401`), поэтому запись теперь
 * берёт и этот `200` — и только его; к доле F8-41 он не прибавляет ничего:
 * её правило читает только `401` на семи глаголах.
 *
 * Модуль без зависимостей, кроме разборщика тела консоли (`rpc-status.ts`, сам
 * без зависимостей): его читает и фикстура, и сторож, запускаемый голым `node`
 * после прогона, — поэтому путь импорта с расширением.
 */

import { parseRpcStatus, reasonOfDetails } from "../../shared/src/api/rpc-status.ts";
import { ACCESS_NOT_RESTORED, AUTHENTICATION_FAILED, LOGIN_WITH_SECOND_FACTOR_FAILED } from "./lane-texts.ts";

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

/**
 * Тексты отказа службы, идущего в счёт. Каждый — один на все причины своей
 * формы запроса; перечень закрыт: текст вне него в счёт не идёт.
 */
export const SERVICE_REFUSAL_TEXTS: readonly string[] = [
  AUTHENTICATION_FAILED,
  LOGIN_WITH_SECOND_FACTOR_FAILED,
  ACCESS_NOT_RESTORED,
];

/** Выдача испытания входа ключом (Ф13): `200` на нём — попытка доли S4. */
export const ACCESS_KEY_BEGIN = "/iam/v1/auth/access-key/begin";

/** Вход ключом (Ф13): единый отказ `401` на нём — попытка доли S4. */
export const ACCESS_KEY_LOGIN = "/iam/v1/auth/access-key/login";

/** Причина отказа КРАЯ в удостоверении (приёмка KA1, Р2): след оси он не пишет. */
export const EDGE_REFUSAL_REASON = "AUTHN_REQUIRED";

/** Отказ `401` полосы формы — в той форме, в какой его получил выпускающий. */
export interface RecordedRefusal {
  path: string;
  status: number;
  code: number | null;
  message: string;
  /**
   * `ErrorInfo.reason` тела; `null` — причины не назвали. Записи прежних
   * отчётов поля не несут — для счёта это то же, что `null`.
   */
  reason?: string | null;
}

/** Запись одной пробы. */
export interface ScenarioRefusals {
  scenario: string;
  refusals: RecordedRefusal[];
}

/** Записывается ли ответ: `401` на пути полосы формы — любой, судит сторож. */
export function recordableRefusal(path: string, status: number, text: string): RecordedRefusal | null {
  if (status !== 401 || !path.startsWith("/iam/v1/auth/")) return null;
  // Разборщик тела — тот же, что у экранов консоли. Тело не `google.rpc.Status` —
  // записывается без кода и текста; в счёт оно не пойдёт.
  const body = parseRpcStatus(text);
  if (!body) return { path, status, code: null, message: "", reason: null };
  return { path, status, code: body.code, message: body.message, reason: reasonOfDetails(body.details) };
}

/**
 * Записывается ли ответ полосы: `401` на пути полосы формы (как прежде) и
 * выдача испытания входа ключом `200` (доля S4). Любой другой ответ — нет.
 */
export function recordableAttempt(path: string, status: number, text: string): RecordedRefusal | null {
  if (path === ACCESS_KEY_BEGIN && status === 200) return { path, status, code: null, message: "", reason: null };
  return recordableRefusal(path, status, text);
}

/** Нужно ли ответ читать телом, чтобы записать: решается по пути и статусу, до тела. */
export function mayRecord(path: string, status: number): boolean {
  return (status === 401 && path.startsWith("/iam/v1/auth/")) || (status === 200 && path === ACCESS_KEY_BEGIN);
}

/** Идёт ли отказ в счёт оси источника. */
export function countsTowardSourceAxis(r: RecordedRefusal): boolean {
  return (
    r.status === 401 &&
    r.code === 16 &&
    SERVICE_REFUSAL_TEXTS.includes(r.message) &&
    r.reason !== EDGE_REFUSAL_REASON &&
    SOURCE_AXIS_VERBS.includes(r.path)
  );
}

/** Идёт ли запись в долю S4 (приёмка F8-S4, Р8): выдача испытания либо единый отказ входа ключом. */
export function countsTowardAccessKeyShare(r: RecordedRefusal): boolean {
  if (r.path === ACCESS_KEY_BEGIN) return r.status === 200;
  return (
    r.path === ACCESS_KEY_LOGIN &&
    r.status === 401 &&
    r.code === 16 &&
    r.message === AUTHENTICATION_FAILED &&
    r.reason !== EDGE_REFUSAL_REASON
  );
}

/** Доля счёта неверных предъявлений: чьё правило счёта и чья ручка. */
export interface SourceShare {
  /** Сценарий-держатель доли — им начинаются строки вердикта. */
  label: string;
  /** Ручка бюджета доли в шаге работы. */
  env: string;
  /** Что считать: «отказов», «попыток». */
  unit: string;
  counts: (r: RecordedRefusal) => boolean;
}

/** Доля F8-41: отказы семи глаголов F8 Р7. */
export const CEREMONY_SHARE: SourceShare = {
  label: "F8-41",
  env: "KACHO_CEREMONY_FAILURE_BUDGET",
  unit: "отказов в счёт оси источника",
  counts: countsTowardSourceAxis,
};

/** Доля S4: выдачи испытания и единые отказы входа ключом (приёмка F8-S4, Р8). */
export const ACCESS_KEY_SHARE: SourceShare = {
  label: "F8S4-15",
  env: "KACHO_ACCESS_KEY_SOURCE_BUDGET",
  unit: "попыток входа ключом в доле S4",
  counts: countsTowardAccessKeyShare,
};

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
export function judgeBudget(
  runs: ReadonlyArray<ReadonlyArray<ScenarioRefusals>>,
  budget: number,
  share: SourceShare = CEREMONY_SHARE,
): BudgetVerdict {
  if (!Number.isInteger(budget) || budget < 0) {
    throw new Error(`бюджет доли ${share.label} не объявлен целым неотрицательным числом: ${String(budget)}`);
  }
  if (runs.length === 0) throw new Error("сторож бюджета не получил ни одного прогона — вердикта нет");
  const byScenario = new Map<string, number>();
  const perRun = runs.map((run) => {
    let n = 0;
    for (const s of run) {
      const counted = s.refusals.filter(share.counts).length;
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
    `[${share.label}] ${share.unit}: по прогонам ${perRun.join(" · ")} · всего ${total}` +
      ` · бюджет ${budget} на прогон, ${ceiling} на ${runs.length}`,
    ...[...byScenario.entries()].sort().map(([s, n]) => `    ${n} — ${s}`),
  ];
  if (!ok) {
    for (const r of overRuns)
      report.push(`БЮДЖЕТ ПРЕВЫШЕН [${share.label}]: прогон ${r.i + 1} потратил ${r.n} при бюджете ${budget}`);
    if (total > ceiling)
      report.push(`БЮДЖЕТ ПРЕВЫШЕН [${share.label}]: ${runs.length} прогонов потратили ${total} при бюджете ${ceiling}`);
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
