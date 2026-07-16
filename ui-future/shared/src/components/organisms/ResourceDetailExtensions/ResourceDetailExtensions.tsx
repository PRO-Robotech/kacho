// resource-detail-extensions — реестр доменных расширений detail-страницы.
//
// ResourceShell остаётся generic (Обзор/связанные/Операции/JSON + формы-панели).
// Доменно-специфичный контент конкретного ресурса (доп. строки Обзора, доменные
// табы — SG-правила, RouteTable-маршруты, Instance NIC/power, TG targets, IPAM,
// IAM access-bindings — кнопки-действия в шапке) подключается ЗДЕСЬ, по spec.id,
// переиспользуя уже существующие доменные компоненты/логику кастом-страниц.
//
// Так раскатка эталона на все ресурсы не теряет доменную функциональность и не
// раздувает ResourceShell. Карта миграции:
// docs/superpowers/specs/2026-05-30-kacho-ui-rollout-migration-map.json

import { type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Tag, Typography } from "antd";

import { toast } from "@shared/lib/toast";
import type { DetailTab } from "@shared/components/organisms/DetailShell";

import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { SgRulesPanel, type SgRule } from "@shared/components/organisms/SgRulesPanel";
import { RoutesPanel } from "@shared/components/organisms/RoutesPanel";
import { SubnetCidrPanel } from "@shared/components/organisms/SubnetCidrPanel";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { ReferrerLink } from "@shared/lib/spec-columns";
import { api } from "@shared/api/client";
import { getByPath } from "@shared/lib/resource-registry";

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
  /** Контент под Обзор-таблицей (отдельные секции-таблицы с подписью, напр.
   *  статические маршруты RouteTable). */
  overviewBelow?: (ctx: DetailExtCtx) => ReactNode;
  headerActions?: (ctx: DetailExtCtx) => ReactNode;
  extraTabs?: (ctx: DetailExtCtx) => DetailTab[];
  /** Кастомная embedded create-форма для child-create-роута, которого НЕТ в
   *  REGISTRY (напр. "privileges" → AccessBindingCreateForm с залоченным
   *  субъектом). ResourceShell зовёт это в child-create branch, когда REGISTRY-spec
   *  для childRoute не найден. Форма сама навигирует через onSuccess/onCancel. */
  childCreate?: (childRoute: string, ctx: DetailExtCtx) => ReactNode;
  hideOperations?: boolean;
  title?: (data: Record<string, unknown>) => string | undefined;
}

// ─────────────────────────── helpers ───────────────────────────

const dash = <Typography.Text type="secondary">—</Typography.Text>;

function txt(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? s : dash;
}

function mono(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? <span style={{ fontFamily: "ui-monospace, monospace", fontSize: 12 }}>{s}</span> : dash;
}

function boolTag(v: unknown, yes = "Да", no = "Нет"): ReactNode {
  return v ? <Tag color="green">{yes}</Tag> : <Tag>{no}</Tag>;
}

// CIDR-блоки — нейтральные (цвет текста) теги, друг под другом, клик = копировать.
function cidrTags(items: string[] | undefined): ReactNode {
  if (!items || items.length === 0) return dash;
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
      {items.map((c) => (
        <Tag
          key={c}
          title="Нажмите, чтобы скопировать"
          onClick={(e) => {
            e.stopPropagation();
            navigator.clipboard?.writeText(c);
            toast.success(`Скопировано: ${c}`);
          }}
          style={{ margin: 0, cursor: "pointer", fontFamily: "ui-monospace, monospace", fontSize: 12 }}
        >
          {c}
        </Tag>
      ))}
    </span>
  );
}

// Ссылки на ресурсы (иконка + имя), друг под другом — единый вид как везде.
function refLinks(ids: string[] | undefined, specId: string): ReactNode {
  if (!ids || ids.length === 0) return dash;
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
      {ids.map((id) => (
        <RefNameLink key={id} specId={specId} refId={id} maxChars={28} />
      ))}
    </span>
  );
}

// ── RouteTable static_routes ──
interface StaticRoute {
  destination_prefix?: string;
  next_hop_address?: string;
  gateway_id?: string;
}
// Статические маршруты — PROP таблицы маршрутизации (не смежный ресурс).
// Показываем ОТДЕЛЬНОЙ таблицей с подписью под Обзором (overviewBelow);
// добавление/правка — через «Редактировать» (generic array-field static_routes).

