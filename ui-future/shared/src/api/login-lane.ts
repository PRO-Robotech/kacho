// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Клиент полосы формы нашей службы доступа — ЕДИНСТВЕННАЯ дверь консоли к
// церемониям личности (приёмка F8, Р1–Р2).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ И ПОЧЕМУ ОДНИМ МЕСТОМ
//
// Служба объявляет глаголы `/iam/v1/auth/*` одним перечнем, край их
// ретранслирует, раздача уводит `^/iam/v1/` на край безусловно. Консоль зовёт
// их отсюда и ни откуда больше: экраны входа, регистрации, выхода, параметров
// учётной записи, подтверждения адреса почты, восстановления доступа и окно
// повышения уровня. Второй клиент тех же глаголов
// разошёлся бы с первым молча — ровно так расходились две копии окна повышения.
//
// ЧЕГО КОНСОЛЬ НЕ ДЕЛАЕТ (Р2). Правило пароля, занятость адреса, годность кода
// и потолок темпа живут в службе. Отказ приходит `google.rpc.Status`
// `{code, message, details}`, и экран показывает `message` ДОСЛОВНО; имя поля
// берётся из `Illegal argument <поле>: …`. Своего правила здесь нет: два места
// об одном предмете устарели бы тихо — в сторону «пускаем то, что служба
// отвергнет» или «отвергаем то, что служба пустила бы».
//
// ПРИЗНАК ФОРМЫ. Каждая меняющая состояние форма предъявляет признак СВОЕГО
// вида (`GET /iam/v1/auth/csrf?form=<вид>`); чужой вид служба отвергает `403`
// с причиной `FORM_TOKEN_REJECTED`. Держатель признака — `FormTokenHolder`:
// экран берёт признак у него и после такого отказа добывает свежий.
//
// ПЕЧЕНЬЯ. Носитель сессии и контекст формы — `httpOnly`, консоль их не читает
// и не пишет: их держит браузер, выдаёт и гасит служба.

import { displayText } from "@shared/lib/display-text";
import type { SecondFactorMethod } from "@shared/lib/step-up-methods";
import { orderedTransport } from "./carrier-order";
import { formContextEpoch, noteFormContextChanged } from "./lane-epochs";
import { refusalActionOf, type RefusalSigns } from "./refusal-action";
import { parseRpcStatus, reasonOfDetails } from "./rpc-status";
import { acrFromChallenge, challengeError, requestFreshPresentation, requestStepUp } from "./step-up";

export { LANE_REASON } from "./lane-reasons";

/** Пути глаголов полосы — точные, как их объявляет служба. */
export const LOGIN_LANE = {
  csrf: "/iam/v1/auth/csrf",
  login: "/iam/v1/auth/login",
  logout: "/iam/v1/auth/logout",
  register: "/iam/v1/auth/register",
  password: "/iam/v1/auth/password",
  // Первый пароль из живой сессии (служба — приёмка FP; экран — приёмка F8,
  // ред. 12, Р13): у человека без пароля; носителя не ставит (её Р5).
  passwordEnroll: "/iam/v1/auth/password/enroll",
  secondFactor: "/iam/v1/auth/second-factor",
  enroll: "/iam/v1/auth/second-factor/enroll",
  confirm: "/iam/v1/auth/second-factor/confirm",
  remove: "/iam/v1/auth/second-factor/remove",
  backupCodes: "/iam/v1/auth/second-factor/backup-codes",
  stepUp: "/iam/v1/auth/step-up",
  // Подтверждение адреса почты (приёмка F6b, Р6 службы): запрос письма с кодом и
  // предъявление кода — оба под сессией человека, адреса в теле нет.
  verifyEmail: "/iam/v1/auth/verify-email",
  verifyEmailConfirm: "/iam/v1/auth/verify-email/confirm",
  // Вход ключом доступа без пароля (приёмка Ф13, Р1; экран — приёмка F8-S4):
  // выдача испытания и предъявление утверждения — оба без сессии, у каждого
  // свой вид признака формы.
  accessKeyBegin: "/iam/v1/auth/access-key/begin",
  accessKeyLogin: "/iam/v1/auth/access-key/login",
  // Восстановление доступа (приёмка Ф5 службы; экран — приёмка F8-S3): запрос
  // кода на адрес почты и предъявление кода с новым паролем — оба без сессии, у
  // каждого свой вид признака формы.
  recovery: "/iam/v1/auth/recovery",
  recoveryComplete: "/iam/v1/auth/recovery/complete",
} as const;

