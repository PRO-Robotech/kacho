// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useId, useRef, type KeyboardEvent } from "react";
import { Form, Input } from "antd";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import type { SecondFactorMethod } from "@shared/lib/step-up-methods";

// Выбор способа КОДА и поле кода — для окна подтверждения действия и диалогов
// опасных действий экрана «Безопасность» (приёмка F8-S7, Р5, Р6).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ОТЛИЧАЕТСЯ ОТ `SecondFactorCodeField`
//
// Поле входа (`SecondFactorCodeField`) открывается БЕЗ выбора (условие C16
// приёмки F8): на экране входа консоль не знает, что заведено у человека. Здесь
// знает — перечень способов называет служба (`stepUpMethods`, SUA Р6), и выбор
// по умолчанию задаёт приёмка (Р5): код из приложения, а запасной код — никогда
// (он расходуется). Выбранный способ несёт ПОЛЕ своим именем: «Код из
// приложения» либо «Запасной код», — и это имя у человека одно: и подпись
// переключателя, и подпись поля.
//
// ПОЧЕМУ ПЕРЕКЛЮЧАТЕЛИ — `role="radio"`, А НЕ `<input type="radio">`. Подпись
// поля кода совпадает с подписью его способа. У поля ввода-переключателя она
// была бы его `<label>`, и одно имя называло бы два поля ввода сразу — диктор
// и поиск по подписи не отличили бы поле кода от переключателя. Переключатель
// роли `radio` своё имя берёт из содержимого, а не из подписи поля; клавиатура
// — шаблон группы переключателей WAI-ARIA: в обходе группа — ОДНА остановка,
// стрелки переводят выбор, пробел выбирает.

/** Подпись способа кода — и переключателя, и поля. */
export const FACTOR_CODE_LABEL: Record<SecondFactorMethod, string> = {
  totp: "Код из приложения",
  lookup_secret: "Запасной код",
};

interface Props {
  /** Имя группы переключателей. */
  groupLabel: string;
  /** Способы в порядке показа. */
  methods: readonly SecondFactorMethod[];
  /** Выбранный способ; `null` — кода не выбрано (выбран ключ либо ничего). */
  value: SecondFactorMethod | null;
  onChange: (method: SecondFactorMethod) => void;
  code: string;
  onCodeChange: (code: string) => void;
  /** Текст отказа о поле кода — дословно из ответа службы. */
  codeError?: string | null;
  disabled?: boolean;
}

export function FactorCodeChoice({
  groupLabel,
  methods,
  value,
  onChange,
  code,
  onCodeChange,
  codeError = null,
  disabled = false,
}: Props) {
  const id = useId();
  const codeId = `${id}-code`;
  const errorId = fieldErrorId(codeId);
  const radios = useRef<Array<HTMLDivElement | null>>([]);
  // В обходе группа — одна остановка: выбранный переключатель, а без выбора —
  // первый.
  const tabStop = value !== null && methods.includes(value) ? methods.indexOf(value) : 0;

  const choose = (i: number) => {
    const m = methods[i];
    if (m === undefined || disabled) return;
    onChange(m);
    radios.current[i]?.focus();
  };

  const onKey = (i: number, e: KeyboardEvent<HTMLDivElement>) => {
    const n = methods.length;
    if (e.key === "ArrowDown" || e.key === "ArrowRight") {
      e.preventDefault();
      choose((i + 1) % n);
    } else if (e.key === "ArrowUp" || e.key === "ArrowLeft") {
      e.preventDefault();
      choose((i - 1 + n) % n);
    } else if (e.key === " " || e.key === "Enter") {
      e.preventDefault();
      choose(i);
    }
  };

  return (
    <>
      <div
        role="radiogroup"
        aria-label={groupLabel}
        aria-disabled={disabled || undefined}
        style={{ display: "flex", flexWrap: "wrap", gap: 16, marginBottom: 12 }}
      >
        {methods.map((m, i) => {
          const checked = value === m;
          return (
            <div
              key={m}
              ref={(el) => {
                radios.current[i] = el;
              }}
              role="radio"
              aria-checked={checked}
              aria-disabled={disabled || undefined}
              tabIndex={i === tabStop && !disabled ? 0 : -1}
              onClick={() => choose(i)}
              onKeyDown={(e) => onKey(i, e)}
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 8,
                cursor: disabled ? "not-allowed" : "pointer",
                opacity: disabled ? 0.5 : 1,
              }}
            >
              <span
                aria-hidden
                style={{
                  width: 14,
                  height: 14,
                  borderRadius: "50%",
                  border: "1px solid var(--kc-border-strong, var(--kc-border))",
                  boxShadow: checked ? "inset 0 0 0 4px var(--kc-cyan)" : undefined,
                  flexShrink: 0,
                }}
              />
              {FACTOR_CODE_LABEL[m]}
            </div>
          );
        })}
      </div>
      {value !== null && methods.includes(value) && (
        <Form.Item label={FACTOR_CODE_LABEL[value]} htmlFor={codeId}>
          <Input
            id={codeId}
            disabled={disabled}
            value={code}
            onChange={(e) => onCodeChange(e.target.value)}
            autoComplete="one-time-code"
            inputMode={value === "totp" ? "numeric" : "text"}
            aria-invalid={codeError ? true : undefined}
            aria-describedby={codeError ? errorId : undefined}
            status={codeError ? "error" : undefined}
          />
          <FieldError id={errorId} message={codeError ?? undefined} />
        </Form.Item>
      )}
    </>
  );
}
