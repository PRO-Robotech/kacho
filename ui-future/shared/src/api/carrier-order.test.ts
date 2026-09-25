// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { requestUrl } from "@shared/test/fetch-capture";
import { SubscriptionHub, type EventSourceLike } from "@shared/lib/subscription/hub";
import { orderedTransport } from "./carrier-order";
import { api } from "./client";
import { FormTokenHolder, formToken, loginLane } from "./login-lane";
import { setStepUpRequester } from "./step-up";

// Модульная проба упорядочения (приёмка F8, Р10, §11.1) — держатель ПОРЯДКА.
//
// Браузерная пара F8-46/F8-47 судит исход у страницы и порядка не различает:
// консоль, отменяющая чтение уже после выпуска глагола, у неё зелёная (N19).
// Здесь порядок и часы управляемы: сеть — дублёр, который отвечает, когда
// велит проба, и пишет ленту событий — выпуск, отмену и исход каждого
// обращения, открытие и закрытие потока. Лента и судится.
//
// По каждому из семи глаголов, которые консоль зовёт в S1+S2 (вход,
// регистрация, смена пароля, подтверждение, снятие, перечеканка, повышение), и
// по каждому из трёх исходов глагола — ответ, отказ края `503`, «ответа нет»:
// чтение в полёте отменено до выпуска глагола и выпущено снова после исхода;
// мутация в полёте дождалась исхода; поток закрыт до глагола и открыт после;
// обращение, начатое между выпуском глагола и его исходом, выпущено после
// исхода. Близнецы — заведение второго фактора и признак формы — носителя не
// ставят, и ничего из этого у них нет. Обе стороны краснеют на обезвреженном
// упорядочении: консоль без него и консоль, упорядочивающая всякую отправку, —
// тем же прогоном, а не рассказом.

/** Событие ленты: выпуск, отмена и исход обращения; открытие и закрытие потока. */
type Tape = string[];

interface Wire {
  label: string;
  answer(status: number, body?: unknown, headers?: Record<string, string>): void;
  drop(): void;
}

/**
 * Ответ так, как его видит `fetch`: статус, заголовки, текст тела. Как у
 * браузера, отмена обращения после заголовков срывает чтение тела.
 */
function response(status: number, body: unknown, headers: Record<string, string>, signal?: AbortSignal | null): Response {
  const h = Object.fromEntries(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v]));
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: String(status),
    headers: { get: (n: string) => h[n.toLowerCase()] ?? null },
    text: () =>
      signal?.aborted
        ? Promise.reject(new DOMException("The operation was aborted.", "AbortError"))
        : Promise.resolve(body === undefined ? "" : JSON.stringify(body)),
  } as unknown as Response;
}

/**
 * Дублёр сети. Признак формы отвечает сам (он не предмет пробы, кроме близнеца,
 * который выключает это умолчание); всё прочее ждёт ответа, названного пробой.
 */
function installNet(tape: Tape, opts: { autoFormToken: boolean } = { autoFormToken: true }) {
  const original = globalThis.fetch;
  const waiting: Wire[] = [];
  let tokens = 0;
  globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(requestUrl(input), "http://console.test");
    const label = `${(init?.method ?? "GET").toUpperCase()} ${url.pathname}`;
    tape.push(`выпуск ${label}`);
    return new Promise<Response>((resolve, reject) => {
      let done = false;
      const wire: Wire = {
        label,
        answer: (status, body = {}, headers = {}) => {
          if (done) return;
          done = true;
          tape.push(`исход ${label} ${status}`);
          resolve(response(status, body, headers, init?.signal));
        },
        drop: () => {
          if (done) return;
          done = true;
          tape.push(`исход ${label} ответа нет`);
          reject(new TypeError("Failed to fetch"));
        },
      };
      init?.signal?.addEventListener("abort", () => {
        if (done) return;
        done = true;
        const i = waiting.indexOf(wire);
        if (i >= 0) waiting.splice(i, 1);
        tape.push(`отмена ${label}`);
        reject(new DOMException("The operation was aborted.", "AbortError"));
      });
      if (opts.autoFormToken && label === "GET /iam/v1/auth/csrf") {
        wire.answer(200, { csrfToken: `tok-${++tokens}` });
        return;
      }
      waiting.push(wire);
    });
  };
  return {
    /** Ждущее обращение по метке — первое выпущенное и ещё без исхода. */
    take(label: string): Wire {
      const i = waiting.findIndex((w) => w.label === label);
      if (i < 0) throw new Error(`обращение «${label}» не выпущено либо уже получило исход:\n${tape.join("\n")}`);
      return waiting.splice(i, 1)[0];
    },
    /** Ответить всем ждущим — конец сценария, дальше лента не судится. */
    drain(): void {
      for (const w of waiting.splice(0)) w.answer(200, {});
    },
    restore(): void {
      globalThis.fetch = original;
    },
  };
}

