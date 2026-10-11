// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { expect, type TestInfo } from "@playwright/test";
import { LANE, lastIssued, newSeed, seedAddress, seedConfirmedHuman, seedSecondFactor, totpCode, type Seed } from "./ceremony-seed";
import { test } from "./fixtures";
import { conditionNotCreated } from "./mail-receiver";
import { answerOf, signedIn } from "./session-lane";

/**
 * Ф12-35 «в» — смена перечня ключей обёртки секретов второго фактора МЕЖДУ
 * шагами одной пробы, через край (kacho#3038, держатель позиции — kacho#1281).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО СУДИТСЯ
 *
 *   Ф12 — `docs/engineering/acceptance/second-factor-totp-and-recovery-codes.md`
 *         (PRO-Robotech/kaname, ветка 296 @ 937cdade5)
 *         c6ea48973b048e0430bf3712e317aca39ac7df2e77dcb5e8d71e65c59aa33e2f — APPROVED.
 *
 * Половины «а» и «б» (отказ старта) держат пробы процесса службы; «г» — посев
 * мимо глаголов семейства, это предмет набора службы. Здесь — «в»: фактор
 * заведён под `K1`; процесс перезапущен с перечнем `K2,K1` — код сверяется
 * (`200`), самоотчёт старта печатает число ключей 2; перезапущен с `K2` без
 * `K1` — предъявление кода `503` с фиксированным текстом, без `Retry-After`,
 * запись журнала уровня ошибки, клетка счётчика «материал не открывается»
 * выросла, в счёт попыток не идёт.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * КТО СОЗДАЁТ УСЛОВИЕ
 *
 * Перезапуск службы с другим перечнем — не глагол продукта, и проба его не
 * изображает: условие создаёт оснастка стенда, `deploy/scripts/stand-second-factor-wrap-rotate.sh`
 * (цель `make -C deploy stand-second-factor-wrap-rotate`). `K1` — ключ стенда до
 * пробы, `K2` — свежий, один на весь ход; материал ключей проба не видит.
 * Скрипт меняет секрет, перекатывает службу, ждёт готовности края и печатает
 * число ключей из самоотчёта ЗАПУЩЕННОГО процесса. Кластер скрипт выбирает сам:
 * пространство стенда проб (`stand-ns.sh run`) — его названным контекстом,
 * иначе рабочее пространство стенда kind. Не смог — «условие не создано», а не
 * красное. После пробы перечень возвращается к ключу стенда — и на падении.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ОДИН ФАКТ МЕЖДУ ОТКАЗОМ И БЛИЗНЕЦОМ
 *
 * Отказ (`K2`) и одно-фактный близнец (`K2,K1`) — тот же человек, тот же вход
 * паролем, тот же глагол повышения и верный код своего шага; отличается только
 * перечень ключей запущенного процесса. Близнец идёт ПОСЛЕ отказов и после
 * `N + 1` предъявлений, где `N` — предел неверных предъявлений на адрес у
 * процесса: если бы `503` шли в счёт, близнец получил бы отказ по частоте, а не
 * `200`. Внутренняя сторона того же — клетка «неверный код» не растёт.
 */

const ROTATOR = "deploy/scripts/stand-second-factor-wrap-rotate.sh";

/** Текст отказа «материал не открывается» — дословно Ф12-35 «Тогда» (Р2: текст один). */
const UNAVAILABLE_TEXT = "second factor cannot be verified; ask the administrator of this installation";

/** Отрицательный контроль текста (Ф12-35 «И»): ни «позже», ни класса причины, ни имени ручки. */
const FORBIDDEN_WORDS = ["temporarily", "try again", "later", "key", "wrap", "secret", "material", "decrypt", "encrypt"];

/** Клетки счётчиков службы (Ф12-43): «материал не открывается», отказ «недоступно», «неверный код». */
const CELL_UNREADABLE = 'kaname_second_factor_presentations_total{method="totp",outcome="material-unreadable"}';
const CELL_UNAVAILABLE = 'kaname_second_factor_refusals_total{reason="unavailable"}';
const CELL_MISMATCHED = 'kaname_second_factor_presentations_total{method="totp",outcome="mismatched"}';

/** Предел одного вызова оснастки: перекат службы и готовность края — минуты, не десятки минут. */
const ROTATOR_BUDGET_MS = 10 * 60_000;

interface Rotator {
  /** Перечень `K1`/`K2` — процесс перезапущен с ним; число ключей из самоотчёта старта. */
  set(keys: string): number;
  restore(): string | null;
  cell(series: string): number;
  /** Записи журнала службы уровня ошибки с момента `since`. */
  errors(since: string): string[];
  /** Предел неверных предъявлений на адрес у запущенного процесса. */
  limit(): number;
  dispose(): void;
}

