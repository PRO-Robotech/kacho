// Хелперы вкладки «Токены»: перевод TTL-пресетов/дней в ttl_seconds, расчёт
// состояния срока и словарь ВИДОВ удостоверения. Чистые функции без React/antd —
// тестируются напрямую.
//
// ─────────────────────────────────────────────────────────────────────────────
// ВИД УДОСТОВЕРЕНИЯ РЕШАЕТ ВСЁ ОСТАЛЬНОЕ (#1235)
//
// Контракт (`proto/kaname/cloud/iam/v1/credential_kind.proto`) различает виды по
// тому, ЧЕМ удостоверение себя предъявляет, и у них РАЗНЫЙ срок:
//
//   KEYPAIR — ключевая пара ES256; вызывающий сам подписывает `client_assertion`
//             и обменивает его. `ttl_seconds = 0` означает БЕССРОЧНО.
//   SECRET  — однострочный непрозрачный секрет, предъявляемый КАК ЕСТЬ
//             (`Authorization: Bearer` на крае, поле пароля `docker login -p`).
//             БЕССРОЧНОГО СЕКРЕТА НЕ БЫВАЕТ НИ В КАКОМ НАПИСАНИИ: ноль означает
//             «срок не назван» и разрешается умолчанием политики.
//
// Отсюда правило этого файла: величины и слова выбираются ПО ВИДУ, а «бессрочно»
// произносится только там, где контракт это допускает.

/** Имена видов — ДОСЛОВНО как на проводе (protojson сериализует enum именем). */
export const CREDENTIAL_KIND_KEYPAIR = "CREDENTIAL_KIND_KEYPAIR" as const;
export const CREDENTIAL_KIND_SECRET = "CREDENTIAL_KIND_SECRET" as const;
export const CREDENTIAL_KIND_FEDERATED = "CREDENTIAL_KIND_FEDERATED" as const;
export const CREDENTIAL_KIND_LEGACY = "CREDENTIAL_KIND_LEGACY" as const;

export type CredentialKind =
  | typeof CREDENTIAL_KIND_KEYPAIR
  | typeof CREDENTIAL_KIND_SECRET
  | typeof CREDENTIAL_KIND_FEDERATED
  | typeof CREDENTIAL_KIND_LEGACY;

/** Виды, которые консоль ВПРАВЕ выпустить. */
export type IssuableCredentialKind = typeof CREDENTIAL_KIND_KEYPAIR | typeof CREDENTIAL_KIND_SECRET;

// Верхняя граница ttl_seconds ключевой пары — из proto (value) <=63072000 (~2 года).
export const MAX_TTL_SECONDS = 63072000;
const SECONDS_PER_DAY = 86400;
export const MAX_TTL_DAYS = MAX_TTL_SECONDS / SECONDS_PER_DAY; // 730

// Срок вида SECRET — величины ПОЛИТИКИ, не наши.
//
// Источник — `pkg/tokenpolicy/policy.go`: `SecretCredentialTTLDefault` (30 суток)
// и `SecretCredentialTTLCeiling` (90 суток). Второго написания здесь НЕ заводится
// в смысле правила: это ЗЕРКАЛО для формы, а решение принимает сервер — срок
// сверх потолка он ОТВЕРГАЕТ с именем поля, а не урезает молча. Зеркало нужно,
// чтобы клиент не отправлял заведомо отвергаемое и чтобы величина была НАЗВАНА
// ЧИСЛОМ на экране; расхождение с сервером даёт отказ, а не тихий приём.
export const SECRET_TTL_DEFAULT_DAYS = 30;
export const SECRET_TTL_CEILING_DAYS = 90;
export const SECRET_TTL_DEFAULT_SECONDS = SECRET_TTL_DEFAULT_DAYS * SECONDS_PER_DAY;
export const SECRET_TTL_CEILING_SECONDS = SECRET_TTL_CEILING_DAYS * SECONDS_PER_DAY;

export interface TtlPreset {
  key: string;
  label: string;
  // ttl_seconds; 0 = без срока действия (expires_at не заполняется).
  seconds: number;
}

// Пресеты срока жизни КЛЮЧЕВОЙ ПАРЫ. «custom» (в UI — «Свой срок») не входит
// сюда: там пользователь вводит число дней вручную.
export const TTL_PRESETS: TtlPreset[] = [
  { key: "30d", label: "30 дней", seconds: 30 * SECONDS_PER_DAY },
  { key: "90d", label: "90 дней", seconds: 90 * SECONDS_PER_DAY },
  { key: "1y", label: "1 год", seconds: 365 * SECONDS_PER_DAY },
  { key: "never", label: "Без срока", seconds: 0 },
];

