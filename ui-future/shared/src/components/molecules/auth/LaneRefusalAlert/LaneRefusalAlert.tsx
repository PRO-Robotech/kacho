// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { Alert, Typography } from "antd";
import type { LaneRefusal } from "@shared/api/login-lane";

// Отказ глагола на экране церемонии — ОДНА форма на все экраны.
//
// Текст — ДОСЛОВНО тот, что вернула служба (Р2): консоль отказ не переводит, не
// обогащает и не объясняет. В особенности она не различает причин отказа входа
// (Р4): «пароль неверен» и «адреса нет» приходят побайтово одинаково, и экран —
// функция ТОЛЬКО тела ответа, поэтому и экраны побайтово равны.
//
// Сверх текста консоль говорит ровно то, что знает сама и что человеку нужно,
// чтобы действовать: срок из заголовка `Retry-After` у отказа по частоте,
// приглашение повторить у отказа доступности и СЛЕДУЮЩИЙ ШАГ у отказов `16` и
// `9` (#2953). Ни одно из них не судит о содержимом формы.
//
// СЛЕДУЮЩИЙ ШАГ — ОДИН ТЕКСТ НА КОД И ЭКРАН. Служба отдаёт `16` и `9` одним
// текстом на все причины (неверный пароль и незаведённый адрес; занятый адрес и
// потолок темпа регистрации), и подсказка обязана быть такой же: она перечисляет
// пути, не выбирая между ними, и от текста отказа не зависит. Подсказка, которая
// различает причины, была бы оракулом чужой учётной записи.
//
// Подсказка шага стоит РЯДОМ с окном отказа, а не в нём: окно несёт текст службы
// дословно и ни словом больше (F8-15, F8S3-09), а шаг — слово консоли.

/** Срок повтора — числом из заголовка, без пересчёта в «примерно». */
export function retryNotice(seconds: number): string {
  return `Повторить можно через ${seconds} с.`;
}

export const RETRY_INVITATION = "Отправьте форму ещё раз.";

/**
 * Экран, на котором показан отказ, — от него зависит, какой следующий шаг
 * назвать. Не назван — шаг общий, верный на любом экране.
 */
export type RefusalContext = "sign-in" | "registration" | "recovery";

/** Следующий шаг к отказу `16` (UNAUTHENTICATED) — по экрану. */
const NEXT_STEP_UNAUTHENTICATED: Record<RefusalContext | "any", string> = {
  "sign-in":
    "Проверьте адрес почты и пароль и отправьте форму ещё раз. Если пароль забыт, восстановите доступ по " +
    "ссылке «Не получается войти?».",
  registration: "Проверьте введённые данные и отправьте форму ещё раз.",
  recovery: "Проверьте код из последнего письма и отправьте форму ещё раз либо отправьте код ещё раз.",
  any: "Проверьте введённые данные и отправьте форму ещё раз.",
};

/** Следующий шаг к отказу `9` (FAILED_PRECONDITION) — по экрану. */
const NEXT_STEP_FAILED_PRECONDITION: Record<RefusalContext | "any", string> = {
  "sign-in": "Обновите страницу и войдите ещё раз. Если отказ повторяется, обратитесь к администратору облака.",
  registration:
    "Если учётная запись с этим адресом у вас уже есть, войдите или восстановите доступ. Если нет — " +
    "отправьте форму ещё раз позже.",
  recovery: "Отправьте код ещё раз и введите код из нового письма. Если отказ повторяется, обратитесь к администратору облака.",
  any: "Обновите страницу и повторите действие. Если отказ повторяется, обратитесь к администратору облака.",
};

/** Подсказка, которая говорит о сроке или повторе, — её место в окне отказа. */
function retryHint(refusal: LaneRefusal, secondsLeft?: number | null): string | null {
  if (refusal.code === 8 && refusal.retryAfterSeconds !== null) {
    // Срок, отсчитанный экраном, сильнее застывшего заголовка; истёк — не назван.
    if (secondsLeft === undefined) return retryNotice(refusal.retryAfterSeconds);
    return secondsLeft === null ? null : retryNotice(secondsLeft);
  }
  // Доступность (`UNAVAILABLE`), обращение без ответа и ответ без тела отказа
  // от промежуточного узла — повтор может пройти, и человеку это сказано.
  if (refusal.code === 14 || refusal.status === 0 || (refusal.code === null && refusal.status >= 500)) {
    return RETRY_INVITATION;
  }
  return null;
}

/** Следующий шаг к отказу, текст которого служба отдаёт одним на все причины. */
function nextStep(refusal: LaneRefusal, context?: RefusalContext): string | null {
  if (refusal.code === 16) return NEXT_STEP_UNAUTHENTICATED[context ?? "any"];
  if (refusal.code === 9) return NEXT_STEP_FAILED_PRECONDITION[context ?? "any"];
  return null;
}

/** Что добавить к тексту службы — только то, что нужно для действия. */
export function refusalHint(refusal: LaneRefusal, context?: RefusalContext): string | null {
  return retryHint(refusal) ?? nextStep(refusal, context);
}

export function LaneRefusalAlert({
  refusal,
  context,
  secondsLeft,
}: {
  refusal: LaneRefusal;
  /** Экран отказа — от него зависит названный шаг. */
  context?: RefusalContext;
  /** Секунды до повтора, отсчитанные экраном; `null` — срок истёк. Не задано — срок из ответа. */
  secondsLeft?: number | null;
}) {
  const inside = retryHint(refusal, secondsLeft);
  const step = nextStep(refusal, context);
  return (
    <>
      <Alert type="error" showIcon message={refusal.message} description={inside ?? undefined} />
      {step !== null && (
        <Typography.Paragraph data-testid="refusal-next-step" style={{ margin: "8px 0 0" }}>
          {step}
        </Typography.Paragraph>
      )}
    </>
  );
}
