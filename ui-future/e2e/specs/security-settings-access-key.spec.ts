// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { authenticatorWithKey, pageAuthenticator, presentAccessKey } from "./access-key-seed";
import { noteSubstituted } from "./ceremony-budget";
import { LANE, codeOutsideWindow, totpCode } from "./ceremony-seed";
import { ceremonyCensus, test } from "./fixtures";
import { SESSION_NOT_FRESH, fulfillWith, laneTooManyAttempts } from "./producer-answers";
import {
  SECTION_ORDER,
  STEP_UP_BEGIN,
  anotherSession,
  keysTurnedOff,
  nextStepCode,
  openSecurity,
  seedStepUp,
  sentBodies,
  stepUpMethodsSent,
  stepUpWindow,
  withSeeded,
  type Human,
  type SecurityScreen,
} from "./security-screen";

/**
 * Экран «Безопасность» и окно подтверждения — сценарии С КЛЮЧОМ доступа
 * (приёмка F8-S7 воркспейса, отпечаток 559ae688…; реализация kacho#3230):
 * F8-S7-01, -02, -10, -20…-23, -25, -26, -30…-33, -42, -43, -50, -60, -70, -71.
 * Имя файла несёт `access-key` — проект набора ставит пробам ключа условие П4
 * (происхождение браузера набора принимается для ключей доступа).
 *
 * ПОДСТАНОВКА — только ответ производителя (F8 Р9): отказ свежести
 * (С-несвежая), отказ частоты (С-частота), суждённый отказ ключа (F8-S7-31) и
 * состояние «ключи выключены на установке» (С-выкл) поверх настоящего ответа.
 *
 * ЧЕМ КРАСНЕЕТ ДО КОДА: на `/settings` прежний экран (`openSecurity`), нет
 * доступных имён приёмки (таблицы, «Удалить ключ доступа «<имя>»», «Подтвердить
 * ключом доступа», «Другой способ»), нет ветви ключа в окне подтверждения.
 *
 * С-л (F8-S7-26, F8-S7-43) — С-кл со строкой пароля, снятой записью в
 * хранилище службы (`security-screen.ts`, «ПОСЕВ С-л»).
 */

const DELETE_KEY = (h: Human) => `/iam/v1/users/${h.key!.userId}/accessKeys/${h.key!.accessKeyId}`;

/** «Удалить» ключа и подтверждение диалога удаления. */
async function deleteKey(page: Page, s: SecurityScreen, h: Human) {
  await s.keyDelete(h.keyName).click();
  await page.getByRole("dialog").getByRole("button", { name: "Удалить", exact: true }).click();
}

/**
 * Число полос прокрутки страницы, кроме документа: Р1 — прокрутка одна. Ею
 * служит область содержимого под шапкой (`PageFrame`, `ui-page-single-scroll`:
 * рабочая область каркаса и документ сами не прокручиваются — та же мерка, что
 * у пробы прокрутки F8, `visibleScrollAreas`), поэтому полос — не больше одной;
 * вторая — таблица раздела со своей прокруткой либо вложенная область.
 */
async function innerScrollers(page: Page): Promise<number> {
  return page.evaluate(() => {
    let n = 0;
    for (const el of Array.from(document.querySelectorAll("body *"))) {
      const st = getComputedStyle(el);
      if (/(auto|scroll)/.test(st.overflowY) && el.scrollHeight > el.clientHeight + 1) n++;
    }
    return n;
  });
}

test("F8-S7-01 · один экран «Безопасность», разделы по порядку, навигация на пять разделов", async ({ page }, testInfo) => {
  // verifies #3230
  await withSeeded(testInfo, "F8-S7-01", "С-ккл", page.context(), async () => {
    const s = await openSecurity(page);
    const names: string[] = [];
    for (const r of await page.getByRole("region").all()) {
      const name = (await r.getAttribute("aria-labelledby"))
        ? await page.locator(`#${await r.getAttribute("aria-labelledby")}`).textContent()
        : null;
      if (name && (SECTION_ORDER as readonly string[]).includes(name.trim())) names.push(name.trim());
    }
    expect(names, "разделы — в порядке Р1").toEqual([...SECTION_ORDER]);
    await expect(s.nav.getByRole("link"), "навигация «Разделы страницы» — пять ссылок").toHaveCount(5);
    await s.nav.getByRole("link", { name: "Ключи доступа", exact: true }).click();
    await expect(s.keys.getByRole("heading", { level: 2, name: "Ключи доступа" }), "фокус — на заголовке раздела").toBeFocused();
    expect(await innerScrollers(page), "полоса прокрутки одна (ui-page-single-scroll)").toBeLessThanOrEqual(1);
  });
});