/** Дать исполниться всем готовым продолжениям — без времени, числом шагов очереди. */
async function flush(): Promise<void> {
  for (let i = 0; i < 200; i++) await Promise.resolve();
}

/** Дождаться условия на ленте — без времени; не выполнилось за конечное число шагов — отказ с лентой. */
async function until(tape: Tape, what: string, cond: () => boolean): Promise<void> {
  for (let i = 0; i < 50 && !cond(); i++) await flush();
  if (!cond()) throw new Error(`не дождались: ${what}\n${tape.join("\n")}`);
}

const issued = (tape: Tape, label: string) => tape.filter((e) => e === `выпуск ${label}`).length;
const at = (tape: Tape, event: string, from = 0) => tape.indexOf(event, from);
const outcomeOf = (tape: Tape, label: string, from = 0) =>
  tape.findIndex((e, i) => i >= from && e.startsWith(`исход ${label} `));

/** Хаб с подставным приёмником: лента пишет открытие и закрытие потока. */
function streamOnTape(tape: Tape) {
  let n = 0;
  const hub = new SubscriptionHub({
    open: () => {
      const id = ++n;
      tape.push(`поток открыт ${id}`);
      const source: EventSourceLike = {
        readyState: 0,
        addEventListener: () => undefined,
        onerror: null,
        close: () => {
          source.readyState = 2;
          tape.push(`поток закрыт ${id}`);
        },
      };
      return source;
    },
    diagnose: () => Promise.resolve({ status: 200, contentType: "text/event-stream", body: "" }),
    log: () => undefined,
  });
  const off = hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
  return { off };
}

type Outcome = "ответ" | "отказ края 503" | "ответа нет";
const OUTCOMES: readonly Outcome[] = ["ответ", "отказ края 503", "ответа нет"];

/** Отказ ретрансляции края — служба не ответила в срок (§1.9, N20). */
const EDGE_UNAVAILABLE = { code: 14, message: "service unavailable; try again later" };

function settle(wire: Wire, outcome: Outcome): void {
  if (outcome === "ответ") wire.answer(200, {});
  else if (outcome === "отказ края 503") wire.answer(503, EDGE_UNAVAILABLE);
  else wire.drop();
}

/** Сетевые пути сценария: чтение и мутация в полёте, обращение, начатое во время глагола. */
const READ = "GET /vpc/v1/networks";
const MUTATION = "POST /vpc/v1/subnets";
const LATE = "GET /vpc/v1/addresses";

/**
 * Один сценарий: поток открыт, чтение и мутация в полёте; глагол начат; во
 * время глагола начато ещё одно чтение; глагол получил исход. Возвращает ленту.
 *
 * Сценарий не предполагает упорядочения: он годится и для обезвреженного —
 * глагол, выпущенный сразу, мутации не ждёт, и она получает ответ в конце.
 */
