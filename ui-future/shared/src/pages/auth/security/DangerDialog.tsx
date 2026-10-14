// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useRef, type ReactNode } from "react";
import { Button, Modal, Typography } from "antd";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";

// Опасное действие экрана «Безопасность» — диалог, называющий последствие
// словами (приёмка F8-S7, Р6).
//
//   • кнопка подтверждения — опасного вида и называет действие («Отключить»,
//     «Удалить», «Выпустить», «Выйти»);
//   • фокус при открытии — на «Отмена»: Enter, нажатый по привычке, ничего не
//     выпускает; отмена ничего не выпускает тоже (F8-60);
//   • диалог держит фокус внутри, закрывается по Esc и возвращает фокус на
//     вызвавший элемент (Р9) — это поведение окна antd, а не своё;
//   • диалог уходит сразу, без анимации: глагол выпускается после закрытия, и
//     окно подтверждения действия не встаёт рядом с ещё видимым диалогом.
//
// Подтверждение вторым фактором, когда действие его требует, — содержимое
// диалога (`children`): поле кода в самом диалоге (Р6).

interface Props {
  open: boolean;
  title: string;
  /** Последствие — фразами предмета, каждая отдельным абзацем. */
  consequences: readonly string[];
  /** Глагол кнопки подтверждения. */
  confirmText: string;
  busy?: boolean;
  /** Подтверждение недоступно (срок частоты, не выбран способ). */
  confirmDisabled?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  children?: ReactNode;
}

export function DangerDialog({
  open,
  title,
  consequences,
  confirmText,
  busy = false,
  confirmDisabled = false,
  onConfirm,
  onCancel,
  children,
}: Props) {
  const cancelRef = useRef<HTMLButtonElement>(null);
  return (
    <Modal
      open={open}
      title={title}
      onCancel={onCancel}
      mask={{ closable: false }}
      // Без анимации ухода: диалог закрывается ДО глагола, и окно подтверждения,
      // если служба его спросит, встаёт одно, а не рядом с уходящим диалогом.
      transitionName=""
      maskTransitionName=""
      afterOpenChange={(isOpen) => {
        if (isOpen) cancelRef.current?.focus();
      }}
      footer={[
        <Button key="cancel" ref={cancelRef} onClick={onCancel} disabled={busy}>
          Отмена
        </Button>,
        <Button key="ok" type="primary" danger loading={busy} disabled={confirmDisabled} onClick={onConfirm}>
          {confirmText}
        </Button>,
      ]}
    >
      {consequences.map((c) => (
        <Typography.Paragraph key={c}>{c}</Typography.Paragraph>
      ))}
      {children !== undefined && (
        <FormGrid label={title} onSubmit={() => (confirmDisabled || busy ? undefined : onConfirm())}>
          {children}
          <button type="submit" hidden aria-hidden tabIndex={-1} />
        </FormGrid>
      )}
    </Modal>
  );
}
