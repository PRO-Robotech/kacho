// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { raiseAssurance } from "./assurance";
import { scopeIsReady, tenantWithProject, test, type Tenant } from "./fixtures";

/**
 * Стадия S4 приёмки «аноним и вошедший — системный класс субъекта» (дом приёмки
 * PRO-Robotech/kaname, docs/engineering/acceptance/anonymous-and-authenticated-are-subject-classes.md;
 * зеркало консоли — kacho#3238): SC-50…53, посев П5 — консоль на стенде, человек вошёл.
 *
 * Утверждается ВИДИМОЕ клиенту: варианты выбора, тексты предупреждений, набор
 * предложенных ролей, текст отказа сервера у поля и показ класса в перечне.
 * Отрицания стоят в паре с положительным контролем — форма отрисовалась, выбор
 * открылся, у близнеца-человека то же место показывает то, чего нет у класса.
 *
 * ПОДПИСЬ ВИДА — решение 6 приёмки: «как консоль подписывает виды сегодня»,
 * значением вида (`user`, `service_account`, `group`), поэтому у класса — `system_class`.
 *
 * ПОЧЕМУ ПОДЪЁМ УРОВНЯ. Пол `AccessBindingService.Create` — «2», а вход паролем
 * даёт «1» (SC-26): без подъёма отправка формы получила бы отказ уровня, и проба
 * мерила бы не выдачу классу, а пол глагола.
 */

const CREATE_PATH = "/iam/access-bindings/create";
const CLASS_TYPE = /^system_class$/;
const USER_TYPE = /^user$/;
const AUTHENTICATED_LABEL = "Вошедшие (любой аутентифицированный)";
const ANONYMOUS_LABEL = "Аноним (без входа)";
const ANONYMOUS_WARNING =
  "Выдача анониму открывает выбранные объекты любому, кто обратится без входа, включая " +
  "посторонних. Выдаются только роли чтения и только названные объекты.";
const AUTHENTICATED_WARNING =
  "Выдача вошедшим открывает выбранные объекты любому пользователю и сервисной учётке " +
  "платформы, включая другие аккаунты. Выдаются только роли чтения.";
const ANONYMOUS_ALL_IN_SCOPE_REFUSAL =
  "Illegal argument target: a grant to system class anonymous names its objects (allInScope is not accepted)";
const READ_ROLE = "vpc.network.view";
const WRITE_ROLE = "vpc.network.edit";
const OBJ_TYPE = "vpc.network";
const OBJ_ID = "enp00000000000000001";
const GRANT_PATH = "/iam/v1/accessBindings";

const visibleOptions = (page: Page) => page.locator(".ant-select-dropdown:visible .ant-select-item-option");

/** Арендатор с проектом, сессия уровня «2», каркас показал область проекта. */
async function grantingTenant(page: Page): Promise<Tenant> {
  const tenant = await tenantWithProject(page);
  await raiseAssurance(page);
  await scopeIsReady(page, tenant.projectId);
  return tenant;
}

async function openCreateForm(page: Page): Promise<void> {
  await page.goto(CREATE_PATH, { waitUntil: "domcontentloaded" });
  await expect(
    page.getByTestId("access-bindings-create-form"),
    "форма выдачи прав не отрисовалась — о вариантах субъекта такой прогон не говорит ничего",
  ).toBeVisible({ timeout: 45_000 });
}

async function chooseSubjectType(page: Page, label: RegExp): Promise<void> {
  await page.getByTestId("access-bindings-subject-type").click();
  const option = visibleOptions(page).filter({ hasText: label });
  await expect(option.first(), `вариант вида субъекта «${String(label)}» в выпадающем списке`).toBeVisible({
    timeout: 15_000,
  });
  await option.first().click();
}

/** Область — проект арендатора: та же координата, что у SC-01. */
async function chooseProjectScope(page: Page, projectId: string): Promise<void> {
  await page.getByTestId("access-bindings-scope").click();
  await visibleOptions(page).filter({ hasText: /^PROJECT$/ }).first().click();
  await page.getByTestId("access-bindings-scope-ref").click();
  const own = visibleOptions(page).filter({ hasText: projectId });
  await expect(own.first(), `проект арендатора ${projectId} среди якорей области`).toBeVisible({ timeout: 30_000 });
  await own.first().click();
}

