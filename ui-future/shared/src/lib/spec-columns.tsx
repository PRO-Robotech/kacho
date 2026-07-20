// spec-columns — преобразование ResourceSpec.columns в Column<row> для ResourceTable.
// Та же логика, что в ResourceListPage, вынесена для переиспользования
// (например, на Subnet detail в tab "IP-адреса" мы рендерим Addresses-таблицу
// с теми же колонками, что и /projects/X/addresses).

import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { Typography } from "antd";
import type { Column } from "@shared/components/organisms/ResourceTable";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import { StatusBadge } from "@shared/components/atoms/StatusBadge";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { getByPath, type ResourceColumn, type ResourceSpec } from "@shared/lib/resource-registry";
import { formatDateTime } from "@shared/lib/datetime";
import { referrerHref, referrerMeta } from "@shared/lib/referrer";

// Ре-экспорт для стабильности публичного API @shared/lib/spec-columns (route/label
// логика вынесена в чистый ./referrer для unit-тестируемости без antd-графа).
export { referrerHref, referrerMeta } from "@shared/lib/referrer";

// Маппинг kacho.cloud.reference.Reference.referrer.type → registry specId, чтобы
// рендерить потребителя как единую ссылку «иконка + имя» (RefNameLink) и иметь
// корректный detail-роут (включая network_interface → kacho-vpc).
const REFERRER_SPEC: Record<string, string> = {
  compute_instance: "compute-instances",
  compute_disk: "compute-disks",
  compute_image: "compute-images",
  compute_snapshot: "compute-snapshots",
  nlb_target_group: "target-groups",
  network_interface: "network-interfaces",
  network_load_balancer: "load-balancers",
  nlb_load_balancer: "load-balancers",
  load_balancer: "load-balancers",
};

// Опции для рендеринга generic-форматов, которым нужен контекст вокруг ячейки.
// Сейчас используется только `projectId` для построения SPA-ссылок в format:
// "references" (used_by → /projects/<projectId>/compute/instances/<id> и т.п.).
export interface FormatCellOpts {
  projectId?: string | null;
}

// ReferrerLink — общий рендер одного referrer'а как «{label} {id}» (plain text,
// no chip), где label — короткая type-метка с семантическим цветом текста, id —
// monospace (<Typography.Text code>, это не чип — просто моно-стиль). Всё
// обёрнуто в один <Link> если href доступен (compute_instance → SPA-route),
// либо в <span> для unknown referrer-типов (forward-compat fallback). Клик по
// link останавливает propagation, чтобы row-onClick в ResourceTable не
// триггерил navigation на parent-ресурс (см. ResourceTable.tsx — там есть
// дополнительный skip на closest('a'), это просто defense-in-depth).
export function ReferrerLink({
  projectId,
  referrer,
}: {
  projectId: string | null | undefined;
  referrer: { type?: string; id?: string } | undefined;
}): ReactNode {
  // Известный тип → единая ссылка «иконка + имя» через RefNameLink (резолв имени
  // + detail-роут). Неизвестный тип — forward-compat fallback (label + id ниже).
  const mappedSpec = referrer?.type ? REFERRER_SPEC[referrer.type] : undefined;
  if (mappedSpec && referrer?.id) {
    return <RefNameLink specId={mappedSpec} refId={referrer.id} projectId={projectId ?? undefined} maxChars={32} />;
  }
  const meta = referrerMeta(referrer?.type);
  const id = referrer?.id ?? "";
  const href = referrerHref(projectId, referrer);
  const inner = (
    <>
      <span style={{ color: meta.color, fontWeight: 500, fontSize: 12 }}>{meta.label}</span>
      <Typography.Text code style={{ fontSize: 12 }} title={id || undefined}>
        {id || "—"}
      </Typography.Text>
    </>
  );
  if (href) {
    return (
      <Link
        to={href}
        onClick={(e) => e.stopPropagation()}
        style={{ display: "inline-flex", alignItems: "baseline", gap: 4 }}
      >
        {inner}
      </Link>
    );
  }
  return <span style={{ display: "inline-flex", alignItems: "baseline", gap: 4 }}>{inner}</span>;
}

