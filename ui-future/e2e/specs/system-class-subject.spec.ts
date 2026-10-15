// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { tenantWithProject, test } from "./fixtures";

/**
 * Стадия S4 приёмки «аноним и вошедший — системный класс субъекта» (дом приёмки
 * PRO-Robotech/kaname, docs/engineering/acceptance/anonymous-and-authenticated-are-subject-classes.md,
 * отпечаток fa4ff26f…bb21757e; зеркало консоли — kacho#3238): SC-50…53, посев П5 —
 * консоль на стенде, человек вошёл.
 *
 * Утверждается ВИДИМОЕ клиенту: подписи вариантов, тексты предупреждений, текст
 * отказа сервера у поля и показ класса в перечне. Отрицания стоят в паре с
 * положительным контролем — форма отрисовалась, выбор субъекта открылся.
 */

const CREATE_PATH = "/iam/access-bindings/create";
const CLASS_LABEL = "Системный класс";
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

async function openCreateForm(page: Page): Promise<void> {
  await page.goto(CREATE_PATH, { waitUntil: "domcontentloaded" });
  await expect(
    page.getByTestId("access-bindings-create-form"),
    "форма выдачи прав не отрисовалась — о вариантах субъекта такой прогон не говорит ничего",
  ).toBeVisible({ timeout: 45_000 });
}

async function chooseSubjectType(page: Page, label: string | RegExp): Promise<void> {
  await page.getByTestId("access-bindings-subject-type").click();
  const option = page.locator(".ant-select-dropdown:visible .ant-select-item-option").filter({ hasText: label });
  await expect(option.first(), `вариант вида субъекта «${String(label)}» в выпадающем списке`).toBeVisible({
    timeout: 15_000,
  });
  await option.first().click();
}

test("SC-50: выбор «Системный класс» даёт два значения без поиска, отправка шлёт SUBJECT_TYPE_SYSTEM_CLASS", async ({
  page,
}) => {
  // verifies #3238
  test.setTimeout(180_000);
  await tenantWithProject(page);
  await openCreateForm(page);

  // Положительный контроль: выбор вида субъекта открывается и в нём есть
  // сегодняшние виды.
  await page.getByTestId("access-bindings-subject-type").click();
  await expect(
    page.locator(".ant-select-dropdown:visible .ant-select-item-option").first(),
    "выпадающий список вида субъекта не открылся",
  ).toBeVisible({ timeout: 15_000 });
  await page.keyboard.press("Escape");

  await chooseSubjectType(page, CLASS_LABEL);
  const form = page.getByTestId("access-bindings-create-form");
  await expect(form.getByText(AUTHENTICATED_LABEL, { exact: true })).toBeVisible();
  await expect(form.getByText(ANONYMOUS_LABEL, { exact: true })).toBeVisible();
  await expect(
    page.getByTestId("access-bindings-subject-id").or(page.getByTestId("access-bindings-subject-ids")),
    "у класса нет поиска по id — поле поиска субъекта обязано исчезнуть",
  ).toHaveCount(0);

  await form.getByText(AUTHENTICATED_LABEL, { exact: true }).click();
  await page.getByTestId("access-bindings-target-kind").getByText("Точечно (перечень объектов)").click();
  await page.getByTestId("target-res-add").click();
  await page.getByTestId("target-res-type").first().fill("vpc.network");
  await page.getByTestId("target-res-id").first().fill("enp00000000000000001");
  await page.getByTestId("access-bindings-role-select").click();
  await page.locator(".ant-select-dropdown:visible .ant-select-item-option").filter({ hasText: "vpc.network.view" }).first().click();

  const sent = page.waitForRequest(
    (r) => r.method() === "POST" && new URL(r.url()).pathname === "/iam/v1/accessBindings",
  );
  await form.locator("button[type=submit]").click();
  const body = (await sent).postDataJSON() as { subjects?: Array<{ type?: string; id?: string }> };
  expect(body.subjects, "форма отправила субъекта не тем видом").toEqual([
    { type: "SUBJECT_TYPE_SYSTEM_CLASS", id: "authenticated" },
  ]);
});

