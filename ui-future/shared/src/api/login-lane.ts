// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Клиент полосы формы нашей службы доступа — ЕДИНСТВЕННАЯ дверь консоли к
// церемониям личности (приёмка F8, Р1–Р2).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ И ПОЧЕМУ ОДНИМ МЕСТОМ
//
// Служба объявляет тринадцать глаголов `/iam/v1/auth/*` одним перечнем, край их
// ретранслирует, раздача уводит `^/iam/v1/` на край безусловно. Консоль зовёт
// их отсюда и ни откуда больше: экраны входа, регистрации, выхода, параметров
// учётной записи и окно повышения уровня. Второй клиент тех же глаголов
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

/** Пути глаголов полосы — точные, как их объявляет служба. */
export const LOGIN_LANE = {
  csrf: "/iam/v1/auth/csrf",
  login: "/iam/v1/auth/login",
  logout: "/iam/v1/auth/logout",
  register: "/iam/v1/auth/register",
  password: "/iam/v1/auth/password",
  secondFactor: "/iam/v1/auth/second-factor",
  enroll: "/iam/v1/auth/second-factor/enroll",
  confirm: "/iam/v1/auth/second-factor/confirm",
  remove: "/iam/v1/auth/second-factor/remove",
  backupCodes: "/iam/v1/auth/second-factor/backup-codes",
  stepUp: "/iam/v1/auth/step-up",
} as const;

/** Маршрут края «кто за этой сессией». Глаголом полосы не является. */
export const SESSION_IDENTITY_PATH = "/iam/v1/auth/me";

/** Вид признака формы — словарь службы. */
export type FormKind = "login" | "logout" | "password" | "register" | "second-factor" | "step-up";

/** Способ предъявления второго фактора: код из приложения либо запасной код. */
export type CodeMethod = SecondFactorMethod;

/** Способ церемонии повышения: пароль (свежесть) либо второй фактор (уровень). */
export type StepUpMethod = "password" | CodeMethod;

export interface SecondFactorPresentation {
  method: CodeMethod;
  code: string;
}

export interface LaneUser {
  id: string;
  email: string;
  displayName: string;
}

export interface LaneSession {
  expiresAt: string;
  assuranceLevel: string;
  emailVerified: boolean;
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

/** Причины отказа, на которые консоль отвечает ДЕЙСТВИЕМ, а не только текстом. */
export const LANE_REASON = {
  formTokenRejected: "FORM_TOKEN_REJECTED",
  tooManyAttempts: "TOO_MANY_ATTEMPTS",
  sessionNotFresh: "SESSION_NOT_FRESH",
  secondFactorNotEnrolled: "SECOND_FACTOR_NOT_ENROLLED",
} as const;

/**
 * Отказ глагола — в той форме, в какой его вернули.
 *
 * `message` — текст отказа, который экран показывает ДОСЛОВНО. Когда тело
 * `google.rpc.Status` не пришло вовсе (обращение не дошло, ответила раздача),
 * текст служба не называла, и консоль называет то, что наблюдала сама, — не
 * выдумывая причины.
 */
export class LaneRefusal extends Error {
  constructor(
    /** HTTP-статус; 0 — ответа не было вовсе. */
    readonly status: number,
    /** `google.rpc.Code`; `null` — тело не `google.rpc.Status`. */
    readonly code: number | null,
    message: string,
    /** `ErrorInfo.reason`; `null` — причины служба не назвала. */
    readonly reason: string | null,
    /** Поле формы из `Illegal argument <поле>: …`; `null` — отказ не о поле. */
    readonly field: string | null,
    /** Срок из `Retry-After`, секунды; `null` — заголовка нет. */
    readonly retryAfterSeconds: number | null,
  ) {
    super(message);
    this.name = "LaneRefusal";
  }
}

const FIELD_REFUSAL = /^Illegal argument ([A-Za-z][A-Za-z0-9.]*): /;

function reasonOf(details: unknown): string | null {
  if (!Array.isArray(details)) return null;
  for (const d of details) {
    if (d && typeof d === "object" && typeof (d as { reason?: unknown }).reason === "string") {
      return (d as { reason: string }).reason;
    }
  }
  return null;
}

function retryAfterOf(res: Response): number | null {
  const raw = res.headers?.get?.("Retry-After") ?? null;
  if (raw === null || !/^\d+$/.test(raw.trim())) return null;
  return Number(raw.trim());
}

/** Отказ из ответа. Разбор один — на все глаголы и на окно повышения. */
export function refusalOf(res: Response, text: string): LaneRefusal {
  let parsed: unknown = null;
  try {
    parsed = text ? JSON.parse(text) : null;
  } catch {
    parsed = null;
  }
  const body = parsed as { code?: unknown; message?: unknown; details?: unknown } | null;
  if (body && typeof body.code === "number" && typeof body.message === "string") {
    const message = body.message;
    const field = body.code === 3 ? (FIELD_REFUSAL.exec(message)?.[1] ?? null) : null;
    return new LaneRefusal(res.status, body.code, message, reasonOf(body.details), field, retryAfterOf(res));
  }
  // Тела отказа служба не прислала — ответила раздача либо промежуточный узел.
  // Причины консоль не знает и не выдумывает; код ответа остаётся в `status`
  // для того, кто чинит, а не в тексте для того, кто читает экран.
  return new LaneRefusal(res.status, null, "Служба не ответила по существу", null, null, retryAfterOf(res));
}

async function exchange<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers:
        body === undefined
          ? { Accept: "application/json" }
          : { Accept: "application/json", "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new LaneRefusal(0, null, "Запрос не дошёл до службы: нет соединения", null, null, null);
  }
  const text = await res.text();
  if (!res.ok) throw refusalOf(res, text);
  return (text ? JSON.parse(text) : {}) as T;
}