async function scenario(verb: string, run: () => Promise<unknown>, outcome: Outcome): Promise<Tape> {
  const tape: Tape = [];
  const net = installNet(tape);
  const stream = streamOnTape(tape);
  try {
    const read = api.get("/vpc/v1/networks").catch(() => undefined);
    const mutation = api.create("/vpc/v1/subnets", { name: "s" }).catch(() => undefined);
    const verbDone = run().catch(() => undefined);
    await flush();
    if (issued(tape, verb) === 0) {
      net.take(MUTATION).answer(200, { operation: { id: "op-1" } });
      await until(tape, `выпуск ${verb}`, () => issued(tape, verb) > 0);
    }
    const late = api.get("/vpc/v1/addresses").catch(() => undefined);
    await flush();
    settle(net.take(verb), outcome);
    await flush();
    net.drain();
    await flush();
    net.drain();
    await Promise.all([read, mutation, verbDone, late]);
    // Снимок — до снятия подписки: закрытие потока уходом страницы предметом
    // сценария не является.
    return [...tape];
  } finally {
    stream.off();
    net.restore();
  }
}

/**
 * Нарушения Р10 пп. 1–3 на ленте глагола, ставящего носитель. Пусто — порядок
 * соблюдён.
 */
function orderBreaches(tape: Tape, verb: string): string[] {
  const out: string[] = [];
  const issue = at(tape, `выпуск ${verb}`);
  const done = outcomeOf(tape, verb, issue);
  if (issue < 0 || done < 0) return [`глагол ${verb} не выпущен либо без исхода`];
  const cancel = at(tape, `отмена ${READ}`);
  if (cancel < 0 || cancel > issue) out.push("п. 1: чтение в полёте не отменено до выпуска глагола");
  const mutationDone = outcomeOf(tape, MUTATION);
  if (mutationDone < 0 || mutationDone > issue) out.push("п. 1: мутация в полёте не дождалась исхода до выпуска глагола");
  const closed = at(tape, "поток закрыт 1");
  if (closed < 0 || closed > issue) out.push("п. 1: поток не закрыт до выпуска глагола");
  const between = tape.slice(issue + 1, done).filter((e) => e.startsWith("выпуск ") || e.startsWith("поток открыт"));
  if (between.length > 0) out.push(`п. 2: между выпуском и исходом глагола выпущено: ${between.join("; ")}`);
  if (at(tape, `выпуск ${READ}`, done) < 0) out.push("п. 3: отменённое чтение не выпущено снова после исхода глагола");
  const late = at(tape, `выпуск ${LATE}`);
  if (late < 0 || late < done) out.push("п. 2: обращение, начатое во время глагола, выпущено до его исхода");
  if (!tape.slice(done).some((e) => e.startsWith("поток открыт"))) out.push("п. 1: поток не открыт снова после исхода");
  return out;
}

/** Нарушения близнеца: у глагола, носителя не ставящего, упорядочения нет. */
function twinBreaches(tape: Tape, verb: string): string[] {
  const out: string[] = [];
  const issue = at(tape, `выпуск ${verb}`);
  const done = outcomeOf(tape, verb, issue);
  if (issue < 0 || done < 0) return [`глагол ${verb} не выпущен либо без исхода`];
  if (at(tape, `отмена ${READ}`) >= 0) out.push("чтение в полёте отменено");
  if (tape.some((e) => e.startsWith("поток закрыт"))) out.push("поток закрыт");
  const mutationDone = outcomeOf(tape, MUTATION);
  if (mutationDone >= 0 && mutationDone < issue) out.push("глагол ждал исхода мутации");
  const late = at(tape, `выпуск ${LATE}`);
  if (late < 0 || late > done) out.push("обращение, начатое во время глагола, ждало его исхода");
  return out;
}

const holder = (kind: ConstructorParameters<typeof FormTokenHolder>[0]) => new FormTokenHolder(kind);
const byCode = { method: "lookup_secret" as const, code: "abcd-efgh" };

