// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import {
  QUOTA_OWNER_PATHS,
  type OwnerAnswer,
  postureOf,
  reasonFromBody,
} from "./quota-posture";

/**
 * ЧТЕНИЕ витрины квот у живого края — вторая половина к решению о посадке.
 *
 * Решение («какая посадка следует из этих ответов») живёт в `quota-posture.ts`
 * без единого импорта и проверяется голым `node` до подъёма стенда. Здесь —
 * только добыча входа для него: открыть витрину и записать, что ответил каждый
 * владелец пределов.
 *
 * Разделение несущее, а не косметическое: слепив их, самопроверку решения
 * пришлось бы гонять браузером и стендом, то есть не гонять никогда.
 */

/** Что наблюдалось при открытии витрины. */
export interface Showcase {
  /** По одному на КАЖДОГО выписанного владельца, в том же порядке. */
  answers: OwnerAnswer[];
  /** Пути, фактически наблюдённые в сети, — против выписанных. */
  observed: string[];
  /** Выписанные, но не спрошенные. */
  silent: string[];
}

/**
 * Открывает витрину квот и собирает, что ответил КАЖДЫЙ владелец.
 *
 * Наблюдатель ставится ДО перехода: ответ, пришедший раньше подписки, был бы
 * потерян, и «владелец молчал» означало бы «мы не слушали».
 */
export async function openShowcase(page: Page, projectId: string): Promise<Showcase> {
  const observed = new Set<string>();
  page.on("response", (r) => {
    const { pathname } = new URL(r.url());
    if (/^\/[a-z]+\/v1\/quotas$/.test(pathname)) observed.add(pathname);
  });

  const waits = QUOTA_OWNER_PATHS.map((path) =>
    page.waitForResponse((r) => new URL(r.url()).pathname === path, { timeout: 60_000 }),
  );
  await page.goto(`/projects/${projectId}/quotas`, { waitUntil: "domcontentloaded" });
  const settled = await Promise.allSettled(waits);

  const answers: OwnerAnswer[] = [];
  for (const [i, s] of settled.entries()) {
    const path = QUOTA_OWNER_PATHS[i];
    if (s.status !== "fulfilled") {
      answers.push({ path, answered: false, status: 0, reason: "", kinds: [] });
      continue;
    }
    const status = s.value.status();
    if (status === 200) {
      const body = (await s.value.json()) as { quotas?: Array<{ kind: string }> };
      answers.push({
        path,
        answered: true,
        status,
        reason: "",
        kinds: (body.quotas ?? []).map((q) => q.kind),
      });
      continue;
    }
    // Тело читается ОДИН раз и отдаётся разбору текстом: повторное чтение
    // потока ответа у playwright законно, но признак обязан браться тем же
    // разбором, что проверен самопроверкой, — иначе их два.
    let text = "";
    try {
      text = await s.value.text();
    } catch {
      text = "";
    }
    answers.push({ path, answered: true, status, reason: reasonFromBody(text), kinds: [] });
  }

  return {
    answers,
    observed: [...observed].sort(),
    silent: answers.filter((a) => !a.answered).map((a) => a.path),
  };
}

/**
 * Посадка установлена — либо прогон останавливается, назвав разнобой.
 *
 * Вынесено сюда, потому что этого требуют ВСЕ пробы витрины, а формулировка
 * отказа обязана быть одна: вторая редакция той же фразы разошлась бы с первой
 * молча.
 */
export function postureOrFail(answers: OwnerAnswer[]): "deployed" | "absent" {
  const posture = postureOf(answers);
  expect(
    posture.kind === "split" ? posture.detail : "",
    "владельцы пределов разошлись в понимании установки: посадка ОДНА на установку — " +
      "домен величин либо развёрнут, либо объявлен отсутствующим. Разнобой (часть отказала " +
      "другим признаком, часть не ответила) посадкой не является и читается как находка",
  ).toBe("");
  return posture.kind === "deployed" ? "deployed" : "absent";
}
