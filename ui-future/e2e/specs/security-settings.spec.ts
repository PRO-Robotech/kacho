// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect } from "@playwright/test";
import { LANE, totpCode } from "./ceremony-seed";
import { ceremonyCensus, test } from "./fixtures";
import { EDGE_BACKEND_UNREACHABLE, SESSION_NOT_FRESH, fulfillWith } from "./producer-answers";
import {
  anotherSession,
  nextStepCode,
  openSecurity,
  sentBodies,
  stepUpWindow,
  withSeeded,
} from "./security-screen";

/**
 * Экран «Безопасность» — сценарии БЕЗ ключа доступа (приёмка F8-S7 воркспейса,
 * отпечаток 559ae688…; реализация kacho#3230): F8-S7-11, -12, -24, -27, -40,
 * -41, -61. Сценарии, у которых в «Дано» есть ключ, — в
 * `security-settings-access-key.spec.ts`: им нужно условие П4 (происхождение
 * браузера набора принимается для ключей), и проект набора ставит его зависимостью.
 *
 * Имя пробы начинается с ID сценария, ссылка на задачу — внутри `test(…)`.
 * Повторов нет; ждётся условие.
 *
 * ЧЕМ КРАСНЕЕТ ДО КОДА: на `/settings` прежний экран «Параметры учётной записи»
 * (`openSecurity` называет это словами) либо нет доступного имени из приёмки —
 * таблицы способов, QR-кода, окна «Сохраните запасные коды», пустых состояний.
 */

test("F8-S7-11 · сразу после заведения: «Ещё не использовалось» у кода и «Ещё не использовались» у запасных кодов", async ({
  page,
}, testInfo) => {
  // verifies #3230 — близнец F8-S7-10: предъявлений после подтверждения не было.
  await withSeeded(testInfo, "F8-S7-11", "С-к", page.context(), async () => {
    const s = await openSecurity(page);
    const totp = s.secondFactor.getByRole("row", { name: /Приложение-аутентификатор/ });
    const codes = s.secondFactor.getByRole("row", { name: /Запасные коды/ });
    await expect(totp, "строка «Приложение-аутентификатор» таблицы способов (Р2)").toBeVisible();
    await expect(totp, "«Последнее использование» кода — «Ещё не использовалось»").toContainText("Ещё не использовалось");
    await expect(codes, "«Последнее использование» запасных кодов — «Ещё не использовались»").toContainText(
      "Ещё не использовались",
    );
    await expect(s.secondFactor.getByText(/^[—–-]$/), "прочерков на месте факта нет (Р2)").toHaveCount(0);
  });
});

/** Чтение перечня своих ключей — `GET /iam/v1/users/{id}/accessKeys` (Р3). */
const isKeysList = (url: URL) => /^\/iam\/v1\/users\/[^/]+\/accessKeys$/.test(url.pathname);

test("3238 · перечень ключей не прочитан: «Второй фактор» показывает способы и называет непрочитанный перечень", async ({
  page,
}, testInfo) => {
  // verifies #3238 — находка console-e2e на 897e11c8f: край ответил 503 на перечень
  // ключей, и раздел «Второй фактор» остался без таблицы способов, хотя состояние
  // фактора прочитано (F8-S7-11 не нашла строку «Приложение-аутентификатор»).
  await withSeeded(testInfo, "3238-keys", "С-к", page.context(), async () => {
    await page.route(isKeysList, (route) =>
      route.request().method() === "GET" ? fulfillWith(route, EDGE_BACKEND_UNREACHABLE) : route.continue(),
    );
    const s = await openSecurity(page);
    // Условие создано: раздел ключей назвал отказ чтения.
    await expect(s.keys.getByText("Перечень ключей не прочитан.", { exact: true }), "раздел ключей назвал отказ").toBeVisible();
    const totp = s.secondFactor.getByRole("row", { name: /Приложение-аутентификатор/ });
    const keys = s.secondFactor.getByRole("row", { name: /^Ключи доступа/ });
    const codes = s.secondFactor.getByRole("row", { name: /Запасные коды/ });
    await expect(totp, "строка «Приложение-аутентификатор» — состояние фактора прочитано").toBeVisible();
    await expect(totp, "код настроен").toContainText("Настроено");
    await expect(codes, "строка «Запасные коды»").toContainText("Осталось 10 из 10");
    await expect(keys, "строка «Ключи доступа» называет, что перечень не прочитан, а не «Нет»").toContainText("Перечень не прочитан");
    await expect(keys, "о непрочитанном перечне — не «Нет»").not.toContainText(/(^|\s)Нет(\s|$)/);
    await expect(s.secondFactor.getByText("Второй фактор не настроен", { exact: false }), "приглашение «не настроен» — не на отказе").toHaveCount(0);
  });
});

