// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createHmac } from "node:crypto";
import { expect, request, type APIRequestContext, type APIResponse, type BrowserContext, type TestInfo } from "@playwright/test";
import { runTag } from "./fixtures";

/**
 * Посев «Дано» сценариев церемонии — глаголами НАШЕЙ службы, отдельным
 * контекстом запросов (приёмка F8, §11 преамбула).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ОТДЕЛЬНЫЙ КОНТЕКСТ, А НЕ `page.request`
 *
 * `page.request` делит банку печенья с браузером сценария. Регистрация посева
 * выдаёт сессию, подтверждение второго фактора перевыпускает носитель — и в
 * общей банке браузер получил бы живую сессию посева: экран входа по F8-11 формы
 * не показал бы, и сценарий не дошёл бы до своего «Когда». Поэтому посев ходит
 * СВОИМ контекстом, а где «Дано» называет сессию у браузера, носитель переносится
 * явно — `transferSession`.
 *
 * ПОЧЕМУ ГЛАГОЛАМИ, А НЕ ЭКРАНОМ
 *
 * Экран ведёт только ту церемонию, которую сценарий судит. Посев экраном сделал
 * бы каждый сценарий заложником чужого экрана: регистрация, упавшая по своей
 * причине, роняла бы вход, выход и смену пароля одним текстом.
 *
 * КАЖДЫЙ ШАГ УТВЕРЖДАЕТ СВОЙ ИСХОД ответом, который получил сам. Шаг, собирающий
 * условие и не утверждающий его, при отказе падает потом — на утверждении
 * сценария, и виновником называется невиновный экран.
 *
 * ЗАПИСЬ ВЫПУСКАЮЩЕГО. Обращения этого контекста перепись страниц не видит
 * (Р6 п. 4): событий страниц они не порождают. Поэтому посев держит свои ответы
 * сам, в порядке выпуска, — `issued`. Отрицание о поставщике по коду посева
 * держит статическая перепись набора, а не эта запись.
 */

/** Пароль посева. Отличим от ввода человека: ни один сценарий его не вводит сам. */
export const SEED_PASSWORD = "Kacho-E2E-2026!x";

/** Имя носителя сессии нашей службы — то, что браузер держит после входа. */
export const SESSION_COOKIE = "kaname_session";

/** Виды признака формы глаголов полосы (`GET /iam/v1/auth/csrf?form=<вид>`). */
export type FormKind = "login" | "logout" | "password" | "register" | "second-factor" | "step-up";

