// src/components/form/FieldLabel.tsx
// FieldLabel — единый label для Form.Item: текст + опц. info-tooltip справа.
// Звёздочку required рисует ConfigProvider.requiredMark (App.tsx §4.3), НЕ здесь.
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
