// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Locator, type Page } from "@playwright/test";

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

/** register проводит регистрацию до РАБОЧЕЙ СЕССИИ и отдаёт почту арендатора.
 *
 * Вынесено из `registerAndSignIn` без изменения его поведения: проект арендатора
 * добывается двумя разными способами (см. `tenantWithProject`), а вход — один и
 * тот же, и второй его копии заводить незачем. */
export async function register(page: Page): Promise<string> {
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

  return email;
}

/** registerAndSignIn проводит регистрацию до рабочей сессии и отдаёт проект арендатора. */
export async function registerAndSignIn(page: Page): Promise<Tenant> {
  const email = await register(page);

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

/**
 * tenantWithProject — арендатор со СВОИМ проектом, добытым независимо от того,
 * попала ли его строка на первую страницу списка.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ НЕ ХВАТАЕТ `registerAndSignIn`
 *
 * Тот берёт проект первой строкой `GET /iam/v1/projects`. Список курсорный и
 * фильтруется по правам ПОСТРАНИЧНО: на стенде, где накопились чужие строки,
 * первая страница может целиком состоять из недоступных — тогда ответ выглядит
 * как «проектов нет», хотя свой проект есть и читается по идентификатору.
 * Наблюдалось 2026-08-15 на стенде показа: `{"projects":[],"nextPageToken":"…"}`
 * — пустая страница С ПРОДОЛЖЕНИЕМ, то есть не «пусто», а «не на этой странице».
 *
 * ПОЧЕМУ ЗАПАСНОЙ ПУТЬ НЕ ЯВЛЯЕТСЯ МАСКОЙ
 *
 * Он не подменяет ответ края и ничего не прощает: аккаунт заводится ПУБЛИЧНЫМ
 * API, а его проект признаётся годным только после того, как прочитан по своему
 * адресу (`GET /iam/v1/projects/<id>` → 200). Идентификатор, взятый из метаданных
 * операции и не подтверждённый чтением, был бы фантомом — операция чеканит его
 * ДО того, как асинхронная часть могла отказать.
 */
export async function tenantWithProject(page: Page): Promise<Tenant> {
  const email = await register(page);

  const own = await firstProjectId(page);
  if (own !== "") return { email, projectId: own };

  const res = await page.request.post("/iam/v1/accounts", { data: { name: `acc${runTag()}` } });
  const body = (await res.json()) as { metadata?: { defaultProjectId?: string } };
  expect(
    res.status(),
    `аккаунт не заведён: край ответил ${res.status()} — условие проб не создано, ` +
      `и всё дальнейшее меряло бы не консоль`,
  ).toBe(200);

  const projectId = body.metadata?.defaultProjectId ?? "";
  expect(projectId, "создание аккаунта не назвало проект по умолчанию").not.toBe("");

  await expect
    .poll(async () => (await page.request.get(`/iam/v1/projects/${projectId}`)).status(), {
      message:
        `проект ${projectId} не читается по своему адресу: идентификатор из метаданных ` +
        `операции без подтверждения чтением — фантом, и пробы шли бы по несуществующему ресурсу`,
      timeout: 60_000,
    })
    .toBe(200);

  return { email, projectId };
}

async function firstProjectId(page: Page): Promise<string> {
  for (let i = 0; i < 8; i++) {
    const res = await page.request.get("/iam/v1/projects");
    if (res.ok()) {
      const body = (await res.json()) as { projects?: Array<{ id: string }> };
      const id = body.projects?.[0]?.id ?? "";
      if (id) return id;
    }
    await new Promise((r) => setTimeout(r, 2_000));
  }
  return "";
}

/**
 * createdResourceId доводит асинхронную мутацию до ПРОВЕРЕННОГО ресурса.
 *
 * Идентификатор из `metadata` сам по себе ничего не доказывает: он выделяется до
 * асинхронной части и остаётся в ответе даже тогда, когда та отказала. Поэтому
 * здесь он подтверждается чтением ресурса по его собственному адресу — это
 * сильнее проверки `op.error` и не зависит от того, какой службе принадлежит
 * запись операции.
 *
 * ОТВЕТ ПРИНИМАЕТСЯ ПО ФОРМЕ, А НЕ ПО ПРОИСХОЖДЕНИЮ. Мутацию рождают два разных
 * пути пробы, и типы ответов у них РАЗНЫЕ: запрос, отправленный самой пробой
 * (`page.request.post` → `APIResponse`), и ответ, ПЕРЕХВАЧЕННЫЙ у страницы,
 * когда мутацию отправила форма (`page.waitForResponse` → `Response`). Помощнику
 * нужны от обоих ровно два метода, поэтому он их и требует. Привязка к одному из
 * двух классов заставила бы завести вторую копию этой проверки для форм — и
 * вторая копия разошлась бы с первой молча, как это уже случалось с помощниками
 * адресации.
 */
export async function createdResourceId(
  page: Page,
  response: { status(): number; text(): Promise<string> },
  metadataField: string,
  addressOf: (id: string) => string,
  subject: string,
): Promise<string> {
  const text = await response.text();
  expect(
    response.status(),
    `${subject}: край отверг создание — ${response.status()} ${text.slice(0, 300)}. ` +
      `Это УСЛОВИЕ пробы, а не её предмет: вердикта о консоли такой прогон не даёт`,
  ).toBe(200);

  const body = JSON.parse(text) as { metadata?: Record<string, string> };
  const id = body.metadata?.[metadataField] ?? "";
  expect(id, `${subject}: операция не назвала ${metadataField}`).not.toBe("");

  await expect
    .poll(async () => (await page.request.get(addressOf(id))).status(), {
      message: `${subject}: ресурс ${id} не читается по своему адресу — операция вернула фантом`,
      timeout: 60_000,
    })
    .toBe(200);

  return id;
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

/**
 * Таблица С ДАННЫМИ на странице — та, у которой есть `tbody`.
 *
 * Общая таблица консоли (`ResourceTable`) держит фиксированную шапку колонок над
 * скроллящимся телом (`ui.md`, правило 10) — библиотека реализует это ДВУМЯ
 * `<table>`: один несёт `<thead>` без строк, другой — `<tbody>` со строками. Оба
 * видимы, `page.locator("table")` резолвит два элемента, и `.first()` не годится:
 * у заголовочной таблицы строк нет, и следующее утверждение о содержимом било бы
 * мимо предмета. `:has(tbody)` верен и в режиме БЕЗ фиксированной шапки, где обе
 * части живут в одном `<table>`.
 *
 * Живёт здесь, а не в спеке: витрин, показывающих эту таблицу, больше одной, и
 * вторая копия объяснения разошлась бы с первой молча.
 */
export function dataTable(page: Page): Locator {
  return page.locator("table").filter({ has: page.locator("tbody") });
}