test("F8-S7-02 · сводка без второго фактора — рекомендация и «Настроить»; у С-ккл рекомендации нет", async ({
  page,
}, testInfo) => {
  // verifies #3230 — близнец F8-S7-01.
  const advice = "Включите второй фактор: без него действия, меняющие доступ, недоступны";
  await withSeeded(testInfo, "F8-S7-02", "С-п", page.context(), async () => {
    const s = await openSecurity(page);
    await expect(s.summary, "сводка — «Подтверждение действий: не настроено»").toContainText("Подтверждение действий: не настроено");
    await expect(s.summary, "сводка — рекомендация").toContainText(advice);
    await s.summary.getByRole("button", { name: "Настроить", exact: true }).click();
    await expect(s.secondFactor.getByRole("heading", { level: 2, name: "Второй фактор" }), "«Настроить» ведёт к разделу").toBeFocused();
  });
  // Близнец — тем же браузером: носитель первого человека сменяется носителем второго.
  await page.context().clearCookies();
  await withSeeded(testInfo, "F8-S7-02t", "С-ккл", page.context(), async () => {
    const s = await openSecurity(page);
    await expect(s.summary, "у С-ккл рекомендации нет").not.toContainText(advice);
  });
});

test("F8-S7-10 · строки способов с датами: код и запасные коды использовались", async ({ page }, testInfo) => {
  // verifies #3230
  await withSeeded(
    testInfo,
    "F8-S7-10",
    "С-ккл",
    page.context(),
    async () => {
      const s = await openSecurity(page);
      const date = /\d{1,2} [а-яё]+\.? \d{4}/;
      const totp = s.secondFactor.getByRole("row", { name: /Приложение-аутентификатор/ });
      await expect(totp).toContainText("Настроено");
      await expect(totp, "«Последнее использование» кода — дата").toContainText(date);
      await expect(totp).not.toContainText("Ещё не использовалось");
      await expect(s.secondFactor.getByRole("row", { name: /Ключи доступа/ })).toContainText("Есть: 1");
      const codes = s.secondFactor.getByRole("row", { name: /Запасные коды/ });
      await expect(codes).toContainText("Осталось 9 из 10");
      await expect(codes).not.toContainText("Ещё не использовались");
    },
    {
      before: async (h) => {
        await seedStepUp(h.seed, "totp", await nextStepCode(h.factor!.secret));
        await seedStepUp(h.seed, "lookup_secret", h.factor!.backupCodes[0]);
      },
    },
  );
});

/** Окно подтверждения на «Удалить» у ключа из сессии «1» (F8-S7-20…23, -30…-33). */
async function openStepUpOnDelete(page: Page, h: Human) {
  const s = await openSecurity(page);
  await deleteKey(page, s, h);
  const w = stepUpWindow(page);
  await expect(w.dialog, "служба ответила указанием на уровень — окно подтверждения открыто").toBeVisible();
  return { s, w };
}

test("F8-S7-20 · по умолчанию — ключ; код и запасной код — в «Другой способ»", async ({ page }, testInfo) => {
  // verifies #3230
  await withSeeded(
    testInfo,
    "F8-S7-20",
    "С-ккл",
    page.context(),
    async (h) => {
      const { w } = await openStepUpOnDelete(page, h);
      await expect(w.keyButton, "кнопка «Подтвердить ключом доступа» (Р5)").toBeVisible();
      await expect(w.otherGroup.getByRole("radio"), "«Другой способ» — «Код из приложения», «Запасной код»").toHaveText([
        "Код из приложения",
        "Запасной код",
      ]);
      await w.otherGroup.getByRole("radio").first().focus();
      await page.keyboard.press("ArrowDown");
      await expect(w.radio("Запасной код"), "стрелки переключают выбор").toBeChecked();
    },
    { level: "1" },
  );
});

