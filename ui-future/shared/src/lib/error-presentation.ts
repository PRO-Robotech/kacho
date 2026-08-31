// Turning a failed request into something to show, without adding meaning the
// server did not send.
//
// The gateway hides existence: when a caller may not see a resource it answers
// NOT_FOUND with a message byte-identical to a real miss, precisely so that the
// two cases cannot be told apart. A 404 in this UI therefore means "absent or
// invisible" and nothing narrower. Rendering it as "this resource does not
// exist" invents a fact; rendering it as "you have no access" invents the
// opposite one. We state both and settle neither.
//
// A 403 carries no such ambiguity and is left alone.

import { ApiError } from "@shared/api/client";
import { displayText } from "@shared/lib/display-text";
import { grpcCodeLabel } from "@shared/lib/grpc-status";
import { QUOTA_VALUES_SET_BY, kindLabel } from "@shared/lib/quota-view";

/**
 * Полоса отказа по пределу. Различаются ДЕЙСТВИЕМ, а не оттенком.
 *
 * У двух первых действие принадлежит АДМИНИСТРАТОРУ (поднять предел, завести
 * предел), у третьей — САМОМУ ВЫЗЫВАЮЩЕМУ (подождать). Производитель завёл её
 * отдельным признаком осознанно и записал почему: повтор по объёму не пройдёт
 * никогда, повтор по темпу пройдёт в следующем окне.
 */
export type QuotaLane = "exceeded" | "not_provisioned" | "rate_exceeded";

export interface QuotaRefusal {
  lane: QuotaLane;
  /** Машинное имя вида, как прислал сервер; `null` — не назван. */
  kind: string | null;
  /** Человеческое имя вида; равно `kind`, когда каталог его ещё не знает. */
  label: string | null;
  limit: number | null;
  /** `null` — значения нет вовсе. Ноль — законная величина и не то же самое. */
  used: number | null;
  /** Адрес витрины квот; `null` — носитель не назван, и адрес не подделывается. */
  href: string | null;
}

/** Куда идти за действующими пределами. */
export const QUOTA_SHOWCASE_HINT =
  "Действующие пределы, занятое и источник каждого значения — в разделе «Квоты» проекта.";

export type ErrorStatus = "403" | "404" | "500" | "warning" | "error";

export interface ErrorPresentation {
  status: ErrorStatus;
  title: string;
  /** The backend's own text, kept verbatim — the message tone is contract. */
  subTitle: string | null;
  /** Extra line shown under the message; null when there is nothing to add. */
  note: string | null;
  /**
   * Числовой код протокола и HTTP-статус — для того, кто чинит, а не для того,
   * кто читает экран. `«5: Region not found»` начиналось с величины, которая
   * ничего не сообщает арендатору, и занимало место, куда смотрят первым.
   */
  devDetail: string | null;
  ambiguousNotFound: boolean;
  /** Отказ по пределу ресурсов; `null` — отказ не про предел. */
  quota: QuotaRefusal | null;
}

/** The only thing a 404 lets us claim. */
export const NOT_FOUND_IS_AMBIGUOUS =
  "Сервер отвечает одинаково, когда ресурса не существует и когда он недоступен вашей учётной записи, — по этому ответу различить эти случаи нельзя.";

/**
 * Заголовок отказа — СЛОВАМИ, а не числом протокола.
 *
 * Здесь стояли голые «403», «404», «500». Число адресовано тому, кто чинит, и
 * ничего не сообщает тому, кто смотрит на экран: человек, упёршийся в отказ,
 * читает крупную цифру и не узнаёт из неё ни что произошло, ни что делать. Тот
 * же довод уже применён строкой ниже к самому сообщению — код протокола ушёл в
 * подсказку, — но заголовок оставался числом.
 *
 * Слова выбраны по СОСТОЯНИЮ, а не по переводу цифры: «Недостаточно прав»
 * говорит о причине, тогда как «Запрещено» звучит как обвинение, а «Ошибка
 * доступа» — как поломка, хотя система работает верно.
 *
 * Само число не потеряно: оно остаётся в подсказке рядом с сообщением, откуда
 * его достанет поддержка.
 */
const TITLES: Record<ErrorStatus, string> = {
  "403": "Недостаточно прав",
  "404": "Не найдено",
  "500": "Сервер не смог ответить",
  warning: "Внимание",
  error: "Ошибка",
};

