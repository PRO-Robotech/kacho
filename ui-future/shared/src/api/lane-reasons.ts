// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Причины отказа полосы формы (`ErrorInfo.reason`), на которые консоль отвечает
 * ДЕЙСТВИЕМ или особым текстом, а не только показом. Словарь — словарь службы
 * (`kaname` `humansession.Reason*`, `registration.ReasonRegistrationRefused`).
 *
 * Три причины подтверждения адреса (приёмка F6b): `EMAIL_NOT_VERIFIED` — отказ
 * края и службы учётной записи с неподтверждённым адресом (Р3, одно значение у
 * обоих производителей); `EMAIL_ALREADY_VERIFIED` и `INVITE_NOT_VALID` — исходы
 * глаголов подтверждения (Р6, Р11 п. 4 службы). Консоль решает по ним, а не по
 * статусу: два исхода `400` / `9` различаются только причиной.
 */
export const LANE_REASON = {
  formTokenRejected: "FORM_TOKEN_REJECTED",
  tooManyAttempts: "TOO_MANY_ATTEMPTS",
  sessionNotFresh: "SESSION_NOT_FRESH",
  secondFactorNotEnrolled: "SECOND_FACTOR_NOT_ENROLLED",
  emailNotVerified: "EMAIL_NOT_VERIFIED",
  emailAlreadyVerified: "EMAIL_ALREADY_VERIFIED",
  inviteNotValid: "INVITE_NOT_VALID",
  // Первый пароль у человека, у которого пароль уже есть (приёмка F8, ред. 12,
  // Р13): раздел «Пароль» возвращается к форме смены.
  passwordAlreadySet: "PASSWORD_ALREADY_SET",
  // Новый адрес заняли между запросом смены и предъявлением кода (приёмка F8-S6,
  // Р4; службы — EC Р5): раздел называет это и возвращается к форме адреса.
  emailInUse: "EMAIL_IN_USE",
  // Повышение ключом доступа (служба — приёмка SUA, Р5, Р7; экран — приёмка
  // F8-S7, Р5): ключа нет либо ключи выключены на установке — окно
  // перечитывает перечень способов, и способ уходит из выбора.
  accessKeyNotRegistered: "ACCESS_KEY_NOT_REGISTERED",
  accessKeysDisabled: "ACCESS_KEYS_DISABLED",
} as const;

/**
 * Приставка причин испытания повышения ключом (SUA Р3): не выдано, уже
 * предъявлено, истекло. Для человека у них один исход — новое испытание.
 */
export const CHALLENGE_REASON_PREFIX = "CHALLENGE_";