// Пресеты срока СЕКРЕТА. Варианта «Без срока» здесь нет и быть не может:
// бессрочного секрета контракт не производит ни при каком входе, поэтому
// предлагать его значило бы обещать исход, которого не бывает.
export const SECRET_TTL_PRESETS: TtlPreset[] = [
  { key: "7d", label: "7 дней", seconds: 7 * SECONDS_PER_DAY },
  { key: "30d", label: `${SECRET_TTL_DEFAULT_DAYS} дней`, seconds: SECRET_TTL_DEFAULT_SECONDS },
  { key: "90d", label: `${SECRET_TTL_CEILING_DAYS} дней`, seconds: SECRET_TTL_CEILING_SECONDS },
];

/** Варианты срока для вида. */
export function ttlPresetsFor(kind: IssuableCredentialKind): TtlPreset[] {
  return kind === CREDENTIAL_KIND_SECRET ? SECRET_TTL_PRESETS : TTL_PRESETS;
}

/** Потолок срока в секундах для вида. */
export function maxTtlSecondsFor(kind: IssuableCredentialKind): number {
  return kind === CREDENTIAL_KIND_SECRET ? SECRET_TTL_CEILING_SECONDS : MAX_TTL_SECONDS;
}

/** Потолок срока в днях для вида. */
export function maxTtlDaysFor(kind: IssuableCredentialKind): number {
  return maxTtlSecondsFor(kind) / SECONDS_PER_DAY;
}

// Перевод дней в ttl_seconds с ограничением диапазона [0 … потолок вида].
// Непозитивное/нечисловое значение → 0 (у ключевой пары это «бессрочно», у
// секрета — «срок не назван», и его разрешает умолчание сервера).
export function ttlDaysToSeconds(days: number, kind: IssuableCredentialKind = CREDENTIAL_KIND_KEYPAIR): number {
  if (!Number.isFinite(days) || days <= 0) return 0;
  const secs = Math.round(days) * SECONDS_PER_DAY;
  return Math.min(secs, maxTtlSecondsFor(kind));
}

/**
 * Слово, которым вид называют клиенту.
 *
 * Вид, которого край не назвал, НЕ ВЫДУМЫВАЕТСЯ: поле без источника не
 * показывается (канон консоли, правило 9). Пустая строка — это «край промолчал»,
 * и вызывающий обязан не рисовать ячейку, а не подставлять умолчание.
 */
export function credentialKindLabel(kind?: string | null): string {
  switch (kind) {
    case CREDENTIAL_KIND_SECRET:
      return "Секрет";
    case CREDENTIAL_KIND_KEYPAIR:
      return "Ключевая пара";
    case CREDENTIAL_KIND_FEDERATED:
      return "Внешний издатель";
    case CREDENTIAL_KIND_LEGACY:
      return "Прежнего образца";
    default:
      return "";
  }
}

/**
 * РАДИУС предъявительского секрета — сказанный клиенту прямо.
 *
 * Умолчание здесь хуже всего: арендатор заводит секрет «для реестра» и кладёт
 * его в переменную сборочного конвейера, считая, что утечка стоит доступа к
 * образам. На деле секрет предъявляется КАК ЕСТЬ на общем крае, сужения по
 * адресатам у этой полосы нет, и перехвативший получает всё, что может учётная
 * запись. Единственное, чего им сделать нельзя, — выпустить или отозвать
 * удостоверение: это требует входа с подтверждением (уровень 2), а секрет даёт
 * уровень 1.
 */
export const SECRET_RADIUS_NOTICE =
  "Секрет предъявляется как есть и действует не только в реестре: он открывает всё, " +
  "что может эта учётная запись на API платформы. Храните его как пароль и отзывайте " +
  "при малейшем подозрении. Выпустить или отозвать удостоверение им нельзя — это " +
  "требует входа с подтверждением.";

export type ExpiryKind = "none" | "expired" | "active" | "unknown";

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

// Состояние срока действия для бейджа списка:
//   none    — expires_at не задан И вид это допускает → «Бессрочный»;
//   unknown — expires_at не задан у вида, где бессрочного НЕ БЫВАЕТ. Строка
//             секрета всегда несёт заполненный срок, поэтому пустое значение
//             здесь — не «навсегда», а «край не прислал»; называть его
//             бессрочным значило бы утверждать о ресурсе неправду;
//   expired — срок в прошлом → «Истек»;
//   active  — «истекает через X».
export function expiryState(
  expiresAt?: string | null,
  now: number = Date.now(),
  kind?: string | null,
): ExpiryState {
  const noDeadline: ExpiryState =
    kind === CREDENTIAL_KIND_SECRET
      ? { kind: "unknown", label: "Срок не получен" }
      : { kind: "none", label: "Бессрочный" };
  if (!expiresAt) return noDeadline;
  const t = new Date(expiresAt).getTime();
  if (Number.isNaN(t)) return noDeadline;
  const delta = t - now;
  if (delta <= 0) return { kind: "expired", label: "Истек" };
  return { kind: "active", label: `истекает через ${humanizeRemaining(delta)}` };
}
