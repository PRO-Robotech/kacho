// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Отказ службы по происхождению удостоверения (глаголы Ф7) — так, как его
 * отдаёт край: `400` · `code` 3 · этот текст · причина `ORIGIN_NOT_ALLOWED`
 * (производитель — `access_keys/refusals.go` службы удостоверений).
 *
 * Записан ОДИН раз: живая проба F7-44-edge (`specs/access-key-binding.spec.ts`)
 * утверждает эту форму на стенде, а край петли самопроверки условия П4
 * (`scripts/access-key-origin-selftest.ts`) отдаёт её же — смена формы у
 * производителя краснит живую пробу, и правка здесь ведёт за собой обе.
 *
 * Модуль без зависимостей: его читает самопроверка, запускаемая голым `node`.
 */
export const ORIGIN_NOT_IN_LIST = "credential origin is not in the declared list";

/** Причина отказа по происхождению в `ErrorInfo` тела. */
export const ORIGIN_NOT_ALLOWED = "ORIGIN_NOT_ALLOWED";
