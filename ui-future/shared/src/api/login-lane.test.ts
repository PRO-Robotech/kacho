// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { requestBody, requestUrl } from "@shared/test/fetch-capture";
import { SIGNED_IN, installLane, refusal, type LaneAnswer } from "@shared/test/lane-fake";
import { FormTokenHolder, LaneRefusal, loginLane, refusalOf, sessionIdentity } from "./login-lane";
import { requestStepUp, setStepUpRequester, type StepUpRequest } from "./step-up";

// Клиент полосы формы — ЕДИНСТВЕННАЯ дверь консоли к церемониям (приёмка F8).
// Здесь закреплено то, что экраны получают от него готовым, и потому видят
// только исходом: разбор отказа, выбор действия на отказ, судьба признака
// формы и три исхода вопроса «есть ли сессия». Сеть — дублёр полосы с телами
// и заголовками её производителей (`kaname` `loginlanehttp.writeRefusal`, край
// `writeHTTPUnauthorized` и вызов пола RFC 9470).

/** Ответ так, как его видит `fetch`: статус, заголовки, текст тела. */
function answer(status: number, headers: Record<string, string> = {}): Response {
  const h = Object.fromEntries(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v]));
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: { get: (n: string) => h[n.toLowerCase()] ?? null },
  } as unknown as Response;
}

/** Отказ края на глаголе с носителем, когда служба ему не ответила (F4d-23): тело без `details`. */
const EDGE_SESSION_ENDED: LaneAnswer = {
  status: 401,
  body: { code: 16, message: "session ended; sign in again" },
  headers: { "WWW-Authenticate": 'Bearer error="invalid_token", error_description="session ended; sign in again"' },
};

/** Вызов пола края RFC 9470. */
const EDGE_FLOOR: LaneAnswer = {
  status: 401,
  body: { code: 16, message: "insufficient_user_authentication" },
  headers: {
    "WWW-Authenticate":
      'Bearer error="insufficient_user_authentication", error_description="Required ACR 2", acr_values="2"',
  },
};

const NOT_FRESH = refusal(403, 7, "re-authentication required: present a credential again", "SESSION_NOT_FRESH");

/**
 * Дублёр службы, держащий КОНТЕКСТ ФОРМЫ так, как его держит служба: один на
 * браузер. Печенье уходит тем, что браузер держит В МОМЕНТ отправки, а ставится
 * ответом — поэтому две выдачи, ушедшие до первого ответа, контекста не несут.
 * Признак принадлежит контексту, в котором выдан; отправка с признаком не того
 * контекста, что несёт печенье, — `403 FORM_TOKEN_REJECTED`, как у службы.
 */
function formContextLane() {
  let cookie: number | null = null;
  let minted = 0;
  let issued = 0;
  const tokenContext = new Map<string, number>();
  const original = globalThis.fetch;
  const reply = (status: number, body: unknown) =>
    ({
      ok: status >= 200 && status < 300,
      status,
      headers: { get: () => null },
      text: () => Promise.resolve(JSON.stringify(body)),
    }) as unknown as Response;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(requestUrl(input), "http://console.test");
    const method = (init?.method ?? "GET").toUpperCase();
    const sent = cookie;
    const body = requestBody(init?.body);
    // Ответ приходит ПОЗЖЕ отправки: печенье, поставленное ответом, уходит
    // только с запросами, отправленными после него.
    for (let i = 0; i < 3; i++) await Promise.resolve();
    if (method === "GET" && url.pathname === "/iam/v1/auth/csrf") {
      const context = sent ?? ++minted;
      const token = `tok-${url.searchParams.get("form") ?? ""}-${++issued}`;
      tokenContext.set(token, context);
      cookie = context;
      return reply(200, { csrfToken: token });
    }
    if (method === "POST") {
      if (sent === null || tokenContext.get(String(body?.csrfToken)) !== sent) {
        return reply(403, refusal(403, 7, "form token rejected", "FORM_TOKEN_REJECTED").body);
      }
      return reply(200, { session: {} });
    }
    return reply(404, refusal(404, 5, `дублёр: ${method} ${url.pathname} не объявлен пробой`).body);
  }) as typeof fetch;
  return {
    contextsMinted: () => minted,
    restore: () => {
      globalThis.fetch = original;
    },
  };
}

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => {
  lane?.restore();
  lane = null;
  setStepUpRequester(null);
});