/** Признак формы данного вида. */
export async function formToken(kind: FormKind): Promise<string> {
  const body = await exchange<{ csrfToken?: unknown }>("GET", `${LOGIN_LANE.csrf}?form=${encodeURIComponent(kind)}`);
  const token = displayText(body.csrfToken);
  if (token === "") {
    throw new LaneRefusal(200, null, "Служба не выдала признак формы", null, null, null);
  }
  return token;
}

/**
 * Держатель признака формы ОДНОГО вида.
 *
 * Признак добывается заранее (экран готов отправить сразу), берётся при
 * отправке и после отказа `FORM_TOKEN_REJECTED` добывается заново — не ожидая
 * следующей отправки. Причина в том, что контекст формы у браузера может
 * смениться под открытым экраном (вход в соседней вкладке), и держать прежний
 * признак значило бы отвергать каждую следующую отправку.
 */
export class FormTokenHolder {
  private pending: Promise<string> | null = null;

  constructor(readonly kind: FormKind) {}

  /** Признак для отправки: добытый заранее либо добываемый сейчас. */
  get(): Promise<string> {
    if (!this.pending) {
      this.pending = formToken(this.kind).catch((e: unknown) => {
        // Не добытый признак не держится: следующая отправка попробует снова.
        this.pending = null;
        throw e;
      });
    }
    return this.pending;
  }

  /** Отбросить держимый и добыть свежий СЕЙЧАС. */
  refresh(): Promise<string> {
    this.pending = null;
    return this.get();
  }
}

/** Отправить форму с признаком её вида; признак отвергнут — держатель добывает свежий. */
async function submit<T>(holder: FormTokenHolder, path: string, body: Record<string, unknown>): Promise<T> {
  const csrfToken = await holder.get();
  try {
    return await exchange<T>("POST", path, { ...body, csrfToken });
  } catch (e) {
    if (e instanceof LaneRefusal && e.reason === LANE_REASON.formTokenRejected) {
      void holder.refresh().catch(() => undefined);
    }
    throw e;
  }
}

export const loginLane = {
  login(holder: FormTokenHolder, form: { email: string; password: string; secondFactor?: SecondFactorPresentation }) {
    const body: Record<string, unknown> = { email: form.email, password: form.password };
    if (form.secondFactor) body.secondFactor = form.secondFactor;
    return submit<SignedIn>(holder, LOGIN_LANE.login, body);
  },
  register(holder: FormTokenHolder, form: { email: string; password: string }) {
    return submit<SignedIn>(holder, LOGIN_LANE.register, { email: form.email, password: form.password });
  },
  logout(holder: FormTokenHolder) {
    return submit<Record<string, never>>(holder, LOGIN_LANE.logout, {});
  },
  changePassword(holder: FormTokenHolder, form: { currentPassword: string; newPassword: string }) {
    return submit<{ session: LaneSession }>(holder, LOGIN_LANE.password, {
      currentPassword: form.currentPassword,
      newPassword: form.newPassword,
    });
  },
  secondFactorState() {
    return exchange<SecondFactorState>("GET", LOGIN_LANE.secondFactor);
  },
  enroll(holder: FormTokenHolder) {
    return submit<Enrollment>(holder, LOGIN_LANE.enroll, {});
  },
  confirm(holder: FormTokenHolder, code: string) {
    return submit<Confirmed>(holder, LOGIN_LANE.confirm, { code });
  },
  remove(holder: FormTokenHolder, factor: SecondFactorPresentation) {
    return submit<Ceremony>(holder, LOGIN_LANE.remove, { method: factor.method, code: factor.code });
  },
  regenerateBackupCodes(holder: FormTokenHolder, factor: SecondFactorPresentation) {
    return submit<Confirmed>(holder, LOGIN_LANE.backupCodes, { method: factor.method, code: factor.code });
  },
  stepUp(
    holder: FormTokenHolder,
    form: { method: "password"; password: string } | { method: CodeMethod; code: string },
  ) {
    return submit<Ceremony>(holder, LOGIN_LANE.stepUp, { ...form });
  },
};

/** Ответ края о сессии: человек и его сессия либо «сессии нет». */
export interface SessionIdentity {
  user: { id: string; email: string; displayName: string; permissions: string[] };
  session: LaneSession | null;
}

/**
 * Кто за этой сессией — по ответу края, а не по чтению чужого поставщика.
 *
 * `null` — сессии нет. Негодный носитель и его отсутствие для консоли одно
 * состояние (край отвечает на них одинаково), и чем именно носитель негоден,
 * консоль не различает и различать не вправе.
 */
export async function sessionIdentity(): Promise<SessionIdentity | null> {
  let res: Response;
  try {
    res = await fetch(SESSION_IDENTITY_PATH, { credentials: "same-origin", headers: { Accept: "application/json" } });
  } catch {
    return null;
  }
  if (!res.ok) return null;
  const text = await res.text();
  type MeBody = { user?: Record<string, unknown> | null; session?: Record<string, unknown> | null } | null;
  let body: MeBody = null;
  try {
    body = text ? (JSON.parse(text) as MeBody) : null;
  } catch {
    return null;
  }
  const user = body?.user;
  if (!user || typeof user !== "object") return null;
  const session = body?.session;
  return {
    user: {
      id: displayText(user.id),
      email: displayText(user.email),
      displayName: displayText(user.displayName),
      permissions: Array.isArray(user.permissions) ? user.permissions.map(String) : [],
    },
    session:
      session && typeof session === "object"
        ? {
            expiresAt: displayText(session.expiresAt),
            assuranceLevel: displayText(session.assuranceLevel),
            emailVerified: session.emailVerified === true,
          }
        : null,
  };
}
