// src/components/form/FormFooter.tsx
// FormFooter — единый футер Create/Edit форм: primary DopplerButton + Cancel.
// pending → pulsing + защита от double-submit. sticky=true делает футер липким
// (для длинных форм — действия всегда видны). Фон elevated (> container тела),
// верхняя граница border-secondary — футер визуально отделяется от тела.
// Theme-aware (--kc-*): чисто и в DARK, и в LIGHT.
import { Button } from "antd";
import { DopplerButton } from "@shared/components/molecules/DopplerButton";

interface Props {
  submitLabel: string;
  submitting: boolean;
  onSubmit: () => void;
  onCancel: () => void;
  sticky?: boolean;
  /** Danger-вариант submit-кнопки (для delete-flow). По умолчанию primary. */
  danger?: boolean;
  /** Блокировка submit (например requireNameConfirm не пройден). */
  submitDisabled?: boolean;
}

export function FormFooter({ submitLabel, submitting, onSubmit, onCancel, sticky, danger, submitDisabled }: Props) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        borderTop: "1px solid var(--kc-border-secondary)",
        // Симметричный вертикальный padding — кнопки по центру (был 14/2, «висели»
        // у низа). Боковые 0 — кнопки выровнены по левому краю полей.
        paddingTop: 16,
        // paddingBottom 0 — нижний отступ под кнопками даёт карточка/панель
        // (FormShell padding 20 / main pane 24); иначе под кнопками копится
        // ~36px и выглядит странно. В sticky-band ниже возвращаем нижний отступ.
        paddingBottom: 0,
        paddingLeft: 0,
        paddingRight: 0,
        marginTop: 10,
        ...(sticky
          ? {
              // Sticky-band (create-страницы) — bleed к краям FormShell-карточки
              // (padding 20px 22px), чтобы полоса шла на всю ширину карточки, а не
              // вставкой; кнопки re-inset на 22 → выровнены с полями.
              background: "var(--kc-elevated)",
              position: "sticky",
              bottom: 0,
              zIndex: 1,
              marginLeft: -22,
              marginRight: -22,
              marginBottom: -20,
              paddingLeft: 22,
              paddingRight: 22,
              paddingBottom: 16,
              borderBottomLeftRadius: 12,
              borderBottomRightRadius: 12,
            }
          : null),
      }}
    >
      <DopplerButton type="primary" danger={danger} onClick={onSubmit} pulsing={submitting} disabled={submitDisabled}>
        {submitLabel}
      </DopplerButton>
      <Button onClick={onCancel} disabled={submitting}>
        Отменить
      </Button>
    </div>
  );
}
