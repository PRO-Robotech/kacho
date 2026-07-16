// resource-detail-extensions — реестр доменных расширений detail-страницы NLB.
//
// ResourceShell остаётся generic (Обзор / связанные / Операции / JSON + формы-
// панели). Доменно-специфичные строки Обзора конкретного ресурса подключаются
// здесь по spec.id. Для NLB: LoadBalancer — регион/схема/размещение/VIP/статус;
// Listener — балансировщик/протокол/порт; TargetGroup — регион/health-check.
// Богатый LoadBalancer-detail (attach/detach TG, per-tab actions) подключается
// отдельной кастом-обёрткой на следующем этапе.

import { type ReactNode } from "react";
import { Tag, Typography } from "antd";

import type { DetailTab } from "@/components/organisms/DetailShell";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { TargetsManager, type Target } from "@/components/organisms/TargetsManager";
import { StatusBadge } from "@/components/atoms/StatusBadge";
import { NlbVipCell } from "@/components/molecules/NlbVipCell";
import { getByPath } from "@/lib/resource-registry";

export interface DescItem {
  label: string;
  value: ReactNode;
}

export interface DetailExtCtx {
  data: Record<string, unknown>;
  projectId: string | null;
  /** Базовый URL detail-страницы ресурса (без хвостов /edit, /json, /<tab>). */
  detailBase: string;
  navigate: (to: string) => void;
}

export interface DetailExtension {
  overviewExtra?: (ctx: DetailExtCtx) => DescItem[];
  /** Контент под Обзор-таблицей (отдельные секции-таблицы с подписью). */
  overviewBelow?: (ctx: DetailExtCtx) => ReactNode;
  headerActions?: (ctx: DetailExtCtx) => ReactNode;
  extraTabs?: (ctx: DetailExtCtx) => DetailTab[];
  hideOperations?: boolean;
  title?: (data: Record<string, unknown>) => string | undefined;
}

// ─────────────────────────── helpers ───────────────────────────

const dash = <Typography.Text type="secondary">—</Typography.Text>;

function txt(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? s : dash;
}

function code(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? (
    <Typography.Text code style={{ fontSize: 12 }}>
      {s}
    </Typography.Text>
  ) : (
    dash
  );
}

function boolTag(v: unknown, yes = "Да", no = "Нет"): ReactNode {
  return v ? <Tag color="green">{yes}</Tag> : <Tag>{no}</Tag>;
}

// ─────────────────────────── реестр ───────────────────────────

export const DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  "load-balancers": {
    // Единая таблица «Обзор»: immutable схема/размещение + mutable-скаляры +
    // резолвнутый VIP пофамильно + drain-зоны. Размещение — только для INTERNAL,
    // зоны без анонса — только для REGIONAL (зеркалит форму создания).
    overviewExtra: ({ data }) => {
      const type = getByPath<string>(data, "type") ?? "";
      const placement = getByPath<string>(data, "placement_type") ?? "";
      const drainZones = (getByPath<string[]>(data, "disabled_announce_zones") ?? []) as string[];
      const items: DescItem[] = [
        { label: "Регион", value: code(getByPath<string>(data, "region_id")) },
        {
          label: "Схема",
          value: type ? <Tag color={type === "INTERNAL" ? "geekblue" : "blue"}>{type}</Tag> : dash,
        },
      ];
      if (type === "INTERNAL") {
        items.push({
          label: "Размещение",
          value: placement ? <Tag color={placement === "REGIONAL" ? "purple" : "cyan"}>{placement}</Tag> : dash,
        });
      }
      items.push(
        { label: "Session affinity", value: code(getByPath<string>(data, "session_affinity")) },
        { label: "IPv4-адрес", value: <NlbVipCell v4AddressId={getByPath<string>(data, "v4_address_id")} /> },
        { label: "IPv6-адрес", value: <NlbVipCell v6AddressId={getByPath<string>(data, "v6_address_id")} /> },
      );
      if (placement === "REGIONAL") {
        items.push({
          label: "Зоны без анонса",
          value:
            drainZones.length > 0 ? (
              <span>
                {drainZones.map((z) => (
                  <Tag key={z} style={{ marginInlineEnd: 4 }}>
                    {z}
                  </Tag>
                ))}
              </span>
            ) : (
              <Typography.Text type="secondary">анонс из всех зон</Typography.Text>
            ),
        });
      }
      items.push(
        { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
        { label: "Защита от удаления", value: boolTag(getByPath<boolean>(data, "deletion_protection")) },
      );
      return items;
    },
  },

  listeners: {
    overviewExtra: ({ data }) => [
      {
        label: "Балансировщик",
        value: (
          <RefNameLink specId="load-balancers" refId={getByPath<string>(data, "load_balancer_id")} maxChars={42} />
        ),
      },
      { label: "Протокол", value: code(getByPath<string>(data, "protocol")) },
      { label: "Порт", value: code(getByPath<number>(data, "port")) },
      { label: "Порт на target", value: code(getByPath<number>(data, "target_port")) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
    ],
  },

  "target-groups": {
    overviewExtra: ({ data }) => [
      { label: "Регион", value: txt(getByPath<string>(data, "region_id")) },
      {
        label: "Drain timeout (с)",
        value: code(getByPath<number>(data, "deregistration_delay_seconds")),
      },
      { label: "Health-check", value: code(getByPath<string>(data, "health_check.name")) },
    ],
    // Управление backend-таргетами (add/remove через :addTargets/:removeTargets)
    // прямо в блоке «Обзор».
    overviewBelow: ({ data, projectId }) => (
      <TargetsManager
        targetGroupId={getByPath<string>(data, "id") ?? ""}
        projectId={projectId}
        targets={getByPath<Target[]>(data, "targets") ?? []}
      />
    ),
  },
};

export function detailExtension(specId: string): DetailExtension | undefined {
  return DETAIL_EXTENSIONS[specId];
}