// ── Address: вычисление IP/семейства/вида ──
function addressInfo(data: Record<string, unknown>): { ip: string; family: string; kind: string } {
  const ext4 = getByPath<{ address?: string }>(data, "external_ipv4_address");
  const int4 = getByPath<{ address?: string }>(data, "internal_ipv4_address");
  const ext6 = getByPath<{ address?: string }>(data, "external_ipv6_address");
  const int6 = getByPath<{ address?: string }>(data, "internal_ipv6_address");
  if (ext4?.address) return { ip: ext4.address, family: "IPv4", kind: "Внешний" };
  if (int4?.address) return { ip: int4.address, family: "IPv4", kind: "Внутренний" };
  if (ext6?.address) return { ip: ext6.address, family: "IPv6", kind: "Внешний" };
  if (int6?.address) return { ip: int6.address, family: "IPv6", kind: "Внутренний" };
  return { ip: "", family: "—", kind: "—" };
}

// AddressRefTag — тег адреса: имя ресурса + доп-алиас (сам IP), кликабельно на
// detail адреса. Резолвит адрес по id (TanStack-дедуп).
function AddressRefTag({ id, projectId }: { id: string; projectId: string | null }) {
  const { data } = useQuery({
    queryKey: ["ref-address", id],
    queryFn: () => api.get<Record<string, unknown>>(`/vpc/v1/addresses/${id}`),
    enabled: !!id,
    staleTime: 30_000,
  });
  const name = (data ? getByPath<string>(data, "name") : "") || id.slice(0, 12);
  const ip = data ? addressInfo(data).ip : "";
  // Единый вид ссылки на ресурс: иконка + имя (+ доп-алиас IP), не тег.
  const content = (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <ResourceIcon specId="addresses" />
      {name}
      {ip && <span style={{ fontFamily: "ui-monospace, monospace", opacity: 0.85 }}> · {ip}</span>}
    </span>
  );
  return projectId ? (
    <Link
      to={`/projects/${projectId}/vpc/addresses/${id}`}
      onClick={(e) => e.stopPropagation()}
      className="text-primary hover:underline"
    >
      {content}
    </Link>
  ) : (
    <span className="text-foreground">{content}</span>
  );
}

function AddressRefTags({ ids, projectId }: { ids: string[] | undefined; projectId: string | null }): ReactNode {
  if (!ids || ids.length === 0) return dash;
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
      {ids.map((id) => (
        <AddressRefTag key={id} id={id} projectId={projectId} />
      ))}
    </span>
  );
}

// ─────────────────────────── реестр ───────────────────────────

