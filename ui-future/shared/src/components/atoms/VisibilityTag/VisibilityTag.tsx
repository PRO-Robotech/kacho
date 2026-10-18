// VisibilityTag — видимость репозитория (его поштучная публикация) либо
// Registry.default_repository_visibility (REG-1 F5). PRIVATE — доступ по правам;
// PUBLIC — anonymous docker pull, перевод поля в PUBLIC требует прав
// администратора реестра. Чтение без входа даёт и выдача роли
// `registry.public.read` классу на реестр (REG-PUB-1); тег её не показывает:
// признака публичности у реестра нет. Пусто → «—».

import { type FC } from "react";
import { Tag, Tooltip, Typography } from "antd";

const VISIBILITY_META: Record<string, { label: string; color: string; hint: string }> = {
  PRIVATE: {
    label: "Приватный",
    color: "default",
    hint: "PRIVATE — репозиторий сам не опубликован: доступ по правам, в том числе по роли публичного чтения, выданной на реестр.",
  },
  PUBLIC: {
    label: "Публичный",
    color: "gold",
    hint: "PUBLIC — anonymous docker pull без аутентификации. Переключение на PUBLIC требует прав администратора реестра.",
  },
};

/** visibilityLabel — человекочитаемая метка видимости (для колонки/detail/тестов). */
export function visibilityLabel(value: unknown): string {
  return VISIBILITY_META[typeof value === "string" ? value : ""]?.label ?? "—";
}

export const VisibilityTag: FC<{ value: unknown }> = ({ value }) => {
  const meta = VISIBILITY_META[typeof value === "string" ? value : ""];
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