describe("разбор отказа полосы — один на все глаголы (C3, C4, C11)", () => {
  it("C3 · тело без `details` и тело с пустым `details` — один и тот же отказ", () => {
    const bare = refusalOf(answer(401), '{"code":16,"message":"session ended; sign in again"}');
    const empty = refusalOf(answer(401), '{"code":16,"message":"session ended; sign in again","details":[]}');
    expect([bare.code, bare.message, bare.reason, bare.field]).toEqual([
      16,
      "session ended; sign in again",
      null,
      null,
    ]);
    expect([empty.code, empty.message, empty.reason, empty.field]).toEqual([bare.code, bare.message, null, null]);
  });

  it("C4 · поле берётся ОДНИМ якорным разборщиком и закрытой таблицей «поле ответа → ввод»", () => {
    const fieldOf = (message: string) =>
      refusalOf(answer(400), JSON.stringify({ code: 3, message, details: [] })).field;
    expect(fieldOf("Illegal argument password: required")).toBe("password");
    expect(fieldOf("Illegal argument email: required")).toBe("email");
    expect(fieldOf("Illegal argument currentPassword: required")).toBe("currentPassword");
    expect(fieldOf("Illegal argument newPassword: too short")).toBe("newPassword");
    expect(fieldOf("Illegal argument code: must be 6 digits")).toBe("code");
    expect(fieldOf("Illegal argument method: required")).toBe("method");
    // Вложенная форма входа называет поле с префиксом — ввод тот же.
    expect(fieldOf("Illegal argument secondFactor.code: must be 6 digits")).toBe("code");
    expect(fieldOf("Illegal argument secondFactor.method: required")).toBe("method");
  });

  it("C4 · поле вне таблицы, признак формы и проза без якоря ни одного ввода не отмечают", () => {
    const fieldOf = (message: string, code = 3) =>
      refusalOf(answer(400), JSON.stringify({ code, message, details: [] })).field;
    // Служба называет признак формы, но ввода у него нет — отказ показывается у формы.
    expect(fieldOf("Illegal argument csrfToken: required")).toBeNull();
    // Поле, которого нет ни на одном экране, — не «ближайшее похожее», а ничьё.
    expect(fieldOf("Illegal argument body: malformed JSON")).toBeNull();
    expect(fieldOf("Illegal argument displayName: unknown field")).toBeNull();
    // Имя поля не в начале — не отказ о поле.
    expect(fieldOf("request refused: Illegal argument password: required")).toBeNull();
    // Код не `INVALID_ARGUMENT` — поля нет, какая бы проза ни пришла.
    expect(fieldOf("Illegal argument password: required", 9)).toBeNull();
  });

  it("C11 · срок из Retry-After — только целые секунды не меньше одной; иначе срока нет", () => {
    const after = (value?: string) =>
      refusalOf(
        answer(429, value === undefined ? {} : { "Retry-After": value }),
        '{"code":8,"message":"too many attempts; try again later","details":[]}',
      ).retryAfterSeconds;
    expect(after("897")).toBe(897);
    expect(after("1")).toBe(1);
    expect(after(undefined)).toBeNull();
    expect(after("0")).toBeNull();
    expect(after("1.5")).toBeNull();
    expect(after("Wed, 21 Oct 2026 07:28:00 GMT")).toBeNull();
    expect(after("")).toBeNull();
  });

  it("C2 · вызов края несёт свой машинный признак — и он доходит до отказа", () => {
    const ended = refusalOf(answer(401, EDGE_SESSION_ENDED.headers), JSON.stringify(EDGE_SESSION_ENDED.body));
    expect(ended.challenge).toBe("invalid_token");
    const floor = refusalOf(answer(401, EDGE_FLOOR.headers), JSON.stringify(EDGE_FLOOR.body));
    expect(floor.challenge).toBe("insufficient_user_authentication");
    const service = refusalOf(answer(401), '{"code":16,"message":"authentication failed","details":[]}');
    expect(service.challenge).toBeNull();
  });
});

