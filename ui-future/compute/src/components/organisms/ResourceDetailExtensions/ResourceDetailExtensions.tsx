// resource-detail-extensions — реестр доменных расширений detail-страницы
// compute-remote.
//
// ResourceShell остаётся generic (Обзор / связанные / Операции / JSON + формы-
// панели). Доменно-специфичные строки Обзора, header-действия и табы инстанса
// подключаются здесь по spec.id:
//   • Обзор (COMP-1) — тип (VM/CONTAINER) / зона / тип машины (+ effectiveResources
//     vCPU·память) / гарантия CPU / boot source (тип·id·имя°·digest°·boot-том°) /
//     сервисный аккаунт / статус (+ status_reason) / FQDN;
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

// effective_resources.memory_mib хранится в МиБ (int64 строкой); приводим к байтам
// для общего formatBytes. Пусто/невалидно → NaN (formatBytes отдаст «—»).
function mibToBytes(v: unknown): number {
  const mib = typeof v === "string" ? Number.parseInt(v, 10) : typeof v === "number" ? v : Number.NaN;
  return Number.isFinite(mib) && mib > 0 ? mib * 1024 * 1024 : Number.NaN;
}

function diskCount(data: Record<string, unknown>): number {
  const boot = getByPath<Record<string, unknown>>(data, "boot_disk");
  const secondary = getByPath<unknown[]>(data, "secondary_disks") ?? [];
  return (boot && (boot.volume_id || boot.device_name) ? 1 : 0) + secondary.length;
}

// ─────────────────────────── реестр ───────────────────────────

export const DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  "compute-instances": {
    overviewExtra: ({ data }) => {
      const memMib = getByPath<unknown>(data, "effective_resources.memory_mib");
      const memBytes = mibToBytes(memMib);
      const bootType = getByPath<string>(data, "boot_source.type");
      const bootId = getByPath<string>(data, "boot_source.id");
      const bootName = getByPath<string>(data, "boot_source.name");
      const bootDigest = getByPath<string>(data, "boot_source.resolved_digest");
      const bootVolume = getByPath<string>(data, "boot_source.materialized_volume.volume_id");
      const statusReason = getByPath<string>(data, "status_reason");
      return [
        { label: "Тип инстанса", value: code(getByPath<string>(data, "instance_kind")) },
        { label: "Зона доступности", value: txt(getByPath<string>(data, "zone_id")) },
        { label: "Тип машины", value: code(getByPath<string>(data, "machine_type_id")) },
        { label: "vCPU", value: txt(getByPath<unknown>(data, "effective_resources.v_cpu")) },
        { label: "Память", value: bytes(memBytes) },
        { label: "Гарантия CPU, %", value: txt(getByPath<unknown>(data, "cpu_guarantee_percent")) },
        { label: "Источник ОС", value: code(bootType) },
        { label: "Образ", value: bootName ? txt(bootName) : code(bootId) },
        { label: "Image digest", value: code(bootDigest) },
        { label: "Boot-том", value: code(bootVolume) },
        { label: "Сервисный аккаунт", value: code(getByPath<string>(data, "service_account.id")) },
        { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
        ...(statusReason ? [{ label: "Причина статуса", value: txt(statusReason) }] : []),
        { label: "FQDN", value: code(getByPath<string>(data, "fqdn")) },
      ];
    },
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
