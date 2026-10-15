// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Разделы экрана «Безопасность» — в порядке задач человека (приёмка F8-S7, Р1).
//
// Адрес заголовка раздела — ПОСТОЯННЫЙ: на него ссылаются `aria-labelledby`
// раздела, ссылка навигации «Разделы страницы» и путь «Настроить второй фактор»
// окна подтверждения (`#security-second-factor`). Выведенный из `useId` он
// менялся бы от порядка монтирования, и ссылка извне вела бы в никуда.

export const SECURITY_SECTION = {
  summary: { id: "security-summary", title: "Сводка" },
  signIn: { id: "security-sign-in", title: "Способ входа" },
  secondFactor: { id: "security-second-factor", title: "Второй фактор" },
  accessKeys: { id: "security-access-keys", title: "Ключи доступа" },
  backupCodes: { id: "security-backup-codes", title: "Запасные коды" },
  sessions: { id: "security-sessions", title: "Активные сессии" },
  // Удаление своей учётной записи (#3186) — последним, ниже задач Р1: оно
  // необратимо и к защите учётной записи не относится, поэтому в навигацию
  // «Разделы страницы» не входит.
  deleteSelf: { id: "security-delete-self", title: "Удаление учётной записи" },
} as const;

export type SecuritySectionKey = keyof typeof SECURITY_SECTION;

/** Разделы навигации — пять; сводка стоит первой под шапкой и в навигацию не входит. */
export const NAVIGATED_SECTIONS: readonly SecuritySectionKey[] = [
  "signIn",
  "secondFactor",
  "accessKeys",
  "backupCodes",
  "sessions",
];

/** Перевести человека к разделу: фокус — на заголовок раздела, раздел — в поле зрения. */
export function goToSection(key: SecuritySectionKey): void {
  const heading = document.getElementById(SECURITY_SECTION[key].id);
  if (!heading) return;
  heading.scrollIntoView?.({ block: "start" });
  heading.focus();
}
