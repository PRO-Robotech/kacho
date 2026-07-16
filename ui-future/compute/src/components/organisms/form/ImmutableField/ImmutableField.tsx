// src/components/form/ImmutableField.tsx
// ImmutableField — неизменяемое/preset-поле как ЗАБЛОКИРОВАННЫЙ инпут (disabled
// AntD Input) с замком-suffix + tooltip-причиной. Инфра-UX best-practice: поле
// выглядит как обычный input формы, но disabled — видно ПОЧЕМУ нельзя править.
import { Input, Tooltip } from "antd";
import { LockOutlined } from "@ant-design/icons";

interface Props {
  value: React.ReactNode;
  /** Причина: "Неизменяемо после создания" (edit) / "Задано из контекста" (create). */
  reason: string;
}

export function ImmutableField({ value, reason }: Props) {
  const empty = value === "" || value === null || value === undefined;
  const lock = (
    <Tooltip title={reason}>
      <LockOutlined aria-label="immutable-lock" style={{ color: "var(--kc-text-tertiary)" }} />
    </Tooltip>
  );

  // Строка/число — реальный disabled-инпут (точный вид AntD).
  if (typeof value === "string" || typeof value === "number") {
    return (
      <Input
        disabled
        value={empty ? "" : String(value)}
        placeholder={empty ? "—" : undefined}
        suffix={lock}
        style={{ fontFamily: "ui-monospace, monospace" }}
      />
    );
  }

  // ReactNode (ссылка/тег) — disabled-input-стилизованная обёртка.
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 8,
        minHeight: 32,
        padding: "0 11px",
        border: "1px solid var(--kc-border)",
        borderRadius: 6,
        background: "var(--kc-container)",
        color: "var(--kc-text-secondary)",
        cursor: "not-allowed",
        fontFamily: "ui-monospace, monospace",
      }}
    >
      <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {empty ? "—" : value}
      </span>
      {lock}
    </div>
  );
}
