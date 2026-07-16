// Хелперы вкладки «Токены» сервисного аккаунта: перевод TTL-пресетов/дней в
// ttl_seconds и расчет состояния срока действия токена (для бейджа в списке).
// Чистые функции без React/antd — тестируются напрямую.

// Верхняя граница ttl_seconds — из proto (value) <=63072000 (~2 года).
export const MAX_TTL_SECONDS = 63072000;
const SECONDS_PER_DAY = 86400;
export const MAX_TTL_DAYS = MAX_TTL_SECONDS / SECONDS_PER_DAY; // 730

export interface TtlPreset {
  key: string;
  label: string;
  // ttl_seconds; 0 = без срока действия (expires_at не заполняется).
  seconds: number;
}

// Пресеты срока жизни токена для модалки создания. «custom» (в UI — «Свой срок»)
// не входит сюда: там пользователь вводит число дней вручную.
export const TTL_PRESETS: TtlPreset[] = [
  { key: "30d", label: "30 дней", seconds: 30 * SECONDS_PER_DAY },
  { key: "90d", label: "90 дней", seconds: 90 * SECONDS_PER_DAY },
  { key: "1y", label: "1 год", seconds: 365 * SECONDS_PER_DAY },
  { key: "never", label: "Без срока", seconds: 0 },
];

// Перевод дней в ttl_seconds с ограничением диапазона [0 … MAX_TTL_SECONDS].
// Непозитивное/нечисловое значение → 0 (бессрочно).
export function ttlDaysToSeconds(days: number): number {
  if (!Number.isFinite(days) || days <= 0) return 0;
  const secs = Math.round(days) * SECONDS_PER_DAY;
  return Math.min(secs, MAX_TTL_SECONDS);
}

export type ExpiryKind = "none" | "expired" | "active";

export interface ExpiryState {
  kind: ExpiryKind;
  label: string;
}

// Человекочитаемый остаток до истечения (минуты/часы/дни).
function humanizeRemaining(ms: number): string {
  const mins = Math.floor(ms / 60000);
  if (mins < 60) return `${Math.max(mins, 1)} мин`;
  const hours = Math.floor(mins / 60);
  if (hours < 48) return `${hours} ч`;
  const days = Math.floor(hours / 24);
  return `${days} дн`;
}

// Состояние срока действия токена для бейджа списка:
//   none    — expires_at не задан → «Бессрочный»;
//   expired — срок в прошлом → «Истек»;
//   active  — «истекает через X».
export function expiryState(expiresAt?: string | null, now: number = Date.now()): ExpiryState {
  if (!expiresAt) return { kind: "none", label: "Бессрочный" };
  const t = new Date(expiresAt).getTime();
  if (Number.isNaN(t)) return { kind: "none", label: "Бессрочный" };
  const delta = t - now;
  if (delta <= 0) return { kind: "expired", label: "Истек" };
  return { kind: "active", label: `истекает через ${humanizeRemaining(delta)}` };
}
