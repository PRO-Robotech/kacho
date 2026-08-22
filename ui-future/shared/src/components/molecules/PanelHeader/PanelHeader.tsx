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
  /**
   * Шапка прижата к краям поверхности, а не стоит внутри её отступа.
   *
   * Разница не косметическая: разделительная линия под шапкой обязана идти от
   * края до края поверхности — так же, как её ведут границы строк таблицы под
   * ней. Внутри отступа линия обрывается, не доходя до границы, и шапка
   * читается как ещё одна вложенная карточка, а не как крышка панели.
   *
   * Прижатую шапку заводит тот, кто снял отступ у самой поверхности; там, где
   * поверхность отступ держит (формы, тела вкладок), значение остаётся ложным —
   * иначе к её отступу прибавился бы второй.
   */
  flush?: boolean;
}

export function PanelHeader({ icon, eyebrow, title, subtitle, right, flush = false }: Props) {
  // С subtitle (3 строки) — плитку по верху; иначе центрируем. KAC-246.
  const align = subtitle ? "flex-start" : "center";
  return (
    <div
      style={{
        display: "flex",
        justifyContent: "space-between",
        gap: 16,
        flexWrap: "wrap",
        alignItems: align,
        // Высота шапки поверхности — 54: та же, что у шапки страницы, поэтому
        // заголовок панели и заголовок экрана читаются одним рядом.
        minHeight: 54,
        // Прижатая шапка держит отступ сама (0 18px) и ничего не отделяет
        // снизу — тело начинается сразу за линией. Шапка внутри отступа
        // поверхности отбивает тело сама, иначе содержимое липнет к линии.
        // 6 + плитка 42 + 6 = ровно 54: отбивка подобрана под высоту шапки,
        // а не назначена отдельно, иначе два числа разъедутся при правке.
        padding: flush ? "6px 18px" : "0 0 14px",
        marginBottom: flush ? 0 : 18,
        // Линия под шапкой — граница ПОВЕРХНОСТИ (--kc-border), а не внутренний
        // разделитель: она отделяет крышку от тела, а не колонку от колонки.
        borderBottom: "1px solid var(--kc-border)",
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
