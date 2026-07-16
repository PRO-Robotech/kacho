// StatusBadge — плотный pill для статуса ресурса.
// Поддерживает оба naming convention: STATUS_* (1.0 flat) и STATE_* (legacy 0.x).
// KAC-246: theme-aware через CSS-vars (--status-<tone>-bg/-fg/-border),
// определённые в index.css для обеих тем (dark — приглушённый фон + яркий текст;
// light — светлый фон + насыщенный текст + чёткий border). Никакого хардкода
// Tailwind-цвета, который бы сломался в другой теме.

import type { CSSProperties } from "react";

type Tone = "ok" | "info" | "warn" | "muted" | "error" | "violet";

const TONE_STYLE: Record<Tone, CSSProperties> = {
  ok: { background: "var(--status-ok-bg)", color: "var(--status-ok-fg)", borderColor: "var(--status-ok-border)" },
  info: {
    background: "var(--status-info-bg)",
    color: "var(--status-info-fg)",
    borderColor: "var(--status-info-border)",
  },
  warn: {
    background: "var(--status-warn-bg)",
    color: "var(--status-warn-fg)",
    borderColor: "var(--status-warn-border)",
  },
  muted: {
    background: "var(--status-muted-bg)",
    color: "var(--status-muted-fg)",
    borderColor: "var(--status-muted-border)",
  },
  error: {
    background: "var(--status-error-bg)",
    color: "var(--status-error-fg)",
    borderColor: "var(--status-error-border)",
  },
  violet: {
    background: "var(--status-violet-bg)",
    color: "var(--status-violet-fg)",
    borderColor: "var(--status-violet-border)",
  },
};

const TONE_BY_STATUS: Record<string, Tone> = {
  ACTIVE: "ok",
  READY: "ok",
  RUNNING: "ok",
  RESERVED: "ok",
  CREATING: "info",
  PROVISIONING: "info",
  STARTING: "info",
  ATTACHING: "info",
  UPDATING: "info",
  STOPPING: "warn",
  DETACHING: "warn",
  DELETING: "warn",
  STOPPED: "muted",
  RELEASED: "muted",
  ERROR: "error",
  IN_USE: "violet",
};

/** Нормализует label: STATUS_RUNNING → RUNNING, STATE_RUNNING → RUNNING, RUNNING → RUNNING. */
function displayLabel(raw: string): string {
  if (raw.startsWith("STATUS_")) return raw.slice(7);
  if (raw.startsWith("STATE_")) return raw.slice(6);
  return raw;
}

export function StatusBadge({ state }: { state?: string }) {
  if (!state) {
    return <span className="text-xs text-muted-foreground">—</span>;
  }
  const display = displayLabel(state);
  const tone = TONE_BY_STATUS[display] ?? "muted";
  return (
    <span
      className="inline-flex items-center rounded px-1.5 h-[20px] text-[11px] font-medium leading-none border"
      style={TONE_STYLE[tone]}
    >
      {display.charAt(0) + display.slice(1).toLowerCase()}
    </span>
  );
}
