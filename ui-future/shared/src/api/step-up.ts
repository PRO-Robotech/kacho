// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Мост между ОТКАЗОМ края по полу уровня уверенности и ОКНОМ подтверждения.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ОН СУЩЕСТВУЕТ (#1213)
//
// Окно повторного подтверждения регистрировало свой обработчик в контексте
// личности — и ЧИТАТЕЛЯ у этой записи не было ни одного во всём дереве консоли.
// Значение писали и не читали: окно не открывалось НИКОГДА, а значит уровень из
// консоли поднять было нечем, сколько бы способов ни включали настройки службы
// личности. Класс назван в конвенциях: «значение, которое ПИШУТ и не ЧИТАЮТ» —
// его нет ни в одном ответе и ни в одном объекте, поэтому и не видно ниоткуда.
//
// Мост нужен потому, что читателем обязан стать клиент API — обычный модуль без
// дерева React, — а обработчик живёт в компоненте. Регистрация здесь заменяет
// импорт компонента клиентом, которого быть не может.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ ОТКАЗОМ ПО ПОЛУ, А ЧТО НЕТ
//
// Ровно то, что край объявляет вызовом RFC 9470: `401` И заголовок
// `WWW-Authenticate` с `error="insufficient_user_authentication"`. Обычная
// неаутентифицированность даёт тот же код и ДРУГОЙ заголовок — принимать её за
// отказ по полу нельзя: окно открывалось бы на каждом отказе и превратилось бы
// в шум, а человеку предлагалось бы подтвердить личность, которой у него сейчас
// нет вовсе.

/**
 * Просьба подтвердить личность — и ЧЕЙ это отказ.
 *
 * Два источника просьбы несут разные требования, и отличить их по одному
 * необязательному уровню нельзя (#1274, круг 1 ревью):
 *
 *   • `floor` — вызов края RFC 9470. Край требует уровня (`acr_values`) либо
 *     свежести второго фактора (один `max_age`). И то и другое закрывает ТОЛЬКО
 *     второй фактор: пароль уровня не поднимает и свежим второй фактор не
 *     делает, повтор действия получил бы тот же отказ;
 *   • `freshness` — отказ службы `SESSION_NOT_FRESH` на глаголе параметров
 *     учётной записи: годится любое предъявление, и пароль тоже.
 *
 * Прежде просьба была одним `acr?: string`, и его отсутствие значило обе вещи
 * сразу: вызов края без уровня открывал ветвь пароля.
 */
export type StepUpRequest = { cause: "floor"; acr?: string } | { cause: "freshness" };

/** Обработчик, поднимающий уровень. Отвергает обещание, если не удалось. */
export type StepUpRequester = (request: StepUpRequest) => Promise<void>;

let requester: StepUpRequester | null = null;

/**
 * Объявить, кто умеет поднять уровень. Зовёт контекст личности, когда окно
 * подтверждения смонтировано, и снимает объявление при размонтировании.
 *
 * Мёртвое окно не должно отвечать за живые запросы, поэтому снятие обязательно.
 */
export function setStepUpRequester(fn: StepUpRequester | null): void {
  requester = fn;
  // Церемонию, которую вёл снятый или заменённый обработчик, некому довести:
  // её ждущие получают «не состоялось» и отдают исходный отказ, а не висят.
  if (inFlight && inFlight.by !== fn) {
    inFlight.abandon();
    inFlight = null;
  }
}

/**
 * Вызов повышения из ответа края.
 *
 * Ответ читается ЧЕРЕЗ НЕОБЯЗАТЕЛЬНЫЙ доступ намеренно: `fetch` подменяем, и
 * подменённый ответ заголовков может не отдавать вовсе. Направление
 * снисходительности здесь ОДНО и оно безопасное — «заголовка нет» означает
 * «повышения не просили», то есть исходный отказ отдаётся как есть. Обратного
 * послабления (счесть повышение состоявшимся) эта функция не производит ни при
 * каком входе.
 */
export function challengeOf(res: { headers?: { get?: (n: string) => string | null } }): string | null {
  const get = res.headers?.get;
  if (typeof get !== "function") return null;
  return get.call(res.headers, "WWW-Authenticate");
}

/**
 * Машинный признак вызова — значение `error=` заголовка `WWW-Authenticate`
 * (RFC 6750 §3): `invalid_token` — носитель негоден либо сессия кончилась,
 * `insufficient_user_authentication` — пол уровня (RFC 9470). `null` — вызова
 * нет либо он без признака.
 */
export function challengeError(wwwAuthenticate: string | null): string | null {
  if (!wwwAuthenticate) return null;
  const m = /(?:^|[\s,])error="([^"]*)"/.exec(wwwAuthenticate);
  return m && m[1] !== "" ? m[1] : null;
}

/** Уровень, которого край требует, — из вызова RFC 9470. */
export function acrFromChallenge(wwwAuthenticate: string | null): string | undefined {
  if (!wwwAuthenticate) return undefined;
  const m = /acr_values="([^"]*)"/.exec(wwwAuthenticate);
  return m && m[1] !== "" ? m[1] : undefined;
}

/**
 * Попросить поднять уровень.
 *
 * Возвращает `true` ТОЛЬКО когда подтверждение действительно состоялось.
 * Некому подтвердить, человек отменил, церемония не прошла — `false`, и
 * вызывающий обязан отдать исходный отказ как есть. Молчаливый `true` здесь
 * означал бы повтор запроса, за который никто не поручился.
 */
export async function requestStepUp(acr?: string): Promise<boolean> {
  return ask({ cause: "floor", acr });
}

/**
 * Попросить предъявить себя заново по отказу службы о свежести сессии.
 * Возвращает то же, что `requestStepUp`, и по той же причине.
 */
export async function requestFreshPresentation(): Promise<boolean> {
  return ask({ cause: "freshness" });
}

/**
 * Церемония, которая идёт СЕЙЧАС (условие C19). Повышение — единственный
 * полёт: одновременные отказы ждут ОДНУ церемонию, и каждый вызывающий потом
 * повторяет своё исходное действие один раз. Прежде второй вызов перезаписывал
 * ожидающее обещание первого в окне — первый запрос не повторялся и не падал,
 * а висел.
 *
 * Просьба другого рода (свежесть против пола) во время идущей церемонии ждёт её
 * исхода и затем спрашивается сама: пол закрывает только второй фактор, и
 * прошедшая свежесть паролем его не удовлетворяет.
 */
let inFlight: {
  request: StepUpRequest;
  by: StepUpRequester;
  outcome: Promise<boolean>;
  abandon: () => void;
} | null = null;

function sameNeed(a: StepUpRequest, b: StepUpRequest): boolean {
  if (a.cause !== b.cause) return false;
  return a.cause === "freshness" || (b.cause === "floor" && a.acr === b.acr);
}

async function ask(request: StepUpRequest): Promise<boolean> {
  while (inFlight) {
    const current = inFlight;
    if (sameNeed(current.request, request)) return current.outcome;
    await current.outcome;
    if (inFlight === current) inFlight = null;
  }
  const fn = requester;
  if (!fn) return false;
  let abandon: () => void = () => undefined;
  const abandoned = new Promise<boolean>((resolve) => {
    abandon = () => resolve(false);
  });
  const outcome = Promise.race([
    (async () => {
      try {
        await fn(request);
        return true;
      } catch {
        return false;
      }
    })(),
    abandoned,
  ]);
  const flight = { request, by: fn, outcome, abandon };
  inFlight = flight;
  try {
    return await outcome;
  } finally {
    if (inFlight === flight) inFlight = null;
  }
}