/**
 * Отказ в правах ОБЪЯСНЯЕТСЯ, а не цитируется.
 *
 * Край на отказе присылает своё имя права — `permission denied: iam.limits.list`.
 * Это точная и совершенно бесполезная строка для того, кто на неё смотрит: она
 * называет внутреннее имя проверки, а не то, чего человеку не хватает и к кому
 * идти. Заодно она раскрывает устройство проверок прав любому, кто в них упрётся.
 *
 * Текст сервера остаётся контрактом и не теряется — он уходит в подсказку, туда
 * же, где уже лежит код протокола (см. `devDetail` ниже). На экране остаётся
 * фраза, отвечающая на вопрос, который человек задаёт в этот момент.
 */
const FORBIDDEN_EXPLANATION =
  "Этот раздел доступен администраторам платформы. Если доступ нужен по работе — запросите его у администратора вашей организации.";

/**
 * Учётные данные не приняты — край отвечает `unauthenticated: credentials
 * required`. Строка точна и вызывающему бесполезна: она не говорит ни что
 * произошло (срок сессии истёк), ни что делать (войти заново).
 */
const REAUTH_EXPLANATION =
  "Сессия истекла или учётные данные не приняты. Войдите заново, чтобы продолжить работу.";

/**
 * Похоже ли сообщение на цитату внутренней проверки прав.
 *
 * ЗАПАСНОЙ ПУТЬ, А НЕ ОСНОВНОЙ. Полоса решается признаком (`REFUSALS` ниже);
 * этот разбор остаётся ровно для отказов, пришедших БЕЗ признака, — их
 * производят места, до которых `ErrorInfo` ещё не дошёл. Вывод полосы из
 * английской фразы молча вернул бы пустоту при первой же смене тона, а тон —
 * часть контракта и меняется осознанно.
 */
export function looksLikePermissionToken(message: string | null): boolean {
  if (!message) return false;
  // Имя права — точечный путь без пробелов (`iam.limits.list`); фраза для
  // человека пробелы содержит всегда.
  return /(^|\s)[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){2,}\s*$/.test(message.trim()) || /permission denied/i.test(message);
}

/**
 * ОТКАЗ ПО ПРЕДЕЛУ ОБЪЯСНЯЕТСЯ, А НЕ ЦИТИРУЕТСЯ (#1605) — тот же ход, что у 403.
 *
 * Производитель отказа один на всю платформу и говорит по-английски машинными
 * именами: `project prj-1 has reached its limit of 5 vpc.network`. Строка точна
 * и контрактна — и для упёршегося бесполезна: вид назван машинным именем, кто
 * задал величину, не сказано, куда идти, не сказано. Следующего шага у клиента
 * не остаётся, а отказ по пределу без следующего шага неотличим от сбоя.
 *
 * Текст сервера остаётся контрактом и НЕ ТЕРЯЕТСЯ — уходит в подсказку, туда же,
 * где уже лежит код протокола.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * КЛЮЧ — ТОКЕН ПРИЗНАКА, А НЕ HTTP-СТАТУС
 *
 * Полосы две, и они приходят РАЗНЫМИ статусами: «место кончилось» —
 * `RESOURCE_EXHAUSTED` (429), «потолок не назван ни на одной области» —
 * `FAILED_PRECONDITION` (400). Ключ по 429 потерял бы вторую полосу целиком,
 * а именно у неё действие администратора другое: не поднять предел, а завести.
 * По той же причине здесь не разбирается ПРОЗА сообщения: полосы различаются
 * машинно по `reason` (`api-conventions.md` §By-lane code-split), а вывод вида
 * из английской фразы молча вернул бы пустоту при первой же смене тона.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ДЕГРАДАЦИЯ ОСМЫСЛЕННАЯ, А НЕ ВСЁ-ИЛИ-НИЧЕГО
 *
 * Величины (вид, предел, занятое, носитель) приезжают в `metadata` признака.
 * Сегодня производители присылают признак и НЕ присылают величин. Отказ, умеющий
 * показать только полный набор, на неполном показал бы хуже прежнего, поэтому
 * показывается то, что приехало: чего сервер не назвал, то не выдумывается —
 * ни вид, ни предел, ни адрес витрины.
 */
