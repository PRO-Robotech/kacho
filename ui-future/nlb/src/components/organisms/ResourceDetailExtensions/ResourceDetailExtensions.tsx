// resource-detail-extensions — реестр доменных расширений detail-страницы NLB.
//
// ResourceShell остаётся generic (Обзор / связанные / Операции / JSON + формы-
// панели). Доменно-специфичные строки Обзора конкретного ресурса подключаются
// здесь по spec.id. Для NLB: LoadBalancer — регион/схема/размещение/VIP/статус;
// Listener — балансировщик/протокол/порт; TargetGroup — регион/порт/окна
// вывода и разгона/проба.
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

/** Ветви пробы целевой группы — ровно одна из четырёх задана (oneof options). */
const HEALTH_CHECK_KINDS = ["tcp", "http", "https", "grpc"] as const;

/**
 * Краткое описание пробы: «<ветвь> :<порт>».
 *
 * Проба не именована — `name` снят с контракта, — поэтому отличать одну от
 * другой приходится тем, что проба собственно делает. Порт берётся из
 * производного `effective_port` (переопределение ветви, иначе порт группы):
 * расхождение порта пробы и порта трафика видно by construction. Ни одной
 * заданной ветви — пусто: молчание ответа не выдаём за настроенную проверку.
 */
function healthCheckSummary(data: Record<string, unknown>): string {
  const kind = HEALTH_CHECK_KINDS.find((k) => getByPath<unknown>(data, `health_check.${k}`) != null);
  if (!kind) return "";
  const port = getByPath<number>(data, "health_check.effective_port");
  return port ? `${kind} :${port}` : kind;
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
      // Единственный backend-порт группы: именно его листенер переотражает в
      // `resolved_backend_port`, и от него же наследуется порт пробы.
      { label: "Порт бэкенда", value: code(getByPath<number>(data, "port")) },
      // Duration приходит строкой секунд с хвостовым «s» («300s») — своей
      // единицы подпись не называет, иначе она противоречила бы значению.
      { label: "Drain timeout", value: code(getByPath<string>(data, "deregistration_delay")) },
      { label: "Slow start", value: code(getByPath<string>(data, "slow_start")) },
      // У пробы нет имени (оно снято с контракта: HealthCheck — встроенный
      // объект-значение, а не адресуемый ресурс). Содержательны выбранная ветвь
      // (tcp|http|https|grpc) и разрешённый порт, а не идентичность.
      { label: "Health-check", value: code(healthCheckSummary(data)) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
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
