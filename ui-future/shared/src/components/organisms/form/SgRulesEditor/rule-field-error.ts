// Отказ края по правилу группы безопасности — в терминах поля, которое оператор
// ВИДИТ.
//
// Край называет путь запроса: `addition_rule_specs[0].ports.from_port`. Такого
// текста нет ни на одном экране, и вызывающему он не помогает — на форме это
// поле подписано «От». Поэтому путь переводится в подпись, а не показывается
// как есть.
//
// Путь приезжает в `google.rpc.BadRequest` (`serviceerr.InvalidArg` кладёт туда
// одно `FieldViolation`), край сериализует детали camelCase'ом
// (`UseProtoNames: false`), а ЗНАЧЕНИЕ поля остаётся строкой, которую записал
// сервис, — то есть snake_case.
//
// Чего здесь НЕ делается: подпись не выдумывается. Отказ, относящийся ко всему
// набору (потолок правил — поле `addition_rule_specs` без хвоста), поля формы не
// имеет, и приписать его видимому полю значило бы указать оператору не туда.

import { errorText } from "@shared/lib/error-presentation";

/** Причина отказа в том виде, в каком её показывают над подвалом формы. */
export interface RuleFieldError {
  /** Подпись поля формы, либо null — когда край поля не назвал или оно не наше. */
  field: string | null;
  /** Текст края, дословно: тон сообщений — часть контракта. */
  message: string;
}

/**
 * Хвост пути запроса → подпись поля в `RuleBody`.
 *
 * Порядок значим: `.cidr_blocks.v4_cidr_blocks[0]` обязан встретить свою запись
 * раньше, чем общее правило источника.
 */
const FIELD_LABELS: ReadonlyArray<readonly [RegExp, string]> = [
  [/\.direction$/, "Направление"],
  [/\.description$/, "Описание"],
  [/\.protocol_name$/, "Имя"],
  [/\.protocol_number$/, "Номер IANA"],
  [/\.ports\.from_port$/, "От"],
  [/\.ports\.to_port$/, "До"],
  [/\.cidr_blocks\.v4_cidr_blocks(\[\d+\])?$/, "IPv4 CIDR"],
  [/\.cidr_blocks\.v6_cidr_blocks(\[\d+\])?$/, "IPv6 CIDR"],
  [/\.security_group_id$/, "Источник"],
  [/\.cidr_group_id$/, "Источник"],
  [/\.target$/, "Источник"],
];

interface Violation {
  field?: unknown;
  description?: unknown;
}

/** Первое `FieldViolation` из деталей ответа, в какой бы форме они ни пришли. */
function firstViolation(details: unknown): Violation | null {
  const blocks = Array.isArray(details) ? details : [details];
  for (const block of blocks) {
    if (typeof block !== "object" || block === null) continue;
    const violations = (block as { fieldViolations?: unknown }).fieldViolations;
    if (!Array.isArray(violations) || violations.length === 0) continue;
    // Явный `unknown`: `Array.isArray` над `unknown` сужает до `any[]`, и без
    // аннотации сюда молча заезжает `any` — то есть проверки ниже компилятор
    // принимал бы на веру, а тело ответа приходит из `JSON.parse`.
    const first: unknown = violations[0];
    // Приведение не нужно: у `Violation` все поля необязательны, поэтому сужение
    // до непустого объекта уже даёт присваиваемый тип.
    if (typeof first === "object" && first !== null) return first;
  }
  return null;
}

/** Подпись поля формы для пути запроса; null — если путь не наш. */
export function ruleFieldLabel(path: string): string | null {
  for (const [pattern, label] of FIELD_LABELS) {
    if (pattern.test(path)) return label;
  }
  return null;
}

export function ruleFieldError(err: unknown): RuleFieldError {
  const violation = firstViolation((err as { details?: unknown } | null)?.details);
  const path = typeof violation?.field === "string" ? violation.field : null;
  const description = typeof violation?.description === "string" ? violation.description : null;

  return {
    field: path ? ruleFieldLabel(path) : null,
    message: description ?? errorText(err),
  };
}
