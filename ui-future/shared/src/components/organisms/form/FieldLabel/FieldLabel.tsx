// src/components/form/FieldLabel.tsx
// FieldLabel — единый label для Form.Item: текст + опц. info-tooltip справа.
// Звёздочку required рисует не этот компонент, а библиотека формы. Настройка,
// ставившая её СПРАВА по решению владельца, жила в недостижимом дереве маршрутов
// vpc и снята вместе с ним (#556); на живых поверхностях её не задаёт никто, и
// звёздочка идёт слева — открытая находка #562.
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