describe("действие на отказ выбирает машинный признак, а не статус (C2)", () => {
  it("C2 · SESSION_NOT_FRESH: повышение «свежесть» и ОДИН повтор того же глагола", async () => {
    lane = installLane({ "POST /iam/v1/auth/second-factor/enroll": (_c, nth) => (nth === 1 ? NOT_FRESH : ENROLLED) });
    const asked: StepUpRequest[] = [];
    setStepUpRequester((r) => {
      asked.push(r);
      return Promise.resolve();
    });
    const out = await loginLane.enroll(new FormTokenHolder("second-factor"));
    expect(out.secret).toBe("S");
    expect(asked).toEqual([{ cause: "freshness" }]);
    expect(lane.of("POST", "/iam/v1/auth/second-factor/enroll")).toHaveLength(2);
  });

  it("C2 · вызов пола на глаголе полосы: повышение «пол», без пароля, и ОДИН повтор", async () => {
    lane = installLane({
      "POST /iam/v1/auth/second-factor/backup-codes": (_c, nth) => (nth === 1 ? EDGE_FLOOR : CONFIRMED),
    });
    const asked: StepUpRequest[] = [];
    setStepUpRequester((r) => {
      asked.push(r);
      return Promise.resolve();
    });
    await loginLane.regenerateBackupCodes(new FormTokenHolder("second-factor"), { method: "lookup_secret", code: "X" });
    expect(asked).toEqual([{ cause: "floor", acr: "2" }]);
    expect(lane.of("POST", "/iam/v1/auth/second-factor/backup-codes")).toHaveLength(2);
  });

  it("C2 · тот же статус 401 без признака пола — показать дословно: ни повышения, ни повтора", async () => {
    // Три смысла 401/16: служба без заголовка, край «сессия кончилась», край пол.
    for (const refused of [refusal(401, 16, "authentication failed"), EDGE_SESSION_ENDED]) {
      lane = installLane({ "POST /iam/v1/auth/password": refused });
      const asked = jest.fn(() => Promise.resolve());
      setStepUpRequester(asked);
      const e = await loginLane
        .changePassword(new FormTokenHolder("password"), { currentPassword: "a", newPassword: "b" })
        .catch((x: unknown) => x);
      expect(e).toBeInstanceOf(LaneRefusal);
      expect((e as LaneRefusal).message).toBe((refused.body as { message: string }).message);
      expect(asked).not.toHaveBeenCalled();
      expect(lane.of("POST", "/iam/v1/auth/password")).toHaveLength(1);
      lane.restore();
    }
  });

  it("C2 · неизвестная причина — показать дословно, и повышения нет, хотя статус тот же, что у свежести", async () => {
    lane = installLane({ "POST /iam/v1/auth/second-factor/enroll": refusal(403, 7, "something new", "NEW_REASON") });
    const asked = jest.fn(() => Promise.resolve());
    setStepUpRequester(asked);
    const e = (await loginLane.enroll(new FormTokenHolder("second-factor")).catch((x: unknown) => x)) as LaneRefusal;
    expect(e.message).toBe("something new");
    expect(asked).not.toHaveBeenCalled();
    expect(lane.of("POST", "/iam/v1/auth/second-factor/enroll")).toHaveLength(1);
  });

  it("C2 · повышение не прошло — отдаётся исходный отказ, второго повтора нет", async () => {
    lane = installLane({ "POST /iam/v1/auth/second-factor/enroll": NOT_FRESH });
    setStepUpRequester(() => Promise.reject(new Error("отменено")));
    const e = (await loginLane.enroll(new FormTokenHolder("second-factor")).catch((x: unknown) => x)) as LaneRefusal;
    expect(e.reason).toBe("SESSION_NOT_FRESH");
    expect(lane.of("POST", "/iam/v1/auth/second-factor/enroll")).toHaveLength(1);
  });

  it("C2 · глагол повышения сам повышения не просит — иначе окно ждало бы само себя", async () => {
    lane = installLane({ "POST /iam/v1/auth/step-up": NOT_FRESH });
    const asked = jest.fn(() => Promise.resolve());
    setStepUpRequester(asked);
    await loginLane.stepUp(new FormTokenHolder("step-up"), { method: "password", password: "p" }).catch(() => null);
    expect(asked).not.toHaveBeenCalled();
  });
});

const ENROLLED: LaneAnswer = { status: 200, body: { secret: "S", otpauthUri: "otpauth://x", expiresAt: "t" } };
const CONFIRMED: LaneAnswer = {
  status: 200,
  body: {
    backupCodes: ["A"],
    session: { expiresAt: "t", assuranceLevel: "2", emailVerified: true },
    assurance: { level: "2", level2Reachable: true, missingForLevel2: [] },
  },
};

