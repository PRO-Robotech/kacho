// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { TestInfo } from "@playwright/test";
import { BUDGET_ATTACHMENT, takeRefusals } from "./ceremony-budget";
import { REGISTRATION_ATTACHMENT, takeRegistrations } from "./registration-budget.ts";

/**
 * Сдать запись пробы о расходе окна источника вложениями отчёта: обращения
 * полосы в счёт неверных предъявлений (доли F8-41 и S4, `BUDGET_ATTACHMENT`) и
 * регистрации (kacho#2909, `REGISTRATION_ATTACHMENT`). Зовётся, когда посев и
 * контексты пробы закрыты, — запись к этому моменту полна.
 *
 * Один код сдачи на всех, кто тратит окно: штатная фикстура проб
 * (`sourceAxisLedger` в `fixtures.ts`) и проба-предусловие, идущая без неё
 * (`preconditions/access-key-origin.precondition.ts`, приёмка F8-S4, DoD п. 10:
 * «сдаёт свою запись тем же вложением, что пробы сценариев»). Запись, взятая
 * из книги и не сданная, расходовала бы окно мимо сторожей.
 */
export async function attachSourceAxisRecords(testInfo: TestInfo): Promise<void> {
  const refusals = takeRefusals(testInfo.testId);
  if (refusals.length > 0) {
    await testInfo.attach(BUDGET_ATTACHMENT, {
      body: JSON.stringify({ scenario: testInfo.title, refusals }),
      contentType: "application/json",
    });
  }
  const registrations = takeRegistrations(testInfo.testId);
  if (registrations > 0) {
    await testInfo.attach(REGISTRATION_ATTACHMENT, {
      body: JSON.stringify({ scenario: testInfo.title, registrations }),
      contentType: "application/json",
    });
  }
}