// reorderNameIdFirst — KAC-245: во всех таблицах первые две колонки по умолчанию
// Name (path="name"), затем ID (path="id"). Извлекаем эти колонки из spec (где бы
// они ни стояли) и ставим первыми, СОХРАНЯЯ их объекты (а значит и кастомные
// render — CopyableName/CopyableId); остальные колонки — в исходном порядке. Если
// name-колонки нет (системные справочники disk-types/compute-zones) — id остаётся
// первым (graceful). Идемпотентно для ресурсов, где порядок уже верный (VPC/compute).
export function reorderNameIdFirst(columns: ResourceColumn[]): ResourceColumn[] {
  const nameCol = columns.find((c) => c.path === "name");
  // Без name-колонки (системные справочники, IAM users) — НЕ выносим id
  // принудительно вперёд: сохраняем авторский порядок. У users первичный
  // идентификатор — email, он должен оставаться первой колонкой. Хойстинг id
  // имеет смысл только чтобы держать его рядом с Name.
  if (!nameCol) return columns;
  const idCol = columns.find((c) => c.path === "id");
  const lead: ResourceColumn[] = [nameCol];
  if (idCol) lead.push(idCol);
  const rest = columns.filter((c) => c !== nameCol && c !== idCol);
  return [...lead, ...rest];
}

export function buildSpecColumns(spec: ResourceSpec, opts: FormatCellOpts = {}): Column<Record<string, unknown>>[] {
  return reorderNameIdFirst(spec.columns).map((c) => ({
    header: c.header,
    className: c.className,
    cell: (row) => (c.render ? c.render(row) : formatCellByFormat(c, row, opts)),
    sortKey: c.format === "datetime" || c.format === "text" || c.format === "uid-short" ? c.path : undefined,
  }));
}

export function formatCellByFormat(
  c: ResourceColumn,
  row: Record<string, unknown>,
  opts: FormatCellOpts = {},
): ReactNode {
  const v = getByPath(row, c.path);
  switch (c.format) {
    case "status":
      return <StatusBadge state={typeof v === "string" ? v : undefined} />;
    case "uid-short":
      return typeof v === "string" && v ? <CopyableId id={v} /> : <Typography.Text type="secondary">—</Typography.Text>;
    case "datetime":
      return typeof v === "string" && v ? (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {formatDateTime(v)}
        </Typography.Text>
      ) : (
        <Typography.Text type="secondary">—</Typography.Text>
      );
    case "code":
      return typeof v === "string" || typeof v === "number" ? (
        <Typography.Text code style={{ fontSize: 12 }}>
          {String(v)}
        </Typography.Text>
      ) : (
        <Typography.Text type="secondary">—</Typography.Text>
      );
    case "list":
      if (Array.isArray(v) && v.length > 0) {
        return (
          <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
            {v.map((item, i) => (
              <span
                key={i}
                style={{
                  fontFamily: "ui-monospace, SFMono-Regular, monospace",
                  fontSize: 12,
                  whiteSpace: "nowrap",
                }}
              >
                {String(item)}
              </span>
            ))}
          </div>
        );
      }
      return <Typography.Text type="secondary">—</Typography.Text>;
    case "references":
      // Generic renderer для output-only списков kacho.cloud.reference.Reference
      // (типичный shape: [{ referrer: { type, id }, type }, ...]). Показываем
      // первый referrer как «{label} {id}» (plain text + link, без chip); full
      // id — в tooltip + as visible text (~20 chars, помещается в cell); "+N
      // more" — тихий subtle <span> (тоже без chip) если рефереров больше
      // одного, с tooltip-listing остальных. Для известных referrer-типов
      // первый элемент обёрнут в SPA-<Link>; для прочих — plain (forward-compat
      // fallback). Клик внутри <a> не триггерит row-navigation (см.
      // ResourceTable.tsx — есть skip на `closest('a')`).
      if (Array.isArray(v) && v.length > 0) {
        const first = v[0] as { referrer?: { type?: string; id?: string } } | undefined;
        const more = v.length > 1 ? v.length - 1 : 0;
        const projectId = opts.projectId ?? (getByPath<string>(row, "project_id") || null);
        const restTitle = more
          ? (v.slice(1) as Array<{ referrer?: { type?: string; id?: string } }>)
              .map((r) => `${r.referrer?.type ?? "?"} ${r.referrer?.id ?? ""}`)
              .join("\n")
          : undefined;
        return (
          <span style={{ display: "inline-flex", alignItems: "baseline", gap: 6, fontSize: 12 }}>
            <ReferrerLink projectId={projectId} referrer={first?.referrer} />
            {more > 0 && (
              <span style={{ color: "rgba(0,0,0,.45)", fontSize: 11 }} title={restTitle}>
                +{more} more
              </span>
            )}
          </span>
        );
      }
      return <Typography.Text type="secondary">—</Typography.Text>;
    case "text":
    default:
      if (v == null || v === "") return <Typography.Text type="secondary">—</Typography.Text>;
      return String(v);
  }
}
