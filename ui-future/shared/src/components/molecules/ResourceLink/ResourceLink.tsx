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
//   • копирование, если нужно, — иконкой РЯДОМ с именем и ВНЕ ссылки: значок
//     принадлежит значению, поэтому стоит вплотную к нему, а не столбцом
//     действий у правого края строки; виден он всегда, тише его делает тон;
//   • иконка типа — по требованию (`icon`): она нужна там, где в одном окне
//     соседствуют разные типы, и лишняя там, где тип назван заголовком;
//   • без адреса ссылка не рисуется: подчёркнутый текст, никуда не ведущий,
//     обещает переход, которого нет.
import type { ReactNode } from "react";
import { useInPropertyRow } from "@shared/components/organisms/DetailShell/property-row-context";
import { Link } from "react-router";
import { Typography } from "antd";
import { CopyableName } from "@shared/components/atoms/CopyableName";
import { cn } from "@shared/lib/utils";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { resourceProjectPath, resourceServicePrefix, REGISTRY } from "@shared/lib/resource-registry";

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
  const own = resourceProjectPath(specId, projectId ?? null);
  if (own) return `${own}/${id}`;
  return resourceServicePrefix(specId) === "iam" ? `/iam/${spec.route}/${id}` : null;
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
  const inPropertyRow = useInPropertyRow();
  const rid = typeof id === "string" ? id : "";
  if (!rid && !name) return <Typography.Text type="secondary">—</Typography.Text>;

  // Обрезка запасного идентификатора — приём против ДЛИННОГО МАШИННОГО id, чей
  // хвост читателю ничего не говорит. Там, где идентификатор и есть имя
  // (каталог размещения geo, #716), значение несёт как раз хвост: `ru-central1-a`
  // превращалось в `ru-central1-…`, и все зоны региона выглядели одинаково.
  const idIsTheName = REGISTRY[specId]?.idIsTheName === true;
  const full = name || (!idIsTheName && rid.length > 12 ? `${rid.slice(0, 12)}…` : rid);
  const shown = maxChars && full.length > maxChars ? `${full.slice(0, maxChars)}…` : full;
  const target = href ?? resourceDetailHref(specId, rid, projectId);

  const label = (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, minWidth: 0 }}>
      {icon ? (
        <span style={{ display: "inline-flex", flexShrink: 0 }}>
          <ResourceIcon specId={specId} />
        </span>
      ) : null}
      {/* Три свойства работают ТОЛЬКО вместе: без `nowrap` многоточие не наступает
          никогда (переносить есть куда — и имя уезжает во вторую строку, поднимая
          строку списка над соседними), а без `minWidth: 0` flex-элемент отказывается
          быть уже своего содержимого, и обрезка снова не наступает. Прежде здесь
          стояло одно `textOverflow` — правило, не делавшее ничего. */}
      <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{shown}</span>
    </span>
  );

  return (
    // `group` — ЯКОРЬ ТОНА значка копирования, а не его появления. Появление
    // отменено решением владельца: значок виден всегда, потому что действие,
    // раскрываемое наведением, недоступно с сенсорного ввода вовсе и не
    // читается глазами как возможность. Осталось различие тона — в покое
    // значок тусклый, а на подходе курсора к ИМЕНИ (то есть к этой обёртке,
    // а не к двенадцати пикселям самого значка) становится ярким.
    <span className="group" style={{ display: "inline-flex", alignItems: "center", gap: 6, minWidth: 0 }} title={full}>
      {target ? (
        // В покое ссылка — цвет сигнала БЕЗ подчёркивания: страница ресурса
        // состоит из ссылок почти целиком, и сплошное подчёркивание превратило
        // бы её в частокол. Наведение даёт обе перемены сразу — цвет текста и
        // подчёркивание с отступом, — поэтому «здесь можно кликнуть» читается,
        // а покой остаётся тихим.
        <Link
          to={target}
          // Переход задан токенами продукта (160 мс, общая кривая) и касается
          // ровно цвета: движение по консоли обязано читаться как одно вещество,
          // а набор Tailwind несёт свою длительность и свою кривую.
          style={{ minWidth: 0, transition: "color var(--kc-duration) var(--kc-ease)" }}
          className={cn(
            "text-primary no-underline",
            "hover:text-foreground hover:underline underline-offset-[3px]",
            !plain && "font-medium",
          )}
        >
          {label}
        </Link>
      ) : (
        <span className={plain ? "text-foreground" : "text-foreground font-medium"}>{label}</span>
      )}
      {/* Копируется ЗНАЧЕНИЕ, а не показанное: усечение — свойство показа, и
          идентификатор с многоточием на конце не находится нигде и не
          вставляется никуда. Ошибку такого рода замечает уже тот, кто вставил. */}
      {/* Свой значок — только вне строки свойств. Внутри неё копирование общее
          (одна кнопка на строку, столбцом справа), и второй значок рядом с
          текстом ломал бы столбец. Спрашиваем МЕСТО, а не проп: так правило
          исполняется само у любого вызывающего, включая тех, кто о нём не знал.
          Явный `copy={false}` при этом продолжает работать. */}
      {copy && !inPropertyRow && (name || rid) ? (
        <CopyableName name={name ?? ""} fallback={rid} iconOnly />
      ) : null}
    </span>
  );
}