test("F8-S7-12 · заведение кода с QR-кодом: секрет не уходит ни в один исходящий запрос", async ({ page }, testInfo) => {
  // verifies #3230
  await withSeeded(testInfo, "F8-S7-12", "С-п", page.context(), async () => {
    const sent = sentBodies(page.context());
    const s = await openSecurity(page);
    const enrolled = page.waitForResponse((r) => r.request().method() === "POST" && new URL(r.url()).pathname === LANE.enroll);
    await s.secondFactor.getByRole("button", { name: "Настроить приложение-аутентификатор", exact: true }).click();
    const enrollRes = await enrolled;
    expect(enrollRes.status(), "enroll — 200").toBe(200);
    const secret = String(((await enrollRes.json()) as { secret?: unknown }).secret ?? "");
    expect(secret, "ответ enroll несёт секрет").not.toBe("");

    const dialog = page.getByRole("dialog", { name: "Настроить приложение-аутентификатор" });
    await expect(dialog.getByRole("img", { name: "QR-код для приложения-аутентификатора" }), "QR-код с доступным именем (Р9)").toBeVisible();
    await expect(dialog.getByText(secret, { exact: true }), "секрет текстом — альтернатива QR-коду").toBeVisible();
    await expect(dialog.getByRole("button", { name: /Копировать/ }), "кнопка копирования секрета").toBeVisible();
    const code = dialog.getByLabel("Код из приложения", { exact: true });
    await expect(code, "поле «Код из приложения» парой «имя — ввод»").toBeVisible();

    await code.fill(totpCode(secret, Date.now()));
    await dialog.getByRole("button", { name: "Подтвердить", exact: true }).click();
    const saved = page.getByRole("dialog", { name: "Сохраните запасные коды" });
    await expect(saved, "окно «Сохраните запасные коды» (F8-S7-40)").toBeVisible();
    await expect(saved.getByRole("listitem"), "в окне 10 кодов").toHaveCount(10);

    const leaked = sent.filter((b) => b.url.includes(secret) || b.body.includes(secret)).map((b) => `${b.method} ${b.path}`);
    expect(leaked, "секрет встречается в исходящем запросе — QR-код не рисуется в браузере").toEqual([]);
  });
});

test("F8-S7-24 · запасной код по умолчанию не выбирается: выбран «Код из приложения»", async ({ page }, testInfo) => {
  // verifies #3230
  await withSeeded(
    testInfo,
    "F8-S7-24",
    "С-к",
    page.context(),
    async () => {
      // Окно открывает отказ свежести на «Выйти на всех, кроме этой» — подстановка
      // ответа производителя (F8 Р9, посев С-несвежая).
      await page.route(LANE.sessionsEndOthers, (route) => fulfillWith(route, SESSION_NOT_FRESH), { times: 1 });
      const s = await openSecurity(page);
      await s.sessions.getByRole("button", { name: "Выйти на всех, кроме этой", exact: true }).click();
      await page.getByRole("dialog").getByRole("button", { name: "Выйти", exact: true }).click();
      const w = stepUpWindow(page);
      await expect(w.dialog, "окно подтверждения открыто").toBeVisible();
      await expect(w.radio("Код из приложения"), "по умолчанию — «Код из приложения» (Р5)").toBeChecked();
      await expect(w.otherGroup.getByRole("radio", { name: "Запасной код", exact: true }), "«Запасной код» — в «Другой способ»").toBeVisible();
      await expect(w.radio("Запасной код")).not.toBeChecked();
    },
    { level: "1", before: async (h) => anotherSession(testInfo, h.human) },
  );
});

