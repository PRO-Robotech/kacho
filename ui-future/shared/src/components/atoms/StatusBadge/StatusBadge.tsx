// StatusBadge — плотный pill для статуса ресурса.
// Поддерживает оба naming convention: STATUS_* (1.0 flat) и STATE_* (legacy 0.x).
// KAC-246: theme-aware через CSS-vars (--status-<tone>-bg/-fg/-border),
// определённые в index.css для обеих тем (dark — приглушённый фон + яркий текст;
// light — светлый фон + насыщенный текст + чёткий border). Никакого хардкода
// Tailwind-цвета, который бы сломался в другой теме.

import type { CSSProperties } from "react";

type Tone = "ok" | "info" | "warn" | "muted" | "error" | "violet";

/**
 * Форма пилюли состояния — ОДНА на продукт, поэтому она объявлена здесь и
 * импортируется всеми, кто рисует состояние (значок статуса, признак открытости
 * площадки). Прежде форму задавал набор Tailwind-классов, и второй такой же
 * набор жил в соседнем атоме: правка одного до другого не доезжала.
 *
 * Числа — из целевого оформления: компактный отступ, радиус 6 (ряд «тег/код»),
 * кегль 11 и вес 560 — тон читается, но пилюля не спорит с именем ресурса
 * рядом. Граница берётся В ТОН заливки, поэтому здесь задан только её вид и
 * толщина; цвет приходит из набора тона.
 */
export const statusPillShape: CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  height: 20,
  padding: "0 7px",
  borderRadius: 6,
  borderWidth: 1,
  borderStyle: "solid",
  fontSize: 11,
  fontWeight: 560,
  lineHeight: 1,
  whiteSpace: "nowrap",
};

const TONE_STYLE: Record<Tone, CSSProperties> = {
  ok: {
    background: "var(--status-ok-bg)",
    color: "var(--status-ok-fg)",
    borderColor: "var(--status-ok-border)",
  },
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

/** Стиль пилюли целиком: форма + тон. Тон стоит ПОСЛЕ формы — цвет границы
 *  приходит из него и перекрыть его формой нельзя. */
export function statusPillStyle(tone: Tone): CSSProperties {
  return { ...statusPillShape, ...TONE_STYLE[tone] };
}

const TONE_BY_STATUS: Record<string, Tone> = {
  ACTIVE: "ok",
  READY: "ok",
  RUNNING: "ok",
  RESERVED: "ok",
  // AVAILABLE — здоровый статус тома (свободен, готов к attach). Пропускался:
  // IN_USE в таблице был, а AVAILABLE — нет, и он падал в fallback "muted",
  // то есть доступный том выглядел неактивным, как STOPPED/RELEASED.
  AVAILABLE: "ok",
  CREATING: "info",
  // MIGRATING — том переезжает на другой тип диска. Тон «в процессе», как у
  // CREATING/UPDATING: перенос длится и наблюдаем, а данные при его отказе
  // остаются на исходном типе, поэтому предупреждением он не является.
  // Незнакомое значение падает в "muted" — тем же тоном, что «остановлен» и
  // «освобождён», — и переезжающий том выглядел бы неактивным.
  MIGRATING: "info",
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
  return <span style={statusPillStyle(tone)}>{display.charAt(0) + display.slice(1).toLowerCase()}</span>;
}