test("F8-S7-21 · ключом: begin → step-up webauthn → повтор действия", async ({ page }, testInfo) => {
  // verifies #3230
  await withSeeded(
    testInfo,
    "F8-S7-21",
    "С-ккл",
    page.context(),
    async (h) => {
      await authenticatorWithKey(page, h.key!);
      const census = ceremonyCensus(page.context());
      const sent = sentBodies(page.context());
      const { s, w } = await openStepUpOnDelete(page, h);
      await w.keyButton.click();
      await expect(w.dialog, "окно закрыто").toBeHidden();
      await expect(s.keyDelete(h.keyName), "действие выполнено — ключа нет").toHaveCount(0);
      const order = census.calls
        .filter((c) => (c.method === "POST" && (c.path === STEP_UP_BEGIN || c.path === LANE.stepUp)) || (c.method === "DELETE" && c.path === DELETE_KEY(h)))
        .map((c) => `${c.method} ${c.path}`);
      expect(order, `перепись: begin → step-up → повтор удаления:\n${census.describe()}`).toEqual([
        `DELETE ${DELETE_KEY(h)}`,
        `POST ${STEP_UP_BEGIN}`,
        `POST ${LANE.stepUp}`,
        `DELETE ${DELETE_KEY(h)}`,
      ]);
      expect(stepUpMethodsSent(sent), "step-up с method «webauthn»").toEqual(["webauthn"]);
    },
    { level: "1" },
  );
});

test("F8-S7-22 · кодом: step-up totp и повтор действия; испытания ключа не выдавалось", async ({ page }, testInfo) => {
  // verifies #3230 — близнец F8-S7-21, отличие — выбранный способ.
  await withSeeded(
    testInfo,
    "F8-S7-22",
    "С-ккл",
    page.context(),
    async (h) => {
      const census = ceremonyCensus(page.context());
      const sent = sentBodies(page.context());
      const { s, w } = await openStepUpOnDelete(page, h);
      await w.radio("Код из приложения").check();
      await w.code.fill(await nextStepCode(h.factor!.secret));
      await w.confirm.click();
      await expect(w.dialog, "окно закрыто").toBeHidden();
      await expect(s.keyDelete(h.keyName), "действие выполнено").toHaveCount(0);
      expect(stepUpMethodsSent(sent), "step-up с method «totp»").toEqual(["totp"]);
      expect(census.matching("POST", STEP_UP_BEGIN), "испытания ключа не выдавалось").toHaveLength(0);
    },
    { level: "1" },
  );
});

test("F8-S7-23 · запасным кодом: остаток показан, окно закрывается после повтора действия", async ({ page }, testInfo) => {
  // verifies #3230
  await withSeeded(
    testInfo,
    "F8-S7-23",
    "С-ккл",
    page.context(),
    async (h) => {
      const sent = sentBodies(page.context());
      const { s, w } = await openStepUpOnDelete(page, h);
      await w.radio("Запасной код").check();
      await w.backupCode.fill(h.factor!.backupCodes[0]);
      await w.confirm.click();
      await expect(page.getByText("Осталось запасных кодов: 9", { exact: true }), "окно показывает остаток").toBeVisible();
      await expect(w.dialog, "окно закрывается после повтора действия").toBeHidden();
      await expect(s.keyDelete(h.keyName)).toHaveCount(0);
      expect(stepUpMethodsSent(sent), "step-up с method «lookup_secret»").toEqual(["lookup_secret"]);
    },
    { level: "1" },
  );
});

