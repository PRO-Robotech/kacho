// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync } from "node:fs";
import path from "node:path";
import { expect, type Page, type Request, type Route, type Worker } from "@playwright/test";
import { test } from "./fixtures";
import { conditionNotCreated } from "./mail-receiver";

/**
 * NTF2-58 · КОНСОЛЬ НА `503` ОГРАНИЧИТЕЛЯ И НА ИСЧЕРПАННЫЙ БЮДЖЕТ РЕШАТЕЛЯ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ (приёмка NTF-2, Р5, сценарий 58; стенд П12)
 *
 * Экран восстановления консоли показывает `message` отказа края ДОСЛОВНО и
 * своего текста не имеет; на `503` звена решатель не запускается и запрос не
 * повторяется; бюджет решателя `Bs` отсчитывает таймер ГЛАВНОГО потока
 * страницы, и по его истечении `Worker` завершён, доказательство не уходит ни
 * тогда, ни после, а следующая отправка получает новый вызов.
 *
 * СТЕНД П12. Страница консоли — из сборки стенда П1; ответ на
 * `POST /iam/v1/auth/recovery` задаёт перехват страницы (`page.route`), к краю
 * П1 запрос восстановления не уходит и счётчики края не меняются; прочие
 * запросы страницы идут к краю. Часы страницы ставит проба (`page.clock`); на
 * создание и завершение `Worker` проба подписана. Часы внутри `Worker` пробой не
 * управляются и в отсчёт бюджета не входят.
 *
 * `Bs` и срок вызова — из общего файла векторов края и консоли
 * (`gateway/internal/middleware/anonmail/testdata/pow_vectors.json`, замысел
 * `issue-2917` З10): тот же файл судит константу консоли модульной пробой.
 */

/**
 * Путь к общему файлу векторов берётся от каталога проб, который объявляет
 * конфигурация прогона (`test.info().project.testDir` → `ui-future/e2e/specs`),
 * а не от `import.meta.url`: прогонщик проб загружает спеки как CommonJS, и
 * файл с `import.meta` Node читает как модуль ES, где `require` загрузчика
 * прогонщика не определён, — спек не загружается, и с ним ни одна проба набора.
 */
function vectorsPath(): string {
  const e2eRoot = path.dirname(test.info().project.testDir);
  return path.join(e2eRoot, "../../gateway/internal/middleware/anonmail/testdata/pow_vectors.json");
}

interface PowVectors {
  challengeTtlSeconds: number;
  solverBudgetSeconds: number;
  vectors: Array<{ challenge: string; bits: number; accepted: boolean }>;
}

function loadVectors(): PowVectors {
  const VECTORS_PATH = vectorsPath();
  const raw = JSON.parse(readFileSync(VECTORS_PATH, "utf8")) as PowVectors;
  if (!Number.isInteger(raw.solverBudgetSeconds) || !Number.isInteger(raw.challengeTtlSeconds)) {
    throw new Error(`фикстура: ${VECTORS_PATH} не несёт целых solverBudgetSeconds/challengeTtlSeconds`);
  }
  const tokens = new Set(raw.vectors.map((v) => v.challenge));
  if (tokens.size < 2) throw new Error(`фикстура: ${VECTORS_PATH} не несёт двух различимых вызовов`);
  return raw;
}

const RECOVERY = "/iam/v1/auth/recovery";
const PROOF = "x-kacho-proof";
const UNIFORM = "Если адрес зарегистрирован, мы отправили письмо";
const STORE_UNAVAILABLE = "request limiter is unavailable";
/** Часы страницы — известный момент, а не часы раннера. */
const PAGE_EPOCH = new Date("2026-10-08T12:00:00Z");

/** Тело вызова края — как его пишет `anonmail.writeChallenge`. */
function challengeBody(token: string, bits: number) {
  return {
    code: 8,
    message: "proof of work required",
    details: [
      {
        "@type": "type.googleapis.com/google.rpc.ErrorInfo",
        reason: "PROOF_OF_WORK_REQUIRED",
        domain: "api-gateway.kacho.cloud",
        metadata: {
          challenge: token,
          difficultyBits: String(bits),
          expiresAt: "2026-10-08T12:05:00Z",
        },
      },
    ],
  };
}

