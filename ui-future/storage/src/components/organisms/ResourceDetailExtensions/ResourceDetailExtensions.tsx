// resource-detail-extensions — реестр доменных расширений detail-страницы
// storage-remote.
//
// ResourceShell остаётся generic (Обзор / связанные / Операции / JSON + формы-
// панели). Доменно-специфичные строки Обзора и header-действия конкретного
// ресурса подключаются здесь по spec.id. Для Storage расширения раскрывают
// том (зона / тип диска / размер / статус / исходный снимок / исходный образ /
// подключения — used_by°), снимок (исходный том / размер / статус) и образ
// (регион / источник snapshot|volume / размер / min-disk / формат / статус).

import { type ReactNode } from "react";
import { Typography } from "antd";

import type { DetailTab } from "@/components/organisms/DetailShell";
import { StatusBadge } from "@/components/atoms/StatusBadge";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { getByPath } from "@/lib/resource-registry";
import { formatBytes } from "@/lib/bytes";

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

function bytes(v: unknown): ReactNode {
  const s = formatBytes(v);
  return s === "—" ? dash : <>{s}</>;
}

// ─────────────────────────── реестр ───────────────────────────

export const DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  // Том: инфраструктурно-нейтральные, tenant-facing строки Обзора.
  volumes: {
    overviewExtra: ({ data }) => {
      const attachments = (getByPath<unknown[]>(data, "attachments") ?? []) as Array<Record<string, unknown>>;
      const attachLabel =
        attachments.length > 0
          ? attachments.map((a) => (a.instance_name as string) || (a.instance_id as string) || "?").join(", ")
          : "";
      return [
        { label: "Зона доступности", value: txt(getByPath<string>(data, "zone_id")) },
        { label: "Тип диска", value: txt(getByPath<string>(data, "disk_type_id")) },
        { label: "Размер", value: bytes(getByPath<unknown>(data, "size_bytes")) },
        { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
        {
          label: "Исходный снимок",
          value: getByPath<string>(data, "source_snapshot_id") ? (
            <RefNameLink
              specId="snapshots"
              refId={getByPath<string>(data, "source_snapshot_id")}
              projectId={getByPath<string>(data, "project_id")}
              maxChars={32}
            />
          ) : (
            dash
          ),
        },
        {
          label: "Исходный образ",
          value: getByPath<string>(data, "source_image_id") ? (
            <RefNameLink
              specId="images"
              refId={getByPath<string>(data, "source_image_id")}
              projectId={getByPath<string>(data, "project_id")}
              maxChars={32}
            />
          ) : (
            dash
          ),
        },
        // used_by° — производно от attachments (кто использует том). Показываем
        // имена инстансов-потребителей; пусто → «—».
        { label: "Используется", value: txt(attachLabel) },
      ];
    },
  },
  // Снимок: исходный том / размер / статус.
  snapshots: {
    overviewExtra: ({ data }) => [
      {
        label: "Исходный том",
        value: getByPath<string>(data, "source_volume_id") ? (
          <RefNameLink
            specId="volumes"
            refId={getByPath<string>(data, "source_volume_id")}
            projectId={getByPath<string>(data, "project_id")}
            maxChars={32}
          />
        ) : (
          dash
        ),
      },
      { label: "Размер", value: bytes(getByPath<unknown>(data, "size_bytes")) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
    ],
  },
  // Образ (STOR-1): регион / источник (snapshot XOR volume) / размер / min-disk /
  // формат / статус. REGIONAL/anycast — placement по region_id, не zone_id.
  images: {
    overviewExtra: ({ data }) => {
      const snap = getByPath<string>(data, "source_snapshot_id");
      const vol = getByPath<string>(data, "source_volume_id");
      const projectId = getByPath<string>(data, "project_id");
      const sourceValue = snap ? (
        <RefNameLink specId="snapshots" refId={snap} projectId={projectId} maxChars={32} />
      ) : vol ? (
        <RefNameLink specId="volumes" refId={vol} projectId={projectId} maxChars={32} />
      ) : (
        dash
      );
      return [
        { label: "Регион", value: txt(getByPath<string>(data, "region_id")) },
        { label: "Размещение", value: txt(getByPath<string>(data, "placement_type")) },
        { label: "Источник", value: sourceValue },
        { label: "Размер", value: bytes(getByPath<unknown>(data, "size_bytes")) },
        { label: "Мин. размер тома", value: bytes(getByPath<unknown>(data, "min_disk_bytes")) },
        { label: "Формат", value: txt(getByPath<string>(data, "format")) },
        { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
      ];
    },
  },
};

export function detailExtension(specId: string): DetailExtension | undefined {
  return DETAIL_EXTENSIONS[specId];
}
