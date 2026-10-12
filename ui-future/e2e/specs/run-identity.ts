// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Величины арендатора набора без зависимостей: их читают и фикстуры
 * (`fixtures.ts`, переэкспорт), и посев (`ceremony-seed.ts`).
 *
 * ПОЧЕМУ ОТДЕЛЬНЫМ МОДУЛЕМ (kacho#3106). Посев брал их у фикстур, а фикстуры
 * теперь сами зовут посев — разбор пробы снимает арендатора его входом
 * (`tenant-removal.ts`). Кольцо импорта `fixtures → посев → fixtures` отдало бы
 * посеву недогруженный модуль, и `SEED_PASSWORD` стал бы `undefined` молча.
 * Лист без импортов кольца не образует by construction.
 */

/** Пароль арендатора набора; тот же у посева «Дано» (`ceremony-seed.ts`). */
export const E2E_PASSWORD = "Kacho-E2E-2026!x";

/** Имя носителя сессии нашей службы — то, что браузер держит после входа. */
export const SESSION_COOKIE = "kaname_session";

/** уникальное имя прогона — из времени; коллизия по UNIQUE(name) иначе даёт 409 */
export function runTag(): string {
  return Date.now().toString(36) + Math.trunc(performance.now()).toString(36);
}
