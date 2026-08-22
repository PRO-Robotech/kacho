// src/components/form/ImmutableField.tsx
// ImmutableField — неизменяемое/preset-поле как ЗАБЛОКИРОВАННЫЙ инпут (disabled
// AntD Input) с замком-suffix + tooltip-причиной. Инфра-UX best-practice: поле
// выглядит как обычный input формы, но disabled — видно ПОЧЕМУ нельзя править.
import { Input, Tooltip } from "antd";
import { LockOutlined } from "@ant-design/icons";
import { MONO_FONT } from "@shared/components/organisms/form/editor-surface";

interface Props {
  value: React.ReactNode;
  /** Причина: "Неизменяемо после создания" (edit) / "Задано из контекста" (create). */
  reason: string;
}

export function ImmutableField({ value, reason }: Props) {
  const empty = value === "" || value === null || value === undefined;
  const lock = (
    <Tooltip title={reason}>
      <LockOutlined aria-label="неизменяемое поле" style={{ color: "var(--kc-text-tertiary)" }} />
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
        style={{ fontFamily: MONO_FONT, fontSize: 11, fontWeight: 520 }}
      />
    );
  }

  // ReactNode (ссылка/тег) — обёртка ровно той же формы, что и поле ввода
  // рядом: та же высота, тот же радиус и тот же фон. Отличие формы читалось бы
  // как «другой вид поля», хотя отличие только в том, что его нельзя править.
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 8,
        minHeight: 38,
        padding: "0 11px",
        border: "1px solid var(--kc-border)",
        borderRadius: 8,
        background: "var(--kc-field)",
        color: "var(--kc-text-secondary)",
        cursor: "not-allowed",
        fontFamily: MONO_FONT,
        fontSize: 11,
        fontWeight: 520,
      }}
    >
      <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {empty ? "—" : value}
      </span>
      {lock}
    </div>
  );
}