test("F8-S7-25 · человек с паролем и ключом без кода заводит код через подтверждение ключом", async ({ page }, testInfo) => {
  // verifies #3230 — kacho#3196.
  await withSeeded(
    testInfo,
    "F8-S7-25",
    "С-кл",
    page.context(),
    async (h) => {
      await authenticatorWithKey(page, h.key!);
      const s = await openSecurity(page);
      const enrolled = page.waitForResponse(
        (r) => r.request().method() === "POST" && new URL(r.url()).pathname === LANE.enroll && r.status() === 200,
      );
      await s.secondFactor.getByRole("button", { name: "Настроить приложение-аутентификатор", exact: true }).click();
      const w = stepUpWindow(page);
      await expect(w.keyButton, "окно предлагает «Подтвердить ключом доступа»").toBeVisible();
      await w.keyButton.click();
      const secret = String(((await (await enrolled).json()) as { secret?: unknown }).secret ?? "");
      const dialog = page.getByRole("dialog", { name: "Настроить приложение-аутентификатор" });
      await dialog.getByLabel("Код из приложения", { exact: true }).fill(totpCode(secret, Date.now()));
      await dialog.getByRole("button", { name: "Подтвердить", exact: true }).click();
      const saved = page.getByRole("dialog", { name: "Сохраните запасные коды" });
      await saved.getByRole("checkbox", { name: "Я сохранил коды", exact: true }).check();
      await saved.getByRole("button", { name: "Готово", exact: true }).click();
      await expect(s.secondFactor.getByRole("row", { name: /Приложение-аутентификатор/ }), "в таблице — «Настроено»").toContainText(
        "Настроено",
      );
    },
    { level: "1" },
  );
});

test("F8-S7-26 · только ключ, отказ свежести: окно предлагает ключ, «Войти заново» не показан", async ({
  page,
}, testInfo) => {
  // verifies #3230 — kaname#729; близнец F8-S7-27, отличие — `stepUpMethods = ["webauthn"]`.
  await withSeeded(
    testInfo,
    "F8-S7-26",
    "С-л",
    page.context(),
    async (h) => {
      await authenticatorWithKey(page, h.key!);
      // Окно открывает отказ свежести на «Выйти на всех, кроме этой» — подстановка
      // ответа производителя (F8 Р9, посев С-несвежая); повтор идёт к службе.
      await page.route(LANE.sessionsEndOthers, (route) => fulfillWith(route, SESSION_NOT_FRESH), { times: 1 });
      const s = await openSecurity(page);
      await s.sessions.getByRole("button", { name: "Выйти на всех, кроме этой", exact: true }).click();
      await page.getByRole("dialog").getByRole("button", { name: "Выйти", exact: true }).click();
      const w = stepUpWindow(page);
      await expect(w.keyButton, "окно предлагает «Подтвердить ключом доступа» (Р5)").toBeVisible();
      await expect(w.signInAgain, "«Войти заново» не показан — у человека есть прямой путь").toHaveCount(0);
      await w.keyButton.click();
      await expect(w.dialog, "окно закрыто").toBeHidden();
      await expect(s.announced("Завершено сессий: 1"), "сессии завершены").toBeVisible();
    },
    { before: async (h) => anotherSession(testInfo, h.human) },
  );
});

test("F8-S7-30 · неверный код: «Подтверждение не прошло…» в role=alert, действие не повторено", async ({ page }, testInfo) => {
  // verifies #3230 — близнец F8-S7-22.
  await withSeeded(
    testInfo,
    "F8-S7-30",
    "С-ккл",
    page.context(),
    async (h) => {
      const census = ceremonyCensus(page.context());
      const { s, w } = await openStepUpOnDelete(page, h);
      await w.radio("Код из приложения").check();
      await w.code.fill(codeOutsideWindow(h.factor!.secret, Date.now()));
      await w.confirm.click();
      await expect(w.alert, "текст отказа").toHaveText("Подтверждение не прошло. Повторите или выберите другой способ.");
      await expect(w.dialog, "окно открыто").toBeVisible();
      expect(census.matching("DELETE", DELETE_KEY(h)), "исходное действие не повторено").toHaveLength(1);
      await expect(s.keyDelete(h.keyName)).toBeVisible();
    },
    { level: "1" },
  );
});