/** Семь глаголов, ставящих носитель, которые консоль зовёт в S1+S2 (N17). */
const CARRIER_VERBS: ReadonlyArray<{ name: string; verb: string; run: () => Promise<unknown> }> = [
  { name: "вход", verb: "POST /iam/v1/auth/login", run: () => loginLane.login(holder("login"), { email: "a@kacho.local", password: "p" }) },
  {
    name: "регистрация",
    verb: "POST /iam/v1/auth/register",
    run: () => loginLane.register(holder("register"), { email: "a@kacho.local", password: "p" }),
  },
  {
    name: "смена пароля",
    verb: "POST /iam/v1/auth/password",
    run: () => loginLane.changePassword(holder("password"), { currentPassword: "p", newPassword: "q" }),
  },
  {
    name: "подтверждение",
    verb: "POST /iam/v1/auth/second-factor/confirm",
    run: () => loginLane.confirm(holder("second-factor"), "123456"),
  },
  { name: "снятие", verb: "POST /iam/v1/auth/second-factor/remove", run: () => loginLane.remove(holder("second-factor"), byCode) },
  {
    name: "перечеканка",
    verb: "POST /iam/v1/auth/second-factor/backup-codes",
    run: () => loginLane.regenerateBackupCodes(holder("second-factor"), byCode),
  },
  {
    name: "повышение",
    verb: "POST /iam/v1/auth/step-up",
    run: () => loginLane.stepUp(holder("step-up"), { method: "password", password: "p" }),
  },
];

describe("F8-46 · упорядочение вокруг глагола, ставящего носитель (Р10)", () => {
  for (const { name, verb, run } of CARRIER_VERBS) {
    for (const outcome of OUTCOMES) {
      it(`F8-46 · ${name}, исход «${outcome}»: чтение отменено и выпущено снова, мутация дождалась, поток закрыт и открыт, новое ждёт исхода`, async () => {
        const tape = await scenario(verb, run, outcome);
        expect({ breaches: orderBreaches(tape, verb), tape }).toEqual({ breaches: [], tape });
      });
    }
  }

  it("F8-46 · единица — сетевое обращение: мутация, получившая вызов повышения, повышения не задерживает, а её повтор выпущен после его исхода", async () => {
    const tape: Tape = [];
    const net = installNet(tape);
    const stepUp = "POST /iam/v1/auth/step-up";
    setStepUpRequester(async () => {
      await loginLane.stepUp(holder("step-up"), { method: "password", password: "p" });
    });
    try {
      const mutation = api.create("/vpc/v1/networks", { name: "n" });
      await flush();
      net.take("POST /vpc/v1/networks").answer(
        401,
        { code: 16, message: "insufficient_user_authentication" },
        {
          "WWW-Authenticate":
            'Bearer error="insufficient_user_authentication", error_description="Required ACR 2", acr_values="2"',
        },
      );
      // Сетевое обращение мутации уже имеет исход — отказ края, — и повышение
      // выпускается, не дожидаясь логического вызова, который его и ждёт.
      await until(tape, `выпуск ${stepUp}`, () => issued(tape, stepUp) > 0);
      expect(issued(tape, "POST /vpc/v1/networks")).toBe(1);
      net.take(stepUp).answer(200, {});
      await until(tape, "повтор мутации", () => issued(tape, "POST /vpc/v1/networks") === 2);
      const retry = tape.lastIndexOf("выпуск POST /vpc/v1/networks");
      expect(retry).toBeGreaterThan(outcomeOf(tape, stepUp));
      net.take("POST /vpc/v1/networks").answer(200, { operation: { id: "op-2" } });
      await expect(mutation).resolves.toEqual({ operation: { id: "op-2" } });
    } finally {
      setStepUpRequester(null);
      net.restore();
    }
  });

  it("F8-46 · ответ, полученный до выпуска глагола, не отменяется: тело читается", async () => {
    // Исход обращения — заголовки (Р6 п. 1). Чтение, у которого исход уже есть,
    // глаголу не мешает, а его отмена сорвала бы чтение тела, а не обращение.
    const tape: Tape = [];
    const net = installNet(tape);
    try {
      const read = api.get<{ networks: unknown[] }>("/vpc/v1/networks");
      net.take(READ).answer(200, { networks: [{ id: "net-1" }] });
      // Ответ транспорта доставлен: его первая реакция отработала, тело ещё не прочитано.
      await Promise.resolve();
      const verb = orderedTransport.fetch("/iam/v1/auth/password", { method: "POST" }, { setsCarrier: true });
      await until(tape, "выпуск глагола", () => issued(tape, "POST /iam/v1/auth/password") > 0);
      net.take("POST /iam/v1/auth/password").answer(200, {});
      await expect(read).resolves.toEqual({ networks: [{ id: "net-1" }] });
      expect(issued(tape, READ)).toBe(1);
      await verb;
    } finally {
      net.restore();
    }
  });

  it("F8-46 · вызывающий получает ответ повторного выпуска: отмены чтения он не видит", async () => {
    const tape: Tape = [];
    const net = installNet(tape);
    try {
      const read = api.get<{ networks: unknown[] }>("/vpc/v1/networks");
      const verb = loginLane.changePassword(holder("password"), { currentPassword: "p", newPassword: "q" });
      await until(tape, "выпуск глагола", () => issued(tape, "POST /iam/v1/auth/password") > 0);
      net.take("POST /iam/v1/auth/password").answer(200, { session: {} });
      await until(tape, "повторный выпуск чтения", () => issued(tape, READ) === 2);
      net.take(READ).answer(200, { networks: [{ id: "net-1" }] });
      await expect(read).resolves.toEqual({ networks: [{ id: "net-1" }] });
      await verb;
    } finally {
      net.restore();
    }
  });
});