/** Маршрут края «кто за этой сессией». Глаголом полосы не является. */
export const SESSION_IDENTITY_PATH = "/iam/v1/auth/me";

/** Вид признака формы — словарь службы. */
export type FormKind =
  | "login"
  | "logout"
  | "password"
  | "password-enroll"
  | "register"
  | "second-factor"
  | "step-up"
  | "verify-email"
  | "verify-email-confirm"
  | "access-key-begin"
  | "access-key-login"
  | "recovery"
  | "recovery-complete";

/** Способ предъявления второго фактора: код из приложения либо запасной код. */
export type CodeMethod = SecondFactorMethod;

/** Способ церемонии повышения: пароль (свежесть) либо второй фактор (уровень). */
export type StepUpMethod = "password" | CodeMethod;

/**
 * Предъявление второго фактора. `method` — ВЫБОР человека; `null` — не выбран,
 * и тогда способа в теле нет вовсе: умолчания нет (условие C16), отказ «способ
 * не назван» называет служба.
 */
export interface SecondFactorPresentation {
  method: CodeMethod | null;
  code: string;
}

export interface LaneUser {
  id: string;
  email: string;
  displayName: string;
}

/**
 * Сессия — в той форме, в какой её присылают глаголы полосы и ответ края о
 * сессии (camelCase провода; соглашение декодирования ответов личности одно —
 * условие C9). Поле, которого ответ не нёс, ОТСУТСТВУЕТ: признак не рисуется, и
 * «не подтверждён» по умолчанию не бывает (условие C8).
 */
export interface LaneSession {
  expiresAt?: string;
  assuranceLevel?: string;
  emailVerified?: boolean;
}

export interface LaneAssurance {
  level: string;
  level2Reachable: boolean;
  missingForLevel2: string[];
}

export interface SignedIn {
  user: LaneUser;
  session: LaneSession;
}

export interface SecondFactorState {
  totp: { enrolled: boolean; confirmedAt?: string; pendingUntil?: string };
  /** Только у заведённого. */
  backupCodes?: { remaining: number; total: number };
}

export interface Enrollment {
  secret: string;
  otpauthUri: string;
  expiresAt: string;
}

export interface Confirmed {
  backupCodes: string[];
  session: LaneSession;
  assurance: LaneAssurance;
}

export interface Ceremony {
  session: LaneSession;
  assurance: LaneAssurance;
  backupCodesRemaining?: number;
}

/**
 * Поле ответа службы → ввод экрана: закрытая таблица (условие C4).
 *
 * Имя поля служба называет ТОЛЬКО прозой — `Illegal argument <поле>: …`:
 * `details` у отказа о поле пуст (`loginlanehttp.writeError`, `FieldError`).
 * Поэтому разборщик один и якорный, а поле, которого нет в таблице, не
 * отмечает НИ ОДНОГО ввода — «ближайшее похожее» не отмечается никогда, и
 * поля «по умолчанию» нет. Вложенная форма входа (`secondFactor.*`) называет
 * те же вводы с префиксом. Признак формы служба называет, но ввода у него нет:
 * отказ о нём показывается у формы целиком.
 */
const REFUSAL_FIELD_INPUT: Readonly<Record<string, RefusalInput | null>> = {
  email: "email",
  password: "password",
  currentPassword: "currentPassword",
  newPassword: "newPassword",
  code: "code",
  method: "method",
  "secondFactor.code": "code",
  "secondFactor.method": "method",
  csrfToken: null,
};

/** Ввод экрана церемонии, который служба может назвать отказом. */
export type RefusalInput = "email" | "password" | "currentPassword" | "newPassword" | "code" | "method";

const FIELD_REFUSAL = /^Illegal argument (\S+): /;

/** Ввод, названный отказом; `null` — отказ не о вводе либо поле вне таблицы. */
function refusalInput(code: number, message: string): RefusalInput | null {
  if (code !== 3) return null;
  const named = FIELD_REFUSAL.exec(message)?.[1];
  if (named === undefined || !Object.prototype.hasOwnProperty.call(REFUSAL_FIELD_INPUT, named)) return null;
  return REFUSAL_FIELD_INPUT[named];
}