/** Варианты ролей, сужённые вводом до ролей сети. */
async function roleOptions(page: Page): Promise<string[]> {
  const select = page.getByTestId("access-bindings-role-select");
  await select.click();
  await select.locator("input").fill("vpc.network.");
  const options = visibleOptions(page);
  await expect(
    options.filter({ hasText: new RegExp(`^${READ_ROLE.replace(/\./g, "\\.")}$`) }).first(),
    `роль чтения ${READ_ROLE} предложена — положительный контроль набора ролей`,
  ).toBeVisible({ timeout: 30_000 });
  return (await options.allInnerTexts()).map((t) => t.trim());
}

async function pickRole(page: Page, name: string): Promise<void> {
  const select = page.getByTestId("access-bindings-role-select");
  await select.click();
  await select.locator("input").fill(name);
  await visibleOptions(page)
    .filter({ hasText: new RegExp(`^${name.replace(/\./g, "\\.")}$`) })
    .first()
    .click();
  await page.keyboard.press("Escape");
}

async function nameOneObject(page: Page): Promise<void> {
  await page.getByTestId("access-bindings-target-kind").getByText("Точечно (перечень объектов)").click();
  if ((await page.getByTestId("target-res-type").count()) === 0) await page.getByTestId("target-res-add").click();
  await page.getByTestId("target-res-type").first().fill(OBJ_TYPE);
  await page.getByTestId("target-res-id").first().fill(OBJ_ID);
}

test("SC-50: «system_class» даёт два класса без поиска, роли — только чтения, отправка шлёт SUBJECT_TYPE_SYSTEM_CLASS", async ({
  page,
}) => {
  // verifies #3238
  test.setTimeout(240_000);
  const tenant = await grantingTenant(page);
  await openCreateForm(page);

  // Близнец набора ролей: у человека роль записи ПРЕДЛАГАЕТСЯ — её отсутствие
  // у класса ниже говорит об отборе для класса, а не о каталоге стенда.
  await chooseSubjectType(page, USER_TYPE);
  await chooseProjectScope(page, tenant.projectId);
  expect(await roleOptions(page), "человеку роль записи предлагается").toContain(WRITE_ROLE);
  await page.keyboard.press("Escape");

  await chooseSubjectType(page, CLASS_TYPE);
  const form = page.getByTestId("access-bindings-create-form");
  await expect(form.getByText(AUTHENTICATED_LABEL, { exact: true })).toBeVisible();
  await expect(form.getByText(ANONYMOUS_LABEL, { exact: true })).toBeVisible();
  await expect(
    page.getByTestId("access-bindings-subject-id").or(page.getByTestId("access-bindings-subject-ids")),
    "у класса нет поиска по id — поле поиска субъекта обязано исчезнуть",
  ).toHaveCount(0);

  await form.getByText(AUTHENTICATED_LABEL, { exact: true }).click();
  const offered = await roleOptions(page);
  expect(offered, "классу роль записи не предлагается (Р4)").not.toContain(WRITE_ROLE);
  await page.keyboard.press("Escape");

  await nameOneObject(page);
  await pickRole(page, READ_ROLE);

  const answered = page.waitForResponse(
    (r) => r.request().method() === "POST" && new URL(r.url()).pathname === GRANT_PATH,
  );
  await form.locator("button[type=submit]").click();
  const res = await answered;
  const body = res.request().postDataJSON() as { subjects?: Array<{ type?: string; id?: string }> };
  expect(body.subjects, "форма отправила субъекта не тем видом").toEqual([
    { type: "SUBJECT_TYPE_SYSTEM_CLASS", id: "authenticated" },
  ]);
  expect(res.status(), `выдача классу принята (SC-01): ${await res.text()}`).toBe(200);
});

test("SC-51: при выдаче классу над кнопкой — предупреждение о видимости; у человека его нет", async ({ page }) => {
  // verifies #3238
  test.setTimeout(240_000);
  await grantingTenant(page);
  await openCreateForm(page);

  // Близнец: вид `user` — предупреждения нет. Он же положительный контроль
  // того, что форма откликается на выбор вида.
  await chooseSubjectType(page, USER_TYPE);
  await expect(page.getByTestId("access-bindings-subject-ids"), "у человека — поиск субъектов").toBeVisible();
  await expect(page.getByText(ANONYMOUS_WARNING)).toHaveCount(0);
  await expect(page.getByText(AUTHENTICATED_WARNING)).toHaveCount(0);

  await chooseSubjectType(page, CLASS_TYPE);
  const form = page.getByTestId("access-bindings-create-form");
  await form.getByText(ANONYMOUS_LABEL, { exact: true }).click();
  await expect(page.getByText(ANONYMOUS_WARNING), "предупреждение при выдаче анониму").toBeVisible();
  await expect(form.locator("button[type=submit]"), "кнопка доступна").toBeEnabled();

  await form.getByText(AUTHENTICATED_LABEL, { exact: true }).click();
  await expect(page.getByText(AUTHENTICATED_WARNING), "предупреждение при выдаче вошедшим").toBeVisible();
  await expect(page.getByText(ANONYMOUS_WARNING), "предупреждение анониму сменилось").toHaveCount(0);
  await expect(form.locator("button[type=submit]"), "кнопка доступна").toBeEnabled();
});

