// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { render, screen } from "@testing-library/react";
import { LaneRefusal } from "@shared/api/login-lane";
import {
  LaneRefusalAlert,
  RETRY_INVITATION,
  refusalHint,
  retryNotice,
  type RefusalContext,
} from "./LaneRefusalAlert";

// Подсказка следующего шага к отказу полосы (#2953, вид «отказ не
// восстанавливает шаг»).
//
// Отказы `16` (UNAUTHENTICATED) и `9` (FAILED_PRECONDITION) служба отдаёт ОДНИМ
// текстом на все причины: «пароль неверен» и «адреса нет» у входа, «адрес
// занят» и «потолок темпа» у регистрации. Экран показывает этот текст дословно и
// сверх него называет, ЧТО ДЕЛАТЬ ДАЛЬШЕ, — одним текстом на код, не зависящим
// ни от чего, кроме кода и экрана: подсказка, различающая причины, стала бы
// оракулом чужой учётной записи.

const of = (status: number, code: number | null, message: string, retryAfterSeconds: number | null = null) =>
  new LaneRefusal(status, code, message, null, null, retryAfterSeconds);

const CONTEXTS: Array<RefusalContext | undefined> = [undefined, "sign-in", "registration", "recovery"];

/** Слова, которыми подсказка назвала бы класс отказа, — их в ней быть не должно. */
const CLASS_WORDS = /неверн|не найден|не заведён|нет такого|заблокир|занят|уже существует|лимит|потол/i;

describe("подсказка следующего шага к отказу полосы", () => {
  it.each(CONTEXTS)("#2953 · отказ 16 получает подсказку шага на экране «%s»", (context) => {
    const hint = refusalHint(of(401, 16, "authentication failed"), context);
    expect(hint).not.toBeNull();
    expect(hint).not.toMatch(CLASS_WORDS);
  });

  it.each(CONTEXTS)("#2953 · отказ 9 получает подсказку шага на экране «%s»", (context) => {
    const hint = refusalHint(of(400, 9, "registration refused"), context);
    expect(hint).not.toBeNull();
    expect(hint).not.toMatch(CLASS_WORDS);
  });

  it("#2953 · подсказка входа называет путь восстановления, регистрации — вход и восстановление", () => {
    expect(refusalHint(of(401, 16, "authentication failed"), "sign-in")).toContain("Не получается войти?");
    const registration = refusalHint(of(400, 9, "registration refused"), "registration") ?? "";
    expect(registration).toMatch(/войдите/);
    expect(registration).toMatch(/восстановите доступ/);
    expect(refusalHint(of(401, 16, "authentication failed"), "recovery")).toMatch(/код/);
  });

  it("#2953 · подсказка — функция ТОЛЬКО кода и экрана: текст отказа её не меняет", () => {
    // Неразличимость: два разных текста под одним кодом дают одну подсказку.
    for (const context of CONTEXTS) {
      expect(refusalHint(of(401, 16, "authentication failed"), context)).toBe(
        refusalHint(of(401, 16, "что угодно другое"), context),
      );
      expect(refusalHint(of(400, 9, "registration refused"), context)).toBe(
        refusalHint(of(400, 9, "иной отказ условия"), context),
      );
    }
  });

  it("положительные близнецы: прежние подсказки целы, отказ о поле подсказки не получает", () => {
    expect(refusalHint(of(429, 8, "too many attempts; try again later", 30))).toBe(retryNotice(30));
    expect(refusalHint(of(503, 14, "request not performed; try again later"))).toBe(RETRY_INVITATION);
    expect(refusalHint(of(0, null, "Запрос не дошёл до службы: нет соединения"))).toBe(RETRY_INVITATION);
    expect(refusalHint(of(400, 3, "Illegal argument email: required"))).toBeNull();
    expect(refusalHint(of(403, 7, "form token rejected"))).toBeNull();
  });

  it("#2953 · текст отказа в окне отказа — дословно, подсказка шага — рядом, а не в нём", () => {
    render(<LaneRefusalAlert refusal={of(401, 16, "authentication failed")} context="sign-in" />);
    expect(screen.getByRole("alert").textContent).toBe("authentication failed");
    expect(screen.getByText(refusalHint(of(401, 16, "x"), "sign-in")!)).toBeInTheDocument();
  });

  it("срок до повтора, отсчитанный экраном, замещает срок из ответа", () => {
    render(<LaneRefusalAlert refusal={of(429, 8, "too many attempts; try again later", 30)} secondsLeft={12} />);
    expect(screen.getByRole("alert")).toHaveTextContent(retryNotice(12));
    expect(screen.getByRole("alert")).not.toHaveTextContent(retryNotice(30));
  });
});
