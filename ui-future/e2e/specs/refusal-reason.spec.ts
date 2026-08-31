// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { registerAndSignIn, runTag, test } from "./fixtures";

/**
 * ПОЛОСА ОТКАЗА РАЗЛИЧАЕТСЯ ПРИЗНАКОМ, А НЕ АНГЛИЙСКОЙ ПРОЗОЙ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ (#1736)
 *
 * Клиент обязан различать полосы МАШИННО — по `reason` в `google.rpc.ErrorInfo`
 * (`api-conventions.md` §By-lane code-split). Разбор отказа по пределу это
 * исполняет и рядом объясняет, почему иначе нельзя: «вывод вида из английской
 * фразы молча вернул бы пустоту при первой же смене тона». Полоса отказа в
 * правах в том же файле делала именно это — решала по подстроке
 * `permission denied`, — а производимый признак `AUTHZ_DENIED` не читался.
 *
 * Тон сообщений — ЧАСТЬ КОНТРАКТА и меняется осознанно. Пока полоса ключуется
 * прозой, такая правка молча возвращает арендатору внутреннее имя проверки
 * вместо объяснения, и ни одно утверждение об экране не краснеет: строка есть,
 * она просто другая.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ БРАУЗЕРОМ, А НЕ МОДУЛЬНОЙ ПРОБОЙ
 *
 * Модульная проба монтирует компонент и о том, что дошло до экрана, не говорит
 * ничего: между краем и текстом стоят разбор тела ответа, маршрутизация,
 * загрузка федеративного модуля и та поверхность, на которой отказ вообще
 * показывается. Здесь утверждается то, что видит человек.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ЭТИ ПРОБЫ НЕ ДОКАЗЫВАЮТ — СКАЗАНО ВСЛУХ, ЧТОБЫ НЕ ПРОЧЛИ ШИРЕ
 *
 * Ответ края ПОДМЕНЯЕТСЯ: добиться настоящего отказа в правах с нужным тоном
 * сообщения значило бы менять тексты сервиса, а они контракт. Поэтому пробы НЕ
 * утверждают, что край производит именно такую прозу, — они утверждают, что
 * решение консоли НЕ ЗАВИСИТ от прозы. Что признак производится, держит гейт
 * `ui-future/deploy/console_refusal_reason_coverage_test.go`, сверяющий словарь
 * консоли с производителями в дереве.
 */

/** Тело отказа в том виде, в каком его собирает край из `google.rpc.Status`. */
function refusalBody(code: number, reason: string, domain: string, message: string, metadata?: Record<string, string>) {
  return JSON.stringify({
    code,
    message,
    details: [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason, domain, ...(metadata ? { metadata } : {}) }],
  });
}

/**
 * Подменяет ответ на СОЗДАНИЕ сети, оставляя чтения нетронутыми.
 *
 * Различение по методу существенно: перехватив и чтения, проба лишила бы
 * страницу данных и упала бы раньше, чем дошла до своего предмета.
 */
async function refuseCreate(page: Page, status: number, body: string) {
  await page.route("**/vpc/v1/networks**", async (route) => {
    if (route.request().method() !== "POST") {
      await route.fallback();
      return;
    }
    await route.fulfill({ status, contentType: "application/json", body });
  });
}

/** Доводит форму создания сети до отправки — тем же путём, каким идёт арендатор. */
async function submitNetworkForm(page: Page, projectId: string, name: string) {
  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const createControl = page.locator('button:has-text("Создать"), a:has-text("Создать")').first();
  await expect(createControl, "на странице сетей нет элемента создания").toBeVisible({ timeout: 30_000 });
  await createControl.click();

  const field = page.locator('input[type="text"]:visible, input:not([type]):visible').first();
  await expect(field, "форма создания не предложила ни одного поля").toBeVisible({ timeout: 20_000 });
  await field.fill(name);

  const cidr = page.locator('input[placeholder="10.20.0.0/16"]:visible').first();
  await expect(cidr, "на форме создания сети нет поля адресного блока").toBeVisible({ timeout: 20_000 });
  await cidr.fill("10.94.0.0/16");

  await Promise.all([
    page.waitForResponse((r) => r.url().includes("/vpc/v1/networks") && r.request().method() === "POST", {
      timeout: 40_000,
    }),
    page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click(),
  ]);
}

