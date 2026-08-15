// Код ошибки в том виде, в каком его присылает край, — и ничего сверх того.
//
// Край собирает REST-ответ из `google.rpc.Status`, где `code` объявлен `int32`.
// В JSON это ЧИСЛО: `{"code":5,"message":"Not Found"}`. Тот же `google.rpc.Status`
// приходит внутри `Operation.error` — там HTTP-статус ответа 200, и код остаётся
// единственным, что говорит о причине отказа.
//
// Почему этот разбор живёт отдельным местом, а не сравнением по месту вызова:
// сравнение со строковым литералом (`code === "7"`) ложно ВСЕГДА, и заметить это
// нельзя ни компилятором (объявление типа он принимает на веру), ни пробой,
// которая кормит разбор объектом собственного сочинения. Одно место, один
// словарь, один предикат — второй разошёлся бы с первым молча.
//
// Форму записи мы не выбираем за отправителя: принимаются число, числовая строка
// и каноническое имя. Всё, чего нет в словаре, — `null`, а не догадка.

/** Канонические коды gRPC (`google.rpc.Code`). */
export const GrpcCode = {
  OK: 0,
  Cancelled: 1,
  Unknown: 2,
  InvalidArgument: 3,
  DeadlineExceeded: 4,
  NotFound: 5,
  AlreadyExists: 6,
  PermissionDenied: 7,
  ResourceExhausted: 8,
  FailedPrecondition: 9,
  Aborted: 10,
  OutOfRange: 11,
  Unimplemented: 12,
  Internal: 13,
  Unavailable: 14,
  DataLoss: 15,
  Unauthenticated: 16,
} as const;

export type GrpcCodeValue = (typeof GrpcCode)[keyof typeof GrpcCode];

/** Имя кода в той форме, в какой его пишет `google.rpc.Code`. */
const CODE_NAMES: Record<number, string> = {
  0: "OK",
  1: "CANCELLED",
  2: "UNKNOWN",
  3: "INVALID_ARGUMENT",
  4: "DEADLINE_EXCEEDED",
  5: "NOT_FOUND",
  6: "ALREADY_EXISTS",
  7: "PERMISSION_DENIED",
  8: "RESOURCE_EXHAUSTED",
  9: "FAILED_PRECONDITION",
  10: "ABORTED",
  11: "OUT_OF_RANGE",
  12: "UNIMPLEMENTED",
  13: "INTERNAL",
  14: "UNAVAILABLE",
  15: "DATA_LOSS",
  16: "UNAUTHENTICATED",
};

const CODE_BY_NAME: Record<string, number> = Object.fromEntries(
  Object.entries(CODE_NAMES).map(([n, name]) => [name, Number(n)]),
);

function codeFromScalar(raw: unknown): number | null {
  if (typeof raw === "number") {
    return Number.isInteger(raw) && raw in CODE_NAMES ? raw : null;
  }
  if (typeof raw !== "string" || raw === "") return null;
  const byName = CODE_BY_NAME[raw.toUpperCase()];
  if (byName !== undefined) return byName;
  // Числовая строка принимается только целиком: «7» — код, «7 ошибок» — нет.
  if (!/^\d+$/.test(raw)) return null;
  const n = Number(raw);
  return n in CODE_NAMES ? n : null;
}

/**
 * Код gRPC из чего угодно, что его несёт: самого значения, `ApiError`,
 * `Operation.error`. Не распознал — `null`, а не «неизвестная ошибка».
 */
export function grpcCodeOf(value: unknown): number | null {
  if (value === null || value === undefined) return null;
  if (typeof value === "number" || typeof value === "string") return codeFromScalar(value);
  if (typeof value !== "object") return null;
  const raw = (value as { code?: unknown }).code;
  return codeFromScalar(raw);
}

/** Имя кода: `PERMISSION_DENIED`. Вне словаря — `null`. */
export function grpcCodeName(value: unknown): string | null {
  const code = grpcCodeOf(value);
  return code === null ? null : CODE_NAMES[code];
}

/**
 * Подпись для РАЗРАБОТЧИКА: `NOT_FOUND (5)`. Пользователю она не адресована —
 * ему адресовано сообщение сервера.
 */
export function grpcCodeLabel(value: unknown): string | null {
  const code = grpcCodeOf(value);
  return code === null ? null : `${CODE_NAMES[code]} (${code})`;
}