test("F8-S7-27 · способов нет: «Нет способа подтверждения», «Настроить второй фактор» и «Войти заново»", async ({
  page,
}, testInfo) => {
  // verifies #3230 — близнец F8-S7-26: отличие — `stepUpMethods = []`.
  await withSeeded(
    testInfo,
    "F8-S7-27",
    "С-п",
    page.context(),
    async () => {
      await page.route(LANE.sessionsEndOthers, (route) => fulfillWith(route, SESSION_NOT_FRESH), { times: 1 });
      const s = await openSecurity(page);
      await s.sessions.getByRole("button", { name: "Выйти на всех, кроме этой", exact: true }).click();
      await page.getByRole("dialog").getByRole("button", { name: "Выйти", exact: true }).click();
      const w = stepUpWindow(page);
      await expect(w.noMethod, "текст «Нет способа подтверждения» (Р5)").toBeVisible();
      await expect(w.setUp, "ссылка «Настроить второй фактор»").toBeVisible();
      await expect(w.signInAgain, "путь «Войти заново» (F8 Р12)").toBeVisible();
      await expect(w.code, "поля кода нет").toHaveCount(0);
      await expect(w.keyButton, "кнопки ключа нет").toHaveCount(0);
    },
    { before: async (h) => anotherSession(testInfo, h.human) },
  );
});

test("F8-S7-40 · запасные коды показаны один раз: «Готово» — только после «Я сохранил коды»", async ({ page }, testInfo) => {
  // verifies #3230
  await withSeeded(testInfo, "F8-S7-40", "С-п", page.context(), async () => {
    const s = await openSecurity(page);
    const enrolled = page.waitForResponse((r) => r.request().method() === "POST" && new URL(r.url()).pathname === LANE.enroll);
    await s.secondFactor.getByRole("button", { name: "Настроить приложение-аутентификатор", exact: true }).click();
    const secret = String(((await (await enrolled).json()) as { secret?: unknown }).secret ?? "");
    const dialog = page.getByRole("dialog", { name: "Настроить приложение-аутентификатор" });
    await dialog.getByLabel("Код из приложения", { exact: true }).fill(totpCode(secret, Date.now()));
    const confirmed = page.waitForResponse((r) => r.request().method() === "POST" && new URL(r.url()).pathname === LANE.confirm);
    await dialog.getByRole("button", { name: "Подтвердить", exact: true }).click();
    const codes = ((await (await confirmed).json()) as { backupCodes?: unknown[] }).backupCodes?.map(String) ?? [];
    expect(codes, "confirm выдал 10 кодов").toHaveLength(10);

    const saved = page.getByRole("dialog", { name: "Сохраните запасные коды" });
    const done = saved.getByRole("button", { name: "Готово", exact: true });
    await expect(done, "«Готово» недоступна без отметки").toBeDisabled();
    await expect(saved.getByRole("button", { name: "Копировать", exact: true }), "«Копировать» доступна").toBeEnabled();
    await expect(saved.getByRole("button", { name: "Скачать текстовым файлом", exact: true }), "«Скачать текстовым файлом» доступна").toBeEnabled();
    await saved.getByRole("checkbox", { name: "Я сохранил коды", exact: true }).check();
    await done.click();
    await page.reload({ waitUntil: "domcontentloaded" });
    const again = await openSecurity(page);
    for (const c of codes) {
      await expect(page.getByText(c, { exact: true }), "запасной код на странице после закрытия окна").toHaveCount(0);
    }
    await expect(again.backup, "раздел «Запасные коды» — «Осталось 10 из 10»").toContainText("Осталось 10 из 10");
  });
});