export const LANE = {
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

/** Одно обращение посева и ответ на него — в порядке выпуска. */
export interface IssuedCall {
  method: "GET" | "POST";
  path: string;
  status: number;
  body: unknown;
}

export interface Seed {
  readonly api: APIRequestContext;
  /** Запись выпускающего: ответы, полученные этим контекстом, в порядке выпуска. */
  readonly issued: IssuedCall[];
  /** Признак формы данного вида; шаг утверждает свой исход. */
  formToken(kind: FormKind): Promise<string>;
  /** Отправка формы глагола с признаком её вида. Исход НЕ утверждается — это делает вызывающий. */
  submit(path: string, kind: FormKind, body: Record<string, unknown>): Promise<APIResponse>;
  /** Чтение глагола без тела. */
  read(path: string): Promise<APIResponse>;
  /** Значение носителя сессии у посева либо пустая строка. */
  sessionBearer(): Promise<string>;
  dispose(): Promise<void>;
}

/** Адрес посева: свой у каждого сценария, чтобы ось потолка по адресу не наследовалась (Р7 ч. 1). */
export function seedAddress(scenario: string): string {
  return `e2e-${scenario.toLowerCase().replace(/[^a-z0-9]+/g, "")}-${runTag()}@kacho.local`;
}

async function record(issued: IssuedCall[], method: "GET" | "POST", path: string, res: APIResponse) {
  const text = await res.text();
  let body: unknown = text;
  try {
    body = text ? (JSON.parse(text) as unknown) : null;
  } catch {
    // Тело не JSON — хранится как есть; утверждать о нём будет вызывающий.
  }
  issued.push({ method, path, status: res.status(), body });
}

/**
 * Свой контекст запросов посева — к тому же стенду, с тем же отношением к
 * сертификату, что у браузера сценария, но со СВОЕЙ банкой печенья.
 */
export async function newSeed(testInfo: TestInfo): Promise<Seed> {
  const use = testInfo.project.use;
  const api = await request.newContext({
    baseURL: use.baseURL,
    ignoreHTTPSErrors: use.ignoreHTTPSErrors,
    extraHTTPHeaders: { Accept: "application/json" },
  });
  const issued: IssuedCall[] = [];
  const seed: Seed = {
    api,
    issued,
    async formToken(kind) {
      const path = `${LANE.csrf}?form=${kind}`;
      const res = await api.get(path);
      await record(issued, "GET", path, res);
      const body = issued[issued.length - 1].body as { csrfToken?: unknown } | null;
      expect(
        res.status(),
        `посев: признак формы «${kind}» не выдан — ${res.status()} ${JSON.stringify(body)}. ` +
          "Это УСЛОВИЕ сценария, а не его предмет",
      ).toBe(200);
      const token = body?.csrfToken;
      expect(typeof token === "string" && token !== "", `посев: ответ признака формы «${kind}» без csrfToken`).toBe(true);
      return token as string;
    },
    async submit(path, kind, body) {
      const csrfToken = await seed.formToken(kind);
      const res = await api.post(path, { data: { ...body, csrfToken } });
      await record(issued, "POST", path, res);
      return res;
    },
    async read(path) {
      const res = await api.get(path);
      await record(issued, "GET", path, res);
      return res;
    },
    async sessionBearer() {
      const state = await api.storageState();
      return state.cookies.find((c) => c.name === SESSION_COOKIE)?.value ?? "";
    },
    dispose: () => api.dispose(),
  };
  return seed;
}

/** Последний ответ посева по пути — тело, как его получил посев. */
export function lastIssued(seed: Seed, path: string): IssuedCall {
  const found = [...seed.issued].reverse().find((c) => c.path === path);
  if (!found) throw new Error(`посев: обращения к ${path} в записи выпускающего нет`);
  return found;
}

export interface SeededHuman {
  email: string;
  password: string;
  /** Уровень уверенности сессии посева из ответа регистрации. */
  assuranceLevel: string;
}

/**
 * Завести человека глаголом регистрации. Шаг утверждает исход: `200`, сессия в
 * теле и носитель у посева. Отказ называется ТЕКСТОМ службы, а не симптомом.
 */
export async function seedHuman(seed: Seed, email: string, password = SEED_PASSWORD): Promise<SeededHuman> {
  const res = await seed.submit(LANE.register, "register", { email, password });
  const call = lastIssued(seed, LANE.register);
  expect(
    res.status(),
    `посев: регистрация ${email} отвергнута — ${res.status()} ${JSON.stringify(call.body)}. ` +
      "Это УСЛОВИЕ сценария: вердикта об экране такой прогон не даёт",
  ).toBe(200);
  const body = call.body as { session?: { assuranceLevel?: unknown } } | null;
  expect(body?.session, "посев: ответ регистрации без session").toBeTruthy();
  expect(await seed.sessionBearer(), "посев: регистрация прошла, а носителя сессии у посева нет").not.toBe("");
  return { email, password, assuranceLevel: String(body?.session?.assuranceLevel ?? "") };
}

/**
 * Перенести носитель посева в браузер сценария — ЯВНО, а не общей банкой.
 * Атрибуты печенья берутся у посева как выданы службой: подделка домена или
 * пути сделала бы сценарий свидетелем носителя, которого служба не выдавала.
 */
export async function transferSession(seed: Seed, context: BrowserContext): Promise<void> {
  const state = await seed.api.storageState();
  const cookie = state.cookies.find((c) => c.name === SESSION_COOKIE);
  expect(cookie, "посев: переносить нечего — носителя сессии у посева нет").toBeTruthy();
  await context.addCookies([cookie!]);
}

// ─── второй фактор ────────────────────────────────────────────────────────────

/** base32 RFC 4648 — форма секрета, которую отдаёт заведение. */
function base32Decode(s: string): Buffer {
  const A = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = 0;
  let value = 0;
  const out: number[] = [];
  for (const ch of s.toUpperCase().replace(/=+$/, "")) {
    const idx = A.indexOf(ch);
    if (idx < 0) continue;
    value = (value << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      out.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(out);
}

/** RFC 6238: шаг 30 с, SHA-1, шесть знаков — контракт кода службы. */
export function totpCode(secret: string, atMs: number): string {
  const counter = Math.floor(atMs / 1000 / 30);
  const buf = Buffer.alloc(8);
  buf.writeUInt32BE(Math.floor(counter / 2 ** 32), 0);
  buf.writeUInt32BE(counter >>> 0, 4);
  const mac = createHmac("sha1", base32Decode(secret)).update(buf).digest();
  const off = mac[mac.length - 1] & 0x0f;
  const bin = ((mac[off] & 0x7f) << 24) | (mac[off + 1] << 16) | (mac[off + 2] << 8) | mac[off + 3];
  return String(bin % 1_000_000).padStart(6, "0");
}

/** Три кода окна ±1 шаг вокруг момента — всё, что служба в этот момент примет. */
export function windowCodes(secret: string, atMs: number): string[] {
  return [-30_000, 0, 30_000].map((d) => totpCode(secret, atMs + d));
}

/**
 * Шесть цифр, НЕ совпадающие ни с одним кодом окна: «неверный» выбирается
 * построением, а не наугад — наугад он с вероятностью 3·10⁻⁶ оказался бы верным.
 */
export function codeOutsideWindow(secret: string, atMs: number): string {
  const taken = new Set(windowCodes(secret, atMs));
  for (let n = 0; n < 1_000_000; n++) {
    const c = String(n).padStart(6, "0");
    if (!taken.has(c)) return c;
  }
  throw new Error("код вне окна не построен — такого не бывает при трёх занятых из миллиона");
}

/** Алфавит запасных кодов службы (десять знаков Crockford). */
export const BACKUP_CODE_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/**
 * Запасной код ТОЙ ЖЕ формы, не входящий в набор: проба знает все десять кодов
 * набора, поэтому значение выбирается построением.
 */
export function backupCodeOutside(codes: readonly string[]): string {
  const taken = new Set(codes.map((c) => c.toUpperCase()));
  for (const ch of BACKUP_CODE_ALPHABET) {
    const c = ch.repeat(10);
    if (!taken.has(c)) return c;
  }
  throw new Error("запасной код вне набора не построен");
}

export interface SeededSecondFactor {
  secret: string;
  backupCodes: string[];
  /** Уровень сессии из ответа подтверждения. */
  assuranceLevel: string;
}

/**
 * Завести и подтвердить второй фактор глаголами службы — ОДНИМ предъявлением
 * кода по времени (приёмка F8, Р9, ось 4).
 *
 * Подтверждение кладёт шаг кода: всякое следующее предъявление кода по времени
 * тем же человеком было бы повтором и тратило бы общую ось источника. Поэтому
 * дальше этот человек предъявляет второй фактор только ЗАПАСНЫМ кодом из ответа
 * этого подтверждения, а ожидания следующего шага здесь нет вовсе.
 */
export async function seedSecondFactor(seed: Seed): Promise<SeededSecondFactor> {
  const enrolled = await seed.submit(LANE.enroll, "second-factor", {});
  const enrollBody = lastIssued(seed, LANE.enroll).body as { secret?: unknown } | null;
  expect(
    enrolled.status(),
    `посев: заведение второго фактора отвергнуто — ${enrolled.status()} ${JSON.stringify(enrollBody)}`,
  ).toBe(200);
  const secret = enrollBody?.secret;
  expect(typeof secret === "string" && secret !== "", "посев: ответ заведения без секрета").toBe(true);

  const confirmed = await seed.submit(LANE.confirm, "second-factor", { code: totpCode(secret as string, Date.now()) });
  const confirmBody = lastIssued(seed, LANE.confirm).body as {
    backupCodes?: unknown;
    session?: { assuranceLevel?: unknown };
  } | null;
  expect(
    confirmed.status(),
    `посев: подтверждение второго фактора отвергнуто — ${confirmed.status()} ${JSON.stringify(confirmBody)}`,
  ).toBe(200);
  const codes = confirmBody?.backupCodes;
  expect(Array.isArray(codes) && codes.length > 0, "посев: подтверждение не выдало запасных кодов").toBe(true);
  return {
    secret: secret as string,
    backupCodes: (codes as unknown[]).map(String),
    assuranceLevel: String(confirmBody?.session?.assuranceLevel ?? ""),
  };
}
