// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * ТЕКСТЫ ОТКАЗОВ СЛУЖБЫ УДОСТОВЕРЕНИЙ, которые утверждают сквозные пробы
 * консоли, — записаны здесь ОДИН раз. Производитель — kaname
 * `internal/apps/kaname/api/access_keys/refusals.go` (ревизия — пин модуля
 * kaname в `go.mod`); значения перенесены оттуда дословно.
 *
 * Почему одно место: у производителя тексты — контракт, и он их меняет
 * намеренно (kaname#757, #802 дописали к отказу следующий шаг). Пока текст
 * жил копией в каждой пробе, смена у производителя краснила пробы порознь
 * (kacho#3305: четыре пробы, три файла, одна причина), и правка требовала
 * переписи дерева. Отсюда текст читают живые пробы стенда, подставленные
 * ответы (`producer-answers.ts`) и край петли самопроверки условия П4
 * (`scripts/access-key-origin-selftest.ts`): смена у производителя — одна
 * правка здесь.
 *
 * Модуль без зависимостей: его читает самопроверка, запускаемая голым `node`.
 */

/**
 * ЕДИНЫЙ отказ полосы утверждения (`TextAssertionNotAccepted`): один текст на
 * все четырнадцать полос §3.0, код 3 — различимый текст сказал бы
 * предъявителю, какая сверка не сошлась.
 */
export const ASSERTION_NOT_ACCEPTED = "access key assertion is not accepted: begin the assertion again";

/**
 * Отказ полосы регистрации по происхождению удостоверения
 * (`TextOriginNotAllowed`) — так, как его отдаёт край: `400` · `code` 3 ·
 * этот текст · причина `ORIGIN_NOT_ALLOWED`. Живая проба F7-44-edge
 * (`specs/access-key-binding.spec.ts`) утверждает эту форму на стенде, а край
 * петли самопроверки условия П4 отдаёт её же.
 */
export const ORIGIN_NOT_IN_LIST =
  "credential origin is not in the declared list: begin registration again from a declared origin, or ask the operator to declare this origin";

/**
 * Сессия не свежа (`TextSessionNotFresh`): `403` · `code` 7 · причина
 * `SESSION_NOT_FRESH`. Тот же текст служба отдаёт и на полосе входа
 * (`humansession.TextSessionNotFresh`).
 */
export const SESSION_NOT_FRESH_TEXT = "re-authentication required: present a credential again";

/** Последний способ входа не снимается (`TextLastSignInMethod`): `400` · `code` 9. */
export const LAST_SIGN_IN_METHOD_TEXT = "last sign-in method cannot be revoked: enrol another sign-in method first";

/** Причина отказа по происхождению в `ErrorInfo` тела. */
export const ORIGIN_NOT_ALLOWED = "ORIGIN_NOT_ALLOWED";

/**
 * Отказ выдачи испытания на установке, объявившей «никого» (перечень
 * происхождений пуст): текст и причина службы удостоверений (kaname,
 * `webauthnverify.TextKeysDisabled` · `ReasonKeysDisabled`). Код — 9
 * (FailedPrecondition). Утверждает его KO-03 (kacho#3229, п.4).
 */
export const KEYS_DISABLED_TEXT = "access keys are turned off on this installation";

/** Причина того же отказа в `ErrorInfo` тела. */
export const ACCESS_KEYS_DISABLED = "ACCESS_KEYS_DISABLED";
