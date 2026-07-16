// TableToolbar — переиспользуемые элементы тулбара таблиц:
//   • поиск по имени/идентификатору (controlled);
//   • шестерёнка-конфигуратор видимости колонок (persist в localStorage).
//
// Используется во встроенных таблицах дочерних ресурсов (ResourceShell) и
// рассчитан на переиспользование на странице-списке (ResourceListPage).

import { useState } from "react";
import { Button, Checkbox, Dropdown, Input, Typography } from "antd";
import { SearchOutlined, SettingOutlined } from "@ant-design/icons";

export interface ToggleCol {
  key: string;
  label: string;
}

/** Видимость колонок с persist в localStorage по ключу. Возвращает множество
 *  СКРЫТЫХ ключей + toggler. */
export function useHiddenColumns(storageKey: string): [Set<string>, (key: string, hidden: boolean) => void] {
  const [hidden, setHidden] = useState<Set<string>>(() => {
    try {
      const raw = localStorage.getItem(storageKey);
      return new Set(raw ? (JSON.parse(raw) as string[]) : []);
    } catch {
      return new Set<string>();
    }
  });

  const toggle = (key: string, h: boolean) => {
    setHidden((prev) => {
      const next = new Set(prev);
      if (h) next.add(key);
      else next.delete(key);
      try {
        localStorage.setItem(storageKey, JSON.stringify([...next]));
      } catch {
        /* localStorage недоступен — игнорируем persist */
      }
      return next;
    });
  };

  return [hidden, toggle];
}

/** TableSearch — controlled поиск-инпут с иконкой. */
export function TableSearch({
  value,
  onChange,
  placeholder = "Поиск по имени или идентификатору",
  width = 260,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  width?: number;
}) {
  return (
    <Input
      allowClear
      prefix={<SearchOutlined style={{ color: "var(--ant-color-text-tertiary, #8c8c8c)" }} />}
      placeholder={placeholder}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      style={{ width }}
    />
  );
}

/** ColumnSettings — шестерёнка с чек-боксами видимости колонок. */
export function ColumnSettings({
  columns,
  hidden,
  onToggle,
}: {
  columns: ToggleCol[];
  hidden: Set<string>;
  onToggle: (key: string, hidden: boolean) => void;
}) {
  return (
    <Dropdown
      trigger={["click"]}
      placement="bottomRight"
      dropdownRender={() => (
        <div
          style={{
            padding: 12,
            minWidth: 180,
            background: "var(--ant-color-bg-elevated, #2d2e35)",
            border: "1px solid var(--ant-color-border-secondary, #383941)",
            borderRadius: 8,
            boxShadow: "0 6px 16px rgba(0,0,0,0.45)",
          }}
        >
          <Typography.Text
            type="secondary"
            style={{ fontSize: 11, textTransform: "uppercase", letterSpacing: 0.5, fontWeight: 600 }}
          >
            Колонки
          </Typography.Text>
          <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 10 }}>
            {columns.map((c) => (
              <Checkbox key={c.key} checked={!hidden.has(c.key)} onChange={(e) => onToggle(c.key, !e.target.checked)}>
                {c.label}
              </Checkbox>
            ))}
          </div>
        </div>
      )}
    >
      <Button icon={<SettingOutlined />} title="Настроить колонки" />
    </Dropdown>
  );
}