type Answer = { status: number; body: unknown };

/** Запрос восстановления, как его увидел перехват: был ли заголовок доказательства и когда. */
interface Seen {
  proof: string | null;
  at: number;
}

/**
 * Поставить перехват `POST /iam/v1/auth/recovery` и подписку на `Worker`.
 * `answer(n, proof)` — ответ на n-й запрос (с 1).
 */
async function stand(page: Page, answer: (n: number, proof: string | null) => Answer) {
  const seen: Seen[] = [];
  const workers: Array<{ worker: Worker; closed: boolean }> = [];
  page.on("worker", (worker) => {
    const life = { worker, closed: false };
    workers.push(life);
    worker.on("close", () => {
      life.closed = true;
    });
  });
  await page.route(`**${RECOVERY}`, async (route: Route, request: Request) => {
    if (request.method() !== "POST") return route.fallback();
    const proof = request.headers()[PROOF] ?? null;
    seen.push({ proof, at: Date.now() });
    const a = answer(seen.length, proof);
    await route.fulfill({
      status: a.status,
      contentType: "application/json; charset=utf-8",
      body: JSON.stringify(a.body),
    });
  });
  await page.clock.install({ time: PAGE_EPOCH });
  await page.goto("/recovery");
  const form = page.getByRole("form");
  const screen = {
    email: page.getByRole("textbox", { name: "Адрес электронной почты" }),
    submit: form.getByRole("button"),
    refusal: page.getByRole("alert"),
  };
  await expect(screen.email, "экран восстановления не показал формы").toBeEditable();
  return { seen, workers, screen };
}

const address = (tag: string) => `ntf2-58-${tag}-${Date.now().toString(36)}@kacho.local`;