function rotatorOf(testInfo: TestInfo): Rotator {
  const repoRoot = path.resolve(path.dirname(testInfo.project.testDir), "..", "..");
  const script = path.join(repoRoot, ROTATOR);
  const state = mkdtempSync(path.join(tmpdir(), "kacho-wrap-rotate-"));
  const call = (args: string[]): { code: number; out: string } => {
    const r = spawnSync("bash", [script, ...args], { encoding: "utf8", timeout: ROTATOR_BUDGET_MS });
    const out = `${r.stdout ?? ""}${r.stderr ?? ""}`;
    console.log(`[Ф12-35] оснастка ${args.join(" ")} → код ${r.status ?? r.signal ?? r.error?.message}\n${out}`);
    return { code: r.status ?? -1, out };
  };
  const need = (args: string[]): string => {
    const r = call(args);
    if (r.code !== 0) {
      conditionNotCreated(
        `условие не создано: оснастка стенда «${args.join(" ")}» не исполнилась (код ${r.code}) — ` +
          r.out.trim().split("\n").slice(-3).join(" | "),
      );
    }
    return r.out;
  };
  const lastNumber = (out: string, what: string): number => {
    const last = out.trim().split("\n").pop() ?? "";
    const n = Number(last);
    if (!Number.isFinite(n)) conditionNotCreated(`условие не создано: оснастка не назвала ${what} числом — «${last}»`);
    return n;
  };
  return {
    set(keys) {
      const out = need(["set", keys, "--state", state]);
      const m = /wrap-rotate: keys=(\d+)/.exec(out);
      if (!m) conditionNotCreated(`условие не создано: оснастка не напечатала самоотчёт старта после перечня ${keys}`);
      return Number(m[1]);
    },
    restore() {
      const r = call(["restore", "--state", state]);
      return r.code === 0 ? null : `перечень ключей стенда НЕ восстановлен (код ${r.code}): ${r.out.trim()}`;
    },
    cell: (series) => lastNumber(need(["cell", series]), `клетку ${series}`),
    errors(since) {
      const lines = need(["errors", since]).trim().split("\n");
      return lines.filter((l) => !/^errors=\d+$/.test(l) && /"level":"ERROR"/.test(l));
    },
    limit: () => lastNumber(need(["limit"]), "предел неверных предъявлений на адрес"),
    dispose: () => rmSync(state, { recursive: true, force: true }),
  };
}

/** Шаг кода по времени (RFC 6238, 30 с) — тот, чей код проба предъявляет. */
const stepOf = (ms: number) => Math.floor(ms / 30_000);

/**
 * Дождаться шага кода, СЛЕДУЮЩЕГО за принятым: служба помнит последний принятый
 * шаг, и тот же код был бы повтором. Ждётся условие (шаг сменился), а не срок.
 */
async function nextStepAfter(accepted: number): Promise<void> {
  await expect
    .poll(() => stepOf(Date.now()), { message: "шаг кода не сменился", timeout: 45_000, intervals: [1_000] })
    .toBeGreaterThan(accepted);
}

/** Повышение кодом по времени носителем посева: ответ края и тело. */
async function stepUpWithTotp(lane: Seed, secret: string): Promise<{ status: number; text: string; retryAfter: string | undefined; step: number }> {
  const at = Date.now();
  const res = await lane.submit(LANE.stepUp, "step-up", { method: "totp", code: totpCode(secret, at) });
  const a = await answerOf(res);
  return { status: a.status, text: a.text, retryAfter: res.headers()["retry-after"], step: stepOf(at) };
}

