// SectionHeader — «шапка» секции/таба detail-страницы через общий PanelHeader:
// [иконка] [eyebrow=действие] [title=название] [actions] + линия. Те же 3 части,
// что и у форм (иконка + название + действие).
//   • icon: по умолчанию — иконка ресурса из DetailHeaderContext (ResourceShell
//     прокидывает её); related-табы передают icon ДОЧЕРНЕГО ресурса явно.
//   • eyebrow: «Обзор» / «Информация» / «Список» / … (действие/вид секции).
import { type ReactNode } from "react";
import { PanelHeader, useDetailHeaderIcon } from "@shared/components/molecules/PanelHeader";

interface Props {
  title: ReactNode;
  /** Блок действий справа (кнопки, поиск, шестерёнка). */
  right?: ReactNode;
  /** Override иконки (например иконка дочернего ресурса в related-табе). */
  icon?: ReactNode;
  /** Действие/вид секции (3-я часть): «Обзор» / «Информация» / «Список» / … */
  eyebrow?: string;
}

export function SectionHeader({ title, right, icon, eyebrow }: Props) {
  const ctxIcon = useDetailHeaderIcon();
  return <PanelHeader icon={icon ?? ctxIcon} eyebrow={eyebrow} title={title} right={right} />;
}
