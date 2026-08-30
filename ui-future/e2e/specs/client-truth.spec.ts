// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { runTag, tenantWithProject, test } from "./fixtures";

/**
 * Линия «клиентская правда» (эпик #1631): три поверхности — контракт, страница
 * документации и консоль — говорят об одном ресурсе одно.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ БРАУЗЕРОМ, А НЕ ЮНИТОМ МОДУЛЯ
 *
 * Все шесть находок сделаны ИМИТАЦИЕЙ КЛИЕНТА С ПУСТЫМ КОНТЕКСТОМ: человек
 * открывает продукт и читает то, что продукт ему говорит. Юнит модуля этого не
 * видит by construction — он монтирует компонент и знает, чего ждать, то есть
 * подтверждает намерение автора, а не то, что прочтёт клиент. Рядом с каждой
 * пробой ниже стоит гейт дерева либо юнит, и это разделение труда, а не
 * дублирование: гейт держит СОГЛАСИЕ двух объявлений (форма имени в консоли и у
 * платформы, подпись и словарь), проба — то, ЧТО ВИДНО НА ЭКРАНЕ.
 *
 * ПОЧЕМУ УТВЕРЖДЕНИЯ О ТЕКСТЕ, А НЕ О РАЗМЕТКЕ
 *
 * Предмет находок — слова продукта. Утверждение о классе или компоненте
 * пережило бы свой предмет: компонент заменят, проба останется зелёной, а
 * находка вернётся к клиенту. Поэтому ниже утверждается видимый текст, адрес
 * перехода и исход действия.
 *
 * ПОЧЕМУ ОТРИЦАНИЯ СТОЯТ В ПАРЕ С ПОЛОЖИТЕЛЬНЫМ
 *
 * «Слова „Каталог“ на экране нет» одинаково зелено на исправной странице и на
 * пустой, не загрузившейся. Каждое отрицание здесь сопровождается измерением,
 * доказывающим, что предмет вообще есть: экран отрисовался, поле нашлось,
 * переключатель нашёлся.
 */

/** Открыть форму создания и дождаться, пока у неё появились подписи полей. */
async function openCreateForm(page: Page, projectId: string, resource: string): Promise<void> {
  await page.goto(`/projects/${projectId}/${resource}/create`, { waitUntil: "domcontentloaded" });
  await expect(
    page.locator("form.ant-form"),
    `форма создания «${resource}» не отрисовалась: дальше проверялся бы пустой экран`,
  ).toBeVisible({ timeout: 45_000 });
}

/**
 * Открыть диалог удаления строки с этим именем.
 *
 * Ищется СТРОКА по видимому имени, а не по индексу: индекс зависит от порядка,
 * а порядок — от того, что ещё лежит в проекте.
 */
async function openDeleteDialog(page: Page, name: string): Promise<void> {
  const row = page.locator("tbody tr", { hasText: name }).first();
  await expect(row, `строка «${name}» не появилась в списке: удалять нечего`).toBeVisible({ timeout: 60_000 });
  await row.locator("button").last().click();
  await page.getByRole("menuitem", { name: /Удалить/ }).click();
}

// ─────────────────────────────────────────────────────────────────────────────

test("форма создания называет ту форму имени, которую платформа принимает", async ({ page }) => {
  // verifies #1604
  //
  // Дефект: подсказка под полем «Имя» обещала подчёркивание и любой регистр —
  // знаки, которых единственная форма имени платформы (DNS label RFC 1123) не
  // принимает. Имя, набранное ПО ПОДСКАЗКЕ, уезжало в асинхронную мутацию и
  // возвращалось отказом по-английски и регуляркой.
  //
  // Утверждается наблюдаемое: что читает клиент под полем и что происходит,
  // когда он вводит имя по прежней подсказке.
  test.setTimeout(180_000);
  const { projectId } = await tenantWithProject(page);
  await openCreateForm(page, projectId, "vpc/networks");

  const form = page.locator("form.ant-form");

  // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: подсказка вообще есть. Без него отрицание ниже
  // зеленело бы на форме, у которой подсказок нет вовсе.
  const hint = form.getByText(/Строчные латинские буквы/).first();
  await expect(
    hint,
    "у поля «Имя» нет подсказки о форме имени: клиенту негде узнать правило до отказа края",
  ).toBeVisible({ timeout: 30_000 });

  const hintText = (await hint.textContent()) ?? "";
  expect(
    hintText,
    `подсказка обещает подчёркивание — «${hintText}». Платформа его не принимает, ` +
      `и клиент строит по этой строке соглашение об именах`,
  ).not.toMatch(/«_»|подчёркивани/i);
  expect(
    hintText,
    `подсказка обещает заглавные буквы — «${hintText}», которых форма не принимает`,
  ).not.toMatch(/любой регистр|заглавн/i);

  // Имя по ПРЕЖНЕЙ подсказке отвергается ФОРМОЙ, а не операцией: клиент узнаёт
  // о правиле сразу, а не после ожидания.
  const nameInput = form.locator("input#name, input[name=name]").first();
  await nameInput.fill(`Web_Net_${runTag()}`);
  await page.locator('button:has-text("Создать")').last().click();

  await expect(
    form.locator("[role=alert]").first(),
    "имя с подчёркиванием принято формой: отказ придёт от края после ожидания операции, " +
      "то есть правило продукт назовёт уже после того, как клиент его нарушил",
  ).toBeVisible({ timeout: 15_000 });

  // ОБРАТНАЯ СТОРОНА, и она тише: объявление СТРОЖЕ платформы отбирает законное
  // имя. Цифра первой — законна (`^[a-z0-9]…`), и форма обязана её принять.
  await nameInput.fill(`9-web-${runTag()}`);
  await expect
    .poll(async () => await form.locator("[role=alert]").count(), {
      message:
        "форма отвергла имя, начинающееся с цифры, — а край такое имя принимает: " +
        "консоль отбирает у клиента законное имя, и по экрану это неотличимо от правила продукта",
      timeout: 15_000,
    })
    .toBe(0);
});

