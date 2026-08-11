// PlacementAnchor — вид размещения ресурса и ССЫЛКА на его якорь.
//
// Якорь — зона (ZONAL) либо регион (REGIONAL, anycast). И то и другое — ресурсы
// каталога geo со своими карточками, поэтому якорь показывается как всякая
// ссылка на чужой ресурс: иконка типа + имя + переход (`RefNameLink`). Плоский
// моноширинный идентификатор, стоявший здесь прежде, не давал ни имени, ни
// перехода — при том что соседние поля той же карточки («Сеть», «Таблица
// маршрутизации») ссылкой уже были.
//
// Компонент существует в ЕДИНСТВЕННОМ числе намеренно: та же ветка
// ZONAL/REGIONAL прежде жила тремя копиями — в ячейке списка, в карточке
// ресурса и в её расширениях, — и правка одной копии молча не доезжала до
// остальных.
import { Tag } from "antd";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { getByPath } from "@shared/lib/path";

interface Props {
  /** Строка ресурса: читаются `placement_type`, `zone_id`, `region_id`. */
  row: Record<string, unknown>;
  /** Обрезать имя якоря по N символов (для узких колонок списка). */
  maxChars?: number;
}

function str(row: Record<string, unknown>, path: string): string {
  const v = getByPath(row, path);
  return typeof v === "string" ? v : "";
}

export function PlacementAnchor({ row, maxChars }: Props) {
  const zone = str(row, "zone_id");
  const region = str(row, "region_id");
  const declared = str(row, "placement_type");

  // `placement_type` — производное поле сервера; когда его нет, вид определяет
  // сам якорь: заполненный регион без зоны и есть REGIONAL.
  const isRegional = declared === "REGIONAL" || (!zone && !!region);
  const anchorId = isRegional ? region : zone;
  const specId = isRegional ? "regions" : "zones";

  if (!declared && !anchorId) return <span className="text-muted-foreground">—</span>;

  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
      <Tag color={isRegional ? "geekblue" : "blue"} style={{ margin: 0 }}>
        {isRegional ? "REGIONAL" : "ZONAL"}
      </Tag>
      {anchorId ? (
        <RefNameLink specId={specId} refId={anchorId} maxChars={maxChars} />
      ) : (
        <span className="text-muted-foreground">—</span>
      )}
    </span>
  );
}
