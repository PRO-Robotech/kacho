// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { ReactNode } from "react";
import { Typography } from "antd";

// Рамка экранов церемонии: вход, регистрация, выход и страница неведомого
// адреса. Экраны стоят ВНЕ каркаса консоли: у человека без сессии нет ни
// проекта, ни разделов, и рейл с ними обещал бы то, чего он получить не может.

export function CeremonyScreen({
  title,
  children,
  footer,
}: {
  title: string;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <main
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 16,
        background: "var(--kc-page)",
        boxSizing: "border-box",
      }}
    >
      <div
        style={{
          width: "100%",
          maxWidth: 420,
          padding: "28px 28px 24px",
          background: "var(--kc-container)",
          border: "1px solid var(--kc-border)",
          borderRadius: 12,
          boxShadow: "var(--kc-shadow-md)",
          boxSizing: "border-box",
        }}
      >
        <Typography.Title level={1} style={{ margin: "0 0 20px", fontSize: 24 }}>
          {title}
        </Typography.Title>
        {children}
        {footer !== undefined && <div style={{ marginTop: 20 }}>{footer}</div>}
      </div>
    </main>
  );
}

/** Поле формы церемонии: подпись над вводом, отказ службы о поле — под ним. */
export function CeremonyField({
  id,
  label,
  error,
  children,
}: {
  id: string;
  label: string;
  error: string | null;
  children: ReactNode;
}) {
  return (
    <div style={{ marginBottom: 16 }}>
      <label htmlFor={id} style={{ display: "block", marginBottom: 4 }}>
        {label}
      </label>
      {children}
      {error && (
        <div id={`${id}-error`} style={{ color: "var(--kc-error)", marginTop: 4 }}>
          {error}
        </div>
      )}
    </div>
  );
}
