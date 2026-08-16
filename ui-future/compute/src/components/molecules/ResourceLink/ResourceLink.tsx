// ResourceLink — ЕДИНСТВЕННЫЙ вид ссылки на ресурс во всей консоли.
//
// Одна ссылка — один компонент: прежде их было несколько (ячейка имени в
// таблице, ссылка на чужой ресурс, ссылка на потребителя), и они разошлись во
// всём — в том, откуда берётся адрес, показывать ли иконку типа и что делает
// клик. Самое дорогое расхождение стоило дня: имя в таблице оборачивалось в
// ссылку ВМЕСТЕ с кнопкой «скопировать имя», клик попадал на кнопку и копировал
// вместо перехода — ссылка была, выглядела рабочей и не работала.
//
// Отсюда правила, которые компонент держит сам:
//   • текст имени — САМА ссылка, внутрь неё не кладётся ничего кликабельного;
//   • копирование, если нужно, — отдельной иконкой РЯДОМ, вне ссылки;
//   • иконка типа — по требованию (`icon`): она нужна там, где в одном окне
//     соседствуют разные типы, и лишняя там, где тип назван заголовком;
//   • без адреса ссылка не рисуется: подчёркнутый текст, никуда не ведущий,
//     обещает переход, которого нет.
import type { ReactNode } from "react";
import { Link } from "react-router";
import { Typography } from "antd";
import { CopyableName } from "@/components/atoms/CopyableName";
import { ResourceIcon } from "@/components/organisms/form/ResourceIcon";
import { resourceProjectPath, REGISTRY } from "@/lib/resource-registry";

export interface ResourceLinkProps {
  /** Тип ресурса — из него берутся иконка и, если не задан `href`, адрес. */
  specId: string;
  /** Идентификатор ресурса. Без него адреса нет. */
  id: string | null | undefined;
  /** Человекочитаемое имя. Пусто → показывается усечённый идентификатор. */
  name?: string | null;
  /** Явный адрес карточки. Задаётся, когда путь не «база + идентификатор». */
  href?: string | null;
  /** Проект для построения адреса, если `href` не задан. */
  projectId?: string | null;
  /** Иконка типа слева от имени. */
  icon?: boolean;
  /** Иконка копирования справа от ссылки (вне неё). */
  copy?: boolean;
  /** Обрезать имя по N символов; полное остаётся в подсказке. */
  maxChars?: number;
  /** Не выделять имя полужирным (в плотных таблицах-списках). */
  plain?: boolean;
}

/** Адрес карточки ресурса. IAM смонтирован под `/iam/<route>` и своего
 *  project-scoped пути не имеет — общая функция отдаёт для него null. */
export function resourceDetailHref(specId: string, id: string, projectId?: string | null): string | null {
  const spec = REGISTRY[specId];
  if (!spec || !id) return null;
  // В реестре этого модуля IAM-ресурсов нет вовсе (их адресация `/iam/<route>`
  // живёт в общем реестре), поэтому путь здесь ровно один.
  const own = resourceProjectPath(specId, projectId ?? null);
  return own ? `${own}/${id}` : null;
}

export function ResourceLink({
  specId,
  id,
  name,
  href,
  projectId,
  icon = false,
  copy = false,
  maxChars,
  plain = false,
}: ResourceLinkProps): ReactNode {
  const rid = typeof id === "string" ? id : "";
  if (!rid && !name) return <Typography.Text type="secondary">—</Typography.Text>;

  const full = name || (rid.length > 12 ? `${rid.slice(0, 12)}…` : rid);
  const shown = maxChars && full.length > maxChars ? `${full.slice(0, maxChars)}…` : full;
  const target = href ?? resourceDetailHref(specId, rid, projectId);

  const label = (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, minWidth: 0 }}>
      {icon ? <ResourceIcon specId={specId} /> : null}
      <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{shown}</span>
    </span>
  );

  return (
    // `group` — ЯКОРЬ появления значка копирования: он не виден в покое и
    // раскрывается наведением на эту обёртку, то есть на имя. Прежде якорем
    // была сама кнопка (текст имени лежал внутри неё); разведя ссылку и
    // кнопку, мы забрали у правила предка, и значку назначили постоянную
    // видимость — она и оказалась слишком заметной (#480).
    <span className="group" style={{ display: "inline-flex", alignItems: "center", gap: 6, minWidth: 0 }} title={full}>
      {target ? (
        <Link to={target} className={plain ? "text-primary hover:underline" : "text-primary hover:underline font-medium"}>
          {label}
        </Link>
      ) : (
        <span className={plain ? "text-foreground" : "text-foreground font-medium"}>{label}</span>
      )}
      {copy && full ? <CopyableName name={full} iconOnly /> : null}
    </span>
  );
}
