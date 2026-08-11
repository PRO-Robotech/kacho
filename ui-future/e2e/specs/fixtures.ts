// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";

/**
 * Общая фикстура проб консоли: завести арендатора и войти под ним.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ КАЖДАЯ ПРОБА ЗАВОДИТ СВОЕГО АРЕНДАТОРА
 *
 * Общий арендатор на все пробы означает, что выдача, сделанная одной, течёт в
 * ожидания другой: проба «этому не видно ничего» зеленеет или краснеет в
 * зависимости от порядка запуска. Своё имя на каждый прогон делает пробы
 * независимыми и позволяет гонять их параллельно, когда это понадобится.
 *
 * ПОЧЕМУ РЕГИСТРАЦИЯ ДВУХШАГОВАЯ И ЭТО НЕ ОБХОД
 *
 * Провайдер личности отдаёт сначала форму профиля, и только на втором шаге —
 * выбор способа входа с полем пароля. Проба, ожидающая пароль на первом шаге,
 * падает на ИСПРАВНОМ продукте: она описывает поток, которого нет. Здесь оба
 * шага пройдены явно, и каждый утверждает своё.
 */
export interface Tenant {
  email: string;
  projectId: string;
}

const PASSWORD = "Kacho-E2E-2026!x";

/** уникальное имя прогона — из времени; коллизия по UNIQUE(name) иначе даёт 409 */
export function runTag(): string {
  return Date.now().toString(36) + Math.trunc(performance.now()).toString(36);
}

/** registerAndSignIn проводит регистрацию до рабочей сессии и отдаёт проект арендатора. */
export async function registerAndSignIn(page: Page): Promise<Tenant> {
  const email = `e2e-${runTag()}@kacho.local`;

  await page.goto("/registration", { waitUntil: "domcontentloaded" });

  // Шаг 1 — профиль. Пароля здесь НЕТ по построению провайдера.
  await page.fill('input[name="traits.email"]', email);
  await page.fill('input[name="traits.name.first"]', "E2E");
  await page.fill('input[name="traits.name.last"]', "Probe");
  await page.click('button[type="submit"]');

  // Шаг 2 — способ входа. Ждём ПОЛЕ, а не время: ожидание временем даёт
  // «красное» на медленном стенде и зелёное на быстром при одном и том же коде.
  const password = page.locator('input[type="password"]');
  await expect(
    password.first(),
    "второй шаг регистрации не предложил пароль — поток входа неполон, " +
      "и без него арендатор не заводится вовсе",
  ).toBeVisible({ timeout: 30_000 });
  await password.first().fill(PASSWORD);
  await page.click('button[type="submit"]');

  // Сессия обязана быть установлена, иначе всё дальнейшее меряет не то.
  await expect
    .poll(
      async () => (await page.context().cookies()).some((c) => /session/i.test(c.name)),
      {
        message:
          "печенье сессии не установлено после регистрации — дальше проверялся бы " +
          "неаутентифицированный доступ под видом аутентифицированного",
        timeout: 30_000,
      },
    )
    .toBe(true);

  // Проект арендатора заводится сам; без него адресовать модули нечем.
  const projectId = await expect
    .poll(
      async () => {
        const res = await page.request.get("/iam/v1/projects");
        if (!res.ok()) return "";
        const body = (await res.json()) as { projects?: Array<{ id: string }> };
        return body.projects?.[0]?.id ?? "";
      },
      {
        message:
          "проект арендатора не появился: край признаёт личность, но решение о " +
          "правах принять не по чему — так выглядит незаписанная модель прав",
        timeout: 45_000,
      },
    )
    .not.toBe("");

  const res = await page.request.get("/iam/v1/projects");
  const body = (await res.json()) as { projects: Array<{ id: string }> };
  void projectId;
  return { email, projectId: body.projects[0].id };
}

/** apiCalls собирает коды ответов API, которые страница сделала сама. */
export function apiCalls(page: Page): string[] {
  const seen: string[] = [];
  page.on("response", (r) => {
    const u = new URL(r.url()).pathname;
    if (/\/v1\//.test(u)) seen.push(`${r.status()} ${u}`);
  });
  return seen;
}
