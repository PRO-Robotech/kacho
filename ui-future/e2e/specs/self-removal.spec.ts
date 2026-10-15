// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect } from "@playwright/test";
import { raiseAssurance } from "./assurance";
import { SESSION_IDENTITY } from "./ceremony-seed";
import { register, test } from "./fixtures";

/**
 * Удаление своей личности из параметров учётной записи (#3186).
 *
 * Служба объявляет самоудаление (kaname `docs/content/api/user.mdx`, «Delete —
 * удалить личность»): уровень сессии `2`, порядок «аккаунт, затем личность».
 * Проба утверждает наблюдаемое: действие есть, необратимое подтверждается вводом
 * адреса, обращение уходит к СВОЕЙ личности (id из ответа края «кто я»), и отказ
 * «владеет аккаунтами» назван следующим шагом. Самостоятельно зарегистрированный
 * человек всегда владеет личным аккаунтом, поэтому исход службы здесь — отказ.
 */

test("в параметрах учётной записи есть удаление своей личности, и оно идёт к службе своим id", async ({ page }) => {
  // verifies #3186 — самоудаление объявлено службой; консоль ведёт к нему и называет шаг «сперва аккаунты».
  test.setTimeout(240_000);
  // Человек заводится `register`, а не `registerAndSignIn`: разбор арендатора
  // поднимает уровень сам и на уже заведённом втором факторе отказывает
  // (SECOND_FACTOR_ALREADY_ENROLLED) — так же поступает приглашающий в
  // `users.spec.ts`; пространство стенда проб снимается целиком.
  await register(page);
  await raiseAssurance(page);

  await page.goto("/settings", { waitUntil: "domcontentloaded" });
  const me = await page.request.get(SESSION_IDENTITY);
  const who = ((await me.json()) as { user?: { id?: string; email?: string } }).user;
  const ownId = who?.id ?? "";
  const ownEmail = who?.email ?? "";
  expect(ownId, "ответ края «кто я» не назвал id личности").not.toBe("");

  const open = page.getByRole("button", { name: "Удалить учётную запись", exact: true });
  await expect(open, "действия удаления своей учётной записи в параметрах нет").toBeVisible({ timeout: 60_000 });
  await open.click();

  const dialog = page.getByRole("dialog");
  const confirm = dialog.getByRole("button", { name: "Удалить навсегда", exact: true });
  await expect(confirm, "необратимое действие подтверждается одним щелчком").toBeDisabled();
  await dialog.getByRole("textbox").fill(ownEmail);
  const deleted = page.waitForRequest(
    (r) => r.method() === "DELETE" && new URL(r.url()).pathname.startsWith("/iam/v1/users/"),
    { timeout: 30_000 },
  );
  await confirm.click();
  expect(new URL((await deleted).url()).pathname, "удаление адресовано не своей личности").toBe(
    `/iam/v1/users/${ownId}`,
  );

  // Самостоятельно зарегистрированный владеет личным аккаунтом — служба отвечает
  // «сперва аккаунт», и экран называет этот шаг.
  await expect(
    page.getByTestId("self-removal-next-step").filter({ hasText: "Сначала удалите аккаунты, которыми вы владеете" }),
    "отказ «владеет аккаунтами» не назвал следующий шаг",
  ).toBeVisible({ timeout: 60_000 });
});