/** Сообщение об отказе — то, что видит человек. Отказ рисуется ролью `alert`. */
function refusal(page: Page) {
  return page.getByRole("alert").first();
}

test("AUTHZ_DENIED объясняется по признаку — даже когда прозы, на которую опирался разбор, НЕТ", async ({ page }) => {
  // verifies #1736
  //
  // Сообщение намеренно не содержит ни `permission denied`, ни точечного имени
  // права: прозаический разбор его НЕ поймает. Это и есть та смена тона, которая
  // сегодня проходит молча и возвращает арендатору имя внутренней проверки.
  const { projectId } = await registerAndSignIn(page);

  await refuseCreate(
    page,
    403,
    refusalBody(7, "AUTHZ_DENIED", "kacho.cloud.iam.v1", "доступ к vpc.networks.create закрыт", {
      action: "vpc.networks.create",
      resource: `project:${projectId}`,
    }),
  );

  await submitNetworkForm(page, projectId, `net-authz-${runTag()}`);

  const message = refusal(page);
  await expect(message, "отказ в правах не показан человеку вовсе").toBeVisible({ timeout: 20_000 });

  const shown = (await message.textContent()) ?? "";
  expect(shown, "отказ не объяснён — на экране осталась строка производителя").toContain("администратор");
  expect(shown, "внутреннее имя проверки показано арендатору").not.toContain("vpc.networks.create");
});

test("отказ БЕЗ признака показывается дословно — контроль к утверждению выше", async ({ page }) => {
  // verifies #1736
  //
  // Парный положительный. Без него «объяснение вместо имени проверки» зеленело
  // бы и на подмене ЛЮБОГО отказа общей фразой, то есть на потере настоящей
  // причины — а это ровно тот исход, ради предотвращения которого пробы и стоят.
  const { projectId } = await registerAndSignIn(page);

  await refuseCreate(
    page,
    403,
    JSON.stringify({ code: 7, message: "Аккаунт заблокирован администратором" }),
  );

  await submitNetworkForm(page, projectId, `net-plain-${runTag()}`);

  const message = refusal(page);
  await expect(message, "отказ не показан человеку вовсе").toBeVisible({ timeout: 20_000 });
  expect(
    (await message.textContent()) ?? "",
    "настоящая причина отказа подменена общей фразой — объяснение стало сокрытием",
  ).toContain("Аккаунт заблокирован администратором");
});

test("QUOTA_RATE_EXCEEDED — ТРЕТЬЯ полоса: ждёт САМ вызывающий, а не администратор", async ({ page }) => {
  // verifies #1736
  //
  // Производитель завёл её отдельным признаком осознанно и записал почему:
  // «повтор по объёму не пройдёт никогда, повтор по темпу пройдёт в следующем
  // окне». Признак производился и в словаре консоли отсутствовал — отказ падал в
  // общую ветку и показывался английской строкой дословно. Клиент, не различающий
  // эти полосы, идёт поднимать предел там, где надо просто подождать.
  const { projectId } = await registerAndSignIn(page);

  await refuseCreate(
    page,
    429,
    refusalBody(8, "QUOTA_RATE_EXCEEDED", "iam.kacho.cloud", "rate limit exceeded for vpc.network", {
      kind: "vpc.network",
    }),
  );

  await submitNetworkForm(page, projectId, `net-rate-${runTag()}`);

  const message = refusal(page);
  await expect(message, "отказ по темпу не показан человеку вовсе").toBeVisible({ timeout: 20_000 });

  const shown = (await message.textContent()) ?? "";
  expect(shown, "не сказано, что делать: следующий шаг у этой полосы — подождать").toContain("Повторите");
  // Действие принадлежит ВЫЗЫВАЮЩЕМУ: про поднятие предела здесь не говорится.
  expect(shown, "полоса темпа свелась с полосой объёма — клиента послали поднимать предел").not.toContain("поднять");
  expect(shown, "английская строка производителя показана арендатору дословно").not.toContain("rate limit exceeded");
});