test("Ф12-35 (в) · перечень K2,K1 сверяет код, K2 без K1 — 503 громко и не в счёт попыток", async ({ browserName: _browserName }, testInfo) => {
  // verifies #3038 — Ф12-35 «в» (Ф12 c6ea4897…) через край; держатель позиции — #1281.
  test.setTimeout(30 * 60_000);
  const rotator = rotatorOf(testInfo);
  const seeds: Seed[] = [];
  let failed: string | null = null;
  try {
    // Дано: человек с подтверждённым адресом и фактором, заведённым под перечнем стенда (K1).
    const owner = await newSeed(testInfo);
    seeds.push(owner);
    const human = await seedConfirmedHuman(owner, seedAddress("f12-35"));
    const factor = await seedSecondFactor(owner);
    let accepted = stepOf(Date.now());

    // Когда: процесс перезапущен с перечнем K2,K1.
    expect(rotator.set("K2,K1"), "самоотчёт старта под перечнем K2,K1 обязан назвать 2 ключа").toBe(2);

    // Тогда: код сверяется — 200, сессия «2».
    const l1 = await signedIn(testInfo, human.email, human.password, "L1 под K2,K1");
    seeds.push(l1);
    await nextStepAfter(accepted);
    const passed = await stepUpWithTotp(l1, factor.secret);
    const passedBody = lastIssued(l1, LANE.stepUp).body as { session?: { assuranceLevel?: unknown } } | null;
    expect(passed.status, `под K2,K1 код фактора, заведённого под K1, обязан сверяться: ${passed.text}`).toBe(200);
    expect(String(passedBody?.session?.assuranceLevel), "повышение кодом под K2,K1 — сессия «2»").toBe("2");
    accepted = passed.step;

    // Когда: процесс перезапущен с перечнем K2 без K1.
    expect(rotator.set("K2"), "самоотчёт старта под перечнем K2 обязан назвать 1 ключ").toBe(1);
    const limit = rotator.limit();
    expect(limit, "предел неверных предъявлений на адрес у процесса — положительное число").toBeGreaterThan(0);
    const l2 = await signedIn(testInfo, human.email, human.password, "L2 под K2");
    seeds.push(l2);
    const before = {
      unreadable: rotator.cell(CELL_UNREADABLE),
      unavailable: rotator.cell(CELL_UNAVAILABLE),
      mismatched: rotator.cell(CELL_MISMATCHED),
    };
    const since = new Date(Date.now() - 1_000).toISOString().replace(/\.\d{3}Z$/, "Z");

    // Тогда: каждое из N + 1 предъявлений верного кода — 503, один текст, без Retry-After.
    const presentations = limit + 1;
    const refused: string[] = [];
    for (let i = 0; i < presentations; i++) {
      const r = await stepUpWithTotp(l2, factor.secret);
      expect(r.status, `предъявление ${i + 1} из ${presentations} под K2: материал не открывается — 503, не 401 и не 429: ${r.text}`).toBe(503);
      expect(r.retryAfter, `предъявление ${i + 1}: заголовка Retry-After быть не должно`).toBeUndefined();
      const body = JSON.parse(r.text) as { code?: unknown; message?: unknown };
      expect({ code: body.code, message: body.message }, `тело отказа ${i + 1} — UNAVAILABLE и текст Ф12-35`).toEqual({
        code: 14,
        message: UNAVAILABLE_TEXT,
      });
      refused.push(r.text);
    }
    expect(new Set(refused).size, "текст отказа один на все предъявления (Р2)").toBe(1);
    for (const w of FORBIDDEN_WORDS) {
      expect(refused[0].toLowerCase(), `текст отказа не называет «${w}» (Ф12-35, отрицательный контроль текста)`).not.toContain(w);
    }

    // Тогда: клетка «материал не открывается» и отказ «недоступно» выросли на число предъявлений;
    // «неверный код» не вырос — в счёт попыток не идёт.
    expect(
      {
        unreadable: rotator.cell(CELL_UNREADABLE) - before.unreadable,
        unavailable: rotator.cell(CELL_UNAVAILABLE) - before.unavailable,
        mismatched: rotator.cell(CELL_MISMATCHED) - before.mismatched,
      },
      "клетки счётчиков службы после предъявлений под K2",
    ).toEqual({ unreadable: presentations, unavailable: presentations, mismatched: 0 });

    // Журнал читается здесь, пока процесс под K2 жив; утверждается последним — после
    // близнеца, чтобы красное о журнале не оставило ветку близнеца неисполненной.
    const errors = rotator.errors(since);

    // Близнец: тот же человек, тот же глагол, перечень снова K2,K1 — код сверяется. Это и
    // «в счёт попыток не идёт»: N + 1 предъявлений выше дали бы отказ по частоте здесь.
    expect(rotator.set("K2,K1"), "самоотчёт старта близнеца — 2 ключа").toBe(2);
    const l3 = await signedIn(testInfo, human.email, human.password, "L3 под K2,K1");
    seeds.push(l3);
    await nextStepAfter(accepted);
    const twin = await stepUpWithTotp(l3, factor.secret);
    expect(twin.status, `близнец под K2,K1 после ${presentations} предъявлений под K2: код обязан сверяться — ${twin.text}`).toBe(200);

    // Тогда (под K2): запись журнала уровня ошибки о неоткрываемом материале второго фактора.
    // Красное здесь — находка службы PRO-Robotech/kaname#700: на образе 296-ae1d4e35 записей 0.
    expect(
      errors.filter((l) => /second.factor|step-up|totp/i.test(l)),
      `запись журнала уровня ошибки о неоткрываемом материале второго фактора с ${since}; записей уровня ошибки всего ${errors.length}:\n${errors.join("\n")}`,
    ).not.toEqual([]);
  } finally {
    // Снятие — и на падении: перечень стенда возвращается к ключу до пробы.
    failed = rotator.restore();
    rotator.dispose();
    await Promise.all(seeds.map((s) => s.dispose()));
    if (failed) console.error(`[Ф12-35] ${failed}`);
  }
  // Проба прошла, а стенд не возвращён — это не зелёное: следующий прогон шёл бы по чужому перечню.
  expect(failed, "перечень ключей стенда после пробы обязан быть восстановлен").toBeNull();
});
