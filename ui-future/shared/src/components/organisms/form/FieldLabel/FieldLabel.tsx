// src/components/form/FieldLabel.tsx
// FieldLabel — единый label для Form.Item: текст + опц. info-tooltip справа.
// Звёздочку required рисует не этот компонент, а `requiredMarkAfterLabel`
// (`@shared/lib/required-mark`), провязанный в `ThemeProvider` — оболочку, через
// которую проходит каждая форма арендатора. Она ставит звёздочку СПРАВА от
// подписи по решению владельца (#562).
// Заменяет 3 разрозненные реализации labelWithInfo (generic/NIC/Subnet).
import { Space, Tooltip } from "antd";
import { QuestionCircleOutlined } from "@ant-design/icons";

interface Props {
  text: React.ReactNode;
  /** Длинные/RFC/optional пояснения — сюда, НЕ в скобки label (CLAUDE.md §4.4). */
  info?: React.ReactNode;
}

export function FieldLabel({ text, info }: Props) {
  if (!info) return <>{text}</>;
  return (
    <Space size={4}>
      {text}
      <Tooltip title={info}>
        <QuestionCircleOutlined aria-label="field-info" style={{ color: "var(--kc-text-tertiary)" }} />
      </Tooltip>
    </Space>
  );
}
