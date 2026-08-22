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

// Плитка типа. Заливка ПЛОСКАЯ, а не градиентная: в целевом оформлении глубина
// делается тоном, и два насыщенных цвета рядом ставит только фирменный
// градиент, который в разметке страниц не участвует. Радиус 8 — тот же, что у
// пункта рейла модулей (44×42): это одна и та же форма «квадратная плитка с
// глифом», и держать её двумя радиусами не за чем. Цвета — токены; прежде
// здесь стоял отменённый бренд-синий, не менявшийся ни в одной теме.
const tileStyle: React.CSSProperties = {
  width: TILE,
  height: TILE,
  borderRadius: 8,
  flexShrink: 0,
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  fontSize: 19,
  color: "var(--kc-primary)",
  background: "var(--kc-primary-bg)",
  border: "1px solid var(--kc-border)",
};

// Надзаголовок — ряд телеметрии целевого оформления: мелкий, сильно разрежённый
// и цвета `--kc-cyan`. Чёрточки перед текстом (класс `.t-kicker`) здесь нет
// намеренно: она принадлежит надзаголовку СТРАНИЦЫ, где заменяет разделитель
// перед крупным заголовком, а тут слева уже стоит плитка — вторая чёрточка
// добавила бы третий элемент в строку, которая называет всего одно слово.
const eyebrowStyle: React.CSSProperties = {
  fontSize: 9,
  fontWeight: 720,
  letterSpacing: "0.14em",
  textTransform: "uppercase",
  color: "var(--kc-cyan)",
  marginBottom: 3,
  whiteSpace: "nowrap",
  overflow: "hidden",
  textOverflow: "ellipsis",
};

const titleStyle: React.CSSProperties = {
  fontSize: 16,
  fontWeight: 590,
  letterSpacing: "-0.02em",
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