/**
 * Отказ глагола — в той форме, в какой его вернули.
 *
 * `message` — текст отказа, который экран показывает ДОСЛОВНО. Когда тело
 * `google.rpc.Status` не пришло вовсе (обращение не дошло, ответила раздача),
 * текст служба не называла, и консоль называет то, что наблюдала сама, — не
 * выдумывая причины.
 */
export class LaneRefusal extends Error implements RefusalSigns {
  constructor(
    /** HTTP-статус; 0 — ответа не было вовсе. */
    readonly status: number,
    /** `google.rpc.Code`; `null` — тело не `google.rpc.Status`. */
    readonly code: number | null,
    message: string,
    /** `ErrorInfo.reason`; `null` — причины служба не назвала. */
    readonly reason: string | null,
    /** Ввод, названный отказом (таблица выше); `null` — отказ не о вводе. */
    readonly field: RefusalInput | null,
    /** Срок из `Retry-After`, целые секунды ≥ 1; `null` — срока не назвали. */
    readonly retryAfterSeconds: number | null,
    /** Вызов края `WWW-Authenticate` целиком; `null` — вызова нет. */
    readonly wwwAuthenticate: string | null = null,
  ) {
    super(message);
    this.name = "LaneRefusal";
  }

  /** Машинный признак вызова (`error=`): `invalid_token`, `insufficient_user_authentication`. */
  get challenge(): string | null {
    return challengeError(this.wwwAuthenticate);
  }
}

/** Любой отказ шага как отказ полосы: не отказ полосы — называется тем, что наблюдалось. */
export function laneRefusalOf(e: unknown): LaneRefusal {
  if (e instanceof LaneRefusal) return e;
  return new LaneRefusal(0, null, e instanceof Error ? e.message : String(e), null, null, null);
}

/**
 * Срок `Retry-After` — дельта-секунды, и производитель пишет целое не меньше
 * одного всегда (`loginlanehttp.retryAfterSeconds`). Нет заголовка или он не в
 * этой форме — срока нет, и он не выдумывается (условие C11): форма даты,
 * ноль и дробь сроком не считаются.
 */
function retryAfterOf(res: Response): number | null {
  const raw = res.headers?.get?.("Retry-After") ?? null;
  if (raw === null || !/^[1-9]\d*$/.test(raw.trim())) return null;
  return Number(raw.trim());
}

function wwwAuthenticateOf(res: Response): string | null {
  return res.headers?.get?.("WWW-Authenticate") ?? null;
}

/**
 * Текст, которым консоль называет ответ, не несущий ни отказа службы, ни того,
 * что глагол обещал: тело отказа не пришло, либо ответ успеха не той формы.
 * Причины консоль не знает и не выдумывает.
 */
export const NOT_BY_SUBSTANCE_TEXT = "Служба не ответила по существу";

/** Отказ из ответа. Разбор один — на все глаголы и на окно повышения. */
export function refusalOf(res: Response, text: string): LaneRefusal {
  const status = parseRpcStatus(text);
  const www = wwwAuthenticateOf(res);
  if (status) {
    return new LaneRefusal(
      res.status,
      status.code,
      status.message,
      reasonOfDetails(status.details),
      refusalInput(status.code, status.message),
      retryAfterOf(res),
      www,
    );
  }
  // Тела отказа служба не прислала — ответила раздача либо промежуточный узел.
  // Причины консоль не знает и не выдумывает; код ответа остаётся в `status`
  // для того, кто чинит, а не в тексте для того, кто читает экран.
  return new LaneRefusal(res.status, null, NOT_BY_SUBSTANCE_TEXT, null, null, retryAfterOf(res), www);
}

/** Ответ глагола: статус, заголовки и тело — как их прислали. */
interface Answered {
  res: Response;
  text: string;
}

/** Что глагол отдаёт вызывающему из успешного ответа. */
type Reader<T> = (answered: Answered) => T;

function bodyOf<T>({ text }: Answered): T {
  return (text ? JSON.parse(text) : {}) as T;
}

