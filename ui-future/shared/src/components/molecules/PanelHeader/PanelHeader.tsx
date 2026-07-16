// PanelHeader — ЕДИНАЯ «шапка» секции для форм и табов detail-страниц:
//   [иконка-плитка] [eyebrow-caps?] [title] [subtitle?]            [actions?]
//   ───────────────────────────────────────────────────────────────────────
// Унифицирует вид FormShell (форма: eyebrow=Создание/Редактирование + subtitle)
// и SectionHeader (табы Обзор/JSON/Связанные/…: icon из контекста + title +
// actions). Линия снизу + фикс-высота → заголовки/линии на одном уровне.
//
// DetailHeaderContext: ResourceShell прокидывает иконку ресурса вниз, и все
// SectionHeader внутри detail-страницы получают её автоматически (без правки
// каждого call-site). Вне detail (нет провайдера) — иконки нет, graceful.
import { createContext, useContext, type ReactNode } from "react";
import { Space } from "antd";
import { ContextBadge } from "@shared/components/atoms/ContextBadge";

interface DetailHeaderCtx {
  icon?: ReactNode;
}

const DetailHeaderContext = createContext<DetailHeaderCtx | null>(null);
export const DetailHeaderProvider = DetailHeaderContext.Provider;
export function useDetailHeaderIcon(): ReactNode | undefined {
  return useContext(DetailHeaderContext)?.icon;
}

interface Props {
  /** Иконка ресурса (оборачивается в плитку). */
  icon?: ReactNode;
  /** Мелкая caps-надпись над заголовком (форма: «Создание»/«Редактирование»). */
  eyebrow?: string;
  title: ReactNode;
  subtitle?: string;
  /** Блок действий справа (кнопки, поиск, счётчик). */
  right?: ReactNode;
}

export function PanelHeader({ icon, eyebrow, title, subtitle, right }: Props) {
  // С subtitle (3 строки) — плитку по верху; иначе центрируем. KAC-246.
  const align = subtitle ? "flex-start" : "center";
  return (
    <div
      style={{
        display: "flex",
        justifyContent: "space-between",
        gap: 12,
        flexWrap: "wrap",
        alignItems: align,
        minHeight: 42,
        paddingBottom: 14,
        marginBottom: 18,
        borderBottom: "1px solid var(--kc-border-secondary)",
      }}
    >
      {/* Единый блок — тот же ContextBadge, что и в detail зоне-2 (нет расхождений). */}
      <div style={{ minWidth: 0, flex: 1 }}>
        <ContextBadge icon={icon} eyebrow={eyebrow} title={title} subtitle={subtitle} />
      </div>
      {right && (
        <Space size={8} wrap style={{ alignItems: "center" }}>
          {right}
        </Space>
      )}
    </div>
  );
}
