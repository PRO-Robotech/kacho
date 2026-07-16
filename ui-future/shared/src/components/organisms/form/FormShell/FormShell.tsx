// src/components/form/FormShell.tsx
// FormShell — единый презентабельный «панель»-контейнер Create/Edit форм:
//   • шапка-band: градиентная иконка-плитка ресурса + caps-verb (Создание/
//     Редактирование) + заголовок (singular) + подзаголовок — по образцу
//     DeleteDialog и welcome-страницы «первый ресурс»;
//   • подложка-карточка (elevated + border + radius + shadow) поверх тёмного
//     фона (modal body / page = --kc-page), на которой лежит тело формы;
//   • единая ширина FORM_WIDTH для ВСЕХ ресурсов (modal == page == custom).
// Рендерится и generic-телом (ResourceFormBody), и кастом-формами
// (InlineSubnet/SG/NIC/AddressPool) → визуальный паритет всех форм.
// Theme-aware (--kc-*): чисто в DARK и LIGHT.
import { createContext, useContext, type ReactNode } from "react";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { PanelHeader, useDetailHeaderIcon } from "@shared/components/molecules/PanelHeader";

/** Единый стандарт ширины формы (modal width / page maxWidth / card maxWidth). */
export const FORM_WIDTH = 820;

// FormBareContext — форма внутри модалки: сама модалка уже даёт поверхность и
// рамку, поэтому FormShell рисует шапку (иконка+действие+тип) + поля БЕЗ своей
// kc-surface-карточки (иначе двойной лайаут: карточка внутри карточки-модалки).
const FormBareContext = createContext(false);

/** Оборачивает форму в модалке — FormShell рендерит шапку без kc-surface-подложки. */
export function FormBareProvider({ children }: { children: ReactNode }) {
  return <FormBareContext.Provider value={true}>{children}</FormBareContext.Provider>;
}

interface Props {
  specId: string;
  mode: "create" | "edit";
  singular: string;
  /** Override заголовка (по умолчанию — singular ресурса). */
  title?: string;
  /** Override подзаголовка (по умолчанию — generic-подсказка по mode). */
  subtitle?: string;
  children: React.ReactNode;
}

export function FormShell({ specId, mode, singular, title, subtitle, children }: Props) {
  const verb = mode === "create" ? "Создание" : "Редактирование";
  const heading = title ?? singular;
  // Подзаголовок-подсказка убран (лишний) — шапка формы = иконка + действие +
  // название, идентична шапке таба. subtitle оставлен как опц. override.

  // Внутри detail-страницы (есть DetailHeaderContext) форма рендерится edit-/
  // child-create-панелью в main-pane, который УЖЕ является поверхностью. Своя
  // kc-surface-карточка тут не нужна — она сдвигала бы шапку формы относительно
  // шапки таба (padding карточки) → «прыжок» иконки/названия/действия при
  // переключении таб↔форма. Embedded → без карточки, шапка ровно как у таба.
  const embedded = useDetailHeaderIcon() !== undefined;
  const bare = useContext(FormBareContext);

  const header = (
    <PanelHeader icon={<ResourceIcon specId={specId} />} eyebrow={verb} title={heading} subtitle={subtitle} />
  );

  if (embedded) {
    // Внутри detail-страницы шапку (иконка+действие+тип) показывает ЗОНА 2
    // (DetailShell.headerEyebrow/Title/Icon) — здесь её НЕ дублируем, только поля.
    return <div style={{ maxWidth: FORM_WIDTH, width: "100%", margin: 0 }}>{children}</div>;
  }

  if (bare) {
    // Внутри модалки: шапка (иконка+действие+тип) + поля, БЕЗ kc-surface-карточки
    // (поверхность — сама модалка). Единый лайаут, без вложенных карточек.
    return (
      <div style={{ width: "100%", margin: 0 }}>
        {header}
        {children}
      </div>
    );
  }

  return (
    // Standalone (create/edit-страница, модалка) — прижато влево, kc-surface
    // (как секции detail/list), не модалка.
    <div style={{ maxWidth: FORM_WIDTH, width: "100%", margin: 0 }}>
      {/* padding 20 (равномерно) = у list-страниц kc-surface padding 20 → бейдж
          PanelHeader на той же позиции (не прыгает list↔create). Было 20px 22px
          (гориз 22) → бейдж create на 2px правее списка. */}
      <div className="kc-surface" style={{ padding: 20 }}>
        {header}
        {children}
      </div>
    </div>
  );
}
