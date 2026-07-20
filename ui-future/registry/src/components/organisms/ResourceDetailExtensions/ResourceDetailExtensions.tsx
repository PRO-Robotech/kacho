// resource-detail-extensions — реестр доменных расширений detail-страницы
// registry-remote.
//
// ResourceShell остаётся generic (Обзор / связанные / Операции / JSON + формы-
// панели). Доменно-специфичные строки Обзора и header-действия конкретного
// ресурса подключаются здесь по spec.id. Для Registry: реестр — endpoint /
// число репозиториев / статус + header-действие «Управление доступом»
// (навигация в IAM-remote к созданию AccessBinding на проекте реестра).

import { type ReactNode } from "react";
import { Button, Typography } from "antd";
import { SafetyCertificateOutlined } from "@ant-design/icons";

import type { DetailTab } from "@/components/organisms/DetailShell";
import { StatusBadge } from "@/components/atoms/StatusBadge";
import { LifecycleTag } from "@/components/atoms/LifecycleTag";
import { VisibilityTag } from "@/components/atoms/VisibilityTag";
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

function code(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? (
    <Typography.Text code copyable style={{ fontSize: 12 }}>
      {s}
    </Typography.Text>
  ) : (
    dash
  );
}

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
  registries: {
    // Доменные строки Обзора реестра: endpoint для docker login/push, регион
    // размещения (REG-1 F4, REGIONAL-anycast), видимость репозиториев по умолчанию
    // (REG-1 F5), число репозиториев (растёт с push) и статус.
    overviewExtra: ({ data }) => [
      { label: "Endpoint", value: code(getByPath<string>(data, "endpoint")) },
      { label: "Регион", value: txt(getByPath<string>(data, "region_id")) },
      { label: "Размещение", value: txt(getByPath<string>(data, "placement_type")) },
      {
        label: "Видимость репозиториев по умолчанию",
        value: <VisibilityTag value={getByPath<string>(data, "default_repository_visibility")} />,
      },
      { label: "Репозиториев", value: txt(getByPath<number>(data, "repository_count") ?? 0) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
    ],
    // «Управление доступом» — доступ к реестру = registry-scoped Role, привязанная
    // на ПРОЕКТЕ реестра (уровни scope — только CLUSTER/ACCOUNT/PROJECT, отдельного
    // per-registry-object scope нет). Кнопка ведёт в IAM-remote к созданию
    // AccessBinding на проекте; форму IAM cross-remote НЕ импортируем.
    headerActions: ({ projectId, navigate }) =>
      projectId ? (
        <Button
          icon={<SafetyCertificateOutlined />}
          onClick={() => navigate(`/projects/${projectId}/iam/access-bindings/create`)}
        >
          Управление доступом
        </Button>
      ) : null,
  },

  // ─────────────────────────── репозиторий ───────────────────────────
  // Доменные строки Обзора репозитория (REG-1): класс исчезаемости (DURABLE/
  // EPHEMERAL, F7), видимость (PRIVATE/PUBLIC, F5), число тегов и агрегатный размер.
  repositories: {
    overviewExtra: ({ data }) => [
      { label: "Класс", value: <LifecycleTag value={getByPath<string>(data, "lifecycle")} /> },
      { label: "Видимость", value: <VisibilityTag value={getByPath<string>(data, "visibility")} /> },
      { label: "Тегов", value: txt(getByPath<number>(data, "tag_count") ?? 0) },
      { label: "Размер", value: bytes(getByPath<unknown>(data, "size_bytes")) },
    ],
  },
};

export function detailExtension(specId: string): DetailExtension | undefined {
  return DETAIL_EXTENSIONS[specId];
}
