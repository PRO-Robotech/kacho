// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useId } from "react";
import { Input, Radio } from "antd";
import type { CodeMethod, SecondFactorPresentation } from "@shared/api/login-lane";
import { STEP_UP_METHODS } from "@shared/lib/step-up-methods";

// Предъявление второго фактора — ОДНО поле на четыре формы: вход, повышение
// уровня, снятие фактора и перечеканку запасных кодов (приёмка F8, Р9).
//
// Способ называет ЧЕЛОВЕК, а не длина кода: служба принимает `method` из формы
// и по длине не гадает (Ф12 Р4). Поэтому выбор способа — явный переключатель, и
// ровно он кладёт в тело `totp` либо `lookup_secret`. Разведи это поле по
// четырём формам — и одна из них однажды отправит код без способа или с чужим.
//
// Правил кода здесь нет (Р2): шесть цифр у кода из приложения и десять знаков у
// запасного судит служба и называет поле отказом. Подсказка формата ввода
// (`inputMode`) — не правило, а клавиатура.

/** Подпись способа. Тип ключа исчерпывающий: способ из перечня без подписи роняет сборку. */
export const CODE_METHOD_LABEL: Record<CodeMethod, string> = {
  totp: "Код из приложения",
  lookup_secret: "Запасной код",
};

export const EMPTY_PRESENTATION: SecondFactorPresentation = { method: "totp", code: "" };

interface Props {
  value: SecondFactorPresentation;
  onChange: (next: SecondFactorPresentation) => void;
  /** Текст отказа о поле кода — дословно из ответа службы; `null` — отказа нет. */
  codeError?: string | null;
  disabled?: boolean;
}

export function SecondFactorCodeField({ value, onChange, codeError = null, disabled }: Props) {
  const id = useId();
  const codeId = `${id}-code`;
  const errorId = `${id}-code-error`;
  return (
    <fieldset style={{ border: 0, margin: 0, padding: 0 }} disabled={disabled}>
      <legend style={{ marginBottom: 8 }}>Способ подтверждения</legend>
      <Radio.Group
        value={value.method}
        onChange={(e) => onChange({ method: e.target.value as CodeMethod, code: value.code })}
        options={STEP_UP_METHODS.map((m) => ({
          value: m,
          label: CODE_METHOD_LABEL[m],
        }))}
      />
      <div style={{ marginTop: 12 }}>
        <label htmlFor={codeId} style={{ display: "block", marginBottom: 4 }}>
          Код
        </label>
        <Input
          id={codeId}
          value={value.code}
          onChange={(e) => onChange({ method: value.method, code: e.target.value })}
          autoComplete="one-time-code"
          inputMode={value.method === "totp" ? "numeric" : "text"}
          aria-invalid={codeError ? true : undefined}
          aria-describedby={codeError ? errorId : undefined}
          status={codeError ? "error" : undefined}
        />
        {codeError && (
          <div id={errorId} style={{ color: "var(--kc-error)", marginTop: 4 }}>
            {codeError}
          </div>
        )}
      </div>
    </fieldset>
  );
}
