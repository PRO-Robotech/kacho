// resource-detail-extensions — реестр доменных расширений detail-страницы
// compute-remote.
//
// ResourceShell остаётся generic (Обзор / связанные / Операции / JSON + формы-
// панели). Доменно-специфичные строки Обзора, header-действия и табы инстанса
// подключаются здесь по spec.id:
//   • Обзор — зона / платформа / vCPU / память / гарантия CPU / образ / статус / FQDN;
//   • header-действия — Запустить / Остановить / Перезапустить (InstanceActions);
//   • табы «Диски» (attach/detach тома) и «Сетевые интерфейсы» (attach/detach NIC).

import { type ReactNode } from "react";
import { Typography } from "antd";

import type { DetailTab } from "@/components/organisms/DetailShell";
import { StatusBadge } from "@/components/atoms/StatusBadge";
import { getByPath } from "@/lib/resource-registry";
import { formatBytes } from "@/lib/bytes";
import { InstanceActions } from "@/components/organisms/instance/InstanceActions";
import { InstanceDisksTab } from "@/components/organisms/instance/InstanceDisksTab";
import { InstanceNicsTab } from "@/components/organisms/instance/InstanceNicsTab";

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

function bytes(v: unknown): ReactNode {
  const s = formatBytes(v);
  return s === "—" ? dash : <>{s}</>;
}

function diskCount(data: Record<string, unknown>): number {
  const boot = getByPath<Record<string, unknown>>(data, "boot_disk");
  const secondary = getByPath<unknown[]>(data, "secondary_disks") ?? [];
  return (boot && (boot.volume_id || boot.device_name) ? 1 : 0) + secondary.length;
}

// ─────────────────────────── реестр ───────────────────────────

export const DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  "compute-instances": {
    overviewExtra: ({ data }) => [
      { label: "Зона доступности", value: txt(getByPath<string>(data, "zone_id")) },
      { label: "Платформа", value: code(getByPath<string>(data, "platform_id")) },
      { label: "vCPU", value: txt(getByPath<unknown>(data, "resources.cores")) },
      { label: "Память", value: bytes(getByPath<unknown>(data, "resources.memory")) },
      { label: "Гарантия CPU, %", value: txt(getByPath<unknown>(data, "cpu_guarantee_percent")) },
      { label: "OCI-образ", value: code(getByPath<string>(data, "image")) },
      { label: "Image digest", value: code(getByPath<string>(data, "image_digest")) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
      { label: "FQDN", value: code(getByPath<string>(data, "fqdn")) },
    ],
    headerActions: ({ data, projectId }) => (
      <InstanceActions
        instanceId={getByPath<string>(data, "id") ?? ""}
        status={getByPath<string>(data, "status")}
        projectId={projectId}
      />
    ),
    extraTabs: ({ data, projectId }) => {
      const instanceId = getByPath<string>(data, "id") ?? "";
      const nics = getByPath<unknown[]>(data, "network_interfaces") ?? [];
      return [
        {
          id: "disks",
          label: "Диски",
          count: diskCount(data),
          render: () => <InstanceDisksTab instanceId={instanceId} projectId={projectId} data={data} />,
        },
        {
          id: "nics",
          label: "Сетевые интерфейсы",
          count: nics.length,
          render: () => <InstanceNicsTab instanceId={instanceId} projectId={projectId} data={data} />,
        },
      ];
    },
  },
};

export function detailExtension(specId: string): DetailExtension | undefined {
  return DETAIL_EXTENSIONS[specId];
}