describe("F8-46 · близнецы: глагол, носителя не ставящий, упорядочения не получает", () => {
  for (const outcome of OUTCOMES) {
    it(`F8-46 · заведение второго фактора, исход «${outcome}»: ничего не отменено, не закрыто и не задержано`, async () => {
      const verb = "POST /iam/v1/auth/second-factor/enroll";
      const tape = await scenario(verb, () => loginLane.enroll(holder("second-factor")), outcome);
      expect({ breaches: twinBreaches(tape, verb), tape }).toEqual({ breaches: [], tape });
    });
  }

  it("F8-46 · признак формы: ничего не отменено, не закрыто и не задержано", async () => {
    const tape: Tape = [];
    const net = installNet(tape, { autoFormToken: false });
    const stream = streamOnTape(tape);
    const verb = "GET /iam/v1/auth/csrf";
    try {
      const read = api.get("/vpc/v1/networks").catch(() => undefined);
      const token = formToken("password").catch(() => undefined);
      await flush();
      const late = api.get("/vpc/v1/addresses").catch(() => undefined);
      await flush();
      net.take(verb).answer(200, { csrfToken: "tok-1" });
      await flush();
      net.drain();
      await Promise.all([read, token, late]);
      expect({ breaches: twinBreaches(tape, verb), tape }).toEqual({ breaches: [], tape });
    } finally {
      stream.off();
      net.restore();
    }
  });
});

describe("F8-46 · держатель способен упасть: обе стороны краснеют на обезвреженном упорядочении", () => {
  it("F8-46 · консоль без упорядочения — смена пароля выпущена мимо него: нарушены все пункты", async () => {
    const verb = "POST /iam/v1/auth/password";
    const tape = await scenario(
      verb,
      () => globalThis.fetch("/iam/v1/auth/password", { method: "POST", body: "{}" }),
      "ответ",
    );
    expect(orderBreaches(tape, verb)).toEqual([
      "п. 1: чтение в полёте не отменено до выпуска глагола",
      "п. 1: мутация в полёте не дождалась исхода до выпуска глагола",
      "п. 1: поток не закрыт до выпуска глагола",
      `п. 2: между выпуском и исходом глагола выпущено: выпуск ${LATE}`,
      "п. 3: отменённое чтение не выпущено снова после исхода глагола",
      "п. 2: обращение, начатое во время глагола, выпущено до его исхода",
      "п. 1: поток не открыт снова после исхода",
    ]);
  });

  it("F8-46 · консоль, упорядочивающая всякую отправку, — заведение второго фактора выпущено упорядочением: близнец красный", async () => {
    const verb = "POST /iam/v1/auth/second-factor/enroll";
    const tape = await scenario(verb, () => orderedTransport.fetch("/iam/v1/auth/second-factor/enroll", { method: "POST" }, { setsCarrier: true }), "ответ");
    expect(twinBreaches(tape, verb)).toEqual([
      "чтение в полёте отменено",
      "поток закрыт",
      "глагол ждал исхода мутации",
      "обращение, начатое во время глагола, ждало его исхода",
    ]);
  });
});
