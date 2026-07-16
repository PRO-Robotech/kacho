// ContextBadge — ЕДИНЫЙ блок «[иконка-плитка] действие(eyebrow) / заголовок(+подзаголовок)».
//
// Единственный источник разметки/стилей этого блока. Используется ВЕЗДЕ, где он
// появляется, чтобы не было визуальных расхождений (= «прыжка») между контекстами:
//   • list-страница  (PanelHeader — слева, справа фильтры/CTA)
//   • detail зона-2   (DetailShell — рейл табов; eyebrow = действие активного таба)
//   • формы           (та же зона-2: «Создание»/«Редактирование» + тип)
//
// НЕ содержит контейнерных отступов/границ — их задаёт место использования
// (PanelHeader: justify-between + bottom-border; DetailShell: padding + bottom-border).
import { type ReactNode } from "react";

const TILE = 42;

const tileStyle: React.CSSProperties = {
  width: TILE,
  height: TILE,
  borderRadius: 12,
  flexShrink: 0,
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  fontSize: 19,
  color: "var(--kc-primary)",
  background: "linear-gradient(135deg, rgba(61,141,245,0.16), rgba(61,141,245,0.05))",
  border: "1px solid rgba(61,141,245,0.22)",
};

const eyebrowStyle: React.CSSProperties = {
  fontSize: 11,
  fontWeight: 600,
  letterSpacing: "0.06em",
  textTransform: "uppercase",
  color: "var(--kc-primary)",
  marginBottom: 2,
  whiteSpace: "nowrap",
  overflow: "hidden",
  textOverflow: "ellipsis",
};

const titleStyle: React.CSSProperties = {
  fontSize: 16,
  fontWeight: 600,
  color: "var(--kc-text)",
  lineHeight: 1.25,
  whiteSpace: "nowrap",
  overflow: "hidden",
  textOverflow: "ellipsis",
};

export interface ContextBadgeProps {
  /** Иконка ресурса (оборачивается в плитку 42×42). */
  icon?: ReactNode;
  /** Действие/секция (caps): «Список»/«Обзор»/«Операции»/«Создание»/… */
  eyebrow?: string;
  /** Заголовок (тип/название предмета). */
  title: ReactNode;
  /** Опциональный подзаголовок (только standalone-форма). */
  subtitle?: string;
}

export function ContextBadge({ icon, eyebrow, title, subtitle }: ContextBadgeProps) {
  // С подзаголовком (3 строки) — плитку по верху; иначе центрируем плитку и
  // текст (текст не вылазит за верх/низ плитки).
  const align = subtitle ? "flex-start" : "center";
  return (
    <div style={{ display: "flex", gap: 12, minWidth: 0, alignItems: align }}>
      {icon && <div style={tileStyle}>{icon}</div>}
      <div style={{ minWidth: 0 }}>
        {eyebrow && <div style={eyebrowStyle}>{eyebrow}</div>}
        <div style={titleStyle}>{title}</div>
        {subtitle && (
          <div
            style={{
              fontSize: 13,
              lineHeight: 1.5,
              marginTop: 4,
              color: "var(--kc-text-secondary)",
            }}
          >
            {subtitle}
          </div>
        )}
      </div>
    </div>
  );
}