/**
 * Вердикт консоли по одному машинному признаку отказа.
 *
 * `passthrough` — РЕШЕНИЕ, а не пропуск: текст производителя уже называет
 * следующий шаг («другой префикс, а не починка кодировщика запроса»), и
 * подменять его своей фразой значило бы потерять названные им координаты.
 * Отсутствие записи решением НЕ является — гейт
 * `ui-future/deploy/console_refusal_reason_coverage_test.go` требует вердикт по
 * каждому производимому токену и падает на записи, которой нечего разбирать.
 */
type RefusalVerdict =
  | { kind: "quota"; lane: QuotaLane }
  | { kind: "explain"; title?: string; text: string }
  | { kind: "passthrough" };

/**
 * ВЕРДИКТ ПО КАЖДОМУ ПРИЗНАКУ, КОТОРЫЙ ПРОИЗВОДИТ ПЛАТФОРМА (#1736).
 *
 * Перечень не выписан — он СВЕРЯЕТСЯ с производителями гейтом, в обе стороны:
 * токен без вердикта есть полоса, о которой арендатору показывают, что
 * придётся; вердикт без производителя есть послабление, пережившее свой
 * предмет.
 *
 * Полосы потока (`SUBSCRIPTION_*`) сюда НЕ входят намеренно: их читает хаб
 * подписки, а не разбор отказа запроса, и второе место об одном предмете
 * разошлось бы с ним молча.
 */
const REFUSALS: Record<string, RefusalVerdict> = {
  // Край и служба личности называют внутреннее имя проверки — оно раскрывает
  // устройство проверок и не отвечает ни на один вопрос смотрящего.
  AUTHZ_DENIED: { kind: "explain", text: FORBIDDEN_EXPLANATION },
  AUTHN_REQUIRED: { kind: "explain", title: "Требуется вход", text: REAUTH_EXPLANATION },

  QUOTA_EXCEEDED: { kind: "quota", lane: "exceeded" },
  QUOTA_NOT_PROVISIONED: { kind: "quota", lane: "not_provisioned" },
  QUOTA_RATE_EXCEEDED: { kind: "quota", lane: "rate_exceeded" },

  // Ниже — полосы, чей текст производителя уже называет следующий шаг: он
  // несёт координату (имя слота, идентификатор подсети, вид ресурса), которую
  // общая фраза потеряла бы. Английский язык этих текстов — предмет ОТДЕЛЬНОЙ
  // задачи (#1691), и закрывается он здесь же: с вердиктом у каждого признака
  // перевод становится сменой одной записи с `passthrough` на `explain`, без
  // единой правки сервиса.
  MEMBERSHIP_CARRIES_RIGHTS: { kind: "passthrough" },
  INVALID_RESOURCE_ID: { kind: "passthrough" },
  RESOURCE_NOT_FOUND: { kind: "passthrough" },
  PEER_RESOURCE_MISSING: { kind: "passthrough" },
  PEER_RESOURCE_STATE: { kind: "passthrough" },
  PEER_UNAVAILABLE: { kind: "passthrough" },
  SUBNET_NO_FREE_ADDRESS: { kind: "passthrough" },
  ALLOCATION_CONTENDED: { kind: "passthrough" },
  EXTERNAL_ADDRESS_UNAVAILABLE: { kind: "passthrough" },
  SUBNET_CIDR_RESERVED: { kind: "passthrough" },
};

/** Носитель, при котором «занято» относится к проекту, — он же адресует витрину. */
const CARRIER_PROJECT = "project";

/**
 * Значение из `metadata` признака.
 *
 * Имена ключей выбирает ПРОИЗВОДИТЕЛЬ, а `ErrorInfo.metadata` — `map<string,string>`,
 * ключи которого protojson отдаёт ДОСЛОВНО (это данные, а не имена полей). Поэтому
 * читаются оба написания, как это уже делает разбор причин отказа в правах.
 * Ключа, которого нет, — `null`: неназванное не подменяется догадкой.
 */
function metaText(md: Record<string, unknown>, ...keys: string[]): string | null {
  for (const k of keys) {
    const v = md[k];
    if (typeof v === "string" && v !== "") return v;
    if (typeof v === "number" && Number.isFinite(v)) return String(v);
  }
  return null;
}

/**
 * Число из `metadata`.
 *
 * НОЛЬ — ЗАКОННАЯ ВЕЛИЧИНА и не то же самое, что «не назвали»: приравняв их,
 * отказ промолчал бы там, где сервер сказал. Нечисловое значение — `null`,
 * а не `NaN`, который дальше печатался бы на экране арендатора.
 */