test("F8-S7-31 · ключ отвергнут службой: тот же текст, что у неверного кода; новая попытка — новое испытание", async ({
  page,
}, testInfo) => {
  // verifies #3230 — близнец F8-S7-21, отличие — ответ службы на `step-up`
  // (подстановка ответа производителя, настоящий — SUA-13).
  await withSeeded(
    testInfo,
    "F8-S7-31",
    "С-ккл",
    page.context(),
    async (h) => {
      await authenticatorWithKey(page, h.key!);
      // Ответ сочинён пробой целиком: службы он не касается и ось источника не
      // тратит (`noteSubstituted`) — в счёт оси идёт только настоящий отказ.
      await page.route(
        LANE.stepUp,
        (route) => {
          noteSubstituted(route.request());
          return route.fulfill({
            status: 401,
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ code: 16, message: "authentication failed", details: [] }),
          });
        },
        { times: 1 },
      );
      const census = ceremonyCensus(page.context());
      const { w } = await openStepUpOnDelete(page, h);
      await w.keyButton.click();
      await expect(w.alert, "тот же текст, что F8-S7-30").toHaveText("Подтверждение не прошло. Повторите или выберите другой способ.");
      await w.keyButton.click();
      await expect.poll(() => census.matching("POST", STEP_UP_BEGIN).length, { message: "новая попытка выдаёт новое испытание" }).toBe(2);
    },
    { level: "1" },
  );
});

test("F8-S7-32 · отказ церемонии браузером: step-up не зовётся", async ({ page }, testInfo) => {
  // verifies #3230 — близнец F8-S7-21: у аутентификатора страницы ключа нет.
  await withSeeded(
    testInfo,
    "F8-S7-32",
    "С-ккл",
    page.context(),
    async (h) => {
      await pageAuthenticator(page, true);
      const census = ceremonyCensus(page.context());
      const { w } = await openStepUpOnDelete(page, h);
      await w.keyButton.click();
      await expect(w.alert.or(w.dialog.getByRole("status"))).toHaveText(
        "Подтверждение ключом прервано в браузере — повторите или выберите другой способ",
      );
      expect(census.matching("POST", LANE.stepUp), "POST /iam/v1/auth/step-up — 0").toHaveLength(0);
    },
    { level: "1" },
  );
});

test("F8-S7-33 · частота: «Повторите через 10 мин.», поле и кнопка недоступны", async ({ page }, testInfo) => {
  // verifies #3230 — близнец F8-S7-22; С-частота с Retry-After: 600.
  await withSeeded(
    testInfo,
    "F8-S7-33",
    "С-ккл",
    page.context(),
    async (h) => {
      await page.route(LANE.stepUp, (route) => fulfillWith(route, laneTooManyAttempts(600)), { times: 1 });
      const { w } = await openStepUpOnDelete(page, h);
      await w.radio("Код из приложения").check();
      await w.code.fill(await nextStepCode(h.factor!.secret));
      await w.confirm.click();
      await expect(w.dialog).toContainText("Слишком много попыток. Повторите через 10 мин.");
      await expect(w.code, "поле недоступно").toBeDisabled();
      await expect(w.confirm, "кнопка недоступна").toBeDisabled();
    },
    { level: "1" },
  );
});

