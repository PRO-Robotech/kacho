// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Причины отказа полосы формы (`ErrorInfo.reason`), на которые консоль отвечает
 * ДЕЙСТВИЕМ или особым текстом, а не только показом. Словарь — словарь службы
 * (`kaname` `humansession.Reason*`, `registration.ReasonRegistrationRefused`).
 */
export const LANE_REASON = {
  formTokenRejected: "FORM_TOKEN_REJECTED",
  tooManyAttempts: "TOO_MANY_ATTEMPTS",
  sessionNotFresh: "SESSION_NOT_FRESH",
  secondFactorNotEnrolled: "SECOND_FACTOR_NOT_ENROLLED",
} as const;