function metaNumber(md: Record<string, unknown>, ...keys: string[]): number | null {
  const raw = metaText(md, ...keys);
  if (raw === null) return null;
  const n = Number(raw);
  return Number.isFinite(n) ? n : null;
}

/** Деталь `google.rpc.ErrorInfo` ответа; `null` — признака нет вовсе. */
function errorInfoOf(details: unknown): { reason: string; metadata: Record<string, unknown> } | null {
  if (!Array.isArray(details)) return null;
  for (const d of details) {
    if (!d || typeof d !== "object") continue;
    const reason = (d as { reason?: unknown }).reason;
    if (typeof reason !== "string" || reason === "") continue;
    const rawMd = (d as { metadata?: unknown }).metadata;
    return { reason, metadata: rawMd && typeof rawMd === "object" ? (rawMd as Record<string, unknown>) : {} };
  }
  return null;
}

/** Собирает отказ по пределу из уже найденной детали. */
function quotaRefusalOf(md: Record<string, unknown>, lane: QuotaLane): QuotaRefusal {
  const kind = metaText(md, "kind", "quota_kind", "quotaKind");
  const carrierType = metaText(md, "carrier_type", "carrierType");
  const carrierId = metaText(md, "carrier_id", "carrierId");

  return {
    lane,
    // Человеческое имя берётся из ЕДИНСТВЕННОГО словаря видов (витрина квот),
    // а не из второй копии рядом: копия разошлась бы с витриной молча, и один
    // предмет назывался бы на экране двумя словами.
    label: kind === null ? null : kindLabel(kind),
    kind,
    limit: metaNumber(md, "limit"),
    used: metaNumber(md, "used"),
    href: carrierType === CARRIER_PROJECT && carrierId ? `/projects/${carrierId}/quotas` : null,
  };
}

const QUOTA_TITLES: Record<QuotaLane, string> = {
  exceeded: "Предел исчерпан",
  not_provisioned: "Предел не задан",
  rate_exceeded: "Слишком часто",
};

/** Сколько именно — ровно из того, что сервер назвал. */
function quotaAmount(q: QuotaRefusal): string {
  if (q.limit === null) return "";
  return q.used === null ? `: ${q.limit}` : `: занято ${q.used} из ${q.limit}`;
}

/**
 * Объяснение отказа.
 *
 * Действие администратора у полос РАЗНОЕ, и это несущее различие: свести их в
 * одну фразу значило бы послать читателя искать, что понизить, там, где не
 * назначено ничего.
 */
function quotaExplanation(q: QuotaRefusal): string {
  const on = q.label === null ? "на этот вид ресурсов" : `на «${q.label}»`;
  if (q.lane === "rate_exceeded") {
    // Действие принадлежит ВЫЗЫВАЮЩЕМУ, а не администратору: величину поднимать
    // не нужно и незачем — темп восстановится в следующем окне. Приклеив сюда
    // «кто задаёт пределы», мы послали бы человека к администратору за тем,
    // чего тот не решает.
    return `Слишком частые запросы ${on}. Повторите через несколько секунд.`;
  }
  const head =
    q.lane === "exceeded"
      ? `В проекте достигнут предел ${on}${quotaAmount(q)}.`
      : `Предел ${on} не задан ни на одной области — создание отклонено.`;
  const action = q.lane === "exceeded" ? "поднять предел может он" : "завести предел может он";
  return `${head} ${QUOTA_VALUES_SET_BY} — ${action}.`;
}

function statusFromHttp(status: number): ErrorStatus {
  if (status === 404) return "404";
  if (status === 403) return "403";
  if (status >= 500) return "500";
  if (status >= 400) return "warning";
  return "error";
}

/** A fetch that never reached the server — distinct from anything it answered. */
export function isNetworkFailure(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  const m = err.message.toLowerCase();
  return (
    err.name === "TypeError" &&
    (m.includes("failed to fetch") || m.includes("networkerror") || m.includes("load failed"))
  );
}

/**
 * Что показать разработчику: имя и число кода протокола плюс HTTP-статус.
 * Код, который край не прислал (не-JSON тело шлюза, пустой ответ), не выдумывается —
 * остаётся один статус.
 */
