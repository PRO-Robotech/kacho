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
  // Причина КРАЯ, а не службы: ограничитель анонимных почтовых глаголов
  // отвечает вызовом доказательства работы (приёмка NTF-2, Р5; замысел
  // `issue-2917` З9, `anonmail.reasonChallenge`). Консоль решает его
  // один раз и повторяет то же тело с заголовком доказательства.
  proofOfWorkRequired: "PROOF_OF_WORK_REQUIRED",
} as const;