async function exchange<T>(method: "GET" | "POST", path: string, body?: unknown, read: Reader<T> = bodyOf): Promise<T> {
  // Глагол, ставящий носитель, выпускается упорядочением вокруг себя (Р10):
  // обращения вкладки, выпущенные раньше, к его выпуску имеют исход, и новые
  // ждут его исхода. Остальные обращения полосы — обычные обращения вкладки.
  let res: Response;
  try {
    res = await orderedTransport.fetch(
      path,
      {
        method,
        credentials: "same-origin",
        headers:
          body === undefined
            ? { Accept: "application/json" }
            : { Accept: "application/json", "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      },
      { setsCarrier: method === "POST" && SETS_CARRIER.has(path) },
    );
  } catch {
    throw new LaneRefusal(0, null, "Запрос не дошёл до службы: нет соединения", null, null, null);
  }
  const text = await res.text();
  if (!res.ok) throw refusalOf(res, text);
  return read({ res, text });
}

/**
 * Очередь выдачи признаков — ОДНА на вкладку, для всех видов форм (условие C12).
 *
 * Контекст формы у службы один на браузер (печенье `kaname_form`): выдача без
 * контекста заводит новый, выдача с контекстом выдаёт признак в нём. Печенье
 * ставит ответ, а уходит оно с запросами, отправленными ПОСЛЕ него. Поэтому две
 * выдачи, ушедшие рядом, пока контекста нет, заводят два контекста; печенье
 * остаётся от последнего ответа, и признак другой формы служба отвергает
 * `FORM_TOKEN_REJECTED`. Так падала отправка экрана параметров, чей признак
 * выдан не последним ответом: экран открывает формы пароля и второго фактора
 * сразу, и обе добывают признак при открытии (прогон F8 на посадке `own`
 * @9038186d0d5: шесть проб экрана параметров упали этим отказом).
 *
 * Выдача следующего признака начинается за ответом предыдущей — любого вида, —
 * и контекст заводит только первая. Лежит на `globalThis` по той же причине,
 * что и эпохи (`lane-epochs.ts`): `@shared` собирается в каждый модуль своей
 * копией, а окно повышения модуля и экраны каркаса делят одно печенье вкладки.
 */
const ISSUANCE_KEY = Symbol.for("kacho.console.form-token-issuance");

function issuance(): { tail: Promise<unknown> } {
  const g = globalThis as unknown as Record<symbol, { tail: Promise<unknown> } | undefined>;
  let queue = g[ISSUANCE_KEY];
  if (!queue) {
    queue = { tail: Promise.resolve() };
    g[ISSUANCE_KEY] = queue;
  }
  return queue;
}

/** Признак формы данного вида — в очереди выдачи вкладки. */
export function formToken(kind: FormKind): Promise<string> {
  const queue = issuance();
  const issued = queue.tail.then(
    () => issueFormToken(kind),
    () => issueFormToken(kind),
  );
  queue.tail = issued.catch(() => undefined);
  return issued;
}

async function issueFormToken(kind: FormKind): Promise<string> {
  const body = await exchange<{ csrfToken?: unknown }>("GET", `${LOGIN_LANE.csrf}?form=${encodeURIComponent(kind)}`);
  const token = displayText(body.csrfToken);
  if (token === "") {
    throw new LaneRefusal(200, null, "Служба не выдала признак формы", null, null, null);
  }
  return token;
}

/**
 * Держатель признака формы ОДНОГО вида (условие C12).
 *
 * Признак — на ОДНУ отправку: отправка забирает его, и следующая добывает
 * свежий. Держать признак через ответ значило бы предъявлять его и тогда, когда
 * ответ сменил контекст формы. Добывается он ПОСЛЕДОВАТЕЛЬНО: отправки одного
 * держателя идут друг за другом, и выдача следующего признака начинается за
 * ответом предыдущей отправки, а не рядом с ним, — иначе признак, выданный до
 * смены контекста, ушёл бы после неё.
 *
 * Признак, добытый заранее (экран готов отправить сразу), принадлежит контексту
 * формы, в котором его выдали; ответ глагола этой вкладки, сменивший контекст,
 * гасит его, и отправка добывает новый.
 *
 * Отказ `FORM_TOKEN_REJECTED` — ОДИН свежий признак сразу за ответом, и
 * отправку повторяет человек: повтора в цикле нет.
 */
export class FormTokenHolder {
  private pending: { token: Promise<string>; context: number } | null = null;
  private queue: Promise<unknown> = Promise.resolve();

  constructor(readonly kind: FormKind) {}

  /** Признак для следующей отправки: добытый заранее либо добываемый сейчас. */
  get(): Promise<string> {
    if (this.pending && this.pending.context !== formContextEpoch()) this.pending = null;
    if (!this.pending) {
      const context = formContextEpoch();
      const token = formToken(this.kind).catch((e: unknown) => {
        // Не добытый признак не держится: следующая отправка попробует снова.
        if (this.pending?.token === token) this.pending = null;
        throw e;
      });
      this.pending = { token, context };
    }
    return this.pending.token;
  }

  /** Отбросить держимый и добыть свежий СЕЙЧАС. */
  refresh(): Promise<string> {
    this.pending = null;
    return this.get();
  }

  /** Забрать признак для ОДНОЙ отправки — держатель его больше не держит. */
  take(): Promise<string> {
    const token = this.get();
    this.pending = null;
    return token;
  }

  /** Отправка этого держателя — после предыдущей, а не рядом с ней. */
  exclusive<T>(step: () => Promise<T>): Promise<T> {
    const run = this.queue.then(step, step);
    this.queue = run.catch(() => undefined);
    return run;
  }
}

/**
 * Глаголы, ставящие носитель (`SetCookie kaname_session` у службы), — перечень
 * закрыт, десять (приёмка F8, Р10, N17; предъявление кода подтверждения адреса —
 * приёмка F6b, Р6 и Р10 службы: успех несёт новый носитель; вход ключом доступа —
 * приёмка F8-S4, Р6). Пять переписывают дайджест той же записи — прежний
 * носитель с этого момента негоден; пять заводят новую запись.
 * Каждый выпускается упорядочением вокруг себя. Глаголы, носителя не ставящие
 * (признак формы, чтение и заведение второго фактора, выход), упорядочения не
 * получают: иначе «отменено перед глаголом» было бы неотличимо от «отменено при
 * любой отправке». Новый глагол, начавший ставить носитель, входит в перечень
 * тем же изменением, что его экран.
 */
export const SETS_CARRIER: ReadonlySet<string> = new Set([
  "/iam/v1/auth/login",
  "/iam/v1/auth/register",
  "/iam/v1/auth/password",
  "/iam/v1/auth/recovery/complete",
  "/iam/v1/auth/second-factor/confirm",
  "/iam/v1/auth/second-factor/remove",
  "/iam/v1/auth/second-factor/backup-codes",
  "/iam/v1/auth/step-up",
  "/iam/v1/auth/verify-email/confirm",
  // Вход ключом доступа выдаёт сессию той же операцией, что вход паролем (Ф13
  // Р4; приёмка F8-S4, Р6). Испытание входа носителя не ставит — его здесь нет.
  "/iam/v1/auth/access-key/login",
]);

/** Глаголы, меняющие контекст формы (`SetCookie kaname_form` у службы). */
export const CHANGES_FORM_CONTEXT: ReadonlySet<string> = new Set([
  "/iam/v1/auth/login",
  "/iam/v1/auth/register",
  "/iam/v1/auth/recovery/complete",
  "/iam/v1/auth/access-key/login",
]);

function noteAnswered(path: string) {
  if (CHANGES_FORM_CONTEXT.has(path)) noteFormContextChanged();
}

/**
 * Отправить форму с признаком её вида и исполнить действие на отказ (условие C2).
 *
 * `raises` — вправе ли отказ этого глагола вести к церемонии повышения. Глаголы
 * без сессии (вход, регистрация, выход) повышения не просят, а глагол самого
 * повышения — тем более: окно ждало бы само себя. Повтор после повышения —
 * ОДИН; второй отказ отдаётся как есть.
 */
async function submit<T>(
  holder: FormTokenHolder,
  path: string,
  body: Record<string, unknown>,
  raises: boolean,
  replayed = false,
  read: Reader<T> = bodyOf,
): Promise<T> {
  try {
    return await holder.exclusive(async () => {
      const csrfToken = await holder.take();
      try {
        const out = await exchange<T>("POST", path, { ...body, csrfToken }, read);
        noteAnswered(path);
        return out;
      } catch (e) {
        if (e instanceof LaneRefusal && refusalActionOf(e, "ceremony") === "fresh-form-token") {
          await holder.refresh().catch(() => undefined);
        }
        throw e;
      }
    });
  } catch (e) {
    if (!(e instanceof LaneRefusal) || !raises || replayed) throw e;
    const action = refusalActionOf(e, "ceremony");
    if (action === "step-up-freshness" && (await requestFreshPresentation())) {
      return submit<T>(holder, path, body, raises, true, read);
    }
    if (action === "step-up-floor" && (await requestStepUp(acrFromChallenge(e.wwwAuthenticate)))) {
      return submit<T>(holder, path, body, raises, true, read);
    }
    throw e;
  }
}

/** Тело предъявления: способа нет, пока человек его не выбрал. */
function presentation(factor: SecondFactorPresentation): Record<string, unknown> {
  return factor.method === null ? { code: factor.code } : { method: factor.method, code: factor.code };
}

export const loginLane = {
  login(holder: FormTokenHolder, form: { email: string; password: string; secondFactor?: SecondFactorPresentation }) {
    const body: Record<string, unknown> = { email: form.email, password: form.password };
    if (form.secondFactor) body.secondFactor = presentation(form.secondFactor);
    return submit<SignedIn>(holder, LOGIN_LANE.login, body, false);
  },
  register(holder: FormTokenHolder, form: { email: string; password: string }) {
    return submit<SignedIn>(holder, LOGIN_LANE.register, { email: form.email, password: form.password }, false);
  },
  logout(holder: FormTokenHolder) {
    return submit<Record<string, never>>(holder, LOGIN_LANE.logout, {}, false);
  },
  changePassword(holder: FormTokenHolder, form: { currentPassword: string; newPassword: string }) {
    return submit<{ session: LaneSession }>(
      holder,
      LOGIN_LANE.password,
      { currentPassword: form.currentPassword, newPassword: form.newPassword },
      true,
    );
  },
  /**
   * Первый пароль человеку без пароля (приёмка F8, ред. 12, Р13). Тело — РОВНО
   * `{newPassword, csrfToken}`: текущего пароля у такого человека нет. Правило
   * пароля и «пароль уже есть» судит служба, консоль — нет. Глагол требует
   * свежего предъявления, поэтому отказ свежести ведёт к повышению и одному
   * повтору тем же паролем. Носителя не ставит и контекста формы не меняет — в
   * перечнях упорядочения его нет (F8-67): членство в них — равенство пути
   * целиком, а `/iam/v1/auth/password` — лишь начало этой строки.
   */
  enrollPassword(holder: FormTokenHolder, form: { newPassword: string }) {
    return submit<{ session: LaneSession }>(holder, LOGIN_LANE.passwordEnroll, { newPassword: form.newPassword }, true);
  },
  secondFactorState() {
    return exchange<SecondFactorState>("GET", LOGIN_LANE.secondFactor);
  },
  enroll(holder: FormTokenHolder) {
    return submit<Enrollment>(holder, LOGIN_LANE.enroll, {}, true);
  },
  confirm(holder: FormTokenHolder, code: string) {
    return submit<Confirmed>(holder, LOGIN_LANE.confirm, { code }, true);
  },
  remove(holder: FormTokenHolder, factor: SecondFactorPresentation) {
    return submit<Ceremony>(holder, LOGIN_LANE.remove, presentation(factor), true);
  },
  regenerateBackupCodes(holder: FormTokenHolder, factor: SecondFactorPresentation) {
    return submit<Confirmed>(holder, LOGIN_LANE.backupCodes, presentation(factor), true);
  },
  stepUp(holder: FormTokenHolder, form: { method: "password"; password: string } | SecondFactorPresentation) {
    const body = "password" in form ? { method: "password", password: form.password } : presentation(form);
    return submit<Ceremony>(holder, LOGIN_LANE.stepUp, body, false);
  },
  /**
   * Запрос письма с новым кодом (приёмка F6b, Р6 и Р9). Тело — только признак
   * формы: адрес служба берёт у человека сессии. Срок до следующего письма —
   * ТОЛЬКО заголовок `Retry-After` ответа; нет заголовка — срока нет, и консоль
   * своего не выдумывает (Р8).
   */
  requestAddressConfirmation(holder: FormTokenHolder) {
    return submit<AddressConfirmationRequested>(holder, LOGIN_LANE.verifyEmail, {}, false, false, ({ res }) => ({
      retryAfterSeconds: retryAfterOf(res),
    }));
  },
  /**
   * Предъявление кода из письма — как введён: своего суждения о содержимом кода
   * консоль не выносит, приведение делает служба (Р8, Р7 службы).
   */
  confirmAddress(holder: FormTokenHolder, code: string) {
    return submit<{ session: LaneSession }>(holder, LOGIN_LANE.verifyEmailConfirm, { code }, false);
  },
  /**
   * Выдача испытания входа ключом (Ф13 Р1, Р9). Тело — только признак формы
   * вида `access-key-begin`. Ответ отдаётся КАК ПРИСЛАН: его разбор в параметры
   * церемонии браузера — дело кодека (`access-key.ts`), который ничего не
   * подставляет. Глагол носителя не ставит и контекста формы не меняет — в
   * перечнях упорядочения его нет (F8-S4, Р6).
   */
  accessKeyBegin(holder: FormTokenHolder) {
    return submit<unknown>(holder, LOGIN_LANE.accessKeyBegin, {}, false);
  },
  /**
   * Предъявление утверждения браузера (Ф13 Р1, Р4). `credential` — закрытая
   * проекция ответа браузера (`assertionBodyOf`), признак — вида
   * `access-key-login`. Успех выдаёт сессию той же операцией, что вход паролем.
   */
  accessKeyLogin(holder: FormTokenHolder, credential: Record<string, unknown>) {
    return submit<SignedIn>(holder, LOGIN_LANE.accessKeyLogin, { credential }, false);
  },
  /**
   * Запрос кода восстановления на адрес почты (Ф5; приёмка F8-S3, Р3). Тело —
   * РОВНО `{email, csrfToken}`: разбор службы строгий. Ответ `200 {}` на любой
   * исход — заведён адрес или нет, — и экран его не различает (Р2). Носителя не
   * ставит и контекста формы не меняет — в перечнях упорядочения его нет (Р7).
   */
  requestRecovery(holder: FormTokenHolder, form: { email: string }) {
    return submit<Record<string, never>>(holder, LOGIN_LANE.recovery, { email: form.email }, false);
  },
  /**
   * Предъявление кода с новым паролем (Ф5; приёмка F8-S3, Р3, Р5). Тело — РОВНО
   * `{email, code, newPassword, csrfToken}`, поля `secondFactor` нет: лишнее поле
   * служба отвергла бы `400`. Код и пароль уходят как введены — своего суждения о
   * содержимом консоль не выносит. Успех отвечает как вход: сессия и новый
   * контекст формы.
   */
  completeRecovery(holder: FormTokenHolder, form: { email: string; code: string; newPassword: string }) {
    return submit<SignedIn>(
      holder,
      LOGIN_LANE.recoveryComplete,
      { email: form.email, code: form.code, newPassword: form.newPassword },
      false,
    );
  },
};

/** Исход запроса письма: срок до следующего — из ответа службы, либо `null`. */
export interface AddressConfirmationRequested {
  retryAfterSeconds: number | null;
}

/** Человек за сессией — в форме провода ответа края (`/iam/v1/auth/me`). */
export interface SessionUser {
  id: string;
  email: string;
  displayName: string;
  /** Вид субъекта; `""` — край его не назвал. */
  subjectType: string;
  permissions: string[];
}

/** Сессия есть: человек и его сессия. */
export interface SessionIdentity {
  kind: "present";
  user: SessionUser;
  /** `null` — край сессии не описал (посадка без нашей сессии). */
  session: LaneSession | null;
}

/**
 * Ответ на вопрос «есть ли сессия» — ТРИ исхода по типу (условие C6):
 *
 *   • `present` — край назвал человека;
 *   • `absent`  — край ответил `{"user":null}`: сессии нет. Негодный носитель
 *                 для консоли — то же состояние, что его отсутствие, и чем
 *                 именно носитель негоден, она не различает и различать не
 *                 вправе (F8-12);
 *   • `unknown` — спросить не удалось: нет ответа, ответ не `2xx`, тело не
 *                 разобрано. Этот исход НЕ рисует «вы вышли» и не уводит на
 *                 вход: человек с живой сессией, чей край на миг не ответил,
 *                 иначе терял бы экран, на котором работал.
 *
 * НЕГОДНЫЙ НОСИТЕЛЬ КРАЙ ОТВЕЧАЕТ НЕ `{"user":null}`. «Кто я» у края — точка
 * предъявления носителя: негодный он отвергает ОДНИМ отказом `401` на все
 * причины, с вызовом `error="invalid_token"` (приёмка KA1, Р2), и гасит печенье.
 * Консоль спрашивает ОДИН раз снова: погашенный носитель второй вопрос уже не
 * несёт, и край отвечает `{"user":null}` — сессии нет; отвечает тем же отказом —
 * `unknown`. Третьего вопроса нет. Прежде первый такой отказ читался как
 * `unknown`, и экран входа при подменённом носителе показывал отказ края и
 * «Проверить снова» вместо формы (прогон F8-12 на посадке `own` @9038186d0d5).
 *
 * НЕ ОТВЕТИЛ АВТОРИТЕТ КРАЯ — НЕ ОТКАЗ. Тогда край отвечает `503` без вызова и
 * без печенья (приёмка KA1, Р1): носитель цел, и это `unknown` с первого
 * вопроса — повторять нечего, и на вход консоль не уводит.
 */
export type SessionAnswer = SessionIdentity | { kind: "absent" } | { kind: "unknown"; refusal: LaneRefusal };

function sessionOf(raw: unknown): LaneSession | null {
  if (!raw || typeof raw !== "object") return null;
  const o = raw as Record<string, unknown>;
  const session: LaneSession = {};
  if (typeof o.expiresAt === "string") session.expiresAt = o.expiresAt;
  if (typeof o.assuranceLevel === "string") session.assuranceLevel = o.assuranceLevel;
  else if (typeof o.assuranceLevel === "number") session.assuranceLevel = String(o.assuranceLevel);
  if (typeof o.emailVerified === "boolean") session.emailVerified = o.emailVerified;
  return session;
}

/**
 * Есть ли сессия — по ответу края, а не по чтению чужого поставщика. ЕДИНСТВЕННЫЙ
 * читатель `/iam/v1/auth/me` в консоли: страж экрана входа, экран параметров,
 * каркас и контекст личности спрашивают здесь (условие C7).
 */
export async function sessionIdentity(): Promise<SessionAnswer> {
  const first = await askSession();
  if (first.kind !== "unknown" || first.refusal.challenge !== "invalid_token") return first;
  return askSession();
}

async function askSession(): Promise<SessionAnswer> {
  let res: Response;
  try {
    res = await orderedTransport.fetch(SESSION_IDENTITY_PATH, {
      credentials: "same-origin",
      headers: { Accept: "application/json" },
    });
  } catch {
    return { kind: "unknown", refusal: new LaneRefusal(0, null, UNKNOWN_SESSION_TEXT, null, null, null) };
  }
  const text = await res.text().catch(() => "");
  if (!res.ok) return { kind: "unknown", refusal: refusalOf(res, text) };
  let body: unknown;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = undefined;
  }
  if (!body || typeof body !== "object" || !("user" in body)) {
    return { kind: "unknown", refusal: new LaneRefusal(res.status, null, UNKNOWN_SESSION_TEXT, null, null, null) };
  }
  const user = body.user;
  if (user === null) return { kind: "absent" };
  if (typeof user !== "object" || Array.isArray(user)) {
    return { kind: "unknown", refusal: new LaneRefusal(res.status, null, UNKNOWN_SESSION_TEXT, null, null, null) };
  }
  const u = user as Record<string, unknown>;
  return {
    kind: "present",
    user: {
      id: displayText(u.id),
      email: displayText(u.email),
      displayName: displayText(u.displayName),
      subjectType: displayText(u.subjectType),
      permissions: Array.isArray(u.permissions) ? u.permissions.map(String) : [],
    },
    session: sessionOf((body as { session?: unknown }).session),
  };
}

/** Что сказать, когда о сессии спросить не удалось: причины консоль не выдумывает. */
export const UNKNOWN_SESSION_TEXT = "Не удалось узнать, есть ли у браузера сессия: служба не ответила по существу";