test.describe("NTF2-58 · консоль на 503 ограничителя и на исчерпанный бюджет решателя", () => {
  test("NTF2-58 (а) · 503 — `request limiter is unavailable` дословно, форма редактируема, без доказательства, Worker не создан", async ({
    page,
  }) => {
    // verifies #2917
    const s = await stand(page, () => ({
      status: 503,
      body: { code: 14, message: STORE_UNAVAILABLE, details: [] },
    }));
    await s.screen.email.fill(address("a"));
    await s.screen.email.press("Enter");

    await expect(s.screen.refusal).toHaveText(STORE_UNAVAILABLE);
    await expect(s.screen.email).toBeEditable();
    await expect(s.screen.submit).toBeEnabled();
    expect(s.seen.map((r) => r.proof)).toEqual([null]);
    expect(s.workers, "на 503 страница не создаёт ни одного Worker").toHaveLength(0);
  });

  test("NTF2-58 (г) · 503 с текстом, выбранным пробой, — показан дословно, копии текста края нет", async ({ page }) => {
    // verifies #2917
    const n = Math.floor(Math.random() * 1_000_000);
    test.info().annotations.push({ type: "limiter probe", description: String(n) });
    console.log(`NTF2-58 (г): <n> = ${n}`);
    const text = `limiter probe ${n}`;
    const s = await stand(page, () => ({
      status: 503,
      body: { code: 14, message: text, details: [] },
    }));
    await s.screen.email.fill(address("g"));
    await s.screen.email.press("Enter");

    await expect(s.screen.refusal).toHaveText(text);
    await expect(page.getByText(STORE_UNAVAILABLE), "экран печатает свою копию текста края").toHaveCount(0);
    await expect(s.screen.email).toBeEditable();
    await expect(s.screen.submit).toBeEnabled();
    expect(s.seen.map((r) => r.proof)).toEqual([null]);
    expect(s.workers).toHaveLength(0);
  });

  test("NTF2-58 (б) · бюджет Bs по таймеру страницы: в Bs − 1 с решатель работает, в Bs завершён, `proof of work required`, повтор — новый вызов", async ({
    page,
  }) => {
    // verifies #2917
    const v = loadVectors();
    const budgetMs = v.solverBudgetSeconds * 1000;
    console.log(`NTF2-58 (б): Bs = ${v.solverBudgetSeconds} с, срок вызова = ${v.challengeTtlSeconds} с`);
    test.info().annotations.push({
      type: "Bs",
      description: `${v.solverBudgetSeconds} s`,
    });
    test.info().annotations.push({
      type: "challenge ttl",
      description: `${v.challengeTtlSeconds} s`,
    });
    expect(budgetMs, "бюджет решателя обязан быть меньше срока вызова").toBeLessThan(v.challengeTtlSeconds * 1000);
    const [first, second] = [...new Set(v.vectors.map((x) => x.challenge))];

    const s = await stand(page, (n, proof) =>
      proof !== null ? { status: 200, body: {} } : { status: 429, body: challengeBody(n === 1 ? first : second, 24) },
    );
    await s.screen.email.fill(address("b"));
    // Часы страницы стоят с этого момента: отсчёт бюджета двигает только проба.
    await page.clock.pauseAt(new Date(PAGE_EPOCH.getTime() + 60_000));
    await s.screen.email.press("Enter");
    await expect
      .poll(() => s.workers.length, {
        message: "на вызов страница не создала Worker",
      })
      .toBe(1);

    const solvedEarly = (moment: string) => {
      const early = s.seen.find((r) => r.proof !== null);
      if (early) {
        conditionNotCreated(
          `условие не создано: решатель нашёл решение раньше Bs (${moment}; запрос с X-Kacho-Proof в ${new Date(early.at).toISOString()}) — вызов решаем в бюджете, случай бюджета не построен`,
        );
      }
    };

    await page.clock.runFor(budgetMs - 1000);
    solvedEarly("Bs − 1 с");
    expect(s.workers[0].closed, "в Bs − 1 с решатель обязан работать").toBe(false);
    await expect(s.screen.submit, "в Bs − 1 с форма в положении отправки").toBeDisabled();

    await page.clock.runFor(1000);
    solvedEarly("Bs");
    await expect
      .poll(() => s.workers[0].closed, {
        message: "в Bs Worker не завершён таймером страницы",
      })
      .toBe(true);
    await expect(s.screen.refusal).toHaveText("proof of work required");
    await expect(s.screen.email).toBeEditable();
    await expect(s.screen.submit).toBeEnabled();

    // «Ни после»: доказательства нет и по истечении срока вызова.
    await page.clock.runFor(v.challengeTtlSeconds * 1000);
    expect(s.seen.filter((r) => r.proof !== null)).toHaveLength(0);

    await s.screen.email.press("Enter");
    await expect.poll(() => s.seen.length, { message: "повторная отправка не ушла" }).toBe(2);
    expect(s.seen[1].proof, "повтор переиспользовал прежний вызов").toBeNull();
    await expect
      .poll(() => s.workers.length, {
        message: "на новый вызов — новый Worker",
      })
      .toBe(2);
  });

  test("NTF2-58 близнец (в) · вызов, решаемый в бюджете, — повтор с X-Kacho-Proof, единый текст, ошибки нет", async ({
    page,
  }) => {
    // verifies #2917
    const v = loadVectors();
    const token = v.vectors[0].challenge;
    const s = await stand(page, (_n, proof) =>
      proof !== null ? { status: 200, body: {} } : { status: 429, body: challengeBody(token, 8) },
    );
    await s.screen.email.fill(address("v"));
    await s.screen.email.press("Enter");

    await expect(page.getByText(UNIFORM)).toBeVisible();
    await expect(s.screen.refusal).toHaveCount(0);
    expect(s.seen).toHaveLength(2);
    expect(s.seen[0].proof).toBeNull();
    expect(s.seen[1].proof?.startsWith(`${token}:`)).toBe(true);
  });
});
