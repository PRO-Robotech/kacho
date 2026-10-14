// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Тексты полосы формы службы удостоверений, которые пробы утверждают побайтово.
 *
 * Производитель — служба, а не консоль: тексты — контракт полосы и стоят у неё
 * константами (`humansession/refusals.go`). Здесь они записаны ОДИН раз, чтобы
 * проба экрана, проба ответа и сторож бюджета судили одним значением: смена
 * текста у производителя правится в одном месте, а не в каждой пробе.
 *
 * Модуль без зависимостей: его читает и сторож, запускаемый голым `node` после
 * прогона (`scripts/ceremony-budget.ts`).
 */

/** Отказ входа БЕЗ поля `secondFactor` и прочих глаголов полосы — один на все причины. */
export const AUTHENTICATION_FAILED = "authentication failed";

/**
 * Отказ входа С полем `secondFactor` — один на все причины этой формы запроса
 * (неверный пароль, неверный код, фактор не заведён). Отличие от
 * [AUTHENTICATION_FAILED] различает присланное, а не найденное: форму запроса
 * знает сам вызывающий.
 */
export const LOGIN_WITH_SECOND_FACTOR_FAILED =
  "authentication failed; check the email, the password and the code, and send secondFactor only if a second factor is enrolled";

/**
 * Отказ ЗАВЕРШЕНИЯ восстановления — один на все причины глагола (код не тот,
 * истёк, применён, адреса нет, личность заблокирована).
 */
export const ACCESS_NOT_RESTORED =
  "access not restored; request a new recovery code, and if a new code does not restore access, ask an administrator";

/**
 * Следующий шаг в теле ответа на ЗАПРОС кода восстановления — один на все
 * исходы: заведённый и незаведённый адрес получают побайтово одно тело.
 */
export const RECOVERY_NEXT_STEP =
  "a letter with a recovery code is sent if this address can recover access; if no letter arrives, " +
  "request again later, sign in and confirm the address, or ask an administrator to reset your sign-in methods";