describe("признак формы добывается на КАЖДУЮ отправку (C12)", () => {
  it("C12 · вторая отправка несёт СВЕЖИЙ признак, а не тот, что ушёл с первой", async () => {
    lane = installLane({ "POST /iam/v1/auth/login": refusal(401, 16, "authentication failed") });
    const holder = new FormTokenHolder("login");
    await loginLane.login(holder, { email: "a", password: "1" }).catch(() => null);
    await loginLane.login(holder, { email: "a", password: "2" }).catch(() => null);
    expect(lane.of("POST", "/iam/v1/auth/login").map((c) => c.body?.csrfToken)).toEqual(["tok-login-1", "tok-login-2"]);
  });

  it("C12 · двух выдач признака одновременно нет: добыча идёт за ответом, а не рядом с ним", async () => {
    let inFlight = 0;
    let overlapped = false;
    const original = globalThis.fetch;
    lane = installLane({ "POST /iam/v1/auth/login": refusal(401, 16, "authentication failed") });
    const fake = globalThis.fetch;
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const isCsrf = String(input).startsWith("/iam/v1/auth/csrf");
      if (isCsrf) {
        inFlight += 1;
        if (inFlight > 1) overlapped = true;
      }
      try {
        return await fake(input, init);
      } finally {
        if (isCsrf) inFlight -= 1;
      }
    }) as typeof fetch;
    try {
      const holder = new FormTokenHolder("login");
      void holder.get();
      await Promise.all([
        loginLane.login(holder, { email: "a", password: "1" }).catch(() => null),
        loginLane.login(holder, { email: "a", password: "2" }).catch(() => null),
      ]);
      expect(overlapped).toBe(false);
      const tokens = lane.of("POST", "/iam/v1/auth/login").map((c) => c.body?.csrfToken);
      expect(new Set(tokens).size).toBe(tokens.length);
    } finally {
      globalThis.fetch = original;
    }
  });

  it("C12 · ответ глагола, сменившего контекст формы, гасит признаки, добытые заранее другими формами", async () => {
    lane = installLane({
      "POST /iam/v1/auth/register": SIGNED_IN,
      "POST /iam/v1/auth/logout": { status: 200, body: {} },
    });
    const logout = new FormTokenHolder("logout");
    await logout.get(); // добыт заранее — экран выхода открыт раньше регистрации
    await loginLane.register(new FormTokenHolder("register"), { email: "a", password: "p" });
    await loginLane.logout(logout);
    expect(lane.of("POST", "/iam/v1/auth/logout")[0].body?.csrfToken).toBe("tok-logout-2");
  });

  it("C12 · формы разных видов, открытые одним экраном, получают признаки ОДНОГО контекста формы", async () => {
    // Экран параметров открывает две формы сразу (пароль и второй фактор), и
    // обе добывают признак при открытии. Контекст формы у службы ОДИН на браузер
    // (печенье `kaname_form`): выдача без контекста заводит новый, выдача с ним —
    // выдаёт признак в нём. Две выдачи, ушедшие рядом без контекста, заводят два,
    // печенье остаётся от последнего ответа, и признак первого вида служба
    // отвергает `FORM_TOKEN_REJECTED` (прогон F8 на посадке `own`, 19 красных,
    // из них 6 — этот отказ на /settings).
    const lane = formContextLane();
    try {
      const password = new FormTokenHolder("password");
      const secondFactor = new FormTokenHolder("second-factor");
      void password.get().catch(() => undefined);
      void secondFactor.get().catch(() => undefined);
      const changed = await loginLane
        .changePassword(password, { currentPassword: "p", newPassword: "q" })
        .then(() => "принято", (e: unknown) => (e instanceof LaneRefusal ? `${e.status} ${e.reason} ${e.message}` : String(e)));
      expect(changed).toBe("принято");
      expect(lane.contextsMinted()).toBe(1);
    } finally {
      lane.restore();
    }
  });

  it("C12 · отвергнутый признак: ОДИН свежий добыт сразу, отправка не повторена сама", async () => {
    lane = installLane({
      "POST /iam/v1/auth/login": refusal(403, 7, "form token rejected", "FORM_TOKEN_REJECTED"),
    });
    const holder = new FormTokenHolder("login");
    await loginLane.login(holder, { email: "a", password: "p" }).catch(() => null);
    // Свежий признак добывается вслед за ответом — дождаться его выдачи.
    for (let i = 0; i < 5 && lane.of("GET", "/iam/v1/auth/csrf").length < 2; i++) {
      await new Promise((r) => setImmediate(r));
    }
    expect(lane.of("GET", "/iam/v1/auth/csrf")).toHaveLength(2);
    expect(lane.of("POST", "/iam/v1/auth/login")).toHaveLength(1);
  });
});