test("F8-S7-41 · перевыпуск с предупреждением: отмена ничего не выпускает, подтверждение кодом — 10 новых", async ({
  page,
}, testInfo) => {
  // verifies #3230
  await withSeeded(testInfo, "F8-S7-41", "С-к", page.context(), async (h) => {
    const census = ceremonyCensus(page.context());
    const s = await openSecurity(page);
    // Момент выпуска — полным моментом подсказки (Р2: дата на экране — до минуты,
    // полный момент — подсказкой): перевыпуск в ту же минуту, что подтверждение
    // посева, текстом строки неотличим.
    const issued = s.backup.getByRole("row", { name: /Выпущены/ }).locator("time");
    const issuedBefore = await issued.getAttribute("title");
    expect(issuedBefore, "«Выпущены» — момент с полной подсказкой").toBeTruthy();
    const regenerate = s.backup.getByRole("button", { name: "Выпустить новые коды", exact: true });
    await regenerate.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog, "диалог называет последствие").toContainText("Прежние запасные коды перестанут работать сразу.");
    await expect(dialog.getByRole("button", { name: "Отмена", exact: true }), "фокус — на «Отмена»").toBeFocused();
    await expect(dialog.getByLabel("Код из приложения", { exact: true }), "поле «Код из приложения» в диалоге").toBeVisible();
    await dialog.getByRole("button", { name: "Отмена", exact: true }).click();
    expect(census.matching("POST", LANE.backupCodes), "отмена не выпускает обращений …/backup-codes").toHaveLength(0);

    await regenerate.click();
    const code = await nextStepCode(h.factor!.secret);
    await page.getByRole("dialog").getByLabel("Код из приложения", { exact: true }).fill(code);
    const issuedNew = page.waitForResponse(
      (r) => r.request().method() === "POST" && new URL(r.url()).pathname === LANE.backupCodes,
    );
    await page.getByRole("dialog").getByRole("button", { name: "Выпустить", exact: true }).click();
    const fresh = ((await (await issuedNew).json()) as { backupCodes?: unknown[] }).backupCodes?.map(String) ?? [];
    expect(fresh, "перевыпуск выдал 10 кодов").toHaveLength(10);
    const saved = page.getByRole("dialog", { name: "Сохраните запасные коды" });
    await expect(saved.getByRole("listitem"), "окно «Сохраните запасные коды» с 10 новыми").toHaveCount(10);
    for (const c of fresh) {
      await expect(saved.getByText(c, { exact: true }), "новый код показан в окне").toHaveCount(1);
    }
    await saved.getByRole("checkbox", { name: "Я сохранил коды", exact: true }).check();
    await saved.getByRole("button", { name: "Готово", exact: true }).click();
    await expect(saved, "окно закрыто «Готово»").toHaveCount(0);
    for (const c of fresh) {
      await expect(page.getByText(c, { exact: true }), "новый запасной код на странице после «Готово»").toHaveCount(0);
    }
    await expect(issued, "«Выпущены» сдвинуто").not.toHaveAttribute("title", issuedBefore ?? "");
  });
});

test("F8-S7-61 · пустые состояния — на пустом ответе, с действием", async ({ page }, testInfo) => {
  // verifies #3230 — близнец F8-S7-60.
  await withSeeded(testInfo, "F8-S7-61", "С-п", page.context(), async () => {
    const s = await openSecurity(page);
    await expect(s.secondFactor).toContainText("Второй фактор не настроен. Он нужен для действий, меняющих доступ.");
    await expect(s.keys).toContainText("Ключей доступа нет. Входите и подтверждайте действия без пароля и кода.");
    await expect(s.keys.getByRole("button", { name: "Добавить ключ доступа", exact: true })).toBeVisible();
    await expect(s.backup).toContainText("Запасные коды появятся после настройки приложения-аутентификатора.");
    await expect(s.sessions).toContainText("Других активных сессий нет.");
    await expect(s.sessions.getByRole("button", { name: "Выйти на всех, кроме этой", exact: true })).toHaveCount(0);
  });
});
