// LifecycleTag — класс исчезаемости репозитория (REG-1 F7): output-only enum.
// DURABLE — реестр каркаса survives-empty (явный CreateRepository / установленный
// overlay); EPHEMERAL — register-on-first-push, unregister-on-last-tag. Установка
// overlay AUTO-PROMOTE'ит EPHEMERAL→DURABLE. UNSPECIFIED / пусто → «—».

import { type FC } from "react";
import { Tag, Tooltip, Typography } from "antd";

// Метаданные отображения per enum-значение: метка + цвет + пояснение.
const LIFECYCLE_META: Record<string, { label: string; color: string; hint: string }> = {
  DURABLE: {
    label: "Постоянный",
    color: "geekblue",
    hint: "DURABLE — сохраняется пустым (survives-empty): каркас репозитория не исчезает после удаления последнего тега.",
  },
  EPHEMERAL: {
    label: "Эфемерный",
    color: "gold",
    hint: "EPHEMERAL — появляется при первом docker push и исчезает после удаления последнего тега (register-on-first-push).",
  },
};

/** Нормализует enum: REPOSITORY_LIFECYCLE_DURABLE → DURABLE. */
function normalize(value: unknown): string {
  const s = typeof value === "string" ? value : "";
  return s.startsWith("REPOSITORY_LIFECYCLE_") ? s.slice("REPOSITORY_LIFECYCLE_".length) : s;
}

/** lifecycleLabel — человекочитаемая метка класса (для колонки/detail/тестов). */
export function lifecycleLabel(value: unknown): string {
  return LIFECYCLE_META[normalize(value)]?.label ?? "—";
}

export const LifecycleTag: FC<{ value: unknown }> = ({ value }) => {
  const meta = LIFECYCLE_META[normalize(value)];
  if (!meta) {
    return <Typography.Text type="secondary">—</Typography.Text>;
  }
  return (
    <Tooltip title={meta.hint}>
      <Tag color={meta.color} style={{ margin: 0 }}>
        {meta.label}
      </Tag>
    </Tooltip>
  );
};