// ─────────────────────────────────────────────────────────────────────────────

test("подтверждение удаления тяжелее там, где исчезают данные", async ({ page }) => {
  // verifies #1606
  //
  // Дефект: ритуал был перевёрнут относительно риска. Ввод имени руками
  // требовался у сети — там, где край и так откажет, пока есть дети, и терять
  // нечего; у тома, где содержимое исчезает безвозвратно, хватало одного клика,
  // и та же общая фраза не называла, что именно уйдёт.
  //
  // Утверждается наблюдаемое: что видит человек в диалоге удаления у обоих
  // ресурсов. Пара обязательна — «у сети имени не спрашивают» одинаково зелено
  // и на исправном продукте, и на диалоге, который не спрашивает ничего нигде.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);

  const tag = runTag();

  // ── сеть: терять нечего, ритуал лёгкий, предупреждение общее ──────────────
  await page.goto(`/projects/${projectId}/vpc/networks/create`, { waitUntil: "domcontentloaded" });
  const netForm = page.locator("form.ant-form");
  await expect(netForm).toBeVisible({ timeout: 45_000 });
  await netForm.locator("input#name, input[name=name]").first().fill(`net-${tag}`);
  await page.locator('button:has-text("Создать")').last().click();

  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });
  await openDeleteDialog(page, `net-${tag}`);

  const dialog = page.locator("[role=dialog]");
  await expect(
    dialog.getByTestId("delete-consequence"),
    "диалог удаления не сказал ничего о последствиях — тогда сравнивать не с чем",
  ).toBeVisible({ timeout: 30_000 });

  await expect(
    dialog.getByText("Подтвердите удаление — введите имя ресурса"),
    "у сети спрашивают имя руками: край и так откажет, пока есть дети, и терять нечего. " +
      "Ритуал, стоящий на каждом удалении, перестаёт отличать опасное от рядового",
  ).toHaveCount(0);
});

test("удаление тома называет, что именно исчезнет, и просит имя", async ({ page }) => {
  // verifies #1606 — вторая половина пары выше, отдельной пробой: у неё своя
  // фикстура (том), и склеивать их значило бы терять вердикт по одной из
  // половин при отказе другой.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const tag = runTag();

  await page.goto(`/projects/${projectId}/storage/volumes/create`, { waitUntil: "domcontentloaded" });
  const form = page.locator("form.ant-form");
  await expect(form).toBeVisible({ timeout: 45_000 });
  await form.locator("input#name, input[name=name]").first().fill(`vol-${tag}`);
  await page.locator('button:has-text("Создать")').last().click();

  await page.goto(`/projects/${projectId}/storage/volumes`, { waitUntil: "domcontentloaded" });
  await openDeleteDialog(page, `vol-${tag}`);

  const dialog = page.locator("[role=dialog]");
  const consequence = dialog.getByTestId("delete-consequence");
  await expect(consequence).toBeVisible({ timeout: 30_000 });

  const text = (await consequence.textContent()) ?? "";
  expect(
    text,
    `диалог удаления тома говорит общей фразой — «${text}». Она одинакова у тома и у пустой ` +
      `сети, поэтому не сообщает клиенту, чем он рискует: данные тома исчезают безвозвратно`,
  ).toMatch(/Данные тома/);

  await expect(
    dialog.getByText("Подтвердите удаление — введите имя ресурса"),
    "удаление тома проходит одним кликом: самая дорогая ошибка в консоли стоит меньше усилий, " +
      "чем самая безобидная",
  ).toBeVisible({ timeout: 15_000 });
});
