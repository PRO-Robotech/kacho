// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { ReactNode } from "react";
import { Typography } from "antd";
import { FORM_LABEL_WIDTH } from "@shared/components/organisms/form/FormGrid";

// Рамка экранов церемонии: вход, регистрация, выход и страница неведомого
// адреса. Экраны стоят ВНЕ каркаса консоли: у человека без сессии нет ни
// проекта, ни разделов, и рейл с ними обещал бы то, чего он получить не может.
//
// Формы внутри рамки — ОБЩАЯ сетка формы консоли (`FormGrid`: имя слева, ввод
// справа), а не своя «подпись над вводом» (#1274, круг 1 ревью). Поэтому ширина
// карточки выводится из ширины колонки подписи: колонка плюс поле ввода,
// в которое помещается адрес электронной почты, плюс поля карточки.

/** Ширина поля ввода, в которое адрес электронной почты помещается целиком. */
const INPUT_WIDTH = 300;
const CARD_PADDING = 28;

export function CeremonyScreen({
  title,
  children,
  footer,
}: {
  title: string;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <main
      style={{
        // Прокрутка у рамки своя и одна: документ консоли не прокручивается
        // (`body` — `overflow: hidden`), и карточка выше экрана обрезалась бы
        // снизу вместе с кнопкой отправки (#1274).
        height: "100vh",
        overflowY: "auto",
        display: "flex",
        padding: 16,
        background: "var(--kc-page)",
        boxSizing: "border-box",
      }}
    >
      <div
        style={{
          // По центру — полями, а не выравниванием рамки: выравнивание уводило
          // бы верх карточки, выше экрана, за край, до которого не прокрутить.
          margin: "auto",
          width: "100%",
          maxWidth: FORM_LABEL_WIDTH + INPUT_WIDTH + 2 * CARD_PADDING,
          padding: `${CARD_PADDING}px ${CARD_PADDING}px 24px`,
          background: "var(--kc-container)",
          border: "1px solid var(--kc-border)",
          borderRadius: 12,
          boxShadow: "var(--kc-shadow-md)",
          boxSizing: "border-box",
        }}
      >
        <Typography.Title level={1} style={{ margin: "0 0 20px", fontSize: 24 }}>
          {title}
        </Typography.Title>
        {children}
        {footer !== undefined && <div style={{ marginTop: 20 }}>{footer}</div>}
      </div>
    </main>
  );
}