test("F8-S7-42 · отключение кода, последний способ «2» — вторая фраза; у С-ккл её нет", async ({ page }, testInfo) => {
  // verifies #3230
  const first = "Коды из приложения и запасные коды перестанут работать.";
  const second = "Действия, требующие второго фактора, станут недоступны, пока вы не настроите его снова.";
  await withSeeded(testInfo, "F8-S7-42", "С-к", page.context(), async () => {
    const s = await openSecurity(page);
    await s.secondFactor.getByRole("button", { name: "Отключить", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toContainText(first);
    await expect(dialog).toContainText(second);
    await expect(dialog.getByLabel("Код из приложения", { exact: true }), "подтверждение — кодом в диалоге").toBeVisible();
  });
  // Близнец — тем же браузером: носитель первого человека сменяется носителем второго.
  await page.context().clearCookies();
  await withSeeded(testInfo, "F8-S7-42t", "С-ккл", page.context(), async () => {
    const s = await openSecurity(page);
    await s.secondFactor.getByRole("button", { name: "Отключить", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toContainText(first);
    await expect(dialog, "у С-ккл второй фразы нет — ключ остаётся способом «2»").not.toContainText(second);
  });
});

test("F8-S7-43 · удаление последнего способа входа — текст службы дословно, ключ в таблице", async ({
  page,
}, testInfo) => {
  // verifies #3230 — «Дано» С-л; положительный близнец (С-ккл) — F8-S7-71.
  await withSeeded(testInfo, "F8-S7-43", "С-л", page.context(), async (h) => {
    const s = await openSecurity(page);
    await deleteKey(page, s, h);
    await expect(
      s.keys.getByText("last sign-in method cannot be revoked: enrol another sign-in method first", { exact: true }),
      "текст службы дословно",
    ).toBeVisible();
    await expect(s.keyDelete(h.keyName), "ключ в таблице").toBeVisible();
  });
});

test("F8-S7-50 · ключи выключены на установке: состояние раздела, BeginRegistration — 0, «Удалить» — сообщение", async ({
  page,
}, testInfo) => {
  // verifies #3230 — kacho#3229 п. 4; близнец — С-ккл при С-выкл.
  await withSeeded(testInfo, "F8-S7-50", "С-кл", page.context(), async (h) => {
    await page.route(LANE.secondFactor, keysTurnedOff);
    const census = ceremonyCensus(page.context());
    const s = await openSecurity(page);
    await expect(s.keys).toContainText("Ключи доступа на этой установке выключены администратором");
    await expect(s.keys.getByRole("button", { name: "Добавить ключ доступа", exact: true })).toHaveCount(0);
    await expect(s.keyDelete(h.keyName), "заведённый ключ показан с «Удалить»").toBeVisible();
    await expect(s.summary).toContainText("Выключены на установке");
    expect(census.calls.filter((c) => c.path.endsWith(":beginRegistration")), "обращений BeginRegistration — 0").toHaveLength(0);
    await s.keyDelete(h.keyName).click();
    await expect(
      s.announced(
        "Удалить ключ сейчас нельзя: нужен второй фактор, а ключи на этой установке выключены. Попросите администратора установки сбросить ваши ключи доступа.",
      ),
    ).toBeVisible();
    expect(census.matching("DELETE", DELETE_KEY(h)), "обращений Revoke — 0").toHaveLength(0);
    await expect(stepUpWindow(page).keyButton, "окна подтверждения нет").toHaveCount(0);
  });
  // Близнец — тем же браузером: носитель первого человека сменяется носителем второго.
  await page.context().clearCookies();
  await withSeeded(
    testInfo,
    "F8-S7-50t",
    "С-ккл",
    page.context(),
    async (h) => {
      // Подстановка состояния «выкл» стоит на странице с первой части пробы.
      const s = await openSecurity(page);
      await deleteKey(page, s, h);
      const w = stepUpWindow(page);
      await expect(w.code, "близнец: окно подтверждения со способом «Код из приложения»").toBeVisible();
      await expect(w.keyButton, "ключа в окне нет").toHaveCount(0);
    },
    { level: "1" },
  );
});

test("F8-S7-60 · ключи общей таблицей с датами; своих <ul> в разделе — 0", async ({ page }, testInfo) => {
  // verifies #3230
  await withSeeded(
    testInfo,
    "F8-S7-60",
    "С-ккл",
    page.context(),
    async () => {
      const s = await openSecurity(page);
      const table = s.keys.getByRole("table");
      // Колонки — дословно и по порядку (Р3); колонка действия заголовка не несёт.
      // Пустой заголовок таблица выпускает без `scope`, и доступная роль такой
      // клетки — `cell`, а не `columnheader`: строка заголовка читается целиком,
      // в порядке документа — три названные колонки и пустая последней.
      const head = table.getByRole("row").first();
      await expect(head.getByRole("columnheader").or(head.getByRole("cell")), "колонки таблицы ключей (Р3)").toHaveText([
        "Имя",
        "Добавлен",
        "Последнее использование",
        "",
      ]);
      await expect(table.getByRole("row").nth(1)).not.toContainText("Ещё не использовался");
      expect(await s.keys.locator("ul").count(), "своих <ul> в разделе — 0").toBe(0);
    },
    {
      before: async (h) => {
        const used = await presentAccessKey(h.seed, testInfo.project.use.baseURL, {
          credentialId: h.key!.credentialId,
          privateKey: h.key!.privateKey,
          userHandle: h.key!.userHandle,
          signCount: 0,
        });
        expect(used.status, `посев: ключом пользовались — ${used.text.slice(0, 200)}`).toBe(200);
      },
    },
  );
});

test("F8-S7-70 · клавиатура: каждое действие достижимо Tab, диалог держит фокус, Esc возвращает фокус", async ({
  page,
}, testInfo) => {
  // verifies #3230
  await withSeeded(testInfo, "F8-S7-70", "С-ккл", page.context(), async (h) => {
    const s = await openSecurity(page);
    const actions = await page.locator("main").getByRole("button").count();
    const links = await s.nav.getByRole("link").count();
    const stops: string[] = [];
    await page.locator("body").focus();
    // Обход кончается, когда фокус вернулся на УЖЕ посещённый элемент. Узнаётся
    // он по самому узлу документа (равенство ссылок на странице), а не по тексту:
    // у кнопок без текста подпись одна, и обход по ней обрывался на второй такой.
    await page.evaluate(() => {
      (window as unknown as { tabStopsSeen: Element[] }).tabStopsSeen = [];
    });
    for (let i = 0; i < actions + links + 40; i++) {
      await page.keyboard.press("Tab");
      const what = await page.evaluate(() => {
        const a = document.activeElement;
        if (!a || a === document.body) return "";
        const seen = (window as unknown as { tabStopsSeen: Element[] }).tabStopsSeen;
        if (seen.includes(a)) return "";
        seen.push(a);
        return `${a.tagName.toLowerCase()}:${a.getAttribute("role") ?? ""}`;
      });
      if (what === "") break;
      stops.push(what);
    }
    const passive = stops.filter((x) => !/^(button|a|input|select|textarea)/.test(x));
    expect(passive, "в обход попали подписи, даты или признаки").toEqual([]);
    expect(stops.filter((x) => x.startsWith("button")).length, "каждое действие main достижимо Tab").toBeGreaterThanOrEqual(actions);

    const trigger = s.keyDelete(h.keyName);
    await trigger.focus();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    for (let i = 0; i < 6; i++) {
      await page.keyboard.press("Tab");
      expect(await dialog.evaluate((d) => d.contains(document.activeElement)), "диалог держит фокус").toBe(true);
    }
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await expect(trigger, "фокус вернулся на вызвавшую кнопку").toBeFocused();
  });
});

test("F8-S7-71 · экранный диктор: «Ключ доступа «<имя>» удалён», имя кнопки строки, разделы с aria-labelledby", async ({
  page,
}, testInfo) => {
  // verifies #3230 — положительный близнец F8-S7-43 (у С-ккл есть пароль).
  await withSeeded(testInfo, "F8-S7-71", "С-ккл", page.context(), async (h) => {
    const s = await openSecurity(page);
    await expect(s.keyDelete(h.keyName), "доступное имя кнопки строки").toBeVisible();
    for (const name of [
      "Способ входа",
      "Второй фактор",
      "Ключи доступа",
      "Запасные коды",
      "Активные сессии",
    ]) {
      const region = s.section(name);
      const by = await region.getAttribute("aria-labelledby");
      expect(by, `раздел «${name}» — section с aria-labelledby`).toBeTruthy();
      await expect(page.locator(`#${by}`), `aria-labelledby раздела «${name}» — на его заголовок`).toHaveText(name);
    }
    await deleteKey(page, s, h);
    await expect(s.announced(`Ключ доступа «${h.keyName}» удалён`), "область role=status").toBeVisible();
    await expect(s.keyDelete(h.keyName), "ключа в таблице нет").toHaveCount(0);
  });
});