describe("«есть ли сессия» — три исхода по ТИПУ, а не два (C6, C8, C9)", () => {
  it("C6 · сессия есть: человек и сессия ответа края, в той форме, в какой их прислали", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": {
        status: 200,
        body: {
          user: { id: "usr-1", email: "a@kacho.local", displayName: "Анна", subjectType: "user", permissions: ["*"] },
          session: { expiresAt: "t", assuranceLevel: "1", emailVerified: true },
        },
      },
    });
    const who = await sessionIdentity();
    expect(who).toEqual({
      kind: "present",
      user: { id: "usr-1", email: "a@kacho.local", displayName: "Анна", subjectType: "user", permissions: ["*"] },
      session: { expiresAt: "t", assuranceLevel: "1", emailVerified: true },
    });
  });

  it("C6 · сессии нет — только когда край так и ответил", async () => {
    lane = installLane({ "GET /iam/v1/auth/me": { status: 200, body: { user: null } } });
    expect(await sessionIdentity()).toEqual({ kind: "absent" });
  });

  it("C6 · край не ответил по существу — «не спросили», а не «вы вышли»", async () => {
    for (const refused of [
      refusal(503, 14, "unavailable"),
      refusal(401, 16, "session ended; sign in again"),
      { status: 200, body: "не json" } as LaneAnswer,
    ]) {
      lane = installLane({ "GET /iam/v1/auth/me": refused });
      expect((await sessionIdentity()).kind).toBe("unknown");
      lane.restore();
    }
    const original = globalThis.fetch;
    globalThis.fetch = (() => Promise.reject(new TypeError("Failed to fetch"))) as typeof fetch;
    try {
      expect((await sessionIdentity()).kind).toBe("unknown");
    } finally {
      globalThis.fetch = original;
    }
  });

  it("F8-12 · край назвал носитель негодным и погасил его — сессии нет, а не «не спросили»", async () => {
    // Край отвечает на негодный носитель у «кто я» НЕ `{"user":null}`, а отказом
    // `invalid_token` — тем же текстом, что и на недоступность своего авторитета
    // (F4d-23). Различаются они ПОВЕДЕНИЕМ: негодный носитель край гасит, при
    // недоступности носитель цел. Второй вопрос уходит уже без погашенного
    // носителя, и ответ на него решает.
    lane = installLane({
      "GET /iam/v1/auth/me": (_c, nth) => (nth === 1 ? EDGE_SESSION_ENDED : { status: 200, body: { user: null } }),
    });
    expect(await sessionIdentity()).toEqual({ kind: "absent" });
    expect(lane.of("GET", "/iam/v1/auth/me")).toHaveLength(2);
  });

  it("F8-12 · край отвечает «сессия кончилась» и на второй вопрос — «не спросили», и третьего вопроса нет", async () => {
    lane = installLane({ "GET /iam/v1/auth/me": EDGE_SESSION_ENDED });
    const who = await sessionIdentity();
    expect(who.kind).toBe("unknown");
    expect(who.kind === "unknown" ? who.refusal.message : "").toBe("session ended; sign in again");
    expect(lane.of("GET", "/iam/v1/auth/me")).toHaveLength(2);
  });

  it("C8 · поля подтверждённости нет в ответе — признака нет, «не подтверждён» не выдумывается", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": {
        status: 200,
        body: { user: { id: "usr-1", email: "a", displayName: "a" }, session: { expiresAt: "t", assuranceLevel: "1" } },
      },
    });
    const who = await sessionIdentity();
    expect(who.kind === "present" ? who.session : "нет").toEqual({ expiresAt: "t", assuranceLevel: "1" });
  });
});

describe("повышение — единственный полёт (C19)", () => {
  it("C19 · два отказа по полу ждут ОДНУ церемонию, и оба исходных повторяются", async () => {
    let ceremonies = 0;
    let finish: () => void = () => undefined;
    setStepUpRequester(() => {
      ceremonies += 1;
      return new Promise<void>((resolve) => {
        finish = resolve;
      });
    });
    const first = requestStepUp("2");
    const second = requestStepUp("2");
    finish();
    expect(await Promise.all([first, second])).toEqual([true, true]);
    expect(ceremonies).toBe(1);
  });
});
