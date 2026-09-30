// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { Alert } from "antd";
import type { LaneRefusal } from "@shared/api/login-lane";

// Отказ глагола на экране церемонии — ОДНА форма на все экраны.
//
// Текст — ДОСЛОВНО тот, что вернула служба (Р2): консоль отказ не переводит, не
// обогащает и не объясняет. В особенности она не различает причин отказа входа
// (Р4): «пароль неверен» и «адреса нет» приходят побайтово одинаково, и экран —
// функция ТОЛЬКО тела ответа, поэтому и экраны побайтово равны.
//
// Сверх текста консоль говорит ровно то, что знает сама и что человеку нужно,
// чтобы действовать: срок из заголовка `Retry-After` у отказа по частоте и
// приглашение повторить у отказа доступности. Ни то, ни другое не судит о
// содержимом формы.

/** Срок повтора — числом из заголовка, без пересчёта в «примерно». */
export function retryNotice(seconds: number): string {
  return `Повторить можно через ${seconds} с.`;
}

export const RETRY_INVITATION = "Отправьте форму ещё раз.";

/** Что добавить к тексту службы — только то, что нужно для действия. */
export function refusalHint(refusal: LaneRefusal): string | null {
  if (refusal.retryAfterSeconds !== null && refusal.code === 8) return retryNotice(refusal.retryAfterSeconds);
  // Доступность (`UNAVAILABLE`), обращение без ответа и ответ без тела отказа
  // от промежуточного узла — повтор может пройти, и человеку это сказано.
  if (refusal.code === 14 || refusal.status === 0 || (refusal.code === null && refusal.status >= 500)) {
    return RETRY_INVITATION;
  }
  return null;
}

export function LaneRefusalAlert({ refusal }: { refusal: LaneRefusal }) {
  const hint = refusalHint(refusal);
  return <Alert type="error" showIcon message={refusal.message} description={hint ?? undefined} />;
}