export const DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  networks: {
    overviewExtra: ({ data }) => [
      {
        label: "Группа безопасности по умолчанию",
        value: (
          <RefNameLink
            specId="security-groups"
            refId={getByPath<string>(data, "default_security_group_id")}
            maxChars={42}
          />
        ),
      },
    ],
  },

  subnets: {
    overviewExtra: ({ data }) => [
      { label: "Зона", value: mono(getByPath<string>(data, "zone_id")) },
      {
        label: "Сеть",
        value: <RefNameLink specId="networks" refId={getByPath<string>(data, "network_id")} maxChars={42} />,
      },
      {
        label: "Таблица маршрутизации",
        value: getByPath<string>(data, "route_table_id") ? (
          <RefNameLink specId="route-tables" refId={getByPath<string>(data, "route_table_id")} maxChars={42} />
        ) : (
          dash
        ),
      },
      // CIDR-блоки (IPv4/IPv6) — НЕ в таблице Обзора: они управляются отдельными
      // RPC (:add/:remove-cidr-blocks) и показаны отдельной панелью ниже.
    ],
    // CIDR-блоки — отдельная панель управления под Обзором (как «Статические
    // маршруты» у route-tables). Мутируются :add/:remove-cidr-blocks, не PATCH.
    overviewBelow: ({ data, projectId }) => {
      const subnetId = getByPath<string>(data, "id") ?? "";
      const v4 = (getByPath<string[]>(data, "v4_cidr_blocks") ?? []) as string[];
      const v6 = (getByPath<string[]>(data, "v6_cidr_blocks") ?? []) as string[];
      return <SubnetCidrPanel subnetId={subnetId} v4Blocks={v4} v6Blocks={v6} projectId={projectId} />;
    },
  },

  "route-tables": {
    overviewExtra: ({ data }) => [
      {
        label: "Сеть",
        value: <RefNameLink specId="networks" refId={getByPath<string>(data, "network_id")} maxChars={42} />,
      },
    ],
    // Статические маршруты — отдельная таблица с подписью под Обзором.
    overviewBelow: ({ data, projectId }) => {
      // KAC-239: маршруты управляются отдельно от ресурса — RoutesPanel
      // (Добавить / чекбоксы + bulk-delete), не правкой всего RT.
      const routes = (getByPath<StaticRoute[]>(data, "static_routes") ?? []) as StaticRoute[];
      const rtId = getByPath<string>(data, "id") ?? "";
      return <RoutesPanel routeTableId={rtId} projectId={projectId} routes={routes} />;
    },
  },

  "security-groups": {
    overviewExtra: ({ data, projectId }) => {
      // KAC-239 S2: потребители SG (used_by) — к кому подключена группа.
      const usedBy = getByPath<{ referrer?: { type?: string; id?: string } }[]>(data, "used_by") ?? [];
      return [
        {
          label: "Сеть",
          value: getByPath<string>(data, "network_id") ? (
            <RefNameLink specId="networks" refId={getByPath<string>(data, "network_id")} maxChars={42} />
          ) : (
            dash
          ),
        },
        { label: "Default для сети", value: boolTag(getByPath<boolean>(data, "default_for_network")) },
        {
          label: "Потребители",
          value:
            usedBy.length === 0 ? (
              dash
            ) : (
              <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
                {usedBy.map((u, i) => (
                  <ReferrerLink key={i} projectId={projectId} referrer={u.referrer} />
                ))}
              </span>
            ),
        },
      ];
    },
    // req: правила — ОТДЕЛЬНЫМ табом «Правила» (таблица + «Добавить» + чекбоксы +
    // bulk-delete через SgRulesPanel). Бэкенд — UpdateRules по стабильным id.
    extraTabs: ({ data, projectId }) => {
      const all = (getByPath<SgRule[]>(data, "rules") ?? []) as SgRule[];
      const sgId = getByPath<string>(data, "id") ?? "";
      // KAC-243 (scenario 18): network_id SG → SG-target picker в редакторе
      // правил фильтрует кандидатов по той же сети.
      const networkId = getByPath<string>(data, "network_id") ?? "";
      return [
        {
          id: "rules",
          label: "Правила",
          count: all.length,
          render: () => <SgRulesPanel sgId={sgId} projectId={projectId} rules={all} networkId={networkId} />,
        },
      ];
    },
  },

  addresses: {
    overviewExtra: ({ data, projectId }) => {
      const info = addressInfo(data);
      const usedBy = getByPath<{ referrer?: { type?: string; id?: string } }[]>(data, "used_by") ?? [];
      const used = getByPath<boolean>(data, "used") ?? usedBy.length > 0;
      return [
        { label: "IP-адрес", value: cidrTags(info.ip ? [info.ip] : undefined) },
        { label: "Версия", value: txt(info.family) },
        { label: "Вид", value: txt(info.kind) },
        { label: "Используется", value: boolTag(used) },
        {
          label: "Потребители",
          value:
            usedBy.length === 0 ? (
              dash
            ) : (
              <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
                {usedBy.map((u, i) => (
                  <ReferrerLink key={i} projectId={projectId} referrer={u.referrer} />
                ))}
              </span>
            ),
        },
        { label: "Защита от удаления", value: boolTag(getByPath<boolean>(data, "deletion_protection")) },
      ];
    },
  },

  gateways: {
    overviewExtra: ({ data }) => [
      { label: "Тип", value: txt(getByPath<string>(data, "type") || "SHARED_EGRESS_GATEWAY") },
    ],
  },

  "network-interfaces": {
    overviewExtra: ({ data, projectId }) => [
      {
        label: "Подсеть",
        value: <RefNameLink specId="subnets" refId={getByPath<string>(data, "subnet_id")} maxChars={42} />,
      },
      { label: "MAC-адрес", value: mono(getByPath<string>(data, "mac_address")) },
      {
        label: "IPv4-адреса",
        value: <AddressRefTags ids={getByPath<string[]>(data, "v4_address_ids")} projectId={projectId} />,
      },
      {
        label: "IPv6-адреса",
        value: <AddressRefTags ids={getByPath<string[]>(data, "v6_address_ids")} projectId={projectId} />,
      },
      {
        label: "Группы безопасности",
        value: refLinks(getByPath<string[]>(data, "security_group_ids"), "security-groups"),
      },
    ],
  },
};

// Расширения, зарегистрированные app'ом на старте (напр. IAM-remote регистрирует
// доменные детейл-расширения своих ресурсов). Так shared остаётся app-agnostic:
// доменная специфика инжектится потребителем, а не хардкодится здесь. Регистрация
// перекрывает базовую DETAIL_EXTENSIONS для того же specId.
const registeredExtensions: Record<string, DetailExtension> = {};

// registerDetailExtension — подключает доменное расширение detail-страницы для
// ресурса specId (вызывается app'ом на старте, до рендера detail-страниц).
export function registerDetailExtension(specId: string, ext: DetailExtension): void {
  registeredExtensions[specId] = ext;
}

export function detailExtension(specId: string): DetailExtension | undefined {
  return registeredExtensions[specId] ?? DETAIL_EXTENSIONS[specId];
}