test("SC-52: отказ сервера на «всю область» анониму показан у поля цели дословно; перечень объектов — принят", async ({
  page,
}) => {
  // verifies #3238
  test.setTimeout(240_000);
  const tenant = await grantingTenant(page);
  await openCreateForm(page);

  await chooseSubjectType(page, CLASS_TYPE);
  const form = page.getByTestId("access-bindings-create-form");
  await form.getByText(ANONYMOUS_LABEL, { exact: true }).click();
  await chooseProjectScope(page, tenant.projectId);
  await pickRole(page, READ_ROLE);
  await page.getByTestId("access-bindings-target-kind").getByText("Вся область").click();

  const refused = page.waitForResponse(
    (r) => r.request().method() === "POST" && new URL(r.url()).pathname === GRANT_PATH,
  );
  await form.locator("button[type=submit]").click();
  expect((await refused).status(), "сервер отверг выдачу анониму всей области").toBe(400);
  // Строка формы поля цели — БЛИЖАЙШИЙ объемлющий `.ant-form-item`: строки
  // вкладываются, и выбор по фильтру взял бы объемлющий блок (#636).
  const atTarget = page
    .getByTestId("access-bindings-target-kind")
    .locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " ant-form-item ")][1]')
    .getByText(ANONYMOUS_ALL_IN_SCOPE_REFUSAL, { exact: true });
  await expect(atTarget, "текст отказа сервера показан у поля цели дословно").toBeVisible();

  // Близнец — один факт: вид цели. Тот же класс, та же роль, названный объект.
  await nameOneObject(page);
  const accepted = page.waitForResponse(
    (r) => r.request().method() === "POST" && new URL(r.url()).pathname === GRANT_PATH,
  );
  await form.locator("button[type=submit]").click();
  const res = await accepted;
  expect(res.status(), `выдача анониму названного объекта принята (SC-07): ${await res.text()}`).toBe(200);
  await expect(page.getByText(ANONYMOUS_ALL_IN_SCOPE_REFUSAL, { exact: true }), "текста отказа нет").toHaveCount(0);
});

test("SC-53: класс в перечне выдач показан подписью без ссылки; системной выдачи арендатор не видит", async ({
  page,
}) => {
  // verifies #3238
  test.setTimeout(240_000);
  const tenant = await grantingTenant(page);
  // Дано SC-01: выдача классу «вошедшие» в проекте арендатора.
  const created = await page.request.post(GRANT_PATH, {
    data: {
      subjects: [{ type: "SUBJECT_TYPE_SYSTEM_CLASS", id: "authenticated" }],
      roleId: "rolfe683216e63311d3f",
      scopeType: "iam.project",
      scopeId: tenant.projectId,
      target: { resources: { resources: [{ type: OBJ_TYPE, id: OBJ_ID }] } },
    },
  });
  expect(created.status(), `Дано SC-01: выдача классу не заведена: ${await created.text()}`).toBe(200);

  await page.goto("/iam/access-bindings", { waitUntil: "domcontentloaded" });
  const table = page.getByTestId("access-bindings-table");
  await expect(table, "перечень выдач не отрисовался").toBeVisible({ timeout: 45_000 });

  // Положительный контроль строки: выдача SC-01 в перечне есть.
  const classCell = table.getByText(AUTHENTICATED_LABEL, { exact: true }).first();
  await expect(classCell, "субъект-класс выдачи SC-01 показан подписью класса").toBeVisible({ timeout: 45_000 });
  await expect(classCell.locator("xpath=ancestor-or-self::a"), "у класса нет страницы — ссылки нет").toHaveCount(0);
  // Близнец ссылки: у той же строки роль — ссылка; отсутствие ссылки у класса
  // не свойство строки вообще.
  const row = table.locator("tr").filter({ has: classCell });
  await expect(row.locator("a").first(), "у строки выдачи ссылки есть — роль").toBeVisible();

  // Системная выдача `acbc5d32d66fc8dc4952` арендатору не видна (решение 5).
  await expect(table.getByText("платформенная"), "системной выдачи в перечне арендатора нет").toHaveCount(0);
});
