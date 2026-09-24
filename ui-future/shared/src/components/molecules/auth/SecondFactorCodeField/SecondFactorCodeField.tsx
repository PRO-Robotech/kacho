// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useId } from "react";
import { Form, Input, Radio } from "antd";
import type { CodeMethod, SecondFactorPresentation } from "@shared/api/login-lane";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { OWN_STEP_UP_METHODS } from "@shared/lib/step-up-methods";

// Предъявление второго фактора — ОДНО поле на четыре формы: вход, повышение
// уровня, снятие фактора и перечеканку запасных кодов (приёмка F8, Р9).
//
// Способ называет ЧЕЛОВЕК, а не длина кода: служба принимает `method` из формы
// и по длине не гадает (Ф12 Р4). Поэтому выбор способа — явный переключатель, и
// ровно он кладёт в тело `totp` либо `lookup_secret`. Разведи это поле по
// четырём формам — и одна из них однажды отправит код без способа или с чужим.
//
// УМОЛЧАНИЯ НЕТ (условие C16). Переключатель открывается без выбора, и пока
// человек способ не назвал, способа в теле нет вовсе: служба отвечает «способ
// не назван», и отметка встаёт у переключателя. Предвыбранный «код из
// приложения» отправлял бы запасной код способом `totp` у каждого, кто не
// заметил переключателя, — и служба засчитывала бы это неверным предъявлением.
//
// Правил кода здесь нет (Р2): шесть цифр у кода из приложения и десять знаков у
// запасного судит служба и называет поле отказом. Подсказка формата ввода
// (`inputMode`) — не правило, а клавиатура.
//
// ГЕОМЕТРИЯ — НЕ ЗДЕСЬ. Поле отдаёт две строки формы (`Form.Item`), а имя
// слева и ввод справа им задаёт сетка той формы, в которую поле встало
// (`FormGrid`): все четыре формы-хозяйки её несут. Своя раскладка «подпись над
// вводом» здесь была копией канона формы, а отказ у поля — копией `FieldError`
// (#1274, круг 1 ревью).

/** Подпись способа. Тип ключа исчерпывающий: способ из перечня без подписи роняет сборку. */
export const CODE_METHOD_LABEL: Record<CodeMethod, string> = {
  totp: "Код из приложения",
  lookup_secret: "Запасной код",
};

/** Пустое предъявление: способ НЕ выбран, кода нет. */
export const EMPTY_PRESENTATION: SecondFactorPresentation = { method: null, code: "" };

interface Props {
  value: SecondFactorPresentation;
  onChange: (next: SecondFactorPresentation) => void;
  /** Текст отказа о поле кода — дословно из ответа службы; `null` — отказа нет. */
  codeError?: string | null;
  /** Текст отказа о способе — дословно из ответа службы; `null` — отказа нет. */
  methodError?: string | null;
  disabled?: boolean;
}

export function SecondFactorCodeField({ value, onChange, codeError = null, methodError = null, disabled }: Props) {
  const id = useId();
  const codeId = `${id}-code`;
  const methodId = `${id}-method`;
  const errorId = fieldErrorId(codeId);
  const methodErrorId = fieldErrorId(methodId);
  return (
    <>
      <Form.Item label="Способ подтверждения">
        <Radio.Group
          id={methodId}
          aria-label="Способ подтверждения"
          aria-invalid={methodError ? true : undefined}
          aria-describedby={methodError ? methodErrorId : undefined}
          disabled={disabled}
          value={value.method ?? undefined}
          onChange={(e) => onChange({ method: e.target.value as CodeMethod, code: value.code })}
          options={OWN_STEP_UP_METHODS.map((m) => ({
            value: m,
            label: CODE_METHOD_LABEL[m],
          }))}
        />
        <FieldError id={methodErrorId} message={methodError ?? undefined} />
      </Form.Item>
      <Form.Item label="Код" htmlFor={codeId}>
        <Input
          id={codeId}
          disabled={disabled}
          value={value.code}
          onChange={(e) => onChange({ method: value.method, code: e.target.value })}
          autoComplete="one-time-code"
          inputMode={value.method === "totp" ? "numeric" : "text"}
          aria-invalid={codeError ? true : undefined}
          aria-describedby={codeError ? errorId : undefined}
          status={codeError ? "error" : undefined}
        />
        <FieldError id={errorId} message={codeError ?? undefined} />
      </Form.Item>
    </>
  );
}