test("SC-51: при выдаче классу над кнопкой — предупреждение о видимости; у человека его нет", async ({ page }) => {
  // verifies #3238
  test.setTimeout(180_000);
  await tenantWithProject(page);
  await openCreateForm(page);

  // Близнец: вид «Пользователь» — предупреждения нет. Он же положительный контроль
  // того, что форма откликается на выбор вида.
  await chooseSubjectType(page, /^(Пользователь|user)$/);
  await expect(page.getByText(ANONYMOUS_WARNING)).toHaveCount(0);
  await expect(page.getByText(AUTHENTICATED_WARNING)).toHaveCount(0);

  await chooseSubjectType(page, CLASS_LABEL);
  const form = page.getByTestId("access-bindings-create-form");
  await form.getByText(ANONYMOUS_LABEL, { exact: true }).click();
  await expect(page.getByText(ANONYMOUS_WARNING), "предупреждение при выдаче анониму").toBeVisible();
  await expect(form.locator("button[type=submit]"), "кнопка доступна").toBeEnabled();

  await form.getByText(AUTHENTICATED_LABEL, { exact: true }).click();
  await expect(page.getByText(AUTHENTICATED_WARNING), "предупреждение при выдаче вошедшим").toBeVisible();
  await expect(form.locator("button[type=submit]"), "кнопка доступна").toBeEnabled();
});

test("SC-52: отказ сервера на «всю область» анониму показан у поля цели дословно", async ({ page }) => {
  // verifies #3238
  test.setTimeout(180_000);
  await tenantWithProject(page);
  await openCreateForm(page);

  await chooseSubjectType(page, CLASS_LABEL);
  const form = page.getByTestId("access-bindings-create-form");
  await form.getByText(ANONYMOUS_LABEL, { exact: true }).click();
  await page.getByTestId("access-bindings-target-kind").getByText("Вся область").click();
  await page.getByTestId("access-bindings-role-select").click();
  await page.locator(".ant-select-dropdown:visible .ant-select-item-option").filter({ hasText: "vpc.network.view" }).first().click();

  const answered = page.waitForResponse(
    (r) => r.request().method() === "POST" && new URL(r.url()).pathname === "/iam/v1/accessBindings",
  );
  await form.locator("button[type=submit]").click();
  expect((await answered).status(), "сервер отверг выдачу анониму всей области").toBe(400);
  await expect(
    page.getByText(ANONYMOUS_ALL_IN_SCOPE_REFUSAL, { exact: true }),
    "текст отказа сервера показан дословно",
  ).toBeVisible();
});

test("SC-53: класс в перечне выдач показан именем без ссылки, системная выдача помечена «платформенная»", async ({
  page,
}) => {
  // verifies #3238
  test.setTimeout(180_000);
  const tenant = await tenantWithProject(page);
  // Дано SC-01: выдача классу «вошедшие» в проекте арендатора.
  const created = await page.request.post("/iam/v1/accessBindings", {
    data: {
      subjects: [{ type: "SUBJECT_TYPE_SYSTEM_CLASS", id: "authenticated" }],
      roleId: "rolfe683216e63311d3f",
      scopeType: "iam.project",
      scopeId: tenant.projectId,
      target: { resources: { resources: [{ type: "vpc.network", id: "enp00000000000000001" }] } },
    },
  });
  expect(created.status(), `Дано SC-01: выдача классу не заведена: ${await created.text()}`).toBe(200);
  await page.goto("/iam/access-bindings", { waitUntil: "domcontentloaded" });
  const table = page.getByTestId("access-bindings-table");
  await expect(table, "перечень выдач не отрисовался").toBeVisible({ timeout: 45_000 });

  const classCell = table.getByText(/^(authenticated|Вошедшие \(любой аутентифицированный\))$/).first();
  await expect(classCell, "субъект-класс системной выдачи показан именем класса").toBeVisible({ timeout: 45_000 });
  await expect(classCell.locator("xpath=ancestor-or-self::a"), "у класса нет страницы — ссылки нет").toHaveCount(0);
  await expect(table.getByText("платформенная").first(), "системная выдача несёт метку").toBeVisible();
});
