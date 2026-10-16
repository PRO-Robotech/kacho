// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useId, useRef, type KeyboardEvent } from "react";
import { Form, Input } from "antd";
import type { CodeMethod, SecondFactorPresentation } from "@shared/api/login-lane";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { CODE_METHODS } from "@shared/lib/step-up-methods";

// Предъявление второго фактора КОДОМ — ОДНО поле на четыре формы: вход, окно
// подтверждения действия (повышение уровня), снятие фактора и перечеканку
// запасных кодов (приёмка F8, Р9; экран «Безопасность» — приёмка F8-S7, Р5, Р6).
// Встаёт оно в трёх местах: `LoginPage`, `StepUpModal` и диалог кода экрана
// «Безопасность» (`CodeDialog`), который несёт обе формы экрана.
//
// Способ называет ЧЕЛОВЕК, а не длина кода: служба принимает `method` из формы
// и по длине не гадает (Ф12 Р4). Поэтому выбор способа — явный переключатель, и
// ровно он кладёт в тело `totp` либо `lookup_secret`. Разведи это поле по
// формам — и одна из них однажды отправит код без способа или с чужим.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ФОРМЫ РАЗЛИЧАЮТСЯ — ПАРАМЕТРОМ `variant`, А НЕ ВТОРЫМ КОМПОНЕНТОМ
//
// `sign-in` — экран входа. Консоль ещё не знает, что заведено у человека, и
// УМОЛЧАНИЯ НЕТ (условие C16): переключатель открывается без выбора, поле
// «Код» видно сразу, и пока человек способ не назвал, способа в теле нет вовсе
// — служба отвечает «способ не назван», и отметка встаёт у переключателя.
// Предвыбранный «код из приложения» отправлял бы запасной код способом `totp`
// у каждого, кто не заметил переключателя. Переключатель — строка сетки формы
// входа (`FormGrid`): имя слева, выбор справа.
//
// `confirm` — окно подтверждения и диалоги экрана «Безопасность». Перечень
// способов называет служба (`stepUpMethods`, SUA Р6), выбор по умолчанию
// задаёт форма-хозяйка (Р5: код из приложения, запасной — никогда, он
// расходуется), и поле кода появляется под выбранным способом, названное ЕГО
// именем: «Код из приложения» либо «Запасной код».
//
// Выбор умолчания — не параметр поля: его несёт `method` от формы-хозяйки.
//
// ПОЧЕМУ ПЕРЕКЛЮЧАТЕЛИ — `role="radio"`, А НЕ `<input type="radio">`. В
// `confirm` подпись поля кода совпадает с подписью его способа. У поля
// ввода-переключателя она была бы его `<label>`, и одно имя называло бы два
// поля ввода сразу — диктор и поиск по подписи не отличили бы поле кода от
// переключателя. Переключатель роли `radio` своё имя берёт из содержимого, а
// не из подписи поля; клавиатура — шаблон группы переключателей WAI-ARIA: в
// обходе группа — ОДНА остановка, стрелки переводят выбор, пробел выбирает.
// Вход берёт те же переключатели: второй реализации выбора у предмета нет.
//
// Правил кода здесь нет (Р2): шесть цифр у кода из приложения и десять знаков у
// запасного судит служба и называет поле отказом. Подсказка формата ввода
// (`inputMode`) — не правило, а клавиатура.
//
// ГЕОМЕТРИЯ ВВОДА — НЕ ЗДЕСЬ. Поле отдаёт строки формы (`Form.Item`), а имя
// слева и ввод справа им задаёт сетка той формы, в которую поле встало. Отказ
// у поля — общий `FieldError` (#1274, круг 1 ревью).

/** Подпись способа — и переключателя, и поля кода в `confirm`. Тип ключа исчерпывающий: способ без подписи роняет сборку. */
export const CODE_METHOD_LABEL: Record<CodeMethod, string> = {
  totp: "Код из приложения",
  lookup_secret: "Запасной код",
};

/** Пустое предъявление: способ НЕ выбран, кода нет. */
export const EMPTY_PRESENTATION: SecondFactorPresentation = { method: null, code: "" };

/** Подпись поля кода на входе: способ там ещё может быть не назван. */
const SIGN_IN_CODE_LABEL = "Код";

interface Props {
  /** Форма-хозяйка: вход (`sign-in`) либо подтверждение (`confirm`) — см. шапку. */
  variant: "sign-in" | "confirm";
  /** Имя группы переключателей. */
  groupLabel?: string;
  /** Способы в порядке показа. */
  methods?: readonly CodeMethod[];
  /** Выбранный способ; `null` — способ кода не выбран. */
  method: CodeMethod | null;
  onMethodChange: (method: CodeMethod) => void;
  code: string;
  onCodeChange: (code: string) => void;
  /** Текст отказа о поле кода — дословно из ответа службы; `null` — отказа нет. */
  codeError?: string | null;
  /** Текст отказа о способе — дословно из ответа службы; `null` — отказа нет. */
  methodError?: string | null;
  disabled?: boolean;
}

export function SecondFactorCodeField({
  variant,
  groupLabel = "Способ подтверждения",
  methods = CODE_METHODS,
  method,
  onMethodChange,
  code,
  onCodeChange,
  codeError = null,
  methodError = null,
  disabled = false,
}: Props) {
  const id = useId();
  const codeId = `${id}-code`;
  const groupId = `${id}-method`;
  const errorId = fieldErrorId(codeId);
  const methodErrorId = fieldErrorId(groupId);
  const radios = useRef<Array<HTMLDivElement | null>>([]);
  const chosen = method !== null && methods.includes(method) ? method : null;
  // В обходе группа — одна остановка: выбранный переключатель, а без выбора —
  // первый.
  const tabStop = chosen !== null ? methods.indexOf(chosen) : 0;

  const choose = (i: number) => {
    const m = methods[i];
    if (m === undefined || disabled) return;
    onMethodChange(m);
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

  const group = (
    <>
      <div
        id={groupId}
        role="radiogroup"
        aria-label={groupLabel}
        aria-disabled={disabled || undefined}
        aria-invalid={methodError ? true : undefined}
        aria-describedby={methodError ? methodErrorId : undefined}
        style={{ display: "flex", flexWrap: "wrap", gap: 16, marginBottom: variant === "confirm" ? 12 : 0 }}
      >
        {methods.map((m, i) => {
          const checked = chosen === m;
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
              {CODE_METHOD_LABEL[m]}
            </div>
          );
        })}
      </div>
      <FieldError id={methodErrorId} message={methodError ?? undefined} />
    </>
  );

  const codeLabel = variant === "sign-in" ? SIGN_IN_CODE_LABEL : chosen !== null ? CODE_METHOD_LABEL[chosen] : null;

  return (
    <>
      {variant === "sign-in" ? <Form.Item label={groupLabel}>{group}</Form.Item> : group}
      {codeLabel !== null && (
        <Form.Item label={codeLabel} htmlFor={codeId}>
          <Input
            id={codeId}
            disabled={disabled}
            value={code}
            onChange={(e) => onCodeChange(e.target.value)}
            autoComplete="one-time-code"
            inputMode={chosen === "totp" ? "numeric" : "text"}
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