function devDetailOf(err: ApiError): string {
  const code = grpcCodeLabel(err.code);
  return code === null ? `HTTP ${err.status}` : `${code} · HTTP ${err.status}`;
}

/**
 * Текст отказа для пользователя — одной строкой, для тоста и inline-подписи.
 *
 * Единственная реализация на всю консоль: прежде каждое место складывало свою
 * («`${err.code}: ${err.message}`» в 52 местах), и числовой код протокола уезжал
 * на экран арендатора. Код теперь живёт в `presentError().devDetail`.
 */
export function errorText(err: unknown): string {
  const p = presentError(err);
  // ТОСТ — САМАЯ ЧАСТАЯ ПОВЕРХНОСТЬ ОТКАЗА ПО ПРЕДЕЛУ: мутации сообщают о себе
  // им, а не экраном отказа. Поэтому «куда идти» приклеивается здесь, и только
  // к нему: оговорка на КАЖДОМ отказе (например, о неоднозначности промаха)
  // превратила бы тост в шум, и её перестали бы читать вместе с полезной.
  if (p.quota !== null && p.subTitle !== null && p.note !== null) return `${p.subTitle} ${p.note}`;
  return p.subTitle ?? "Ошибка";
}

export function presentError(err: unknown): ErrorPresentation {
  if (err === null || err === undefined) {
    return {
      status: "error",
      title: TITLES.error,
      subTitle: null,
      note: null,
      devDetail: null,
      ambiguousNotFound: false,
      quota: null,
    };
  }

  if (isNetworkFailure(err)) {
    return {
      status: "500",
      title: "Сеть недоступна",
      subTitle: "Не удалось связаться с сервером. Проверьте подключение или повторите позже.",
      note: null,
      devDetail: null,
      ambiguousNotFound: false,
      quota: null,
    };
  }

  if (err instanceof ApiError) {
    const status = statusFromHttp(err.status);
    const ambiguousNotFound = status === "404";
    const dev = devDetailOf(err);

    // ПОЛОСА РЕШАЕТСЯ ПРИЗНАКОМ, А НЕ ПРОЗОЙ (#1736). Проза — запасной путь
    // ниже, ровно для отказов, пришедших без признака.
    const info = errorInfoOf(err.details);
    const verdict = info === null ? undefined : REFUSALS[info.reason];

    // Текст производителя — контракт, и он НЕ ТЕРЯЕТСЯ ни в одной ветке: уходит
    // в подсказку рядом с кодом протокола, откуда его достаёт поддержка.
    const devWithMessage = [dev, err.message].filter(Boolean).join(" · ") || null;

    if (verdict?.kind === "quota" && info !== null) {
      const quota = quotaRefusalOf(info.metadata, verdict.lane);
      return {
        status,
        title: QUOTA_TITLES[quota.lane],
        subTitle: quotaExplanation(quota),
        note: QUOTA_SHOWCASE_HINT,
        devDetail: devWithMessage,
        ambiguousNotFound: false,
        quota,
      };
    }

    if (verdict?.kind === "explain") {
      return {
        status,
        title: verdict.title ?? TITLES[status],
        subTitle: verdict.text,
        note: ambiguousNotFound ? NOT_FOUND_IS_AMBIGUOUS : null,
        devDetail: devWithMessage,
        ambiguousNotFound,
        quota: null,
      };
    }

    // ЗАПАСНОЙ ПУТЬ. Признака нет (или он неизвестен — тогда красен гейт
    // покрытия, а не экран): решаем прозой, как решали до появления признаков.
    // Вердикт `passthrough` попадает сюда же осознанно — он и означает «показать
    // текст сервера», а не «показать вместо него общую фразу».
    const hideToken = verdict === undefined && status === "403" && looksLikePermissionToken(err.message);
    return {
      status,
      title: TITLES[status],
      subTitle: hideToken ? FORBIDDEN_EXPLANATION : err.message,
      note: ambiguousNotFound ? NOT_FOUND_IS_AMBIGUOUS : null,
      devDetail: hideToken ? devWithMessage : dev,
      ambiguousNotFound,
      quota: null,
    };
  }

  return {
    status: "error",
    title: TITLES.error,
    subTitle: err instanceof Error ? err.message : displayText(err),
    note: null,
    devDetail: null,
    ambiguousNotFound: false,
    quota: null,
  };
}
